package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alliprice/headroom/internal/cliproxy"
	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
	dto "github.com/prometheus/client_model/go"
)

func fullResult() *provider.FetchResult {
	res := testResult(6)
	res.Extra = &parse.ExtraUsage{Utilization: 50, UsedCredits: 100, MonthlyLimit: 200}
	res.Credits = []parse.Credit{{
		Key:              "iguana_necktie",
		Name:             "Cloud sessions",
		LimitDollars:     250,
		UsedDollars:      250.09,
		RemainingDollars: 0,
		ExpiresAt:        "2026-11-05T07:59:00+00:00",
	}}
	return res
}

func freshState(id, account string, res *provider.FetchResult) *providerState {
	return &providerState{
		prov:        provider.Provider{ID: id},
		interval:    20 * time.Minute,
		maxAge:      75 * time.Minute,
		account:     account,
		lastResult:  res,
		lastFetch:   time.Now().Add(-time.Minute),
		lastSuccess: true,
	}
}

func labelNames(m *dto.Metric) []string {
	names := make([]string, 0, len(m.Label))
	for _, l := range m.Label {
		names = append(names, l.GetName())
	}
	sort.Strings(names)
	return names
}

func TestEveryFamilyHasExactLabelSet(t *testing.T) {
	quota := []string{"account", "category", "provider"}
	scrape := []string{"account", "provider"}
	credit := []string{"account", "credit", "provider"}
	want := map[string][]string{
		"llm_quota_utilization_ratio":         quota,
		"llm_quota_glide_ratio":               quota,
		"llm_quota_headroom_ratio":            quota,
		"llm_quota_glide_hours":               quota,
		"llm_quota_headroom_hours":            quota,
		"llm_quota_sleep_recovery_ratio":      quota,
		"llm_quota_adjusted_headroom_ratio":   quota,
		"llm_quota_reset_timestamp_seconds":   quota,
		"llm_quota_window_seconds":            quota,
		"llm_extra_utilization_ratio":         scrape,
		"llm_extra_glide_ratio":               scrape,
		"llm_extra_used_dollars":              scrape,
		"llm_extra_limit_dollars":             scrape,
		"llm_credit_limit_dollars":            credit,
		"llm_credit_used_dollars":             credit,
		"llm_credit_remaining_dollars":        credit,
		"llm_credit_expiry_timestamp_seconds": credit,
		"llm_scrape_success":                  scrape,
		"llm_scrape_fresh":                    scrape,
		"llm_scrape_timestamp_seconds":        scrape,
	}
	if len(want) != len(allDescs) {
		t.Fatalf("test covers %d families, collector describes %d", len(want), len(allDescs))
	}

	families := gatherState(t, freshState("claude", "dprice@gmail.com", fullResult()))
	for name, labels := range want {
		family, ok := families[name]
		if !ok {
			t.Errorf("family %s not emitted", name)
			continue
		}
		for _, m := range family.Metric {
			if got := labelNames(m); strings.Join(got, ",") != strings.Join(labels, ",") {
				t.Errorf("%s labels = %v, want %v", name, got, labels)
			}
			for _, l := range m.Label {
				if l.GetValue() == "" {
					t.Errorf("%s has an empty %s label", name, l.GetName())
				}
			}
		}
	}
	for name := range families {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected family %s", name)
		}
	}
}

func TestAccountLabel(t *testing.T) {
	for in, want := range map[string]string{
		" DPrice@Gmail.com ": "dprice@gmail.com",
		"alli@sibilance.io":  "alli@sibilance.io",
		"":                   "unknown",
		"   ":                "unknown",
	} {
		if got := accountLabel(in); got != want {
			t.Errorf("accountLabel(%q) = %q, want %q", in, got, want)
		}
	}
	// A state with no account resolved still labels its series.
	families := gatherState(t, freshState("antigravity", "", testResult(6)))
	if got := gaugeWith(t, families, "llm_scrape_fresh", map[string]string{"provider": "antigravity", "account": "unknown"}); got != 1 {
		t.Fatalf("llm_scrape_fresh = %v", got)
	}
}

func TestCreditGauges(t *testing.T) {
	state := freshState("claude", "dprice@gmail.com", fullResult())
	families := gatherState(t, state)
	labels := map[string]string{"provider": "claude", "account": "dprice@gmail.com", "credit": "Cloud sessions"}
	for name, want := range map[string]float64{
		"llm_credit_limit_dollars":            250,
		"llm_credit_used_dollars":             250.09,
		"llm_credit_remaining_dollars":        0,
		"llm_credit_expiry_timestamp_seconds": float64(time.Date(2026, 11, 5, 7, 59, 0, 0, time.UTC).Unix()),
	} {
		if got := gaugeWith(t, families, name, labels); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	// A credit is not a quota window.
	if _, ok := families["llm_quota_utilization_ratio"]; ok {
		for _, m := range families["llm_quota_utilization_ratio"].Metric {
			for _, l := range m.Label {
				if l.GetName() == "category" && l.GetValue() == "Cloud sessions" {
					t.Fatal("credit emitted as a quota category")
				}
			}
		}
	}

	// Credits go stale with the rest of the cache.
	state.lastFetch = time.Now().Add(-76 * time.Minute)
	assertNoUsageMetrics(t, gatherState(t, state))
}

// A bucket with no running window (Antigravity reports no reset for an
// untouched one) shows its usage but no pacing.
func TestCollectUnstartedWindow(t *testing.T) {
	res := &provider.FetchResult{Categories: []parse.Category{{
		Key: "antigravity_3p_5h", Name: "Claude/GPT Session", Utilization: 0, ResetsAt: "", WindowSeconds: 5 * 3600,
	}}}
	families := gatherState(t, freshState("antigravity", "", res))
	labels := map[string]string{"category": "Claude/GPT Session"}
	for _, name := range []string{
		"llm_quota_utilization_ratio", "llm_quota_glide_ratio", "llm_quota_headroom_ratio",
		"llm_quota_glide_hours", "llm_quota_headroom_hours",
		"llm_quota_sleep_recovery_ratio", "llm_quota_adjusted_headroom_ratio",
	} {
		if got := gaugeWith(t, families, name, labels); got != 0 {
			t.Errorf("%s = %v, want 0", name, got)
		}
	}
	if got := gaugeWith(t, families, "llm_quota_window_seconds", labels); got != 5*3600 {
		t.Errorf("window = %v", got)
	}
	if _, ok := families["llm_quota_reset_timestamp_seconds"]; ok {
		t.Error("an empty reset time emitted a reset timestamp")
	}
}

// One repeated name would fail the Gather for every provider.
func TestCollectDropsRepeatedNames(t *testing.T) {
	res := fullResult()
	res.Categories = append(res.Categories, res.Categories[0])
	res.Credits = append(res.Credits, res.Credits[0])
	families := gatherState(t, freshState("claude", "dprice@gmail.com", res))
	gaugeWith(t, families, "llm_quota_utilization_ratio", map[string]string{"category": "Weekly"})
	gaugeWith(t, families, "llm_credit_limit_dollars", map[string]string{"credit": "Cloud sessions"})
}

func TestNextDelay(t *testing.T) {
	interval := 20 * time.Minute
	for _, tt := range []struct {
		name string
		err  error
		want time.Duration
	}{
		{"success", nil, interval},
		{"plain error", errors.New("API error: 500"), interval},
		{"unavailable", errUnavailable, interval},
		{"shorter wait", &provider.RetryAfterError{Wait: time.Minute, Err: errors.New("429")}, interval},
		{"equal wait", &provider.RetryAfterError{Wait: interval, Err: errors.New("429")}, interval},
		{"longer wait", &provider.RetryAfterError{Wait: 2593 * time.Second, Err: errors.New("429")}, 2593 * time.Second},
		{"wrapped wait", fmt.Errorf("Claude: %w", &provider.RetryAfterError{Wait: time.Hour, Err: errors.New("429")}), time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextDelay(interval, tt.err); got != tt.want {
				t.Fatalf("nextDelay = %s, want %s", got, tt.want)
			}
		})
	}
}

// The default Claude max_age has to outlast the longest gap a Retry-After
// produces: success at T, a 429 at T+20m saying 2593s, next poll at about
// T+63m.
func TestClaudeCacheOutlastsRetryAfter(t *testing.T) {
	interval, maxAge, err := defaultConfig().providerConfig("claude").durations()
	if err != nil {
		t.Fatal(err)
	}
	if maxAge != 75*time.Minute {
		t.Fatalf("Claude max_age = %s, want 75m", maxAge)
	}
	success := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	state := &providerState{interval: interval, maxAge: maxAge, lastResult: testResult(6), lastFetch: success}

	limited := success.Add(interval)
	nextPoll := limited.Add(nextDelay(interval, &provider.RetryAfterError{Wait: 2593 * time.Second, Err: errors.New("429")}))
	if gap := nextPoll.Sub(success); gap < 63*time.Minute || gap > 64*time.Minute {
		t.Fatalf("next poll at +%s, want about +63m", gap)
	}
	if !state.fresh(nextPoll) {
		t.Fatalf("cache is stale at the next poll (+%s)", nextPoll.Sub(success))
	}
	if state.fresh(success.Add(maxAge + time.Nanosecond)) {
		t.Fatal("cache is fresh past max_age")
	}
}

// The loop itself waits out a Retry-After instead of its interval, and stops
// when halted.
func TestRunHonorsRetryAfterAndStops(t *testing.T) {
	for _, tt := range []struct {
		name     string
		err      error
		minCalls int32
		maxCalls int32
	}{
		{"retry after", &provider.RetryAfterError{Wait: time.Hour, Err: errors.New("429")}, 1, 1},
		{"plain error", errors.New("boom"), 3, 1000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			state := newLocalState(provider.Provider{
				ID: "claude",
				Fetch: func() (*provider.FetchResult, bool, error) {
					calls.Add(1)
					return nil, false, tt.err
				},
			}, nil, 5*time.Millisecond, time.Minute)

			done := make(chan struct{})
			go func() { state.run(); close(done) }()
			time.Sleep(100 * time.Millisecond)
			state.halt()
			state.halt() // idempotent
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("run did not stop after halt")
			}
			if n := calls.Load(); n < tt.minCalls || n > tt.maxCalls {
				t.Fatalf("Fetch called %d times, want %d..%d", n, tt.minCalls, tt.maxCalls)
			}
		})
	}
}

// --- local identity ---

func TestIdentityChangeResetsCacheAndTriggersReconcile(t *testing.T) {
	var mu sync.Mutex
	email := "dprice@gmail.com"
	identity := func() string {
		mu.Lock()
		defer mu.Unlock()
		return email
	}
	fetches := 0
	mc := newMultiCollector()
	mc.enableRemote(&fakeLister{}, nil, time.Minute)
	state := mc.addLocal(provider.Provider{
		ID: "claude",
		Fetch: func() (*provider.FetchResult, bool, error) {
			fetches++
			return nil, false, errors.New("offline")
		},
	}, identity, 20*time.Minute, 75*time.Minute)
	if got := state.key().account; got != "dprice@gmail.com" {
		t.Fatalf("initial account = %q", got)
	}

	state.lastResult, state.lastFetch, state.lastSuccess = testResult(6), time.Now(), true
	state.fetch() // same login: cache kept despite the failure
	if state.lastResult == nil {
		t.Fatal("unchanged login dropped the cache")
	}
	select {
	case <-mc.reconcileCh:
		t.Fatal("unchanged login triggered a reconcile")
	default:
	}

	mu.Lock()
	email = "Alli@Sibilance.io"
	mu.Unlock()
	state.fetch()
	if state.lastResult != nil || !state.lastFetch.IsZero() || state.lastSuccess {
		t.Fatal("a new login kept the previous account's cache")
	}
	if got := state.key().account; got != "alli@sibilance.io" {
		t.Fatalf("account = %q, want the new login, lowercased", got)
	}
	select {
	case <-mc.reconcileCh:
	default:
		t.Fatal("a new login did not trigger a reconcile")
	}
	if fetches != 2 {
		t.Fatalf("Fetch called %d times, want 2", fetches)
	}
	families := gatherCollector(t, mc)
	if got := gaugeWith(t, families, "llm_scrape_fresh", map[string]string{"account": "alli@sibilance.io"}); got != 0 {
		t.Fatalf("new account fresh = %v, want 0", got)
	}
}

// --- remote planning ---

type fakeLister struct {
	mu    sync.Mutex
	creds []cliproxy.Credential
	err   error
}

func (f *fakeLister) set(creds []cliproxy.Credential, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creds, f.err = creds, err
}

func (f *fakeLister) Credentials(context.Context) ([]cliproxy.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cliproxy.Credential(nil), f.creds...), f.err
}

func claudeCred(name, email string) cliproxy.Credential {
	return cliproxy.Credential{Name: name, Provider: "claude", Email: email, AuthIndex: "idx-" + name, Status: "active"}
}

func claudeOnly(id string) bool { return id == "claude" }

func TestPlanRemoteSkips(t *testing.T) {
	disabled := claudeCred("c-disabled", "off@example.com")
	disabled.Disabled = true
	unavailable := claudeCred("c-busy", "busy@example.com")
	unavailable.Status = "unavailable"
	creds := []cliproxy.Credential{
		claudeCred("claude-1fd47d36-dprice@gmail.com.json", "DPrice@gmail.com"), // logged in locally
		claudeCred("claude-3334dadd-alli@sibilance.io.json", "alli@sibilance.io"),
		{Name: "codex-69fac321-dprice@gmail.com-pro.json", Provider: "codex", Email: "dprice@gmail.com", AuthIndex: "f6"},
		disabled,
		unavailable,
	}
	local := map[stateKey]bool{{"claude", "dprice@gmail.com"}: true, {"codex", "dprice@gmail.com"}: true}

	plan := planRemote(creds, local, nil, claudeOnly)
	if len(plan) != 2 {
		t.Fatalf("plan = %v, want alli and the unavailable one", plan)
	}
	if got := plan[stateKey{"claude", "alli@sibilance.io"}].Name; got != "claude-3334dadd-alli@sibilance.io.json" {
		t.Errorf("alli credential = %q", got)
	}
	if _, ok := plan[stateKey{"claude", "busy@example.com"}]; !ok {
		t.Error("an unavailable credential was skipped")
	}
}

func TestPlanRemoteDuplicates(t *testing.T) {
	a := claudeCred("b-second", "alli@sibilance.io")
	b := claudeCred("a-first", "ALLI@sibilance.io")
	key := stateKey{"claude", "alli@sibilance.io"}

	if got := planRemote([]cliproxy.Credential{a, b}, nil, nil, claudeOnly)[key]; got.Name != b.Name {
		t.Errorf("fresh plan chose %q, want the lowest name", got.Name)
	}
	polled := map[stateKey]cliproxy.Credential{key: a}
	for _, order := range [][]cliproxy.Credential{{a, b}, {b, a}} {
		if got := planRemote(order, nil, polled, claudeOnly)[key]; got.Name != a.Name {
			t.Errorf("plan chose %q, want the one already polled", got.Name)
		}
	}
	// The polled one vanished: fall back to the lowest remaining name.
	c := claudeCred("c-third", "alli@sibilance.io")
	if got := planRemote([]cliproxy.Credential{c, b}, nil, polled, claudeOnly)[key]; got.Name != b.Name {
		t.Errorf("plan chose %q after the polled one vanished, want a-first", got.Name)
	}
}

type remoteHarness struct {
	mc     *multiCollector
	lister *fakeLister
}

// newRemoteHarness wires the real remote factories to a fake listing.
func newRemoteHarness(t *testing.T) *remoteHarness {
	t.Helper()
	h := &remoteHarness{mc: newMultiCollector(), lister: &fakeLister{}}
	h.mc.startState = func(s *providerState) { t.Errorf("%s was started; nothing here polls", s.logName()) }
	h.mc.enableRemote(h.lister, remoteFactories(defaultConfig()), time.Minute)
	return h
}

func (h *remoteHarness) reconcile(t *testing.T) {
	t.Helper()
	if err := h.mc.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (h *remoteHarness) remoteNames() []string {
	_, remote := h.mc.states()
	var names []string
	for _, s := range remote {
		names = append(names, s.currentCredential().Name)
	}
	return names
}

func (h *remoteHarness) remoteState(account string) *providerState {
	h.mc.mu.Lock()
	defer h.mc.mu.Unlock()
	return h.mc.remote[stateKey{"claude", account}]
}

// withQuota gives a credential the record CLIProxyAPI keeps of its last
// response's rate-limit headers.
func withQuota(cred cliproxy.Credential, observedAt time.Time, session, weekly string) cliproxy.Credential {
	reset5h := strconv.FormatInt(observedAt.Add(2*time.Hour).Unix(), 10)
	reset7d := strconv.FormatInt(observedAt.Add(3*24*time.Hour).Unix(), 10)
	cred.Quota = cliproxy.Quota{ObservedAt: observedAt, Signals: map[string]string{
		"Anthropic-Ratelimit-Unified-5h-Utilization": session,
		"Anthropic-Ratelimit-Unified-5h-Reset":       reset5h,
		"Anthropic-Ratelimit-Unified-5h-Status":      "allowed",
		"Anthropic-Ratelimit-Unified-7d-Utilization": weekly,
		"Anthropic-Ratelimit-Unified-7d-Reset":       reset7d,
		"Anthropic-Ratelimit-Unified-7d-Status":      "allowed",
		"Anthropic-Ratelimit-Unified-Status":         "allowed",
	}}
	return cred
}

func TestReconcileAddsRemovesAndSurvivesFailedListing(t *testing.T) {
	h := newRemoteHarness(t)
	h.mc.addLocal(provider.Provider{ID: "claude"}, func() string { return "dprice@gmail.com" }, 20*time.Minute, 75*time.Minute)

	now := time.Now()
	alli := withQuota(claudeCred("claude-alli", "alli@sibilance.io"), now, "0.3", "0.08")
	other := withQuota(claudeCred("claude-other", "other@example.com"), now, "0.1", "0.2")
	h.lister.set([]cliproxy.Credential{claudeCred("claude-dprice", "dprice@gmail.com"), alli, other}, nil)
	h.reconcile(t)
	if got := h.remoteNames(); strings.Join(got, ",") != "claude-alli,claude-other" {
		t.Fatalf("remote = %v, want alli and other (dprice is local)", got)
	}
	firstAlli := h.remoteState("alli@sibilance.io")
	alliLabels := map[string]string{"account": "alli@sibilance.io"}
	if got := gaugeWith(t, gatherCollector(t, h.mc), "llm_scrape_success", alliLabels); got != 1 {
		t.Fatalf("alli success = %v, want 1", got)
	}

	// A failed listing keeps every state and its data, but the attempt failed.
	h.lister.set(nil, errors.New("proxy down"))
	if err := h.mc.reconcile(context.Background()); err == nil {
		t.Fatal("reconcile hid the listing failure")
	}
	if got := h.remoteNames(); len(got) != 2 {
		t.Fatalf("failed listing changed the states: %v", got)
	}
	families := gatherCollector(t, h.mc)
	if got := gaugeWith(t, families, "llm_scrape_success", alliLabels); got != 0 {
		t.Fatalf("alli success after a failed listing = %v, want 0", got)
	}
	if got := gaugeWith(t, families, "llm_scrape_fresh", alliLabels); got != 1 {
		t.Fatalf("alli fresh after a failed listing = %v, want 1 (data kept)", got)
	}

	// A listing that omits a credential removes its state; the others keep
	// theirs and take the new record.
	busy := withQuota(alli, now.Add(time.Minute), "0.5", "0.1")
	busy.Status = "unavailable" // a status change is not a new credential
	h.lister.set([]cliproxy.Credential{busy}, nil)
	h.reconcile(t)
	if got := h.remoteNames(); strings.Join(got, ",") != "claude-alli" {
		t.Fatalf("remote = %v, want only alli", got)
	}
	if h.remoteState("alli@sibilance.io") != firstAlli {
		t.Fatal("a still-listed credential got a new state")
	}
	families = gatherCollector(t, h.mc)
	if got := gaugeWith(t, families, "llm_quota_utilization_ratio", map[string]string{"account": "alli@sibilance.io", "category": "Session"}); got != 0.5 {
		t.Fatalf("alli Session = %v, want the new record's 0.5", got)
	}
	for _, m := range families["llm_scrape_success"].Metric {
		for _, l := range m.Label {
			if l.GetValue() == "other@example.com" {
				t.Fatal("a removed account still emits series")
			}
		}
	}
}

// The new login takes over an account the proxy was polling, and frees the
// one it left.
func TestReconcileFollowsLocalLogin(t *testing.T) {
	h := newRemoteHarness(t)
	var email atomic.Value
	email.Store("dprice@gmail.com")
	local := h.mc.addLocal(provider.Provider{ID: "claude", Fetch: func() (*provider.FetchResult, bool, error) {
		return testResult(1), false, nil
	}}, func() string { return email.Load().(string) }, 20*time.Minute, 75*time.Minute)

	h.lister.set([]cliproxy.Credential{claudeCred("claude-dprice", "dprice@gmail.com"), claudeCred("claude-alli", "alli@sibilance.io")}, nil)
	h.reconcile(t)
	if got := h.remoteNames(); strings.Join(got, ",") != "claude-alli" {
		t.Fatalf("remote = %v", got)
	}

	email.Store("alli@sibilance.io")
	local.fetch()
	h.reconcile(t)
	if got := h.remoteNames(); strings.Join(got, ",") != "claude-dprice" {
		t.Fatalf("remote = %v, want dprice once alli is local", got)
	}
}

func TestCollectSkipsRemoteDuplicateOfLocal(t *testing.T) {
	mc := newMultiCollector()
	mc.local = []*providerState{freshState("claude", "dprice@gmail.com", testResult(6))}
	mc.remote[stateKey{"claude", "dprice@gmail.com"}] = freshState("claude", "dprice@gmail.com", testResult(50))
	mc.remote[stateKey{"claude", "alli@sibilance.io"}] = freshState("claude", "alli@sibilance.io", testResult(20))

	families := gatherCollector(t, mc) // fails the test if Gather errors
	if got := gaugeWith(t, families, "llm_quota_utilization_ratio", map[string]string{"account": "dprice@gmail.com"}); got != 0.06 {
		t.Fatalf("dprice utilization = %v, want the local 0.06", got)
	}
	if got := gaugeWith(t, families, "llm_quota_utilization_ratio", map[string]string{"account": "alli@sibilance.io"}); got != 0.2 {
		t.Fatalf("alli utilization = %v, want 0.2", got)
	}
	if n := len(families["llm_scrape_success"].Metric); n != 2 {
		t.Fatalf("%d llm_scrape_success series, want 2", n)
	}
}

func TestTriggerReconcileWithoutProxyIsNoop(t *testing.T) {
	mc := newMultiCollector()
	mc.triggerReconcile() // must not block or panic
}
