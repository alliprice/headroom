package parse

import (
	"strings"
	"time"
	"unicode"
)

// geminiFamily extracts the model family from a Gemini model ID.
// "gemini-2.0-flash" -> "flash", "gemini-2.5-flash-lite" -> "flash-lite",
// "gemini-2.5-pro" -> "pro", "gemini-3-pro-preview" -> "pro"
func geminiFamily(modelID string) string {
	lower := strings.ToLower(modelID)
	// flash-lite before flash - more specific match first.
	if strings.Contains(lower, "flash-lite") {
		return "flash-lite"
	}
	if strings.Contains(lower, "flash") {
		return "flash"
	}
	if strings.Contains(lower, "pro") {
		return "pro"
	}
	return modelID
}

// geminiFamilyDisplayName returns the human-readable name for a family.
var geminiFamilyDisplayName = map[string]string{
	"flash":      "Flash",
	"flash-lite": "Flash Lite",
	"pro":        "Pro",
}

// ParseGemini parses the raw Gemini retrieveUserQuota API response into a
// slice of Category values. Buckets are grouped by model family (flash, pro)
// with the most-used value per family. data is the top-level JSON object
// decoded as map[string]any.
func ParseGemini(data map[string]any) []Category {
	raw, ok := data["buckets"]
	if !ok {
		return nil
	}
	buckets, ok := raw.([]any)
	if !ok {
		return nil
	}

	// First pass: find the lowest remainingFraction per family.
	type familyData struct {
		remaining float64
		resetTime string
	}
	families := make(map[string]*familyData)
	// Track insertion order so output is deterministic.
	var familyOrder []string

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

		// Skip untouched models.
		if remaining >= 1 {
			continue
		}

		family := geminiFamily(modelID)
		resetTime, _ := asString(bucket["resetTime"])

		if existing, ok := families[family]; ok {
			// Keep the most-used (lowest remaining) value.
			if remaining < existing.remaining {
				existing.remaining = remaining
				existing.resetTime = resetTime
			}
		} else {
			families[family] = &familyData{remaining: remaining, resetTime: resetTime}
			familyOrder = append(familyOrder, family)
		}
	}

	// Second pass: build categories from deduplicated families.
	var categories []Category
	for _, family := range familyOrder {
		fd := families[family]

		utilization := (1 - fd.remaining) * 100
		if utilization < 0 {
			utilization = 0
		}
		if utilization > 100 {
			utilization = 100
		}

		name := family
		if dn, ok := geminiFamilyDisplayName[family]; ok {
			name = dn
		}

		windowSeconds := 86400
		if fd.resetTime != "" {
			if t, err := time.Parse(time.RFC3339, fd.resetTime); err == nil {
				secs := int(t.Sub(NowFunc()).Seconds())
				if secs > 0 {
					windowSeconds = secs
				}
			}
		}

		categories = append(categories, Category{
			Key:           "gemini_" + family,
			Name:          name,
			Utilization:   utilization,
			ResetsAt:      fd.resetTime,
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
