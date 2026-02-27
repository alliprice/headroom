package cli

import "testing"

func TestCategoryStatus_Conserve(t *testing.T) {
	// usage >= 80% produces "conserve" regardless of headroom
	got := categoryStatus(80, 5)
	if got != "conserve" {
		t.Errorf("categoryStatus(80, 5) = %q, want %q", got, "conserve")
	}
	got = categoryStatus(95, -10)
	if got != "conserve" {
		t.Errorf("categoryStatus(95, -10) = %q, want %q", got, "conserve")
	}
}

func TestCategoryStatus_PlentyOfRoom(t *testing.T) {
	// headroom >= 0 and usage < 80 produces "plenty of room"
	got := categoryStatus(50, 10)
	if got != "plenty of room" {
		t.Errorf("categoryStatus(50, 10) = %q, want %q", got, "plenty of room")
	}
	got = categoryStatus(0, 0)
	if got != "plenty of room" {
		t.Errorf("categoryStatus(0, 0) = %q, want %q", got, "plenty of room")
	}
}

func TestCategoryStatus_SlowDown(t *testing.T) {
	// headroom < 0 and usage < 80 produces "slow down"
	got := categoryStatus(50, -5)
	if got != "slow down" {
		t.Errorf("categoryStatus(50, -5) = %q, want %q", got, "slow down")
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
