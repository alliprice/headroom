package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClaudeKeychainService(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultDir := filepath.Join(home, ".claude")

	for _, tt := range []struct {
		name string
		dir  string
		want string
	}{
		{"default location", defaultDir, "Claude Code-credentials"},
		{"default via home expansion", "~/.claude", "Claude Code-credentials"},
		{"custom absolute", "/Users/alliprice/.claude-alli", "Claude Code-credentials-6cc8e272"},
		{"custom with trailing slash", "/Users/alliprice/.claude-alli/", "Claude Code-credentials-6cc8e272"},
		{"custom via tilde", "~/.claude-alli", "Claude Code-credentials-" + wantHashOf(t, filepath.Join(home, ".claude-alli"))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClaudeKeychainService(tt.dir); got != tt.want {
				t.Fatalf("ClaudeKeychainService(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func wantHashOf(t *testing.T, cleaned string) string {
	t.Helper()
	// Recompute the way the code does, so the test states the rule, not a
	// precomputed digest for a path that depends on the temp HOME.
	return ClaudeKeychainService(cleaned)[len("Claude Code-credentials-"):]
}

func TestClaudeConfigDirLoginEmail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{"emailAddress":" Alli@Sibilance.io "}}`)
	if got := ClaudeConfigDirLoginEmail(dir); got != "Alli@Sibilance.io" {
		t.Fatalf("email = %q", got)
	}
	// Unlike ClaudeLoginEmail, this reader is tied to the directory, not the
	// environment, so each profile resolves its own identity.
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-env")
	if got := ClaudeConfigDirLoginEmail(dir); got != "Alli@Sibilance.io" {
		t.Fatalf("email with env token = %q", got)
	}
	if got := ClaudeConfigDirLoginEmail(t.TempDir()); got != "" {
		t.Fatalf("missing file email = %q, want empty", got)
	}
}

// fakeKeychain stands in for the macOS keychain item.
type fakeKeychain struct {
	mu     sync.Mutex
	data   []byte
	writes int
	readEr error
}

func (f *fakeKeychain) read() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readEr != nil {
		return nil, f.readEr
	}
	return append([]byte(nil), f.data...), nil
}

func (f *fakeKeychain) write(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = append([]byte(nil), data...)
	f.writes++
	return nil
}

func (f *fakeKeychain) item(t *testing.T) *claudeItem {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	item, err := parseClaudeItem(f.data)
	if err != nil {
		t.Fatalf("stored item: %v", err)
	}
	return item
}

// storedItem builds a credentials item with an expiry and extra fields the
// refresh must preserve.
func storedItem(access, refresh string, expiresAt time.Time) []byte {
	m := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":           access,
			"refreshToken":          refresh,
			"expiresAt":             float64(expiresAt.UnixMilli()),
			"refreshTokenExpiresAt": float64(expiresAt.Add(28 * 24 * time.Hour).UnixMilli()),
			"scopes":                []any{"user:inference", "user:profile"},
			"subscriptionType":      "max",
		},
	}
	data, _ := json.Marshal(m)
	return data
}

func newTestSource(t *testing.T, kc *fakeKeychain, now time.Time, refresh bool, handler http.HandlerFunc) *ClaudeKeychainSource {
	t.Helper()
	s := &ClaudeKeychainSource{
		ConfigDir: "/Users/alliprice/.claude-alli",
		Service:   "Claude Code-credentials-6cc8e272",
		Account:   "alliprice",
		Refresh:   refresh,
		readItem:  kc.read,
		writeItem: kc.write,
		now:       func() time.Time { return now },
	}
	if handler != nil {
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		s.http = srv.Client()
		s.tokenURL = srv.URL
	}
	return s
}

// refreshHandler serves one successful token response and records the request
// body so a test can check the fields sent.
func refreshHandler(t *testing.T, resp map[string]any, gotBody *map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if gotBody != nil {
			body := map[string]any{}
			json.NewDecoder(r.Body).Decode(&body)
			*gotBody = body
		}
		json.NewEncoder(w).Encode(resp)
	}
}

func TestTokenNoRefreshWhenFarFromExpiry(t *testing.T) {
	now := time.Now()
	kc := &fakeKeychain{data: storedItem("sk-live", "rt-1", now.Add(2*time.Hour))}
	s := newTestSource(t, kc, now, true, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("refresh fired although the token is fresh")
	})
	got, err := s.Token()
	if err != nil || got != "sk-live" {
		t.Fatalf("Token() = %q, %v", got, err)
	}
	if kc.writes != 0 {
		t.Fatalf("writes = %d, want 0", kc.writes)
	}
}

func TestTokenRefreshesWithinLeeway(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		name   string
		expiry time.Time
	}{
		{"within 15 minutes", now.Add(10 * time.Minute)},
		{"already expired", now.Add(-time.Hour)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kc := &fakeKeychain{data: storedItem("sk-old", "rt-old", tt.expiry)}
			var body map[string]any
			s := newTestSource(t, kc, now, true, refreshHandler(t, map[string]any{
				"access_token": "sk-new", "refresh_token": "rt-new", "expires_in": 28800,
			}, &body))

			got, err := s.Token()
			if err != nil || got != "sk-new" {
				t.Fatalf("Token() = %q, %v", got, err)
			}
			if body["grant_type"] != "refresh_token" || body["refresh_token"] != "rt-old" ||
				body["client_id"] != claudeOAuthClientID || body["scope"] != "user:inference user:profile" {
				t.Fatalf("refresh request body = %#v", body)
			}
			item := kc.item(t)
			if item.accessToken() != "sk-new" || item.refreshToken() != "rt-new" {
				t.Fatalf("stored tokens = %q/%q", item.accessToken(), item.refreshToken())
			}
			if want := now.Add(28800 * time.Second); item.expiresAt().Unix() != want.Unix() {
				t.Fatalf("stored expiry = %s, want %s", item.expiresAt(), want)
			}
			// Fields outside the oauth triple survive the write.
			if item.oauth["subscriptionType"] != "max" || item.oauth["refreshTokenExpiresAt"] == nil {
				t.Fatalf("refresh dropped preserved fields: %#v", item.oauth)
			}
		})
	}
}

func TestTokenNoRefreshForNonRefreshingProfile(t *testing.T) {
	now := time.Now()
	kc := &fakeKeychain{data: storedItem("sk-live", "rt-1", now.Add(5*time.Minute))}
	s := newTestSource(t, kc, now, false, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("a non-refreshing profile must never call the token endpoint")
	})
	got, err := s.Token()
	if err != nil || got != "sk-live" {
		t.Fatalf("Token() = %q, %v", got, err)
	}

	// Once expired, with no refresh, the source reports an auth error rather
	// than handing back a dead token.
	kc.data = storedItem("sk-dead", "rt-1", now.Add(-time.Minute))
	if _, err := s.Token(); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired no-refresh Token() err = %v, want an expiry error", err)
	}
}

func TestForceRefreshKeepsUnchangedRefreshToken(t *testing.T) {
	now := time.Now()
	kc := &fakeKeychain{data: storedItem("sk-old", "rt-keep", now.Add(2*time.Hour))}
	s := newTestSource(t, kc, now, true, refreshHandler(t, map[string]any{
		"access_token": "sk-new", "expires_in": 28800, // no refresh_token in the reply
	}, nil))

	got, err := s.ForceRefresh()
	if err != nil || got != "sk-new" {
		t.Fatalf("ForceRefresh() = %q, %v", got, err)
	}
	if item := kc.item(t); item.refreshToken() != "rt-keep" {
		t.Fatalf("refresh token = %q, want the original kept", item.refreshToken())
	}
}

func TestRefreshNowReturnsExpiry(t *testing.T) {
	now := time.Now()
	kc := &fakeKeychain{data: storedItem("sk-old", "rt-1", now.Add(-time.Minute))}
	s := newTestSource(t, kc, now, true, refreshHandler(t, map[string]any{
		"access_token": "sk-new", "refresh_token": "rt-new", "expires_in": 3600,
	}, nil))
	expiry, err := s.RefreshNow()
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(time.Hour); expiry.Unix() != want.Unix() {
		t.Fatalf("expiry = %s, want %s", expiry, want)
	}
}

func TestRefreshInvalidGrantNamesReLogin(t *testing.T) {
	now := time.Now()
	kc := &fakeKeychain{data: storedItem("sk-old", "rt-dead", now.Add(-time.Minute))}
	s := newTestSource(t, kc, now, true, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token expired"}`))
	})
	_, err := s.ForceRefresh()
	var relogin *ClaudeReloginError
	if !errors.As(err, &relogin) {
		t.Fatalf("err = %v (%T), want *ClaudeReloginError", err, err)
	}
	if !strings.Contains(relogin.Error(), "CLAUDE_CONFIG_DIR=/Users/alliprice/.claude-alli claude") ||
		!strings.Contains(relogin.Error(), "/login") {
		t.Fatalf("message does not name the re-login command: %q", relogin.Error())
	}
	if kc.writes != 0 {
		t.Fatalf("a rejected refresh wrote to the keychain %d times", kc.writes)
	}
}

func TestRefreshHonorsTokenEndpoint429(t *testing.T) {
	now := time.Now()
	kc := &fakeKeychain{data: storedItem("sk-old", "rt-1", now.Add(-time.Minute))}
	s := newTestSource(t, kc, now, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := s.ForceRefresh()
	if wait, ok := RetryAfter(err); !ok || wait != 10*time.Minute {
		t.Fatalf("RetryAfter = %s, %v; want 10m", wait, ok)
	}
	if !RateLimited(err) {
		t.Error("a token-endpoint 429 did not read as rate limited")
	}
	if kc.writes != 0 {
		t.Fatalf("a 429 refresh wrote to the keychain %d times", kc.writes)
	}
}

// fetchClaude retries once with a fresh token after the usage endpoint turns
// the stored token away, and gives up cleanly when the refresh itself fails.
func TestFetchClaudeRefreshesAfterUsage401(t *testing.T) {
	var usageCalls int
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageCalls++
		if r.Header.Get("Authorization") == "Bearer sk-new" {
			w.Write([]byte(`{"five_hour":{"utilization":5,"resets_at":"2099-01-01T00:00:00Z"}}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(usage.Close)

	refreshed := false
	p := NewClaude(ClaudeSource{
		Token:   func() (string, error) { return "sk-old", nil },
		Refresh: func() (string, error) { refreshed = true; return "sk-new", nil },
		Client:  usage.Client(),
		BaseURL: usage.URL,
	})
	res, isAuth, err := p.Fetch()
	if err != nil || isAuth {
		t.Fatalf("Fetch after refresh = isAuth %v, err %v", isAuth, err)
	}
	if !refreshed || usageCalls != 2 || len(res.Categories) != 1 {
		t.Fatalf("refreshed=%v usageCalls=%d cats=%d", refreshed, usageCalls, len(res.Categories))
	}

	// A refresh that fails leaves the fetch an auth failure.
	p = NewClaude(ClaudeSource{
		Token:   func() (string, error) { return "sk-old", nil },
		Refresh: func() (string, error) { return "", &ClaudeReloginError{ConfigDir: "/x", Reason: "invalid_grant"} },
		Client:  usage.Client(),
		BaseURL: usage.URL,
	})
	_, isAuth, err = p.Fetch()
	if err == nil || !isAuth {
		t.Fatalf("Fetch with failing refresh = isAuth %v, err %v", isAuth, err)
	}
	var relogin *ClaudeReloginError
	if !errors.As(err, &relogin) {
		t.Fatalf("err = %v, want the re-login error to survive", err)
	}
}

// A profile with no Refresh hook never retries a 401.
func TestFetchClaudeNoRetryWithoutRefresh(t *testing.T) {
	var calls int
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(usage.Close)
	p := NewClaude(ClaudeSource{Token: func() (string, error) { return "sk", nil }, Client: usage.Client(), BaseURL: usage.URL})
	if _, isAuth, err := p.Fetch(); err == nil || !isAuth || calls != 1 {
		t.Fatalf("Fetch = isAuth %v, err %v, calls %d", isAuth, err, calls)
	}
}
