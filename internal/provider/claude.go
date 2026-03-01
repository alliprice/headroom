package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/alliprice/headroom/internal/parse"
)

// Claude is the provider for Claude API usage data.
var Claude = Provider{
	ID:          "claude",
	DisplayName: "Claude",
	CategoryIDs: []string{"five_hour", "seven_day", "seven_day_opus"},
	Probe:       nil, // always attempted
	Fetch:       fetchClaude,
	Demo:        demoClaude,
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
	"seven_day_opus":   "Opus",
	"seven_day_sonnet": "Sonnet",
}

var claudeCategoryOrder = []string{"five_hour", "seven_day", "seven_day_opus"}

// --- credential chain ---

// claudeCredentials is the JSON structure stored by Claude Code
// in both macOS Keychain and ~/.claude/.credentials.json.
type claudeCredentials struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// claudeConfigDir returns the Claude Code config directory.
// Respects CLAUDE_CONFIG_DIR, defaults to ~/.claude.
func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".claude")
	}
	return filepath.Join(home, ".claude")
}

// claudeCredentialProvider retrieves an OAuth access token from a single source.
type claudeCredentialProvider interface {
	getToken() (string, error)
}

var claudeCredentialChain = []claudeCredentialProvider{
	claudeEnvProvider{},
	claudeFileProvider{},
	claudeKeychainProvider{},
}

func claudeGetAccessToken() (string, error) {
	return claudeGetAccessTokenFromChain(claudeCredentialChain)
}

func claudeGetAccessTokenFromChain(chain []claudeCredentialProvider) (string, error) {
	var lastErr error
	for _, p := range chain {
		token, err := p.getToken()
		if err == nil {
			return token, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", fmt.Errorf("all credential providers failed (last: %w)", lastErr)
	}
	return "", fmt.Errorf("no credential providers configured")
}

// env provider - CLAUDE_CODE_OAUTH_TOKEN

type claudeEnvProvider struct{}

func (claudeEnvProvider) getToken() (string, error) {
	token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if token == "" {
		return "", fmt.Errorf("CLAUDE_CODE_OAUTH_TOKEN not set")
	}
	return token, nil
}

// file provider - ~/.claude/.credentials.json

type claudeFileProvider struct{}

func (claudeFileProvider) getToken() (string, error) {
	path := filepath.Join(claudeConfigDir(), ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("credentials file: %w", err)
	}

	var creds claudeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("credentials file: invalid JSON: %w", err)
	}

	token := creds.ClaudeAiOauth.AccessToken
	if token == "" {
		return "", fmt.Errorf("credentials file: no token found")
	}
	return token, nil
}

// keychain provider - macOS Keychain

type claudeKeychainProvider struct{}

func (claudeKeychainProvider) getToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("keychain access timed out")
		}

		var notFound *exec.Error
		if errors.As(err, &notFound) && errors.Is(notFound.Err, exec.ErrNotFound) {
			return "", fmt.Errorf("'security' command not found (macOS only)")
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("keychain error - run 'claude' to re-authenticate")
		}

		return "", fmt.Errorf("keychain error: %w", err)
	}

	var creds claudeCredentials
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &creds); err != nil {
		return "", fmt.Errorf("invalid credentials - run 'claude' to re-authenticate")
	}

	token := creds.ClaudeAiOauth.AccessToken
	if token == "" {
		return "", fmt.Errorf("no token found - run 'claude' to authenticate")
	}

	return token, nil
}

// --- fetch + parse ---

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
	token, err := claudeGetAccessToken()
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

// --- demo ---

func demoClaude() *FetchResult {
	now := time.Now().UTC()
	weekly := 40 + rand.Float64()*40
	sonnet := weekly * (0.1 + rand.Float64()*0.15)
	weeklyReset := now.Add(time.Duration(24+rand.Intn(120)) * time.Hour).Format(time.RFC3339)

	cats := []parse.Category{
		{
			Key:           "five_hour",
			Name:          "Session",
			Utilization:   20 + rand.Float64()*60,
			ResetsAt:      now.Add(time.Duration(1+rand.Intn(4)) * time.Hour).Format(time.RFC3339),
			WindowSeconds: 5 * 3600,
		},
		{
			Key:           "seven_day",
			Name:          "Weekly",
			Utilization:   weekly,
			ResetsAt:      weeklyReset,
			WindowSeconds: 7 * 24 * 3600,
		},
		{
			Key:           "seven_day_sonnet",
			Name:          "Sonnet",
			Utilization:   sonnet,
			ResetsAt:      weeklyReset,
			WindowSeconds: 7 * 24 * 3600,
		},
	}
	extra := &parse.ExtraUsage{
		MonthlyLimit: 10000,
		UsedCredits:  3500 + rand.Float64()*3000,
	}
	extra.Utilization = extra.UsedCredits / extra.MonthlyLimit * 100
	return &FetchResult{Categories: cats, Extra: extra}
}
