package provider

import (
	"strconv"
	"strings"
	"time"

	"github.com/alliprice/headroom/internal/parse"
)

// claudeUnifiedPrefix starts the rate-limit headers Anthropic sends on every
// inference response. They carry the session and weekly windows only: no
// per-model limits and no credits.
const claudeUnifiedPrefix = "anthropic-ratelimit-unified-"

var claudeUnifiedWindows = []struct {
	id      string // header segment
	key     string
	name    string
	seconds int
}{
	{"5h", "five_hour", "Session", 5 * 3600},
	{"7d", "seven_day", "Weekly", 7 * 24 * 3600},
}

// ClaudeUnifiedCategories reads the Session and Weekly windows from recorded
// anthropic-ratelimit-unified-* response headers. Names match
// case-insensitively. For each window, -utilization is a fraction (0..1),
// -reset is unix seconds and -status is allowed or rejected.
//
// A window with no utilization is skipped. A rejected window reads at least
// 100%. A window whose reset is not after now has rolled over since the
// headers were recorded, so it reads 0% with no reset time.
func ClaudeUnifiedCategories(signals map[string]string, now time.Time) []parse.Category {
	lower := make(map[string]string, len(signals))
	for name, value := range signals {
		lower[strings.ToLower(name)] = strings.TrimSpace(value)
	}

	var categories []parse.Category
	for _, w := range claudeUnifiedWindows {
		prefix := claudeUnifiedPrefix + w.id + "-"
		fraction, err := strconv.ParseFloat(lower[prefix+"utilization"], 64)
		if err != nil {
			continue
		}
		utilization := fraction * 100
		if strings.EqualFold(lower[prefix+"status"], "rejected") {
			utilization = max(utilization, 100)
		}

		var resetsAt string
		if secs, err := strconv.ParseInt(lower[prefix+"reset"], 10, 64); err == nil && secs > 0 {
			reset := time.Unix(secs, 0).UTC()
			if reset.After(now) {
				resetsAt = reset.Format(time.RFC3339)
			} else {
				utilization = 0
			}
		}

		categories = append(categories, parse.Category{
			Key:           w.key,
			Name:          w.name,
			Utilization:   utilization,
			ResetsAt:      resetsAt,
			WindowSeconds: w.seconds,
		})
	}
	return categories
}
