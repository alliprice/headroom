package provider

import (
	"testing"
)

// claudeUsageFixture mirrors the shape of /api/oauth/usage: two live meters,
// a feature flag that arrives shaped like a meter, several flags that arrive
// as bare nulls, and the blocks that are not quota at all.
func claudeUsageFixture() map[string]any {
	return map[string]any{
		"five_hour": map[string]any{
			"utilization": 10.0,
			"resets_at":   "2026-09-20T00:09:59.785109+00:00",
		},
		"seven_day": map[string]any{
			"utilization": 12.0,
			"resets_at":   "2026-09-26T13:59:59.785138+00:00",
		},
		"nimbus_quill": map[string]any{
			"utilization": 0.0,
			"resets_at":   nil,
		},
		"tangelo":                    nil,
		"seven_day_opus":             nil,
		"member_dashboard_available": true,
		"limits":                     []any{},
		"extra_usage": map[string]any{
			"is_enabled":    false,
			"monthly_limit": 20000.0,
			"used_credits":  0.0,
		},
	}
}

func categoryNames(t *testing.T, data map[string]any) []string {
	t.Helper()
	categories, _ := parseClaude(data)
	names := make([]string, 0, len(categories))
	for _, c := range categories {
		names = append(names, c.Name)
	}
	return names
}

func TestParseClaudeEmitsLiveMeters(t *testing.T) {
	categories, _ := parseClaude(claudeUsageFixture())
	if len(categories) != 2 {
		t.Fatalf("parseClaude returned %d categories, want 2: %#v", len(categories), categories)
	}
	if categories[0].Name != "Session" || categories[0].WindowSeconds != 5*3600 {
		t.Errorf("first category = %#v", categories[0])
	}
	if categories[1].Name != "Weekly" || categories[1].WindowSeconds != 7*24*3600 {
		t.Errorf("second category = %#v", categories[1])
	}
	if categories[0].Utilization != 10 {
		t.Errorf("Session utilization = %v, want 10", categories[0].Utilization)
	}
}

// A meter with no reset time has no window to pace against. Anthropic ships
// feature flags through this endpoint shaped like meters; nimbus_quill is one.
func TestParseClaudeDropsMeterWithoutResetTime(t *testing.T) {
	for _, name := range categoryNames(t, claudeUsageFixture()) {
		if name == "Nimbus Quill" {
			t.Fatal("a meter with a null resets_at must not become a bar")
		}
	}
}

// The counterpart: nothing is matched against a list of known ids, so a meter
// Anthropic publishes later still shows up without a code change.
func TestParseClaudeKeepsNewlyPublishedMeter(t *testing.T) {
	data := claudeUsageFixture()
	data["seven_day_fable"] = map[string]any{
		"utilization": 23.0,
		"resets_at":   "2026-09-26T13:59:59.785138+00:00",
	}

	categories, _ := parseClaude(data)
	var found bool
	for _, c := range categories {
		if c.Key != "seven_day_fable" {
			continue
		}
		found = true
		if c.Name != "Fable" {
			t.Errorf("name = %q, want %q (derived from the seven_day_ prefix)", c.Name, "Fable")
		}
		if c.WindowSeconds != 7*24*3600 {
			t.Errorf("WindowSeconds = %d, want %d", c.WindowSeconds, 7*24*3600)
		}
		if c.Utilization != 23 {
			t.Errorf("Utilization = %v, want 23", c.Utilization)
		}
	}
	if !found {
		t.Fatalf("newly published meter was dropped: %#v", categories)
	}
}

func TestParseClaudeDerivesFiveHourPrefix(t *testing.T) {
	data := claudeUsageFixture()
	data["five_hour_fable"] = map[string]any{
		"utilization": 5.0,
		"resets_at":   "2026-09-20T00:09:59.785109+00:00",
	}

	categories, _ := parseClaude(data)
	for _, c := range categories {
		if c.Key == "five_hour_fable" {
			if c.Name != "Fable" || c.WindowSeconds != 5*3600 {
				t.Fatalf("category = %#v, want name Fable on a five hour window", c)
			}
			return
		}
	}
	t.Fatal("five_hour_fable was dropped")
}

// Known ids keep their curated names rather than the derived one.
func TestParseClaudeKnownNamesWinOverPrefix(t *testing.T) {
	data := claudeUsageFixture()
	data["seven_day_opus"] = map[string]any{
		"utilization": 40.0,
		"resets_at":   "2026-09-26T13:59:59.785138+00:00",
	}

	for _, name := range categoryNames(t, data) {
		if name == "Opus" {
			return
		}
	}
	t.Fatal("seven_day_opus should render as Opus")
}

// Ranging a Go map is randomised; the second pass must not shuffle bars
// between refreshes once more than one unmapped meter is live.
func TestParseClaudeSecondPassOrderIsStable(t *testing.T) {
	data := claudeUsageFixture()
	for _, key := range []string{"seven_day_fable", "seven_day_zephyr", "seven_day_aurora"} {
		data[key] = map[string]any{
			"utilization": 1.0,
			"resets_at":   "2026-09-26T13:59:59.785138+00:00",
		}
	}

	first := categoryNames(t, data)
	for i := 0; i < 50; i++ {
		if got := categoryNames(t, data); !equalStrings(got, first) {
			t.Fatalf("order changed between runs:\n  %v\n  %v", first, got)
		}
	}
	// Known meters keep their curated order ahead of the sorted remainder.
	want := []string{"Session", "Weekly", "Aurora", "Fable", "Zephyr"}
	if !equalStrings(first, want) {
		t.Errorf("order = %v, want %v", first, want)
	}
}

func TestParseClaudeExtraUsageDisabled(t *testing.T) {
	_, extra := parseClaude(claudeUsageFixture())
	if extra != nil {
		t.Fatalf("extra usage is disabled in the fixture, got %#v", extra)
	}
}

func TestParseClaudeExtraUsageEnabled(t *testing.T) {
	data := claudeUsageFixture()
	data["extra_usage"] = map[string]any{
		"is_enabled":    true,
		"monthly_limit": 20000.0,
		"used_credits":  5000.0,
	}

	_, extra := parseClaude(data)
	if extra == nil {
		t.Fatal("expected extra usage")
	}
	if extra.Utilization != 25 {
		t.Errorf("Utilization = %v, want 25", extra.Utilization)
	}
}

func TestParseClaudeEmpty(t *testing.T) {
	categories, extra := parseClaude(map[string]any{})
	if len(categories) != 0 {
		t.Errorf("got %d categories, want 0", len(categories))
	}
	if extra != nil {
		t.Errorf("got %#v, want nil extra usage", extra)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
