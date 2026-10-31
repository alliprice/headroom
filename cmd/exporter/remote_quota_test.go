package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alliprice/headroom/internal/cliproxy"
	"github.com/alliprice/headroom/internal/provider"
)

// syncBuffer collects log output from more than one goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	})
	return buf
}

func TestRemoteFactoriesUseCLIProxyTimings(t *testing.T) {
	factories := remoteFactories(defaultConfig())
	if len(factories) != 1 || factories["claude"] == nil {
		t.Fatalf("factories = %v, want claude only", factories)
	}
	s := factories["claude"](claudeCred("claude-alli", "Alli@Sibilance.io"))
	if !s.remote || s.prov.ID != "claude" || s.key().account != "alli@sibilance.io" {
		t.Fatalf("remote state = %#v", s)
	}
	if s.interval != 5*time.Minute || s.maxAge != 6*time.Hour {
		t.Fatalf("remote timings = %s/%s, want cliproxy relist 5m and max_age 6h", s.interval, s.maxAge)
	}

	cfg := defaultConfig()
	cfg.CLIProxy.Relist, cfg.CLIProxy.MaxAge = "1m", "90m"
	s = remoteFactories(cfg)["claude"](claudeCred("claude-alli", "alli@sibilance.io"))
	if s.interval != time.Minute || s.maxAge != 90*time.Minute {
		t.Fatalf("overridden timings = %s/%s, want 1m/90m", s.interval, s.maxAge)
	}
}

// The record's observed_at, not the time it was read, is the data's age: it
// sets the timestamp, and max_age (6h) runs from it.
func TestRemoteObservedAtDrivesTimestampAndFreshness(t *testing.T) {
	for _, tt := range []struct {
		age       time.Duration
		wantFresh float64
	}{
		{5*time.Hour + 59*time.Minute, 1},
		{6*time.Hour + time.Minute, 0},
	} {
		t.Run(tt.age.String(), func(t *testing.T) {
			h := newRemoteHarness(t)
			observed := time.Now().Add(-tt.age).Truncate(time.Second)
			h.lister.set([]cliproxy.Credential{withQuota(claudeCred("claude-alli", "alli@sibilance.io"), observed, "0.3", "0.08")}, nil)
			h.reconcile(t)

			families := gatherCollector(t, h.mc)
			labels := map[string]string{"provider": "claude", "account": "alli@sibilance.io"}
			if got := gaugeWith(t, families, "llm_scrape_timestamp_seconds", labels); got != float64(observed.Unix()) {
				t.Errorf("timestamp = %v, want observed_at %d", got, observed.Unix())
			}
			if got := gaugeWith(t, families, "llm_scrape_fresh", labels); got != tt.wantFresh {
				t.Errorf("fresh = %v, want %v", got, tt.wantFresh)
			}
			// Old data that the listing did return is still a success.
			if got := gaugeWith(t, families, "llm_scrape_success", labels); got != 1 {
				t.Errorf("success = %v, want 1", got)
			}
			if tt.wantFresh == 0 {
				assertNoUsageMetrics(t, families)
				return
			}
			weekly := map[string]string{"account": "alli@sibilance.io", "category": "Weekly"}
			if got := gaugeWith(t, families, "llm_quota_utilization_ratio", weekly); got != 0.08 {
				t.Errorf("Weekly = %v, want 0.08", got)
			}
			// The 5h window reset two hours after the observation, so it has
			// rolled over: zero used, no reset time.
			session := map[string]string{"account": "alli@sibilance.io", "category": "Session"}
			if got := gaugeWith(t, families, "llm_quota_utilization_ratio", session); got != 0 {
				t.Errorf("rolled-over Session = %v, want 0", got)
			}
			for _, m := range families["llm_quota_reset_timestamp_seconds"].Metric {
				for _, l := range m.Label {
					if l.GetName() == "category" && l.GetValue() == "Session" {
						t.Error("rolled-over Session still has a reset timestamp")
					}
				}
			}
		})
	}
}

func TestRemoteWithoutSignalsIsNotASuccess(t *testing.T) {
	logs := captureLog(t)
	h := newRemoteHarness(t)
	h.lister.set([]cliproxy.Credential{claudeCred("claude-new", "new@example.com")}, nil)
	h.reconcile(t)

	families := gatherCollector(t, h.mc)
	labels := map[string]string{"account": "new@example.com"}
	for _, name := range []string{"llm_scrape_success", "llm_scrape_fresh"} {
		if got := gaugeWith(t, families, name, labels); got != 0 {
			t.Errorf("%s = %v, want 0", name, got)
		}
	}
	if _, ok := families["llm_scrape_timestamp_seconds"]; ok {
		t.Error("an account with no record has a timestamp")
	}
	assertNoUsageMetrics(t, families)
	if !strings.Contains(logs.String(), "[claude new@example.com via cliproxy] fetch error: no quota observed yet") {
		t.Errorf("log does not say no quota was observed:\n%s", logs)
	}
}

// failingTransport stands in for http.DefaultTransport and fails any request
// that reaches it.
type failingTransport struct {
	mu   sync.Mutex
	urls []string
}

func (f *failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.urls = append(f.urls, r.URL.String())
	f.mu.Unlock()
	return nil, errors.New("unexpected HTTP request")
}

// End to end through the real cliproxy client: reading a remote account
// sends the proxy one listing request and nothing else, and never reaches
// Anthropic.
func TestRemoteReadMakesNoUpstreamCalls(t *testing.T) {
	stray := &failingTransport{}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = stray
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	observed := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)
	listing := `{"files":[
	  {"name":"claude-3334dadd-alli@sibilance.io.json","provider":"claude","email":"alli@sibilance.io","auth_index":"2a",
	   "quota":{"observed_at":"` + observed + `","signals":{
	     "Anthropic-Ratelimit-Unified-5h-Utilization":"0.3","Anthropic-Ratelimit-Unified-5h-Reset":"4102444800",
	     "Anthropic-Ratelimit-Unified-5h-Status":"allowed","Anthropic-Ratelimit-Unified-7d-Utilization":"0.08",
	     "Anthropic-Ratelimit-Unified-7d-Reset":"4102444800","Anthropic-Ratelimit-Unified-7d-Status":"rejected"}}},
	  {"name":"codex-69fac321-dprice@gmail.com-pro.json","provider":"codex","email":"dprice@gmail.com","auth_index":"f6",
	   "quota":{"observed_at":"` + observed + `","signals":{"X-Codex-Primary-Used-Percent":"12"}}}
	]}`

	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.URL.Path != cliproxy.AuthFilesPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		io.WriteString(w, listing)
	}))
	t.Cleanup(srv.Close)

	client := &cliproxy.Client{BaseURL: srv.URL, Key: func() (string, error) { return "mgmt", nil }, HTTP: srv.Client()}
	mc := newMultiCollector()
	mc.enableRemote(client, remoteFactories(defaultConfig()), time.Hour)
	for i := 0; i < 3; i++ {
		if err := mc.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	got := strings.Join(paths, ",")
	mu.Unlock()
	want := strings.Repeat("GET "+cliproxy.AuthFilesPath+",", 3)
	if got != strings.TrimSuffix(want, ",") {
		t.Errorf("proxy saw %s, want only three listings", got)
	}
	stray.mu.Lock()
	if len(stray.urls) != 0 {
		t.Errorf("requests outside the proxy client: %v", stray.urls)
	}
	stray.mu.Unlock()

	families := gatherCollector(t, mc)
	session := map[string]string{"provider": "claude", "account": "alli@sibilance.io", "category": "Session"}
	if got := gaugeWith(t, families, "llm_quota_utilization_ratio", session); got != 0.3 {
		t.Errorf("Session = %v, want 0.3", got)
	}
	weekly := map[string]string{"provider": "claude", "account": "alli@sibilance.io", "category": "Weekly"}
	if got := gaugeWith(t, families, "llm_quota_utilization_ratio", weekly); got != 1 {
		t.Errorf("rejected Weekly = %v, want 1", got)
	}
	if n := len(families["llm_scrape_success"].Metric); n != 1 {
		t.Errorf("%d accounts emitted, want alli only (codex has no remote reader)", n)
	}
	if _, ok := families["llm_credit_limit_dollars"]; ok {
		t.Error("a remote account emitted credits; headers carry none")
	}
}

func TestLogWait(t *testing.T) {
	state := newLocalState(provider.Provider{ID: "claude"}, func() string { return "dprice@gmail.com" }, 20*time.Minute, 75*time.Minute)
	limited := &provider.HTTPStatusError{Code: 429, Status: "429 Too Many Requests"}
	unavailable := &provider.HTTPStatusError{Code: 503, Status: "503 Service Unavailable"}
	for _, tt := range []struct {
		name string
		err  error
		want string // "" means no line
	}{
		{"long wait", &provider.RetryAfterError{Wait: 37 * time.Minute, Err: limited}, "[claude dprice@gmail.com] rate limited; server asked to wait 37m0s; next poll in 37m0s"},
		{"zero wait", &provider.RetryAfterError{Wait: 0, Err: limited}, "[claude dprice@gmail.com] rate limited; server asked to wait 0s; next poll in 20m0s"},
		{"short wait", &provider.RetryAfterError{Wait: 30 * time.Second, Err: limited}, "[claude dprice@gmail.com] rate limited; server asked to wait 30s; next poll in 20m0s"},
		{"no header", limited, "[claude dprice@gmail.com] rate limited; no Retry-After; next poll in 20m0s"},
		{"non-429 wait", &provider.RetryAfterError{Wait: time.Minute, Err: unavailable}, "[claude dprice@gmail.com] server asked to wait 1m0s; next poll in 20m0s"},
		{"plain error", errors.New("boom"), ""},
		{"success", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLog(t)
			state.logWait(tt.err, nextDelay(state.interval, tt.err))
			got := strings.TrimSpace(logs.String())
			if got != tt.want {
				t.Fatalf("log = %q, want %q", got, tt.want)
			}
		})
	}
}

// The poll loop logs the wait itself, whatever the provider wrapped it in.
func TestRunLogsRetryAfterFromHumanizedClaudeError(t *testing.T) {
	logs := captureLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	state := newLocalState(provider.NewClaude(provider.ClaudeSource{
		Token:   func() (string, error) { return "sk-test", nil },
		Client:  srv.Client(),
		BaseURL: srv.URL,
	}), nil, 20*time.Minute, 75*time.Minute)

	done := make(chan struct{})
	go func() { state.run(); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "next poll in") {
		if time.Now().After(deadline) {
			t.Fatalf("no wait line logged:\n%s", logs)
		}
		time.Sleep(5 * time.Millisecond)
	}
	state.halt()
	<-done

	want := "[claude unknown] rate limited; server asked to wait 2m0s; next poll in 20m0s"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log missing %q:\n%s", want, logs)
	}
}
