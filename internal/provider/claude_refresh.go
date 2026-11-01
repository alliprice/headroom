package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Claude OAuth refresh, mirroring what Claude Code and CLIProxyAPI send to
// the token endpoint. Only a profile the person is not actively using should
// be refreshed here: refresh tokens rotate, so refreshing a profile Claude
// Code also drives would race it and could log the person out.
const (
	claudeTokenURL      = "https://platform.claude.com/v1/oauth/token"
	claudeOAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	// claudeRefreshLeeway is how long before expiry a proactive refresh fires.
	claudeRefreshLeeway = 15 * time.Minute
	// claudeDefaultScope is used only when the stored item records no scopes.
	claudeDefaultScope        = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	claudeKeychainServiceBase = "Claude Code-credentials"
	claudeRefreshTimeout      = 30 * time.Second
)

// defaultClaudeConfigLocation is the config directory Claude Code uses when
// CLAUDE_CONFIG_DIR is unset. Its keychain item has the unsuffixed service.
func defaultClaudeConfigLocation() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(filepath.Join(".", ".claude"))
	}
	return filepath.Join(home, ".claude")
}

// ClaudeKeychainService returns the keychain service name Claude Code files a
// profile's credentials under: the unsuffixed base for the default location,
// else the base plus the first 8 hex of sha256 over the cleaned absolute
// config directory path.
func ClaudeKeychainService(configDir string) string {
	abs := cleanConfigDir(configDir)
	if abs == defaultClaudeConfigLocation() {
		return claudeKeychainServiceBase
	}
	sum := sha256.Sum256([]byte(abs))
	return claudeKeychainServiceBase + "-" + hex.EncodeToString(sum[:])[:8]
}

// ClaudeCleanConfigDir resolves a config directory to the canonical absolute
// path used for its identity, keychain service and de-duplication: no
// trailing slash, no "~".
func ClaudeCleanConfigDir(dir string) string { return cleanConfigDir(dir) }

// ClaudeDefaultConfigDir reports whether dir is Claude Code's default config
// location, whose keychain item carries the unsuffixed service and whose
// token the exporter must never refresh.
func ClaudeDefaultConfigDir(dir string) bool {
	return cleanConfigDir(dir) == defaultClaudeConfigLocation()
}

// cleanConfigDir resolves a config directory to the canonical absolute path
// the service hash is taken over: no trailing slash, no "~".
func cleanConfigDir(dir string) string {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), "/"))
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Clean(dir)
}

// ClaudeConfigDirLoginEmail reads oauthAccount.emailAddress from
// <configDir>/.claude.json, the identity of the login kept in that profile.
// It returns "" when the file or the field is absent.
func ClaudeConfigDirLoginEmail(configDir string) string {
	data, err := os.ReadFile(filepath.Join(cleanConfigDir(configDir), ".claude.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		OAuthAccount struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.OAuthAccount.EmailAddress)
}

// ClaudeReloginError is a refresh the server refused for good, such as
// invalid_grant once the refresh token has expired or been revoked. It names
// the command that re-establishes the login.
type ClaudeReloginError struct {
	ConfigDir string
	Reason    string
}

func (e *ClaudeReloginError) Error() string {
	return fmt.Sprintf("Claude token refresh rejected (%s); re-login with: CLAUDE_CONFIG_DIR=%s claude  then run /login",
		e.Reason, e.ConfigDir)
}

// claudeItem is one credentials item, kept as a raw map so a write preserves
// every field the refresh does not touch (refreshTokenExpiresAt, scopes,
// subscriptionType, and anything Claude Code adds later).
type claudeItem struct {
	raw   map[string]any
	oauth map[string]any
}

func parseClaudeItem(data []byte) (*claudeItem, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid credentials - run 'claude' to re-authenticate")
	}
	oauth, _ := raw["claudeAiOauth"].(map[string]any)
	if oauth == nil {
		return nil, fmt.Errorf("credentials have no claudeAiOauth block")
	}
	return &claudeItem{raw: raw, oauth: oauth}, nil
}

func (i *claudeItem) accessToken() string {
	s, _ := i.oauth["accessToken"].(string)
	return s
}

func (i *claudeItem) refreshToken() string {
	s, _ := i.oauth["refreshToken"].(string)
	return s
}

func (i *claudeItem) expiresAt() time.Time {
	ms, ok := i.oauth["expiresAt"].(float64)
	if !ok || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(ms))
}

// scopeString is the stored scopes space-joined, as the token endpoint wants
// them, or the known default when the item records none.
func (i *claudeItem) scopeString() string {
	raw, ok := i.oauth["scopes"].([]any)
	if !ok || len(raw) == 0 {
		return claudeDefaultScope
	}
	scopes := make([]string, 0, len(raw))
	for _, s := range raw {
		if str, ok := s.(string); ok && str != "" {
			scopes = append(scopes, str)
		}
	}
	if len(scopes) == 0 {
		return claudeDefaultScope
	}
	return strings.Join(scopes, " ")
}

// applyRefresh writes the new tokens and expiry into the oauth block in place,
// leaving every other field untouched.
func (i *claudeItem) applyRefresh(accessToken, refreshToken string, expiresAt time.Time) {
	i.oauth["accessToken"] = accessToken
	i.oauth["refreshToken"] = refreshToken
	i.oauth["expiresAt"] = float64(expiresAt.UnixMilli())
}

func (i *claudeItem) marshal() ([]byte, error) {
	return json.Marshal(i.raw)
}

// ClaudeKeychainSource reads one profile's credentials from its keychain item
// and, when Refresh is set, keeps the access token alive by refreshing it
// before expiry. A per-source lock serializes refreshes for that profile.
type ClaudeKeychainSource struct {
	ConfigDir string
	Service   string
	Account   string
	Refresh   bool

	// Injected in tests; nil uses the real keychain, clock and endpoint.
	readItem  func() ([]byte, error)
	writeItem func([]byte) error
	now       func() time.Time
	http      *http.Client
	tokenURL  string

	mu sync.Mutex
}

// NewClaudeKeychainSource builds a source for the profile at configDir,
// reading and writing the matching keychain item under the current macOS
// user account.
func NewClaudeKeychainSource(configDir string, refresh bool) *ClaudeKeychainSource {
	account := ""
	if u, err := user.Current(); err == nil {
		account = u.Username
	}
	return &ClaudeKeychainSource{
		ConfigDir: cleanConfigDir(configDir),
		Service:   ClaudeKeychainService(configDir),
		Account:   account,
		Refresh:   refresh,
	}
}

func (s *ClaudeKeychainSource) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *ClaudeKeychainSource) read() ([]byte, error) {
	if s.readItem != nil {
		return s.readItem()
	}
	return claudeKeychainRead(s.Service, s.Account)
}

func (s *ClaudeKeychainSource) write(data []byte) error {
	if s.writeItem != nil {
		return s.writeItem(data)
	}
	return claudeKeychainWrite(s.Service, s.Account, data)
}

func (s *ClaudeKeychainSource) client() *http.Client {
	if s.http != nil {
		return s.http
	}
	return &http.Client{Timeout: claudeRefreshTimeout}
}

func (s *ClaudeKeychainSource) endpoint() string {
	if s.tokenURL != "" {
		return s.tokenURL
	}
	return claudeTokenURL
}

// Token returns the profile's access token, refreshing first when a
// refreshing profile's token is within the leeway of expiry or already past
// it. Suitable as ClaudeSource.Token.
func (s *ClaudeKeychainSource) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.readLocked()
	if err != nil {
		return "", err
	}

	now := s.clock()
	expiry := item.expiresAt()
	needsRefresh := s.Refresh && (expiry.IsZero() || !now.Before(expiry.Add(-claudeRefreshLeeway)))
	if !needsRefresh {
		if !expiry.IsZero() && !now.Before(expiry) {
			return "", fmt.Errorf("token expired %s ago - run 'claude' to re-authenticate", now.Sub(expiry).Round(time.Second))
		}
		return item.accessToken(), nil
	}

	refreshed, err := s.refreshLocked(item)
	if err != nil {
		return "", err
	}
	return refreshed.accessToken(), nil
}

// ForceRefresh refreshes regardless of expiry, writes the result back, and
// returns the new access token. Suitable as ClaudeSource.Refresh.
func (s *ClaudeKeychainSource) ForceRefresh() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readLocked()
	if err != nil {
		return "", err
	}
	refreshed, err := s.refreshLocked(item)
	if err != nil {
		return "", err
	}
	return refreshed.accessToken(), nil
}

// RefreshNow refreshes once and returns the new expiry, for the maintenance
// flag. No token is returned or logged.
func (s *ClaudeKeychainSource) RefreshNow() (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readLocked()
	if err != nil {
		return time.Time{}, err
	}
	refreshed, err := s.refreshLocked(item)
	if err != nil {
		return time.Time{}, err
	}
	return refreshed.expiresAt(), nil
}

func (s *ClaudeKeychainSource) readLocked() (*claudeItem, error) {
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	return parseClaudeItem(data)
}

// refreshLocked exchanges the refresh token for a new access token and writes
// it back into the keychain item. The caller holds s.mu.
func (s *ClaudeKeychainSource) refreshLocked(item *claudeItem) (*claudeItem, error) {
	refreshToken := item.refreshToken()
	if refreshToken == "" {
		return nil, &ClaudeReloginError{ConfigDir: s.ConfigDir, Reason: "no refresh token stored"}
	}

	body, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     claudeOAuthClientID,
		"scope":         item.scopeString(),
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), claudeRefreshTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "axios/1.15.2")

	resp, err := s.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(respBody, &parsed)

	if resp.StatusCode == http.StatusTooManyRequests {
		status := &HTTPStatusError{Code: resp.StatusCode, Status: resp.Status}
		if wait, ok := parseRetryAfter(resp.Header.Get("Retry-After"), s.clock()); ok {
			return nil, &RetryAfterError{Wait: wait, Err: status}
		}
		return nil, status
	}
	if parsed.Error == "invalid_grant" || (resp.StatusCode == http.StatusBadRequest && parsed.Error != "") {
		reason := parsed.Error
		if reason == "" {
			reason = "invalid_grant"
		}
		return nil, &ClaudeReloginError{ConfigDir: s.ConfigDir, Reason: reason}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	if parsed.AccessToken == "" {
		return nil, fmt.Errorf("token refresh response had no access_token")
	}

	newRefresh := parsed.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken
	}
	expiry := s.clock().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	item.applyRefresh(parsed.AccessToken, newRefresh, expiry)

	data, err := item.marshal()
	if err != nil {
		return nil, err
	}
	if err := s.write(data); err != nil {
		return nil, fmt.Errorf("writing refreshed credentials: %w", err)
	}
	return item, nil
}

// claudeKeychainRead reads one keychain item's secret as raw bytes.
func claudeKeychainRead(service, account string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{"find-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	args = append(args, "-w")

	out, err := exec.CommandContext(ctx, "security", args...).Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("keychain access timed out")
		}
		return nil, fmt.Errorf("keychain has no item for service %q - run 'claude' to re-authenticate", service)
	}
	return []byte(strings.TrimSpace(string(out))), nil
}

// claudeKeychainWrite stores data back into the keychain item, replacing it if
// it exists (-U). The secret is passed as an argument, so it is briefly
// visible in this process's argv; on a single-user machine that is the same
// trust boundary as the keychain read itself, and security has no option to
// take the password on stdin for add-generic-password. The secret is never
// logged, and any error text from security does not carry it.
func claudeKeychainWrite(service, account string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{"add-generic-password", "-U", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	args = append(args, "-w", string(data))

	out, err := exec.CommandContext(ctx, "security", args...).CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("keychain write timed out")
		}
		return fmt.Errorf("keychain write failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
