package parse

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func TestCalcSleepRecoveryPct_ThreeNights(t *testing.T) {
	// Tuesday noon to Friday noon = 3 sleep periods (Wed 0-7, Thu 0-7, Fri 0-7)
	now := time.Date(2025, 3, 25, 12, 0, 0, 0, time.UTC) // Tuesday noon
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = time.Now }()

	resetAt := "2025-03-28T12:00:00Z" // Friday noon
	windowSeconds := 7 * 24 * 3600    // 7 days

	got := CalcSleepRecoveryPct(resetAt, windowSeconds)
	// 3 full sleep periods (Wed, Thu, Fri 0:00-7:00)
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
	// No sleep periods between noon and 5pm same day
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

func TestCalcSleepRecoveryPct_DuringNight(t *testing.T) {
	// 3am - should only count remaining 4 hours of tonight's sleep (3am-7am)
	// plus any future nights
	now := time.Date(2025, 3, 25, 3, 0, 0, 0, time.UTC) // 3am Tuesday
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = time.Now }()

	resetAt := "2025-03-26T12:00:00Z" // Wednesday noon
	windowSeconds := 7 * 24 * 3600

	got := CalcSleepRecoveryPct(resetAt, windowSeconds)
	// Tonight: 4 hours remaining (3am-7am)
	// Wed night: 7 hours (0am-7am)
	wantHours := 4.0 + 7.0
	want := wantHours * 3600 / float64(windowSeconds) * 100
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("CalcSleepRecoveryPct during night = %.2f, want %.2f (%.0f hours)", got, want, wantHours)
	}
}

func TestCalcSleepRecoveryPct_GradualDrain(t *testing.T) {
	// Verify the value decreases gradually through the night, no cliff
	windowSeconds := 7 * 24 * 3600
	resetAt := "2025-03-26T12:00:00Z" // next day noon

	var values []float64
	for hour := 23; hour <= 31; hour++ { // 11pm through 7am next day
		h := hour % 24
		d := 25
		if hour >= 24 {
			d = 26
		}
		now := time.Date(2025, 3, d, h, 0, 0, 0, time.UTC)
		NowFunc = func() time.Time { return now }
		v := CalcSleepRecoveryPct(resetAt, windowSeconds)
		values = append(values, v)
	}
	NowFunc = time.Now

	// Values should be monotonically non-increasing through the night
	for i := 1; i < len(values); i++ {
		if values[i] > values[i-1]+0.001 {
			t.Errorf("Sleep recovery increased from hour %d to %d: %.4f -> %.4f",
				22+i, 23+i, values[i-1], values[i])
		}
	}

	// 11pm and midnight should be equal (no cliff at midnight)
	// Actually at 11pm there's no sleep happening yet, at midnight sleep starts draining
	// But the key thing: no big jump between any consecutive hours
	for i := 1; i < len(values); i++ {
		diff := values[i-1] - values[i]
		maxDiff := 1.0 * 3600 / float64(windowSeconds) * 100 * 1.1 // ~1 hour's worth + margin
		if diff > maxDiff {
			t.Errorf("Cliff detected between hours: diff=%.4f, max expected=%.4f", diff, maxDiff)
		}
	}
}

func TestCalcSleepRecoveryPct_SleepEndsAtLocalSevenOnDSTNights(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	windowSeconds := 7 * 24 * 3600
	cases := []struct {
		name      string
		now       time.Time
		reset     time.Time
		wantHours float64
	}{
		// Clocks fall back at 02:00 on Nov 1 2026, so midnight to 07:00 CST
		// is eight elapsed hours; at 06:30 half an hour of sleep remains.
		{"fall back", time.Date(2026, 11, 1, 6, 30, 0, 0, chicago), time.Date(2026, 11, 1, 12, 0, 0, 0, chicago), 0.5},
		// Clocks spring forward at 02:00 on Mar 8 2026, so midnight to 07:00
		// CDT is six elapsed hours; at 07:30 the night is over.
		{"spring forward", time.Date(2026, 3, 8, 7, 30, 0, 0, chicago), time.Date(2026, 3, 8, 12, 0, 0, 0, chicago), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			NowFunc = func() time.Time { return c.now }
			defer func() { NowFunc = time.Now }()

			got := CalcSleepRecoveryPct(c.reset.UTC().Format(time.RFC3339), windowSeconds)
			want := c.wantHours * 3600 / float64(windowSeconds) * 100
			if got < want-0.001 || got > want+0.001 {
				t.Errorf("CalcSleepRecoveryPct = %.4f, want %.4f (%.1f hours)", got, want, c.wantHours)
			}
		})
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
