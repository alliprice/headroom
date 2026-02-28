package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/alliprice/headroom/internal/auth"
	"github.com/alliprice/headroom/internal/parse"
)

// Claude is the provider for Claude API usage data.
var Claude = Provider{
	ID:          "claude",
	DisplayName: "Claude",
	CategoryIDs: []string{"five_hour", "seven_day", "seven_day_opus"},
	Probe:       nil, // always attempted
	Fetch:       fetchClaude,
}

// claude-specific constants

var claudeWindowDurations = map[string]int{
	"five_hour":        5 * 3600,
	"seven_day":        7 * 24 * 3600,
	"seven_day_opus":   7 * 24 * 3600,
	"seven_day_sonnet": 7 * 24 * 3600,
}

var claudeDisplayNames = map[string]string{
	"five_hour":        "Session",
	"seven_day":        "Weekly",
	"seven_day_opus":   "Opus (weekly)",
	"seven_day_sonnet": "Sonnet (weekly)",
}

var claudeCategoryOrder = []string{"five_hour", "seven_day", "seven_day_opus"}

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

func fetchClaude() (*FetchResult, bool, error) {
	token, err := auth.GetAccessToken()
	if err != nil {
		return nil, true, err
	}

	data, err := fetchClaudeAPI(token)
	if err != nil {
		isAuth := strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "authenticate")
		return nil, isAuth, err
	}

	cats, extra := parseClaude(data)
	return &FetchResult{Categories: cats, Extra: extra}, false, nil
}

// fetchClaudeAPI retrieves usage data from the Claude API using the provided
// OAuth token.
func fetchClaudeAPI(token string) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("Network error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("Token expired - run 'claude' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", resp.Status)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("Network error: %w", err)
	}
	return data, nil
}

// parseClaude parses the raw Claude usage API response into categories and
// optional extra usage.
func parseClaude(data map[string]any) ([]parse.Category, *parse.ExtraUsage) {
	var categories []parse.Category
	seen := make(map[string]bool)

	// First pass: emit known keys in preferred order.
	for _, key := range claudeCategoryOrder {
		raw, ok := data[key]
		if !ok {
			continue
		}
		seen[key] = true

		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		utilization, hasUtil := parse.AsFloat64(entry["utilization"])
		resetsAt, hasResets := parse.AsString(entry["resets_at"])

		if !hasUtil && !hasResets {
			continue
		}
		if !hasUtil {
			utilization = 0.0
		}

		window, wok := claudeWindowDurations[key]
		if !wok {
			window = 5 * 3600
		}

		name, nok := claudeDisplayNames[key]
		if !nok {
			name = key
		}

		categories = append(categories, parse.Category{
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

		utilization, hasUtil := parse.AsFloat64(entry["utilization"])
		resetsAt, hasResets := parse.AsString(entry["resets_at"])

		if !hasUtil && !hasResets {
			continue
		}
		if !hasUtil {
			utilization = 0.0
		}

		window, wok := claudeWindowDurations[key]
		if !wok {
			window = 7 * 24 * 3600
		}

		name, nok := claudeDisplayNames[key]
		if !nok {
			name = titleCase(key)
		}

		categories = append(categories, parse.Category{
			Key:           key,
			Name:          name,
			Utilization:   utilization,
			ResetsAt:      resetsAt,
			WindowSeconds: window,
		})
	}

	// Parse extra_usage block.
	var extra *parse.ExtraUsage
	if eu, ok := data["extra_usage"].(map[string]any); ok {
		if isEnabled, _ := eu["is_enabled"].(bool); isEnabled {
			limit, _ := parse.AsFloat64(eu["monthly_limit"])
			used, _ := parse.AsFloat64(eu["used_credits"])
			var util float64
			if limit > 0 {
				util = used / limit * 100
			}
			extra = &parse.ExtraUsage{
				MonthlyLimit: limit,
				UsedCredits:  used,
				Utilization:  util,
			}
		}
	}

	return categories, extra
}
