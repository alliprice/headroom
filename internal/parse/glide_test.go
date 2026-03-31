package parse

import (
	"testing"
	"time"
)

func TestCalcSleepRecoveryPct_ThreeNights(t *testing.T) {
	// Tuesday noon to Friday noon = 3 nights
	now := time.Date(2025, 3, 25, 12, 0, 0, 0, time.UTC) // Tuesday
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = time.Now }()

	resetAt := "2025-03-28T12:00:00Z" // Friday noon
	windowSeconds := 7 * 24 * 3600    // 7 days

	got := CalcSleepRecoveryPct(resetAt, windowSeconds)
	// 3 nights * 7 hours * 3600 / (7*24*3600) * 100 = 12.5%
	want := 3.0 * DefaultSleepHoursPerNight * 3600 / float64(windowSeconds) * 100
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("CalcSleepRecoveryPct = %.2f, want %.2f", got, want)
	}
}

func TestCalcSleepRecoveryPct_SameDay(t *testing.T) {
	now := time.Date(2025, 3, 25, 12, 0, 0, 0, time.UTC)
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = time.Now }()

	resetAt := "2025-03-25T17:00:00Z" // same day, 5 hours later
	windowSeconds := 5 * 3600

	got := CalcSleepRecoveryPct(resetAt, windowSeconds)
	if got != 0.0 {
		t.Errorf("CalcSleepRecoveryPct same day = %.2f, want 0.0", got)
	}
}

func TestCalcSleepRecoveryPct_ResetInPast(t *testing.T) {
	now := time.Date(2025, 3, 25, 12, 0, 0, 0, time.UTC)
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = time.Now }()

	resetAt := "2025-03-24T12:00:00Z" // yesterday
	windowSeconds := 7 * 24 * 3600

	got := CalcSleepRecoveryPct(resetAt, windowSeconds)
	if got != 0.0 {
		t.Errorf("CalcSleepRecoveryPct past = %.2f, want 0.0", got)
	}
}

func TestCalcSleepRecoveryPct_MidnightBoundary(t *testing.T) {
	// 11:59 PM to next day 12:00 AM = 1 night
	now := time.Date(2025, 3, 25, 23, 59, 0, 0, time.UTC)
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = time.Now }()

	resetAt := "2025-03-26T00:00:00Z"
	windowSeconds := 24 * 3600

	got := CalcSleepRecoveryPct(resetAt, windowSeconds)
	want := 1.0 * DefaultSleepHoursPerNight * 3600 / float64(windowSeconds) * 100
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("CalcSleepRecoveryPct midnight = %.2f, want %.2f", got, want)
	}
}

func TestCalcSleepAdjustedGlide(t *testing.T) {
	now := time.Date(2025, 3, 25, 12, 0, 0, 0, time.UTC)
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = time.Now }()

	resetAt := "2025-03-28T12:00:00Z"
	windowSeconds := 7 * 24 * 3600

	glide := CalcGlideSlope(resetAt, windowSeconds)
	recovery := CalcSleepRecoveryPct(resetAt, windowSeconds)
	adj := CalcSleepAdjustedGlide(resetAt, windowSeconds)

	want := glide + recovery
	if want > 100 {
		want = 100
	}
	if adj < want-0.01 || adj > want+0.01 {
		t.Errorf("CalcSleepAdjustedGlide = %.2f, want %.2f (glide=%.2f, recovery=%.2f)", adj, want, glide, recovery)
	}
}
