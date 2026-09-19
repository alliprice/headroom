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
	"sort"
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
		ExpiresAt   int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// tokenAt returns the access token, rejecting one that has already expired.
// Expiry has to be caught here rather than left to the API: the chain stops at
// the first source that yields a token, so a stale credentials file shadows a
// live keychain entry for as long as it sits on disk. ExpiresAt is epoch
// milliseconds and is absent in older files, so zero means no expiry recorded.
func (c claudeCredentials) tokenAt(now time.Time) (string, error) {
	token := c.ClaudeAiOauth.AccessToken
	if token == "" {
		return "", fmt.Errorf("no token found")
	}
	if c.ClaudeAiOauth.ExpiresAt > 0 {
		expiry := time.UnixMilli(c.ClaudeAiOauth.ExpiresAt)
		if !now.Before(expiry) {
			return "", fmt.Errorf("token expired %s ago", now.Sub(expiry).Round(time.Second))
		}
	}
	return token, nil
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

	token, err := creds.tokenAt(time.Now())
	if err != nil {
		return "", fmt.Errorf("credentials file: %w", err)
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

	token, err := creds.tokenAt(time.Now())
	if err != nil {
		return "", fmt.Errorf("%w - run 'claude' to re-authenticate", err)
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
		return nil, true, fmt.Errorf("%s", humanizeError("Claude", err))
	}

	data, err := fetchClaudeAPI(token)
	if err != nil {
		isAuth := strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "authenticate")
		return nil, isAuth, fmt.Errorf("%s", humanizeError("Claude", err))
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

// claudeWindowPrefixes derives a window from an unmapped meter's key. A slice
// rather than a map so the match order is fixed.
var claudeWindowPrefixes = []struct {
	prefix  string
	seconds int
}{
	{"seven_day_", 7 * 24 * 3600},
	{"five_hour_", 5 * 3600},
}

// claudeCategory builds a usage category from one entry of the usage API.
//
// A meter with no reset time has no window to pace against, so it is not a
// quota bar. Anthropic ships feature flags through this same endpoint shaped
// like meters - nimbus_quill arrives as an object carrying utilization 0 and a
// null resets_at - and the reset time is what tells the two apart. Never an
// allowlist of known ids: Anthropic does publish genuinely new meters, and
// those have to appear on their own.
func claudeCategory(key string, entry map[string]any) (parse.Category, bool) {
	resetsAt, ok := parse.AsString(entry["resets_at"])
	if !ok || resetsAt == "" {
		return parse.Category{}, false
	}

	utilization, hasUtil := parse.AsFloat64(entry["utilization"])
	if !hasUtil {
		utilization = 0
	}

	name, hasName := claudeDisplayNames[key]
	window, hasWindow := claudeWindowDurations[key]

	// An unmapped meter takes its name and window from its prefix, so a newly
	// published seven_day_fable reads "Fable" over seven days, the way
	// seven_day_opus already reads "Opus".
	if !hasName || !hasWindow {
		for _, p := range claudeWindowPrefixes {
			if !strings.HasPrefix(key, p.prefix) {
				continue
			}
			if !hasName {
				name, hasName = titleCase(strings.TrimPrefix(key, p.prefix)), true
			}
			if !hasWindow {
				window, hasWindow = p.seconds, true
			}
			break
		}
	}
	if !hasName {
		name = titleCase(key)
	}
	if !hasWindow {
		window = 7 * 24 * 3600
	}

	return parse.Category{
		Key:           key,
		Name:          name,
		Utilization:   utilization,
		ResetsAt:      resetsAt,
		WindowSeconds: window,
	}, true
}

// parseClaude parses the raw Claude usage API response into categories and
// optional extra usage.
func parseClaude(data map[string]any) ([]parse.Category, *parse.ExtraUsage) {
	var categories []parse.Category
	seen := make(map[string]bool)

	add := func(key string) {
		entry, ok := data[key].(map[string]any)
		if !ok {
			return
		}
		if category, ok := claudeCategory(key, entry); ok {
			categories = append(categories, category)
		}
	}

	// First pass: emit known keys in preferred order.
	for _, key := range claudeCategoryOrder {
		if _, ok := data[key]; !ok {
			continue
		}
		seen[key] = true
		add(key)
	}

	// Second pass: everything else the API publishes, sorted. Ranging a Go map
	// is randomised, which would shuffle the bars between refreshes as soon as
	// more than one unmapped meter is live.
	rest := make([]string, 0, len(data))
	for key := range data {
		if seen[key] || key == "extra_usage" {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)
	for _, key := range rest {
		add(key)
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
