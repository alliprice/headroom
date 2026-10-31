package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func gatherState(t *testing.T, state *providerState) map[string]*dto.MetricFamily {
	t.Helper()
	return gatherCollector(t, &multiCollector{local: []*providerState{state}})
}

func gatherCollector(t *testing.T, mc *multiCollector) map[string]*dto.MetricFamily {
	t.Helper()

	registry := prometheus.NewRegistry()
	registry.MustRegister(mc)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	result := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		result[family.GetName()] = family
	}
	return result
}

// gaugeWith returns the one gauge in family name whose labels include all of
// want.
func gaugeWith(t *testing.T, families map[string]*dto.MetricFamily, name string, want map[string]string) float64 {
	t.Helper()
	family, ok := families[name]
	if !ok {
		t.Fatalf("missing family %q", name)
	}
	var found []*dto.Metric
	for _, m := range family.Metric {
		labels := make(map[string]string, len(m.Label))
		for _, l := range m.Label {
			labels[l.GetName()] = l.GetValue()
		}
		match := true
		for k, v := range want {
			if labels[k] != v {
				match = false
				break
			}
		}
		if match {
			found = append(found, m)
		}
	}
	if len(found) != 1 || found[0].Gauge == nil {
		t.Fatalf("%s%v matched %d gauges, want 1", name, want, len(found))
	}
	return found[0].Gauge.GetValue()
}

func gaugeValue(t *testing.T, families map[string]*dto.MetricFamily, name string) float64 {
	t.Helper()
	family, ok := families[name]
	if !ok || len(family.Metric) != 1 || family.Metric[0].Gauge == nil {
		t.Fatalf("missing single gauge %q", name)
	}
	return family.Metric[0].Gauge.GetValue()
}

func testResult(utilization float64) *provider.FetchResult {
	return &provider.FetchResult{Categories: []parse.Category{{
		Key:           "weekly",
		Name:          "Weekly",
		Utilization:   utilization,
		ResetsAt:      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		WindowSeconds: 7 * 24 * 60 * 60,
	}}}
}

func assertNoUsageMetrics(t *testing.T, families map[string]*dto.MetricFamily) {
	t.Helper()
	for name := range families {
		if strings.HasPrefix(name, "llm_quota_") || strings.HasPrefix(name, "llm_extra_") || strings.HasPrefix(name, "llm_credit_") {
			t.Errorf("ineligible cache emitted %s", name)
		}
	}
}

func TestFreshMaximumAgeBoundary(t *testing.T) {
	lastFetch := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	state := &providerState{
		interval:   10 * time.Minute,
		maxAge:     35 * time.Minute,
		lastResult: testResult(6),
		lastFetch:  lastFetch,
		// A failed attempt must not disqualify a populated, unexpired cache.
		lastSuccess: false,
	}
	for _, tt := range []struct {
		name string
		age  time.Duration
		want bool
	}{
		{"first missed poll", 10 * time.Minute, true},
		{"second missed poll", 20 * time.Minute, true},
		{"recovery poll", 30 * time.Minute, true},
		{"before expiry", 35*time.Minute - time.Nanosecond, true},
		{"at expiry", 35 * time.Minute, true},
		{"after expiry", 35*time.Minute + time.Nanosecond, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := state.fresh(lastFetch.Add(tt.age)); got != tt.want {
				t.Fatalf("fresh at age %s = %v, want %v", tt.age, got, tt.want)
			}
		})
	}
}

func TestFreshRequiresCacheTimestampAndPositiveMaxAge(t *testing.T) {
	for _, tt := range []struct {
		name   string
		modify func(*providerState)
	}{
		{"no result", func(s *providerState) { s.lastResult = nil }},
		{"no timestamp", func(s *providerState) { s.lastFetch = time.Time{} }},
		{"zero max_age", func(s *providerState) { s.maxAge = 0 }},
		{"negative max_age", func(s *providerState) { s.maxAge = -time.Minute }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
			state := &providerState{
				maxAge:      35 * time.Minute,
				lastResult:  testResult(6),
				lastFetch:   now,
				lastSuccess: true,
			}
			tt.modify(state)
			if state.fresh(now) {
				t.Fatal("invalid cache is fresh")
			}
		})
	}
}

func TestCollectEmitsFreshProviderResult(t *testing.T) {
	state := &providerState{
		prov:        provider.Provider{ID: "claude"},
		interval:    20 * time.Minute,
		maxAge:      40 * time.Minute,
		lastResult:  testResult(6),
		lastFetch:   time.Now().Add(-10 * time.Minute),
		lastSuccess: true,
	}

	families := gatherState(t, state)
	if got := gaugeValue(t, families, "llm_scrape_success"); got != 1 {
		t.Fatalf("llm_scrape_success = %v, want 1", got)
	}
	if got := gaugeValue(t, families, "llm_scrape_fresh"); got != 1 {
		t.Fatalf("llm_scrape_fresh = %v, want 1", got)
	}
	if got := gaugeValue(t, families, "llm_quota_utilization_ratio"); got != 0.06 {
		t.Fatalf("llm_quota_utilization_ratio = %v, want 0.06", got)
	}
}

func TestCollectSuppressesExpiredQuotaAndExtraUsage(t *testing.T) {
	for _, lastSuccess := range []bool{true, false} {
		name := "last attempt failed"
		if lastSuccess {
			name = "last attempt succeeded"
		}
		t.Run(name, func(t *testing.T) {
			state := &providerState{
				prov:        provider.Provider{ID: "codex"},
				interval:    10 * time.Minute,
				maxAge:      35 * time.Minute,
				lastResult:  testResult(100),
				lastFetch:   time.Now().Add(-time.Minute),
				lastSuccess: lastSuccess,
			}
			state.lastResult.Extra = &parse.ExtraUsage{Utilization: 50, UsedCredits: 100, MonthlyLimit: 200}
			families := gatherState(t, state)
			if got := gaugeValue(t, families, "llm_scrape_fresh"); got != 1 {
				t.Fatalf("unexpired llm_scrape_fresh = %v, want 1", got)
			}
			for _, metric := range []string{
				"llm_quota_utilization_ratio", "llm_quota_glide_ratio", "llm_quota_headroom_ratio",
				"llm_quota_glide_hours", "llm_quota_headroom_hours", "llm_quota_sleep_recovery_ratio",
				"llm_quota_adjusted_headroom_ratio", "llm_quota_reset_timestamp_seconds", "llm_quota_window_seconds",
				"llm_extra_utilization_ratio", "llm_extra_glide_ratio", "llm_extra_used_dollars", "llm_extra_limit_dollars",
			} {
				gaugeValue(t, families, metric)
			}

			state.lastFetch = time.Now().Add(-36 * time.Minute)
			families = gatherState(t, state)
			wantSuccess := 0.0
			if lastSuccess {
				wantSuccess = 1
			}
			if got := gaugeValue(t, families, "llm_scrape_success"); got != wantSuccess {
				t.Fatalf("llm_scrape_success = %v, want %v", got, wantSuccess)
			}
			if got := gaugeValue(t, families, "llm_scrape_fresh"); got != 0 {
				t.Fatalf("expired llm_scrape_fresh = %v, want 0", got)
			}
			if got := gaugeValue(t, families, "llm_scrape_timestamp_seconds"); got != float64(state.lastFetch.Unix()) {
				t.Fatalf("successful timestamp = %v, want %d", got, state.lastFetch.Unix())
			}
			assertNoUsageMetrics(t, families)
		})
	}
}

func TestFailedPollsPreserveCachedResult(t *testing.T) {
	for _, failure := range []string{"Probe", "Fetch"} {
		t.Run(failure, func(t *testing.T) {
			fetchCalls := 0
			state := &providerState{
				prov: provider.Provider{
					ID:    "codex",
					Probe: func() bool { return failure != "Probe" },
					Fetch: func() (*provider.FetchResult, bool, error) {
						fetchCalls++
						return nil, false, errors.New("fetch failed")
					},
				},
				interval:    10 * time.Minute,
				maxAge:      35 * time.Minute,
				lastResult:  testResult(6),
				lastFetch:   time.Now().Add(-20 * time.Minute),
				lastSuccess: true,
			}
			originalResult, originalFetch := state.lastResult, state.lastFetch
			for poll := 1; poll <= 2; poll++ {
				state.fetch()
				if state.lastSuccess {
					t.Fatalf("failed poll %d remained successful", poll)
				}
				if state.lastResult != originalResult || !state.lastFetch.Equal(originalFetch) {
					t.Fatalf("failed poll %d changed successful cache or timestamp", poll)
				}
				if !state.fresh(originalFetch.Add(time.Duration(poll) * 10 * time.Minute)) {
					t.Fatalf("cache is not fresh after %d missed polls", poll)
				}
				families := gatherState(t, state)
				if got := gaugeValue(t, families, "llm_scrape_success"); got != 0 {
					t.Fatalf("llm_scrape_success = %v, want 0", got)
				}
				if got := gaugeValue(t, families, "llm_scrape_fresh"); got != 1 {
					t.Fatalf("llm_scrape_fresh = %v, want 1", got)
				}
				if got := gaugeValue(t, families, "llm_quota_utilization_ratio"); got != 0.06 {
					t.Fatalf("cached utilization = %v, want 0.06", got)
				}
				if got := gaugeValue(t, families, "llm_scrape_timestamp_seconds"); got != float64(originalFetch.Unix()) {
					t.Fatalf("successful timestamp = %v, want %d", got, originalFetch.Unix())
				}
			}
			wantCalls := 2
			if failure == "Probe" {
				wantCalls = 0
			}
			if fetchCalls != wantCalls {
				t.Fatalf("Fetch called %d times, want %d", fetchCalls, wantCalls)
			}
		})
	}
}

func TestInitialFailureHasNoCacheOrTimestamp(t *testing.T) {
	for _, failure := range []string{"Probe", "Fetch"} {
		t.Run(failure, func(t *testing.T) {
			state := &providerState{
				prov: provider.Provider{
					ID:    "codex",
					Probe: func() bool { return failure != "Probe" },
					Fetch: func() (*provider.FetchResult, bool, error) {
						return nil, false, errors.New("initial fetch failed")
					},
				},
				interval: 10 * time.Minute,
				maxAge:   35 * time.Minute,
			}
			// Startup is not successful or fresh before the first attempt, either.
			for attempt := 0; attempt < 2; attempt++ {
				families := gatherState(t, state)
				for _, metric := range []string{"llm_scrape_success", "llm_scrape_fresh"} {
					if got := gaugeValue(t, families, metric); got != 0 {
						t.Fatalf("%s = %v, want 0", metric, got)
					}
				}
				if _, ok := families["llm_scrape_timestamp_seconds"]; ok {
					t.Fatal("never-populated cache emitted a successful timestamp")
				}
				assertNoUsageMetrics(t, families)
				if attempt == 0 {
					state.fetch()
				}
			}
		})
	}
}

func TestSuccessfulFetchRecoversCache(t *testing.T) {
	for _, age := range []time.Duration{32 * time.Minute, 36 * time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			newResult := testResult(12)
			state := &providerState{
				prov: provider.Provider{
					ID: "codex",
					// A nil Probe is also a supported successful acquisition path.
					Fetch: func() (*provider.FetchResult, bool, error) { return newResult, false, nil },
				},
				interval:    10 * time.Minute,
				maxAge:      35 * time.Minute,
				lastResult:  testResult(6),
				lastFetch:   time.Now().Add(-age),
				lastSuccess: false,
			}
			before := time.Now()
			state.fetch()
			after := time.Now()
			if !state.lastSuccess || state.lastResult != newResult || state.lastFetch.Before(before) || state.lastFetch.After(after) {
				t.Fatal("successful fetch did not replace cache and successful timestamp")
			}
			families := gatherState(t, state)
			for _, metric := range []string{"llm_scrape_success", "llm_scrape_fresh"} {
				if got := gaugeValue(t, families, metric); got != 1 {
					t.Fatalf("%s = %v, want 1", metric, got)
				}
			}
			if got := gaugeValue(t, families, "llm_quota_utilization_ratio"); got != 0.12 {
				t.Fatalf("recovered utilization = %v, want 0.12", got)
			}
			if got := gaugeValue(t, families, "llm_scrape_timestamp_seconds"); got != float64(state.lastFetch.Unix()) {
				t.Fatalf("recovered timestamp = %v, want %d", got, state.lastFetch.Unix())
			}
		})
	}
}

func TestMultiCollectorAddKeepsResolvedDurations(t *testing.T) {
	mc := newMultiCollector()
	mc.addLocal(provider.Provider{ID: "codex"}, nil, 10*time.Minute, 35*time.Minute)
	if len(mc.local) != 1 || mc.local[0].interval != 10*time.Minute || mc.local[0].maxAge != 35*time.Minute {
		t.Fatal("collector did not retain resolved provider durations")
	}
}
