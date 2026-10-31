package cli

import (
	"strings"
	"testing"
)

func TestCategoryStatus_Conserve(t *testing.T) {
	got := categoryStatus(80, 5, 3)
	if got != "conserve" {
		t.Errorf("categoryStatus(80, 5, 3) = %q, want %q", got, "conserve")
	}
	got = categoryStatus(95, -10, -15)
	if got != "conserve" {
		t.Errorf("categoryStatus(95, -10, -15) = %q, want %q", got, "conserve")
	}
}

func TestCategoryStatus_ConserveAdjusted(t *testing.T) {
	got := categoryStatus(50, 15, 5)
	if got != "conserve" {
		t.Errorf("categoryStatus(50, 15, 5) = %q, want %q", got, "conserve")
	}
}

func TestCategoryStatus_SlowDownSleepRecovers(t *testing.T) {
	got := categoryStatus(50, 15, -5)
	if got != "slow down" {
		t.Errorf("categoryStatus(50, 15, -5) = %q, want %q", got, "slow down")
	}
}

func TestCategoryStatus_PlentyOfRoom(t *testing.T) {
	got := categoryStatus(50, -10, -20)
	if got != "plenty of room" {
		t.Errorf("categoryStatus(50, -10, -20) = %q, want %q", got, "plenty of room")
	}
	got = categoryStatus(0, 0, -5)
	if got != "plenty of room" {
		t.Errorf("categoryStatus(0, 0, -5) = %q, want %q", got, "plenty of room")
	}
}

func TestCategoryStatus_SlowDown(t *testing.T) {
	got := categoryStatus(50, 5, -5)
	if got != "slow down" {
		t.Errorf("categoryStatus(50, 5, -5) = %q, want %q", got, "slow down")
	}
}

func TestExtraStatus_Warning(t *testing.T) {
	got := extraStatus(80)
	if got != "warning" {
		t.Errorf("extraStatus(80) = %q, want %q", got, "warning")
	}
	got = extraStatus(100)
	if got != "warning" {
		t.Errorf("extraStatus(100) = %q, want %q", got, "warning")
	}
}

func TestExtraStatus_OnTrack(t *testing.T) {
	got := extraStatus(79.9)
	if got != "on track" {
		t.Errorf("extraStatus(79.9) = %q, want %q", got, "on track")
	}
	got = extraStatus(0)
	if got != "on track" {
		t.Errorf("extraStatus(0) = %q, want %q", got, "on track")
	}
}

// failure reporting

func TestFailureMessageDropsProviderPrefix(t *testing.T) {
	got := failureMessage("Alibaba Token Plan", "Alibaba Token Plan: session expired", 0)
	if got != "session expired" {
		t.Errorf("got %q, want %q", got, "session expired")
	}
}

func TestFailureMessageFlattensMultilineJSON(t *testing.T) {
	raw := "quota request failed: {\n  \"error\": {\n    \"code\": 3\n  }\n}"
	got := failureMessage("Alibaba Token Plan", raw, 0)
	if strings.Contains(got, "\n") {
		t.Errorf("message still spans lines: %q", got)
	}
	if !strings.Contains(got, `"code": 3`) {
		t.Errorf("flattening lost content: %q", got)
	}
}

func TestFailureMessageTruncates(t *testing.T) {
	got := failureMessage("X", strings.Repeat("a", 500), 50)
	if len(got) != 50 {
		t.Errorf("len = %d, want 50", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated message should be marked: %q", got)
	}
}

// A message shorter than the limit is left exactly as it is.
func TestFailureMessageShortMessageUntouched(t *testing.T) {
	got := failureMessage("X", "short", 200)
	if got != "short" {
		t.Errorf("got %q, want %q", got, "short")
	}
}
