package parse

import (
	"strings"
	"time"
	"unicode"
)

// ParseGemini parses the raw Gemini retrieveUserQuota API response into a
// slice of Category values. data is the top-level JSON object decoded as
// map[string]any.
func ParseGemini(data map[string]any) []Category {
	var categories []Category

	raw, ok := data["buckets"]
	if !ok {
		return nil
	}
	buckets, ok := raw.([]any)
	if !ok {
		return nil
	}

	for _, b := range buckets {
		bucket, ok := b.(map[string]any)
		if !ok {
			continue
		}

		remaining, hasRemaining := asFloat64(bucket["remainingFraction"])
		if !hasRemaining {
			continue
		}

		modelID, _ := asString(bucket["modelId"])

		// Skip _vertex routing variants - they mirror the base model quota.
		if strings.HasSuffix(modelID, "_vertex") {
			continue
		}

		utilization := (1 - remaining) * 100
		if utilization < 0 {
			utilization = 0
		}
		if utilization > 100 {
			utilization = 100
		}

		key := "gemini_" + modelID

		resetTime, _ := asString(bucket["resetTime"])

		windowSeconds := 86400 // default 24h
		if resetTime != "" {
			if t, err := time.Parse(time.RFC3339, resetTime); err == nil {
				secs := int(t.Sub(NowFunc()).Seconds())
				if secs > 0 {
					windowSeconds = secs
				}
			}
		}

		categories = append(categories, Category{
			Key:           key,
			Name:          geminiDisplayName(modelID),
			Utilization:   utilization,
			ResetsAt:      resetTime,
			WindowSeconds: windowSeconds,
		})
	}

	return categories
}

// geminiDisplayName converts a Gemini model ID into a human-friendly name.
// "gemini-2.0-flash" -> "2.0 Flash", "gemini-2.5-pro" -> "2.5 Pro"
func geminiDisplayName(modelID string) string {
	name := modelID
	// Strip "gemini-" prefix if present.
	if strings.HasPrefix(name, "gemini-") {
		name = name[len("gemini-"):]
	}
	// Replace hyphens with spaces and title-case each word.
	words := strings.Split(name, "-")
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
