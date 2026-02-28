package parse

// Window durations in seconds
const (
	WindowFiveHour     = 5 * 3600
	WindowSevenDay     = 7 * 24 * 3600
	WindowSevenDayOpus = 7 * 24 * 3600
)

// WindowDurations maps API keys to window durations in seconds.
var WindowDurations = map[string]int{
	"five_hour":        WindowFiveHour,
	"seven_day":        WindowSevenDay,
	"seven_day_opus":   WindowSevenDayOpus,
	"seven_day_sonnet": WindowSevenDay,
}

// DisplayNames maps API keys to human-readable names.
var DisplayNames = map[string]string{
	"five_hour":        "Session",
	"seven_day":        "Weekly",
	"seven_day_opus":   "Opus (weekly)",
	"seven_day_sonnet": "Sonnet (weekly)",
}

// CategoryOrder is the preferred display order for categories.
var CategoryOrder = []string{"five_hour", "seven_day", "seven_day_opus"}

// Category represents a single usage category (Claude or Codex).
type Category struct {
	Key           string
	Name          string
	Utilization   float64
	ResetsAt      string // ISO 8601 timestamp
	WindowSeconds int
}

// ExtraUsage represents the extra/overflow usage billing info.
type ExtraUsage struct {
	MonthlyLimit float64 // in cents
	UsedCredits  float64 // in cents
	Utilization  float64 // percentage 0-100
}

// Refresh intervals in seconds
const (
	RefreshFocused     = 300
	RefreshUnfocused   = 600
	RefreshOnAuthError = 30 * 60
)

// Sleep mode
const (
	SleepAfterUnfocusedSeconds = 2 * 60 * 60
)
