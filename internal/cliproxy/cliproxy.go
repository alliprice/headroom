// Package cliproxy reads the credentials a CLIProxyAPI server holds through
// its management API.
//
// Nothing here logs the management key, a token, or a raw listing.
package cliproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// AuthFilesPath lists the credentials the proxy holds.
	AuthFilesPath = "/v0/management/auth-files"
	// DefaultKeyService is the keychain service holding the management key.
	DefaultKeyService = "cliproxyapi-management"

	defaultTimeout = 30 * time.Second
)

// ErrUnauthorized means the proxy rejected the management key.
var ErrUnauthorized = errors.New("cliproxy: management key rejected (401)")

// Credential is one auth file the proxy holds.
type Credential struct {
	Name      string
	Provider  string
	Email     string
	AuthIndex string
	Status    string
	Disabled  bool
	Quota     Quota
}

// Quota is what the proxy last recorded of a credential's rate-limit
// response headers. Reading it costs nothing upstream; it is only as new as
// the last request the proxy sent with that credential.
type Quota struct {
	ObservedAt time.Time         // zero when the proxy has recorded nothing
	Signals    map[string]string // canonical header name to value
}

// Signal returns one recorded header, matching its name case-insensitively
// as HTTP does.
func (q Quota) Signal(name string) (string, bool) {
	v, ok := q.Signals[http.CanonicalHeaderKey(name)]
	return v, ok
}

// Client talks to one CLIProxyAPI management API.
type Client struct {
	BaseURL string
	Key     func() (string, error)
	HTTP    *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c *Client) url(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

// do sends a management request. Any status but 200 is an error, and a 401
// also tells cached keys to re-read theirs.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.Key == nil {
		return nil, errors.New("cliproxy: no management key source")
	}
	key, err := c.Key()
	if err != nil {
		return nil, fmt.Errorf("cliproxy: management key: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("cliproxy: %s %s: %w", req.Method, req.URL.Path, err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		keyGeneration.Add(1)
		return nil, ErrUnauthorized
	}
	return nil, fmt.Errorf("cliproxy: %s %s: %s", req.Method, req.URL.Path, resp.Status)
}

// Credentials lists the auth files the proxy holds.
func (c *Client) Credentials(ctx context.Context) ([]Credential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(AuthFilesPath), nil)
	if err != nil {
		return nil, fmt.Errorf("cliproxy: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var listing struct {
		Files []struct {
			Name      string `json:"name"`
			Provider  string `json:"provider"`
			Type      string `json:"type"`
			Email     string `json:"email"`
			AuthIndex string `json:"auth_index"`
			Status    string `json:"status"`
			Disabled  bool   `json:"disabled"`
			Quota     struct {
				ObservedAt string                     `json:"observed_at"`
				Signals    map[string]json.RawMessage `json:"signals"`
			} `json:"quota"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		// Deliberately not the body: the listing carries tokens.
		return nil, errors.New("cliproxy: listing is not valid JSON")
	}

	creds := make([]Credential, 0, len(listing.Files))
	for _, f := range listing.Files {
		provider := f.Provider
		if provider == "" {
			provider = f.Type
		}
		creds = append(creds, Credential{
			Name:      f.Name,
			Provider:  provider,
			Email:     f.Email,
			AuthIndex: f.AuthIndex,
			Status:    f.Status,
			Disabled:  f.Disabled,
			Quota:     decodeQuota(f.Quota.ObservedAt, f.Quota.Signals),
		})
	}
	return creds, nil
}

func decodeQuota(observedAt string, raw map[string]json.RawMessage) Quota {
	var q Quota
	if t, err := time.Parse(time.RFC3339Nano, observedAt); err == nil {
		q.ObservedAt = t
	}
	if len(raw) > 0 {
		q.Signals = make(map[string]string, len(raw))
		for name, value := range raw {
			if v, ok := signalValue(value); ok {
				q.Signals[http.CanonicalHeaderKey(name)] = v
			}
		}
	}
	return q
}

// signalValue reads one recorded header value. The proxy writes strings; a
// list (a header sent more than once) yields its first value, and a bare
// number or bool its text.
func signalValue(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s), true
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return strings.TrimSpace(list[0]), true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String(), true
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return strconv.FormatBool(b), true
	}
	return "", false
}

// keyGeneration counts management 401s. Key sources compare it with the
// value they cached under and re-read when it moved. A source is a bare func,
// so the 401 cannot be routed to the one that produced the key; every cached
// key is dropped instead, which costs one extra keychain read each.
var keyGeneration atomic.Uint64

// KeychainKey returns a key source that reads the management key from the
// macOS keychain item with the given service, caches it, and reads it again
// after the proxy rejects a key.
func KeychainKey(service string) func() (string, error) {
	return cachedKey(func() (string, error) { return keychainPassword(service) })
}

// cachedKey caches read's result until the next management 401.
func cachedKey(read func() (string, error)) func() (string, error) {
	var (
		mu     sync.Mutex
		key    string
		cached bool
		gen    uint64
	)
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		now := keyGeneration.Load()
		if cached && gen == now {
			return key, nil
		}
		k, err := read()
		if err != nil {
			cached = false
			return "", err
		}
		key, cached, gen = k, true, now
		return key, nil
	}
}

func keychainPassword(service string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("keychain lookup of %q timed out", service)
		}
		return "", fmt.Errorf("keychain has no item for service %q", service)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", fmt.Errorf("keychain item for service %q is empty", service)
	}
	return key, nil
}
