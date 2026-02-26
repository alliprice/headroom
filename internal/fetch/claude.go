package fetch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FetchClaude retrieves usage data from the Claude API using the provided OAuth token.
// Returns (nil, error) on failure. The error message is suitable for display to the user.
func FetchClaude(token string) (map[string]any, error) {
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
