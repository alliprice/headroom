package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- usage API over HTTP ---

func claudeTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchClaudeAPISuccess(t *testing.T) {
	srv := claudeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/oauth/usage" {
			t.Errorf("request = %s %s, want GET /api/oauth/usage", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Errorf("anthropic-beta = %q", got)
		}
		w.Write([]byte(`{"five_hour":{"utilization":7,"resets_at":"2026-11-01T00:00:00Z"}}`))
	})

	data, err := fetchClaudeAPI(srv.Client(), srv.URL+"/", "sk-test")
	if err != nil {
		t.Fatalf("fetchClaudeAPI: %v", err)
	}
	if _, ok := data["five_hour"]; !ok {
		t.Fatalf("decoded %v, want five_hour", data)
	}
}

func TestFetchClaudeAPIRetryAfter(t *testing.T) {
	for _, tt := range []struct {
		name   string
		header func() string
		want   time.Duration
	}{
		{"seconds", func() string { return "2593" }, 2593 * time.Second},
		{"http date", func() string { return time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat) }, 10 * time.Minute},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := claudeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", tt.header())
				w.WriteHeader(http.StatusTooManyRequests)
			})

			_, err := fetchClaudeAPI(srv.Client(), srv.URL, "sk-test")
			var ra *RetryAfterError
			if !errors.As(err, &ra) {
				t.Fatalf("err = %v (%T), want *RetryAfterError", err, err)
			}
			if diff := ra.Wait - tt.want; diff < -2*time.Second || diff > 2*time.Second {
				t.Fatalf("Wait = %s, want about %s", ra.Wait, tt.want)
			}
			if !strings.Contains(ra.Err.Error(), "429") {
				t.Errorf("wrapped error %q does not name the status", ra.Err)
			}
		})
	}
}

func TestFetchClaudeAPI429WithoutRetryAfterIsPlain(t *testing.T) {
	srv := claudeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := fetchClaudeAPI(srv.Client(), srv.URL, "sk-test")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want a 429 API error", err)
	}
	if _, ok := RetryAfter(err); ok {
		t.Fatal("a 429 without Retry-After reported a wait")
	}
}

func TestFetchClaudeAPIUnauthorizedIsAuthError(t *testing.T) {
	srv := claudeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	p := NewClaude(ClaudeSource{
		Token:   mockCredentialProvider{tok: "sk-test"}.getToken,
		Client:  srv.Client(),
		BaseURL: srv.URL,
	})
	_, isAuth, err := p.Fetch()
	if err == nil || !isAuth {
		t.Fatalf("Fetch = isAuth %v, err %v; want an auth error", isAuth, err)
	}
	if !strings.Contains(err.Error(), "re-authenticate") {
		t.Errorf("err = %q, want the re-authenticate hint", err)
	}
}

// The provider keeps the wait through humanize, which is what the exporter
// reads to push its next poll out.
func TestClaudeFetchKeepsRetryAfter(t *testing.T) {
	srv := claudeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	p := NewClaude(ClaudeSource{
		Token:   func() (string, error) { return "sk-test", nil },
		Client:  srv.Client(),
		BaseURL: srv.URL,
	})
	_, isAuth, err := p.Fetch()
	if isAuth {
		t.Error("a 429 is not an auth error")
	}
	if got, want := err.Error(), "Claude: rate limited - will retry"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	if wait, ok := RetryAfter(err); !ok || wait != 10*time.Minute {
		t.Fatalf("RetryAfter = %s, %v; want 10m", wait, ok)
	}
}

func TestClaudeTokenErrorIsAuthError(t *testing.T) {
	p := NewClaude(ClaudeSource{Token: mockCredentialProvider{err: errors.New("no token found")}.getToken})
	if _, isAuth, err := p.Fetch(); err == nil || !isAuth {
		t.Fatalf("Fetch = isAuth %v, err %v; want an auth error", isAuth, err)
	}
}

// The package-level provider stays the local one, so the registry, the CLI
// and the TUI see no difference.
func TestClaudeDefaultProviderIdentity(t *testing.T) {
	if Claude.ID != "claude" || Claude.DisplayName != "Claude" || Claude.Fetch == nil || Claude.Demo == nil {
		t.Fatalf("Claude = %#v", Claude)
	}
	if ByID("claude") == nil {
		t.Fatal("ByID(claude) = nil")
	}
}

// --- a live response, saved from /api/oauth/usage on 2026-10-31 ---

func loadClaudeSample(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "oauth-usage-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseClaudeSampleCategories(t *testing.T) {
	categories, _ := parseClaude(loadClaudeSample(t))

	want := []struct {
		key, name string
		util      float64
		window    int
	}{
		{"five_hour", "Session", 43, 5 * 3600},
		{"seven_day", "Weekly", 16, 7 * 24 * 3600},
		{"weekly_scoped_fable", "Fable", 6, 7 * 24 * 3600},
	}
	if len(categories) != len(want) {
		t.Fatalf("got %d categories, want %d: %#v", len(categories), len(want), categories)
	}
	for i, w := range want {
		c := categories[i]
		if c.Key != w.key || c.Name != w.name || c.Utilization != w.util || c.WindowSeconds != w.window {
			t.Errorf("category %d = %#v, want %s %q %v%% over %ds", i, c, w.key, w.name, w.util, w.window)
		}
	}
	if fable := categories[2]; fable.ResetsAt != "2026-11-07T14:00:00.015530+00:00" {
		t.Errorf("Fable ResetsAt = %q", fable.ResetsAt)
	}
}

// iguana_necktie is the $250 Claude Code cloud-session credit. It is shaped
// like a meter but is dollars, so it must not become a bar, and its code name
// must not leak into a title-cased label.
func TestParseClaudeSampleCredit(t *testing.T) {
	data := loadClaudeSample(t)
	categories, _ := parseClaude(data)
	for _, c := range categories {
		if c.Key == "iguana_necktie" || strings.Contains(c.Name, "Iguana") {
			t.Fatalf("credit leaked into categories: %#v", c)
		}
	}

	credits := parseClaudeCredits(data)
	if len(credits) != 1 {
		t.Fatalf("got %d credits, want 1: %#v", len(credits), credits)
	}
	c := credits[0]
	if c.Key != "iguana_necktie" || c.Name != "Cloud sessions" {
		t.Errorf("credit = %#v, want iguana_necktie named Cloud sessions", c)
	}
	if c.LimitDollars != 250 || math.Abs(c.UsedDollars-250.09) > 0.01 || c.RemainingDollars != 0 {
		t.Errorf("credit dollars = %v / %v / %v, want 250 / 250.09 / 0", c.LimitDollars, c.UsedDollars, c.RemainingDollars)
	}
	if c.ExpiresAt != "2026-11-05T07:59:00+00:00" {
		t.Errorf("ExpiresAt = %q", c.ExpiresAt)
	}
}

func TestClaudeFetchSampleEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "oauth-usage-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := claudeTestServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write(raw) })
	p := NewClaude(ClaudeSource{
		Token:   func() (string, error) { return "sk-test", nil },
		Client:  srv.Client(),
		BaseURL: srv.URL,
	})
	res, _, err := p.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Categories) != 3 || len(res.Credits) != 1 {
		t.Fatalf("result = %d categories, %d credits; want 3, 1", len(res.Categories), len(res.Credits))
	}
	if res.Extra != nil {
		t.Errorf("extra usage is disabled in the sample, got %#v", res.Extra)
	}
	encoded, _ := json.Marshal(res)
	if strings.Contains(string(encoded), "Iguana Necktie") {
		t.Fatal(`"Iguana Necktie" appears in the result`)
	}
}

func TestParseClaudeUnknownCreditKeepsRawKey(t *testing.T) {
	data := claudeUsageFixture()
	data["copper_kite"] = map[string]any{
		"utilization":   10.0,
		"resets_at":     "2026-12-01T00:00:00Z",
		"limit_dollars": 50.0,
		"used_dollars":  5.0,
	}
	for _, name := range categoryNames(t, data) {
		if name == "Copper Kite" {
			t.Fatal("a dollar credit became a bar")
		}
	}
	credits := parseClaudeCredits(data)
	if len(credits) != 1 || credits[0].Name != "copper_kite" || credits[0].RemainingDollars != 45 {
		t.Fatalf("credits = %#v, want copper_kite with 45 remaining", credits)
	}
}

func TestParseClaudeScopedLimits(t *testing.T) {
	scoped := func(name any, resetsAt any) map[string]any {
		return map[string]any{
			"kind":      "weekly_scoped",
			"percent":   12.0,
			"resets_at": resetsAt,
			"scope":     map[string]any{"model": map[string]any{"id": nil, "display_name": name}},
		}
	}
	data := claudeUsageFixture()
	data["limits"] = []any{
		map[string]any{"kind": "session", "percent": 10.0, "resets_at": "2026-09-20T00:09:59Z", "scope": nil},
		map[string]any{"kind": "weekly_all", "percent": 12.0, "resets_at": "2026-09-26T13:59:59Z", "scope": nil},
		scoped("Fable", "2026-09-26T13:59:59Z"),
		scoped("Fable", "2026-09-26T13:59:59Z"), // repeated name
		scoped(nil, "2026-09-26T13:59:59Z"),     // no model name
		scoped("", "2026-09-26T13:59:59Z"),
		scoped("Quill Pro", nil), // window not started
	}

	got := categoryNames(t, data)
	want := []string{"Session", "Weekly", "Fable", "Quill Pro"}
	if !equalStrings(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	categories, _ := parseClaude(data)
	if categories[3].Key != "weekly_scoped_quill_pro" || categories[3].ResetsAt != "" {
		t.Errorf("unstarted scoped limit = %#v", categories[3])
	}
}

// A top-level meter for the same model wins; the scoped entry must not add a
// second series under the same name.
func TestParseClaudeScopedLimitDefersToTopLevelMeter(t *testing.T) {
	data := claudeUsageFixture()
	data["seven_day_fable"] = map[string]any{"utilization": 23.0, "resets_at": "2026-09-26T13:59:59Z"}
	data["limits"] = []any{map[string]any{
		"kind":      "weekly_scoped",
		"percent":   23.0,
		"resets_at": "2026-09-26T13:59:59Z",
		"scope":     map[string]any{"model": map[string]any{"display_name": "Fable"}},
	}}
	categories, _ := parseClaude(data)
	count := 0
	for _, c := range categories {
		if c.Name == "Fable" {
			count++
			if c.Key != "seven_day_fable" {
				t.Errorf("Fable came from %s, want seven_day_fable", c.Key)
			}
		}
	}
	if count != 1 {
		t.Fatalf("%d Fable categories, want 1", count)
	}
}

// --- login identity ---

func TestClaudeLoginEmailFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	writeFile(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{"emailAddress":" DPrice@Gmail.com "}}`)
	if got := ClaudeLoginEmail(); got != "DPrice@Gmail.com" {
		t.Fatalf("ClaudeLoginEmail = %q", got)
	}
}

func TestClaudeLoginEmailFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"emailAddress":"alli@sibilance.io"}}`)
	if got := ClaudeLoginEmail(); got != "alli@sibilance.io" {
		t.Fatalf("ClaudeLoginEmail = %q", got)
	}
}

func TestClaudeLoginEmailUnknown(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	for _, tt := range []struct {
		name     string
		contents string // "" means no file
	}{
		{"missing file", ""},
		{"no oauth account", `{"numStartups":3}`},
		{"invalid JSON", `not json`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			if tt.contents != "" {
				writeFile(t, filepath.Join(dir, ".claude.json"), tt.contents)
			}
			if got := ClaudeLoginEmail(); got != "" {
				t.Fatalf("ClaudeLoginEmail = %q, want empty", got)
			}
		})
	}
}

// A token in the environment wins the credential chain and says nothing about
// whose it is, so the file's email would be a guess.
func TestClaudeLoginEmailEnvTokenMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-env")
	writeFile(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{"emailAddress":"dprice@gmail.com"}}`)
	if got := ClaudeLoginEmail(); got != "" {
		t.Fatalf("ClaudeLoginEmail = %q, want empty in env-token mode", got)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

// interceptTransport answers every request itself, recording the URL, so
// the default-source path can run without reaching the network.
type interceptTransport struct {
	urls   []string
	header http.Header
	status int
}

func (it *interceptTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	it.urls = append(it.urls, r.URL.String())
	return &http.Response{
		StatusCode: it.status,
		Status:     fmt.Sprintf("%d %s", it.status, http.StatusText(it.status)),
		Header:     it.header,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    r,
	}, nil
}

// The local path: the credential chain for the token and api.anthropic.com
// for the URL, as provider.Claude uses them. A 429's Retry-After must come
// out of humanize intact, and a 429 without one must still read as rate
// limited, so the exporter can log either.
func TestClaudeDefaultSourceKeepsRetryAfter(t *testing.T) {
	oldChain := claudeCredentialChain
	claudeCredentialChain = []claudeCredentialProvider{mockCredentialProvider{tok: "sk-chain"}}
	t.Cleanup(func() { claudeCredentialChain = oldChain })

	for _, tt := range []struct {
		name     string
		header   http.Header
		wantWait time.Duration
		hasWait  bool
	}{
		{"with Retry-After", http.Header{"Retry-After": {"2220"}}, 37 * time.Minute, true},
		{"zero Retry-After", http.Header{"Retry-After": {"0"}}, 0, true},
		{"no Retry-After", http.Header{}, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			it := &interceptTransport{header: tt.header, status: http.StatusTooManyRequests}
			p := NewClaude(ClaudeSource{Client: &http.Client{Transport: it}})
			_, isAuth, err := p.Fetch()
			if isAuth || err == nil {
				t.Fatalf("Fetch = isAuth %v, err %v; want a rate-limit error", isAuth, err)
			}
			if len(it.urls) != 1 || it.urls[0] != "https://api.anthropic.com/api/oauth/usage" {
				t.Fatalf("requests = %v, want the default usage URL once", it.urls)
			}
			if got, want := err.Error(), "Claude: rate limited - will retry"; got != want {
				t.Errorf("message = %q, want %q", got, want)
			}
			if !RateLimited(err) {
				t.Error("RateLimited = false for a 429")
			}
			wait, ok := RetryAfter(err)
			if ok != tt.hasWait || wait != tt.wantWait {
				t.Fatalf("RetryAfter = %s, %v; want %s, %v", wait, ok, tt.wantWait, tt.hasWait)
			}
		})
	}
}

func TestRateLimitedOnlyFor429(t *testing.T) {
	if RateLimited(&HTTPStatusError{Code: 503, Status: "503 Service Unavailable"}) {
		t.Error("a 503 read as rate limited")
	}
	if RateLimited(errors.New("API error: 429 Too Many Requests")) {
		t.Error("an untyped error read as rate limited")
	}
	if !RateLimited(fmt.Errorf("x: %w", &RetryAfterError{Err: &HTTPStatusError{Code: 429}})) {
		t.Error("a wrapped 429 did not read as rate limited")
	}
}
