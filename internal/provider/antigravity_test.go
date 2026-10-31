package provider

import (
	"math"
	"testing"
)

// antigravityReportJSON is a real `agy -p /usage --output-format json`
// envelope, trimmed of prose fields. Note the CLI lists weekly before 5h
// within each group, which is not the order the bars render in.
const antigravityReportJSON = `{
  "conversation_id": "",
  "status": "SUCCESS",
  "response": "Gemini Models\tWeekly Limit Remaining\t97%\t2026-09-26T17:00:01Z\n",
  "command": {
    "name": "usage",
    "data": {
      "description": "Within each group, models share a weekly limit and a 5-hour limit.",
      "groups": [
        {
          "name": "Gemini Models",
          "description": "Models within this group: Gemini Flash, Gemini Pro",
          "buckets": [
            {"id": "gemini-weekly", "name": "Weekly Limit Remaining", "window": "weekly",
             "remaining_fraction": 0.9722581505775452, "reset_time": "2026-09-26T17:00:01Z"},
            {"id": "gemini-5h", "name": "Five Hour Limit Remaining", "window": "5h",
             "remaining_fraction": 0.8886289000511169, "reset_time": "2026-09-19T22:00:01Z"}
          ]
        },
        {
          "name": "Claude and GPT models",
          "description": "Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
          "buckets": [
            {"id": "3p-weekly", "name": "Weekly Limit Remaining", "window": "weekly",
             "remaining_fraction": 1, "reset_time": "2026-09-26T17:16:09Z"},
            {"id": "3p-5h", "name": "Five Hour Limit Remaining", "window": "5h",
             "remaining_fraction": 1, "reset_time": "2026-09-19T22:16:09Z"}
          ]
        }
      ]
    }
  }
}`

func TestParseAntigravityReturnsBarsInDisplayOrder(t *testing.T) {
	categories, err := parseAntigravity([]byte(antigravityReportJSON))
	if err != nil {
		t.Fatalf("parseAntigravity returned error: %v", err)
	}
	if len(categories) != 4 {
		t.Fatalf("parseAntigravity returned %d categories, want 4", len(categories))
	}

	want := []struct {
		key  string
		name string
	}{
		{"antigravity_gemini_5h", "Gemini Session"},
		{"antigravity_gemini_weekly", "Gemini Weekly"},
		{"antigravity_3p_5h", "Claude/GPT Session"},
		{"antigravity_3p_weekly", "Claude/GPT Weekly"},
	}
	for i, w := range want {
		if categories[i].Key != w.key || categories[i].Name != w.name {
			t.Errorf("category %d = %#v, want key %q name %q", i, categories[i], w.key, w.name)
		}
	}
}

func TestParseAntigravityUtilizationInvertsRemainingFraction(t *testing.T) {
	categories, err := parseAntigravity([]byte(antigravityReportJSON))
	if err != nil {
		t.Fatalf("parseAntigravity returned error: %v", err)
	}

	// gemini-5h: remaining 0.8886289000511169 -> 11.13711% used.
	if got := categories[0].Utilization; math.Abs(got-11.13711) > 0.0001 {
		t.Errorf("Gemini Session utilization = %v, want ~11.13711", got)
	}
	// 3p-5h: untouched quota reads as zero used, not as a missing bar.
	if got := categories[2].Utilization; math.Abs(got) > 0.000001 {
		t.Errorf("Claude/GPT Session utilization = %v, want 0", got)
	}
}

func TestParseAntigravityCarriesWindowAndResetTime(t *testing.T) {
	categories, err := parseAntigravity([]byte(antigravityReportJSON))
	if err != nil {
		t.Fatalf("parseAntigravity returned error: %v", err)
	}

	if categories[0].WindowSeconds != 5*3600 {
		t.Errorf("Gemini Session WindowSeconds = %d, want %d", categories[0].WindowSeconds, 5*3600)
	}
	if categories[1].WindowSeconds != 7*24*3600 {
		t.Errorf("Gemini Weekly WindowSeconds = %d, want %d", categories[1].WindowSeconds, 7*24*3600)
	}
	if categories[0].ResetsAt != "2026-09-19T22:00:01Z" {
		t.Errorf("Gemini Session ResetsAt = %q", categories[0].ResetsAt)
	}
}

// An unrecognised bucket must be dropped rather than rendered under its raw id.
func TestParseAntigravitySkipsUnknownBucket(t *testing.T) {
	data := `{"status":"SUCCESS","command":{"data":{"groups":[{"buckets":[
		{"id":"gemini-5h","window":"5h","remaining_fraction":0.5,"reset_time":"2026-09-19T22:00:01Z"},
		{"id":"nimbus-quill-7h","window":"7h","remaining_fraction":0.5,"reset_time":"2026-09-19T22:00:01Z"}
	]}]}}}`

	categories, err := parseAntigravity([]byte(data))
	if err != nil {
		t.Fatalf("parseAntigravity returned error: %v", err)
	}
	if len(categories) != 1 {
		t.Fatalf("parseAntigravity returned %d categories, want 1", len(categories))
	}
	if categories[0].Key != "antigravity_gemini_5h" {
		t.Errorf("category = %#v, want the known bucket only", categories[0])
	}
}

func TestParseAntigravityRejectsNonSuccessStatus(t *testing.T) {
	data := `{"status":"ERROR","command":{"data":{"groups":[]}}}`
	if _, err := parseAntigravity([]byte(data)); err == nil {
		t.Fatal("expected error for non-SUCCESS status")
	}
}

func TestParseAntigravityRejectsMalformedJSON(t *testing.T) {
	if _, err := parseAntigravity([]byte("not json")); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseAntigravityEmptyGroups(t *testing.T) {
	categories, err := parseAntigravity([]byte(`{"status":"SUCCESS","command":{"data":{"groups":[]}}}`))
	if err != nil {
		t.Fatalf("parseAntigravity returned error: %v", err)
	}
	if len(categories) != 0 {
		t.Fatalf("parseAntigravity returned %d categories, want 0", len(categories))
	}
}

func TestAntigravityAuthError(t *testing.T) {
	cases := []struct {
		message string
		want    bool
	}{
		{"You are not logged into Antigravity.", true},
		{"error getting token source: token expired", true},
		{"connection refused", false},
		{"", false},
	}
	for _, c := range cases {
		if got := antigravityAuthError(c.message); got != c.want {
			t.Errorf("antigravityAuthError(%q) = %v, want %v", c.message, got, c.want)
		}
	}
}

// The exporter runs under a launchd PATH without ~/.local/bin, where the
// installer puts agy, so discovery must not rely on LookPath alone.
func TestAntigravityCommandPathFallsBackToLocalBin(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	path, err := antigravityCommandPath()
	if err != nil {
		t.Skip("agy not installed at ~/.local/bin; nothing to verify")
	}
	if path == "" {
		t.Fatal("antigravityCommandPath returned an empty path")
	}
}
