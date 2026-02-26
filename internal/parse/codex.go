package parse

import (
	"time"
)

// ParseCodex parses the raw Codex usage API response into a slice of
// Category values. data is the top-level JSON object decoded as map[string]any.
func ParseCodex(data map[string]any) []Category {
	var categories []Category

	result, _ := data["result"].(map[string]any)
	rateLimits, _ := result["rateLimits"].(map[string]any)

	type mapping struct {
		rpcKey         string
		catKey         string
		name           string
		defaultWinMins int
	}

	mappings := []mapping{
		{"primary", "codex_primary", "Session", 300},
		{"secondary", "codex_secondary", "Weekly", 10080},
	}

	for _, m := range mappings {
		raw, ok := rateLimits[m.rpcKey]
		if !ok {
			continue
		}
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		windowMins := m.defaultWinMins
		if wm, ok := entry["windowDurationMins"].(float64); ok {
			windowMins = int(wm)
		}

		var resetsISO string
		if resetsUnix, ok := entry["resetsAt"].(float64); ok && resetsUnix != 0 {
			t := time.Unix(int64(resetsUnix), 0).UTC()
			resetsISO = t.Format(time.RFC3339)
		}

		utilization, _ := asFloat64(entry["usedPercent"])

		categories = append(categories, Category{
			Key:           m.catKey,
			Name:          m.name,
			Utilization:   utilization,
			ResetsAt:      resetsISO,
			WindowSeconds: windowMins * 60,
		})
	}

	return categories
}
