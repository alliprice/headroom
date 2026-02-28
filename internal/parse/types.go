package parse

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

// AsFloat64 extracts a float64 from any, returning (0, false) when absent or
// not a number.
func AsFloat64(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

// AsString extracts a string from any, returning ("", false) when absent or
// not a string.
func AsString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
