package parse

import (
	"fmt"
	"time"
)

// NowFunc is the clock source for all time-dependent calculations. Tests can
// override this to freeze time; production code uses the default time.Now.
var NowFunc = time.Now

// CalcGlideSlope returns the percentage of the window that has elapsed, as a
// value in [0, 100]. resetsAt is an ISO 8601 / RFC 3339 timestamp string.
// Returns 0.0 if resetsAt is empty or unparseable.
func CalcGlideSlope(resetsAt string, windowSeconds int) float64 {
	if resetsAt == "" || windowSeconds <= 0 {
		return 0.0
	}

	t, err := time.Parse(time.RFC3339, resetsAt)
	if err != nil {
		return 0.0
	}

	now := NowFunc().UTC()
	remaining := t.Sub(now).Seconds()
	elapsed := float64(windowSeconds) - remaining
	pct := elapsed / float64(windowSeconds) * 100

	if pct < 0 {
		return 0.0
	}
	if pct > 100 {
		return 100.0
	}
	return pct
}

// CalcMonthGlide returns the percentage of the current calendar month that
// has elapsed, as a value in [0, 100]. Useful for pacing extra usage against
// a monthly budget.
func CalcMonthGlide() float64 {
	now := NowFunc()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)
	total := monthEnd.Sub(monthStart).Seconds()
	elapsed := now.Sub(monthStart).Seconds()
	return elapsed / total * 100
}

// FormatMonthReset returns a human-readable string for when the current
// calendar month ends (the next 1st), e.g. "Resets Mar 1".
func FormatMonthReset() string {
	now := NowFunc()
	nextFirst := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	return "Resets " + nextFirst.Format("Jan 2")
}

// FormatResetTime returns a human-readable string describing when resetsAt
// will occur relative to now. resetsAt is an ISO 8601 / RFC 3339 timestamp.
// Returns "" if resetsAt is empty or unparseable.
func FormatResetTime(resetsAt string) string {
	if resetsAt == "" {
		return ""
	}

	t, err := time.Parse(time.RFC3339, resetsAt)
	if err != nil {
		return ""
	}

	now := NowFunc().UTC()
	remaining := t.Sub(now).Seconds()

	if remaining <= 0 {
		return "Resetting now"
	}

	if remaining < 3600 {
		mins := int(remaining / 60)
		return fmt.Sprintf("Resets in %d min", mins)
	}

	if remaining < 24*3600 {
		hrs := int(remaining / 3600)
		mins := int(int(remaining)%3600) / 60
		if mins > 0 {
			return fmt.Sprintf("Resets in %d hr %d min", hrs, mins)
		}
		return fmt.Sprintf("Resets in %d hr", hrs)
	}

	// More than 24 hours away: show day and time in local timezone.
	local := t.Local()
	day := local.Format("Mon")
	hour := local.Hour() % 12
	if hour == 0 {
		hour = 12
	}
	ampm := "AM"
	if local.Hour() >= 12 {
		ampm = "PM"
	}
	minute := local.Format("04")
	return fmt.Sprintf("Resets %s %d:%s %s", day, hour, minute, ampm)
}

// FormatUpdatedAgo returns a human-readable string describing how long ago
// lastFetchTime was. Pass nil to indicate the data has never been fetched.
func FormatUpdatedAgo(lastFetchTime *time.Time) string {
	if lastFetchTime == nil {
		return "Updated: never"
	}

	elapsed := NowFunc().Sub(*lastFetchTime).Seconds()

	if elapsed < 5 {
		return "Updated: just now"
	}
	if elapsed < 60 {
		return fmt.Sprintf("Updated: %ds ago", int(elapsed))
	}
	if elapsed < 3600 {
		return fmt.Sprintf("Updated: %dm ago", int(elapsed/60))
	}
	return fmt.Sprintf("Updated: %dh ago", int(elapsed/3600))
}
