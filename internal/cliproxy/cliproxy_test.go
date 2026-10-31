package cliproxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "mgmt-key"

func staticKey() (string, error) { return testKey, nil }

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL + "/", Key: staticKey, HTTP: srv.Client()}
}

// The listing's shape as CLIProxyAPI serves it, with the token-bearing
// fields reduced to placeholders.
const listingJSON = `{"files":[
  {"name":"claude-1fd47d36-dprice@gmail.com.json","provider":"claude","type":"claude","email":"dprice@gmail.com",
   "auth_index":"72d933e7d4132669","status":"active","disabled":false,"unavailable":false,"id_token":{"plan_type":"x"}},
  {"name":"claude-3334dadd-alli@sibilance.io.json","type":"claude","email":"alli@sibilance.io",
   "auth_index":"2a361c109675bd27","status":"unavailable","disabled":true},
  {"name":"codex-69fac321-dprice@gmail.com-pro.json","provider":"codex","type":"codex","email":"dprice@gmail.com",
   "auth_index":"f64941bdaa9de77a","status":"active","disabled":false}
],"observed_at":"2026-10-31T22:16:03Z"}`

func TestCredentialsDecodesListing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != AuthFilesPath {
			t.Errorf("request = %s %s, want GET %s", r.Method, r.URL.Path, AuthFilesPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("Authorization = %q, want the management key as a Bearer token", got)
		}
		w.Write([]byte(listingJSON))
	})

	creds, err := c.Credentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Credential{
		{Name: "claude-1fd47d36-dprice@gmail.com.json", Provider: "claude", Email: "dprice@gmail.com", AuthIndex: "72d933e7d4132669", Status: "active"},
		// provider missing: falls back to type.
		{Name: "claude-3334dadd-alli@sibilance.io.json", Provider: "claude", Email: "alli@sibilance.io", AuthIndex: "2a361c109675bd27", Status: "unavailable", Disabled: true},
		{Name: "codex-69fac321-dprice@gmail.com-pro.json", Provider: "codex", Email: "dprice@gmail.com", AuthIndex: "f64941bdaa9de77a", Status: "active"},
	}
	if len(creds) != len(want) {
		t.Fatalf("got %d credentials, want %d", len(creds), len(want))
	}
	for i := range want {
		got := creds[i]
		got.Quota = Quota{}
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("credential %d = %#v, want %#v", i, got, want[i])
		}
	}
}

// The quota block as CLIProxyAPI records it: the response headers from the
// last request it sent with the credential, as strings.
const quotaListingJSON = `{"files":[
  {"name":"claude-alli.json","provider":"claude","email":"alli@sibilance.io","auth_index":"a",
   "quota":{"observed_at":"2026-10-31T17:49:01.341846-05:00","signals":{
     "Anthropic-Ratelimit-Unified-5h-Utilization":"0.32",
     "anthropic-ratelimit-unified-5h-reset":"1793495400",
     "Anthropic-Ratelimit-Unified-5h-Status":["allowed","ignored"],
     "Anthropic-Ratelimit-Unified-7d-Utilization":0.09,
     "Anthropic-Ratelimit-Unified-Overage-Disabled":true,
     "Anthropic-Ratelimit-Unified-Odd":{"nested":1}}}},
  {"name":"claude-new.json","provider":"claude","email":"new@example.com","auth_index":"b"},
  {"name":"claude-bad-time.json","provider":"claude","email":"x@example.com","auth_index":"c",
   "quota":{"observed_at":"yesterday","signals":{}}}
]}`

func TestCredentialsDecodesQuota(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(quotaListingJSON)) })
	creds, err := c.Credentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 3 {
		t.Fatalf("got %d credentials, want 3", len(creds))
	}

	q := creds[0].Quota
	if want := time.Date(2026, 10, 31, 22, 49, 1, 341846000, time.UTC); !q.ObservedAt.Equal(want) {
		t.Errorf("ObservedAt = %s, want %s", q.ObservedAt, want)
	}
	for name, want := range map[string]string{
		"Anthropic-Ratelimit-Unified-5h-Utilization":   "0.32",
		"Anthropic-Ratelimit-Unified-5h-Reset":         "1793495400", // sent lowercase
		"anthropic-ratelimit-unified-5h-status":        "allowed",    // asked lowercase; first of a list
		"Anthropic-Ratelimit-Unified-7d-Utilization":   "0.09",       // a bare number
		"Anthropic-Ratelimit-Unified-Overage-Disabled": "true",
	} {
		if got, ok := q.Signal(name); !ok || got != want {
			t.Errorf("Signal(%s) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := q.Signal("Anthropic-Ratelimit-Unified-Odd"); ok {
		t.Error("an object-valued signal was kept")
	}
	if _, ok := q.Signal("Anthropic-Ratelimit-Unified-7d-Reset"); ok {
		t.Error("an absent signal was reported")
	}

	if q := creds[1].Quota; !q.ObservedAt.IsZero() || len(q.Signals) != 0 {
		t.Errorf("a credential without quota = %#v, want zero", q)
	}
	if q := creds[2].Quota; !q.ObservedAt.IsZero() {
		t.Errorf("an unparseable observed_at = %s, want zero", q.ObservedAt)
	}
}

func TestCredentialsUnauthorizedClearsCachedKey(t *testing.T) {
	var reads atomic.Int32
	key := cachedKey(func() (string, error) {
		reads.Add(1)
		return testKey, nil
	})
	var status atomic.Int32
	status.Store(http.StatusOK)

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		w.Write([]byte(`{"files":[]}`))
	})
	c.Key = key

	for i := 0; i < 3; i++ {
		if _, err := c.Credentials(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("key read %d times over three good calls, want 1 (cached)", got)
	}

	status.Store(http.StatusUnauthorized)
	_, err := c.Credentials(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}

	status.Store(http.StatusOK)
	if _, err := c.Credentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("key read %d times, want 2: a 401 must drop the cached key", got)
	}
}

func TestCredentialsServerError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := c.Credentials(context.Background()); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want a 502 error", err)
	}
}

func TestCredentialsKeyError(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", Key: func() (string, error) { return "", errors.New("no keychain item") }}
	if _, err := c.Credentials(context.Background()); err == nil {
		t.Fatal("a key failure must fail the listing")
	}
}

// KeychainKey shells out to security(1); a stand-in on PATH counts the reads.
func TestKeychainKeyCachesUntilUnauthorized(t *testing.T) {
	bin := t.TempDir()
	count := filepath.Join(t.TempDir(), "reads")
	script := "#!/bin/sh\n" +
		"[ \"$1 $2 $3 $4\" = \"find-generic-password -s test-service -w\" ] || exit 44\n" +
		"echo x >> '" + count + "'\n" +
		"echo ' " + testKey + " '\n"
	if err := os.WriteFile(filepath.Join(bin, "security"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	reads := func() int {
		data, _ := os.ReadFile(count)
		return strings.Count(string(data), "x")
	}

	key := KeychainKey("test-service")
	for i := 0; i < 3; i++ {
		got, err := key()
		if err != nil || got != testKey {
			t.Fatalf("key() = %q, %v; want the trimmed keychain value", got, err)
		}
	}
	if n := reads(); n != 1 {
		t.Fatalf("security ran %d times, want 1", n)
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	c.Key = key
	if _, err := c.Credentials(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if _, err := key(); err != nil {
		t.Fatal(err)
	}
	if n := reads(); n != 2 {
		// The rejected call used the cached key; the 401 then dropped it.
		t.Fatalf("security ran %d times, want 2", n)
	}

	if _, err := KeychainKey("other-service")(); err == nil {
		t.Fatal("a missing keychain item must be an error")
	}
}
