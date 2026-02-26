package parse

import (
	"strings"
	"unicode"
)

// titleCase replaces underscores with spaces and title-cases each word.
func titleCase(s string) string {
	words := strings.Split(strings.ReplaceAll(s, "_", " "), " ")
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

// ParseClaude parses the raw Claude usage API response into categories and
// optional extra usage. data is the top-level JSON object decoded as
// map[string]any.
func ParseClaude(data map[string]any) ([]Category, *ExtraUsage) {
	var categories []Category
	seen := make(map[string]bool)

	// First pass: emit known keys in preferred order.
	for _, key := range CategoryOrder {
		raw, ok := data[key]
		if !ok {
			continue
		}
		seen[key] = true

		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		utilization, hasUtil := asFloat64(entry["utilization"])
		resetsAt, hasResets := asString(entry["resets_at"])

		if !hasUtil && !hasResets {
			continue
		}
		if !hasUtil {
			utilization = 0.0
		}

		window, wok := WindowDurations[key]
		if !wok {
			window = WindowFiveHour
		}

		name, nok := DisplayNames[key]
		if !nok {
			name = key
		}

		categories = append(categories, Category{
			Key:           key,
			Name:          name,
			Utilization:   utilization,
			ResetsAt:      resetsAt,
			WindowSeconds: window,
		})
	}

	// Second pass: any remaining keys not yet seen and not "extra_usage".
	for key, raw := range data {
		if seen[key] || key == "extra_usage" {
			continue
		}

		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		utilization, hasUtil := asFloat64(entry["utilization"])
		resetsAt, hasResets := asString(entry["resets_at"])

		if !hasUtil && !hasResets {
			continue
		}
		if !hasUtil {
			utilization = 0.0
		}

		window, wok := WindowDurations[key]
		if !wok {
			window = WindowSevenDay
		}

		name, nok := DisplayNames[key]
		if !nok {
			name = titleCase(key)
		}

		categories = append(categories, Category{
			Key:           key,
			Name:          name,
			Utilization:   utilization,
			ResetsAt:      resetsAt,
			WindowSeconds: window,
		})
	}

	// Parse extra_usage block.
	var extra *ExtraUsage
	if eu, ok := data["extra_usage"].(map[string]any); ok {
		if isEnabled, _ := eu["is_enabled"].(bool); isEnabled {
			limit, _ := asFloat64(eu["monthly_limit"])
			used, _ := asFloat64(eu["used_credits"])
			var util float64
			if limit > 0 {
				util = used / limit * 100
			}
			extra = &ExtraUsage{
				MonthlyLimit: limit,
				UsedCredits:  used,
				Utilization:  util,
			}
		}
	}

	return categories, extra
}

// asFloat64 extracts a float64 from any, returning (0, false) when absent or
// not a number.
func asFloat64(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

// asString extracts a string from any, returning ("", false) when absent or
// not a string.
func asString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
