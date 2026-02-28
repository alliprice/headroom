package parse

import (
	"testing"
	"time"
)

func TestParseGemini_SingleBucket(t *testing.T) {
	NowFunc = func() time.Time {
		return time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { NowFunc = time.Now })

	data := map[string]any{
		"buckets": []any{
			map[string]any{
				"modelId":           "gemini-2.0-flash",
				"remainingFraction": 0.7,
				"resetTime":         "2025-06-16T12:00:00Z",
			},
		},
	}
	cats := ParseGemini(data)
	if len(cats) != 1 {
		t.Fatalf("expected 1 category, got %d", len(cats))
	}
	c := cats[0]
	if c.Key != "gemini_gemini-2.0-flash" {
		t.Errorf("Key = %q, want %q", c.Key, "gemini_gemini-2.0-flash")
	}
	if c.Name != "2.0 Flash" {
		t.Errorf("Name = %q, want %q", c.Name, "2.0 Flash")
	}
	// Utilization = (1 - 0.7) * 100 = 30
	if c.Utilization < 29.9 || c.Utilization > 30.1 {
		t.Errorf("Utilization = %f, want ~30", c.Utilization)
	}
	if c.ResetsAt != "2025-06-16T12:00:00Z" {
		t.Errorf("ResetsAt = %q, want %q", c.ResetsAt, "2025-06-16T12:00:00Z")
	}
	// Window: 24h = 86400s
	if c.WindowSeconds != 86400 {
		t.Errorf("WindowSeconds = %d, want 86400", c.WindowSeconds)
	}
}

func TestParseGemini_MultipleBuckets(t *testing.T) {
	NowFunc = func() time.Time {
		return time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { NowFunc = time.Now })

	data := map[string]any{
		"buckets": []any{
			map[string]any{
				"modelId":           "gemini-2.0-flash",
				"remainingFraction": 0.7,
				"resetTime":         "2025-06-16T12:00:00Z",
			},
			map[string]any{
				"modelId":           "gemini-2.5-pro",
				"remainingFraction": 0.4,
				"resetTime":         "2025-06-16T12:00:00Z",
			},
		},
	}
	cats := ParseGemini(data)
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(cats))
	}
	if cats[0].Name != "2.0 Flash" {
		t.Errorf("cats[0].Name = %q, want %q", cats[0].Name, "2.0 Flash")
	}
	if cats[1].Name != "2.5 Pro" {
		t.Errorf("cats[1].Name = %q, want %q", cats[1].Name, "2.5 Pro")
	}
	// (1 - 0.4) * 100 = 60
	if cats[1].Utilization < 59.9 || cats[1].Utilization > 60.1 {
		t.Errorf("cats[1].Utilization = %f, want ~60", cats[1].Utilization)
	}
}

func TestParseGemini_ZeroRemaining(t *testing.T) {
	NowFunc = func() time.Time {
		return time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { NowFunc = time.Now })

	data := map[string]any{
		"buckets": []any{
			map[string]any{
				"modelId":           "gemini-2.0-flash",
				"remainingFraction": 0.0,
				"resetTime":         "2025-06-16T12:00:00Z",
			},
		},
	}
	cats := ParseGemini(data)
	if len(cats) != 1 {
		t.Fatalf("expected 1 category, got %d", len(cats))
	}
	if cats[0].Utilization < 99.9 || cats[0].Utilization > 100.1 {
		t.Errorf("Utilization = %f, want 100", cats[0].Utilization)
	}
}

func TestParseGemini_FullRemaining_Skipped(t *testing.T) {
	NowFunc = func() time.Time {
		return time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { NowFunc = time.Now })

	data := map[string]any{
		"buckets": []any{
			map[string]any{
				"modelId":           "gemini-2.0-flash",
				"remainingFraction": 1.0,
				"resetTime":         "2025-06-16T12:00:00Z",
			},
		},
	}
	cats := ParseGemini(data)
	if len(cats) != 0 {
		t.Fatalf("expected 0 categories (0%% utilization skipped), got %d", len(cats))
	}
}

func TestParseGemini_NoBucketsKey(t *testing.T) {
	data := map[string]any{"other": "stuff"}
	cats := ParseGemini(data)
	if cats != nil {
		t.Errorf("expected nil, got %v", cats)
	}
}

func TestParseGemini_EmptyBuckets(t *testing.T) {
	data := map[string]any{"buckets": []any{}}
	cats := ParseGemini(data)
	if len(cats) != 0 {
		t.Errorf("expected 0 categories, got %d", len(cats))
	}
}

func TestParseGemini_MissingRemainingFraction(t *testing.T) {
	data := map[string]any{
		"buckets": []any{
			map[string]any{
				"modelId":   "gemini-2.0-flash",
				"resetTime": "2025-06-16T12:00:00Z",
			},
		},
	}
	cats := ParseGemini(data)
	if len(cats) != 0 {
		t.Errorf("expected 0 categories (missing remainingFraction), got %d", len(cats))
	}
}

func TestParseGemini_VertexVariantsFiltered(t *testing.T) {
	NowFunc = func() time.Time {
		return time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { NowFunc = time.Now })

	data := map[string]any{
		"buckets": []any{
			map[string]any{
				"modelId":           "gemini-2.5-pro",
				"remainingFraction": 0.5,
				"resetTime":         "2025-06-16T12:00:00Z",
				"tokenType":         "REQUESTS",
			},
			map[string]any{
				"modelId":           "gemini-2.5-pro_vertex",
				"remainingFraction": 0.5,
				"resetTime":         "2025-06-16T12:00:00Z",
				"tokenType":         "REQUESTS",
			},
			map[string]any{
				"modelId":           "gemini-2.0-flash",
				"remainingFraction": 0.8,
				"resetTime":         "2025-06-16T12:00:00Z",
				"tokenType":         "REQUESTS",
			},
			map[string]any{
				"modelId":           "gemini-2.0-flash_vertex",
				"remainingFraction": 0.8,
				"resetTime":         "2025-06-16T12:00:00Z",
				"tokenType":         "REQUESTS",
			},
		},
	}
	cats := ParseGemini(data)
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories (vertex filtered), got %d", len(cats))
	}
	if cats[0].Key != "gemini_gemini-2.5-pro" {
		t.Errorf("cats[0].Key = %q, want %q", cats[0].Key, "gemini_gemini-2.5-pro")
	}
	if cats[1].Key != "gemini_gemini-2.0-flash" {
		t.Errorf("cats[1].Key = %q, want %q", cats[1].Key, "gemini_gemini-2.0-flash")
	}
}

func TestGeminiDisplayName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"gemini-2.0-flash", "2.0 Flash"},
		{"gemini-2.5-pro", "2.5 Pro"},
		{"gemini-2.5-flash-lite", "2.5 Flash Lite"},
		{"gemini-3-flash-preview", "3 Flash Preview"},
		{"gemini-1.5-pro-latest", "1.5 Pro Latest"},
		{"unknown-model", "Unknown Model"},
		{"", ""},
	}
	for _, tc := range cases {
		got := geminiDisplayName(tc.input)
		if got != tc.want {
			t.Errorf("geminiDisplayName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseGemini_WindowComputation(t *testing.T) {
	// Freeze time to 12:00 UTC; reset at 18:00 UTC = 6 hours = 21600s
	NowFunc = func() time.Time {
		return time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { NowFunc = time.Now })

	data := map[string]any{
		"buckets": []any{
			map[string]any{
				"modelId":           "gemini-2.0-flash",
				"remainingFraction": 0.5,
				"resetTime":         "2025-06-15T18:00:00Z",
			},
		},
	}
	cats := ParseGemini(data)
	if len(cats) != 1 {
		t.Fatalf("expected 1 category, got %d", len(cats))
	}
	if cats[0].WindowSeconds != 21600 {
		t.Errorf("WindowSeconds = %d, want 21600", cats[0].WindowSeconds)
	}
}
