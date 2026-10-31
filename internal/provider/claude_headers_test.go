package provider

import (
	"testing"
	"time"
)

// Live signals recorded by CLIProxyAPI for one account, 2026-10-31.
func liveSignals() map[string]string {
	return map[string]string{
		"Anthropic-Ratelimit-Unified-5h-Utilization":       "0.3",
		"Anthropic-Ratelimit-Unified-5h-Reset":             "1793495400",
		"Anthropic-Ratelimit-Unified-5h-Status":            "allowed",
		"Anthropic-Ratelimit-Unified-7d-Utilization":       "0.08",
		"Anthropic-Ratelimit-Unified-7d-Reset":             "1793509200",
		"Anthropic-Ratelimit-Unified-7d-Status":            "allowed",
		"Anthropic-Ratelimit-Unified-Status":               "allowed",
		"Anthropic-Ratelimit-Unified-Reset":                "1793495400",
		"Anthropic-Ratelimit-Unified-Representative-Claim": "five_hour",
		"Anthropic-Ratelimit-Unified-Fallback-Percentage":  "0.5",
	}
}

var signalsNow = time.Unix(1793490000, 0) // before both resets

func TestClaudeUnifiedCategoriesLive(t *testing.T) {
	got := ClaudeUnifiedCategories(liveSignals(), signalsNow)
	if len(got) != 2 {
		t.Fatalf("got %d categories, want Session and Weekly: %#v", len(got), got)
	}
	want := []struct {
		key, name, reset string
		util             float64
		window           int
	}{
		{"five_hour", "Session", "2026-11-01T01:10:00Z", 30, 5 * 3600},
		{"seven_day", "Weekly", "2026-11-01T05:00:00Z", 8, 7 * 24 * 3600},
	}
	for i, w := range want {
		c := got[i]
		if c.Key != w.key || c.Name != w.name || c.ResetsAt != w.reset || c.WindowSeconds != w.window {
			t.Errorf("category %d = %#v, want %s %q reset %s window %d", i, c, w.key, w.name, w.reset, w.window)
		}
		if diff := c.Utilization - w.util; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s utilization = %v, want %v", c.Name, c.Utilization, w.util)
		}
	}
}

func TestClaudeUnifiedCategoriesCaseInsensitive(t *testing.T) {
	signals := map[string]string{
		"anthropic-ratelimit-unified-5h-utilization": " 0.5 ",
		"ANTHROPIC-RATELIMIT-UNIFIED-5H-RESET":       "1793495400",
	}
	got := ClaudeUnifiedCategories(signals, signalsNow)
	if len(got) != 1 || got[0].Name != "Session" || got[0].Utilization != 50 || got[0].ResetsAt == "" {
		t.Fatalf("got %#v", got)
	}
}

func TestClaudeUnifiedCategoriesRejected(t *testing.T) {
	signals := liveSignals()
	signals["Anthropic-Ratelimit-Unified-5h-Status"] = "rejected"
	signals["Anthropic-Ratelimit-Unified-7d-Status"] = "Rejected"
	signals["Anthropic-Ratelimit-Unified-7d-Utilization"] = "1.2"
	got := ClaudeUnifiedCategories(signals, signalsNow)
	if got[0].Utilization != 100 {
		t.Errorf("rejected Session at 30%% = %v, want 100", got[0].Utilization)
	}
	if diff := got[1].Utilization - 120; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("rejected Weekly at 120%% = %v, want 120", got[1].Utilization)
	}
}

// The headers describe the window that was running when they were sent. Once
// its reset has passed, that window is gone.
func TestClaudeUnifiedCategoriesPastReset(t *testing.T) {
	signals := liveSignals()
	signals["Anthropic-Ratelimit-Unified-5h-Status"] = "rejected"
	now := time.Unix(1793495400, 0) // exactly the Session reset
	got := ClaudeUnifiedCategories(signals, now)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Utilization != 0 || got[0].ResetsAt != "" {
		t.Errorf("rolled-over Session = %#v, want 0%% with no reset", got[0])
	}
	if got[1].ResetsAt == "" || got[1].Utilization == 0 {
		t.Errorf("running Weekly = %#v, want it untouched", got[1])
	}
}

func TestClaudeUnifiedCategoriesMissingSignals(t *testing.T) {
	signals := liveSignals()
	delete(signals, "Anthropic-Ratelimit-Unified-7d-Utilization")
	if got := ClaudeUnifiedCategories(signals, signalsNow); len(got) != 1 || got[0].Name != "Session" {
		t.Errorf("without 7d utilization got %#v, want Session only", got)
	}

	signals = liveSignals()
	signals["Anthropic-Ratelimit-Unified-5h-Utilization"] = "n/a"
	delete(signals, "Anthropic-Ratelimit-Unified-7d-Reset")
	got := ClaudeUnifiedCategories(signals, signalsNow)
	if len(got) != 1 || got[0].Name != "Weekly" || got[0].ResetsAt != "" || got[0].Utilization != 8 {
		t.Errorf("got %#v, want Weekly at 8%% with no reset", got)
	}

	for _, signals := range []map[string]string{nil, {}, {"X-Codex-Primary-Used-Percent": "12"}} {
		if got := ClaudeUnifiedCategories(signals, signalsNow); len(got) != 0 {
			t.Errorf("signals %v gave %#v, want none", signals, got)
		}
	}
}
