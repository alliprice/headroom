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
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/alliprice/headroom/internal/parse"
)

const (
	claudeDefaultBaseURL = "https://api.anthropic.com"
	claudeUsagePath      = "/api/oauth/usage"
	claudeOAuthBeta      = "oauth-2025-04-20"
	claudeHTTPTimeout    = 10 * time.Second
)

// ClaudeSource says where a Claude provider gets its token and how it reaches
// the usage API. A zero field keeps the local behavior: the credential chain,
// a 10s client, and api.anthropic.com.
type ClaudeSource struct {
	Token   func() (string, error)
	Client  *http.Client
	BaseURL string
	// Refresh, when set, is called after the usage endpoint rejects the token
	// as expired. It returns a fresh token to retry the fetch once with. Only
	// a profile that owns its own token (not the shared credential chain) sets
	// this, so a refresh here never races Claude Code.
	Refresh func() (string, error)
}

// NewClaude builds a Claude provider over src.
func NewClaude(src ClaudeSource) Provider {
	return Provider{
		ID:          "claude",
		DisplayName: "Claude",
		CategoryIDs: []string{"five_hour", "seven_day", "seven_day_opus"},
		Probe:       nil, // always attempted
		Fetch:       func() (*FetchResult, bool, error) { return fetchClaude(src) },
		Demo:        demoClaude,
	}
}

// Claude is the provider for the local Claude Code login.
var Claude = NewClaude(ClaudeSource{})

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

// claudeKeychainAccounts lists the keychain accounts to try, in order.
//
// The keychain allows several items to share one service name, distinguished
// only by account, and an unqualified lookup returns an arbitrary one of them.
// Claude Code writes under the macOS username, so ask for that first; a stale
// duplicate filed under another account would otherwise shadow the live
// credential. The empty account keeps the old unqualified lookup as a
// fallback, for a machine where Claude Code used some other account name.
func claudeKeychainAccounts() []string {
	var accounts []string
	if u, err := user.Current(); err == nil && u.Username != "" {
		accounts = append(accounts, u.Username)
	}
	return append(accounts, "")
}

func (claudeKeychainProvider) getToken() (string, error) {
	var lastErr error
	for _, account := range claudeKeychainAccounts() {
		token, err := claudeKeychainToken(account)
		if err == nil {
			return token, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no keychain accounts to try")
	}
	return "", lastErr
}

// claudeKeychainToken reads one keychain item. An empty account leaves the
// choice to the keychain.
func claudeKeychainToken(account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{"find-generic-password", "-s", "Claude Code-credentials"}
	if account != "" {
		args = append(args, "-a", account)
	}
	args = append(args, "-w")

	cmd := exec.CommandContext(ctx, "security", args...)
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

// --- login identity ---

// ClaudeLoginEmail returns the email of the account Claude Code is logged in
// as, from oauthAccount.emailAddress in its .claude.json. It returns "" when
// that is unknown, and when CLAUDE_CODE_OAUTH_TOKEN is set, since the token in
// the environment then wins the credential chain and may belong to anyone.
func ClaudeLoginEmail() string {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		return ""
	}

	// Claude Code keeps .claude.json inside CLAUDE_CONFIG_DIR when that is set,
	// and in the home directory, beside ~/.claude, when it is not.
	var path string
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		path = filepath.Join(dir, ".claude.json")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, ".claude.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		OAuthAccount struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.OAuthAccount.EmailAddress)
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

func fetchClaude(src ClaudeSource) (*FetchResult, bool, error) {
	getToken := src.Token
	if getToken == nil {
		getToken = claudeGetAccessToken
	}
	client := src.Client
	if client == nil {
		client = &http.Client{Timeout: claudeHTTPTimeout}
	}
	baseURL := src.BaseURL
	if baseURL == "" {
		baseURL = claudeDefaultBaseURL
	}

	token, err := getToken()
	if err != nil {
		return nil, true, humanize("Claude", err)
	}

	data, err := fetchClaudeAPI(client, baseURL, token)
	if err != nil && isClaudeAuthError(err) && src.Refresh != nil {
		// The stored token was accepted locally but the endpoint rejected it,
		// so force a refresh and try once more with the new token.
		if refreshed, rerr := src.Refresh(); rerr != nil {
			return nil, true, humanize("Claude", rerr)
		} else {
			data, err = fetchClaudeAPI(client, baseURL, refreshed)
		}
	}
	if err != nil {
		return nil, isClaudeAuthError(err), humanize("Claude", err)
	}

	cats, extra := parseClaude(data)
	return &FetchResult{Categories: cats, Extra: extra, Credits: parseClaudeCredits(data)}, false, nil
}

// isClaudeAuthError reports whether err is the usage endpoint turning a token
// away. A refresh of a revoked or rotated token can fix it; a rate limit or a
// network fault cannot.
func isClaudeAuthError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "expired") || strings.Contains(msg, "authenticate")
}

// fetchClaudeAPI retrieves usage data from the Claude API using the provided
// OAuth token. Anthropic rate-limits this endpoint per token, so any failure
// that names a Retry-After comes back as a *RetryAfterError for the caller to
// honor.
func fetchClaudeAPI(client *http.Client, baseURL, token string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+claudeUsagePath, nil)
	if err != nil {
		return nil, fmt.Errorf("Network error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", claudeOAuthBeta)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var statusErr error
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			statusErr = fmt.Errorf("Token expired - run 'claude' to re-authenticate")
		} else {
			statusErr = &HTTPStatusError{Code: resp.StatusCode, Status: resp.Status}
		}
		if wait, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return nil, &RetryAfterError{Wait: wait, Err: statusErr}
		}
		return nil, statusErr
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

// claudeIsCredit reports whether a usage block is a dollar credit rather than
// a quota window. iguana_necktie, the Claude Code cloud-session grant, arrives
// shaped like a meter, with utilization and resets_at, but its resets_at is
// when the grant expires and its utilization is dollars spent, so pacing it
// against a window would be meaningless.
func claudeIsCredit(entry map[string]any) bool {
	v, ok := entry["limit_dollars"]
	return ok && v != nil
}

// claudeCreditNames names the credits Anthropic publishes under code names.
// An unmapped credit keeps its raw key rather than a title-cased guess, which
// would read like a real product name.
var claudeCreditNames = map[string]string{
	"iguana_necktie": "Cloud sessions",
}

// parseClaudeCredits returns every top-level block that carries a dollar
// limit, sorted by key.
func parseClaudeCredits(data map[string]any) []parse.Credit {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var credits []parse.Credit
	for _, key := range keys {
		entry, ok := data[key].(map[string]any)
		if !ok || !claudeIsCredit(entry) {
			continue
		}
		limit, _ := parse.AsFloat64(entry["limit_dollars"])
		used, _ := parse.AsFloat64(entry["used_dollars"])
		remaining, ok := parse.AsFloat64(entry["remaining_dollars"])
		if !ok {
			remaining = max(limit-used, 0)
		}
		expiresAt, _ := parse.AsString(entry["resets_at"])
		name, ok := claudeCreditNames[key]
		if !ok {
			name = key
		}
		credits = append(credits, parse.Credit{
			Key:              key,
			Name:             name,
			LimitDollars:     limit,
			UsedDollars:      used,
			RemainingDollars: remaining,
			ExpiresAt:        expiresAt,
		})
	}
	return credits
}

// claudeSlug turns a display name into a key fragment: lowercase, with each
// run of other characters collapsed to one underscore.
func claudeSlug(name string) string {
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			underscore = false
			continue
		}
		if !underscore && b.Len() > 0 {
			b.WriteByte('_')
			underscore = true
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}

// claudeScopedLimits reads the per-model weekly limits that the usage API
// publishes only in limits[], such as Fable. The session and weekly_all
// entries there repeat five_hour and seven_day and are left to them. A name
// that is already taken is skipped: the exporter labels series by name, and
// two bars with one name would collide.
func claudeScopedLimits(data map[string]any, takenNames, takenKeys map[string]bool) []parse.Category {
	list, _ := data["limits"].([]any)

	var categories []parse.Category
	for _, raw := range list {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := parse.AsString(entry["kind"]); kind != "weekly_scoped" {
			continue
		}
		scope, _ := entry["scope"].(map[string]any)
		model, _ := scope["model"].(map[string]any)
		name, _ := parse.AsString(model["display_name"])
		name = strings.TrimSpace(name)
		slug := claudeSlug(name)
		if slug == "" {
			continue
		}
		key := "weekly_scoped_" + slug
		if takenNames[strings.ToLower(name)] || takenKeys[key] {
			continue
		}
		takenNames[strings.ToLower(name)] = true
		takenKeys[key] = true

		// These are quota by declaration, not inferred from shape, so a window
		// that has not started (no reset time yet) still shows, unpaced.
		percent, _ := parse.AsFloat64(entry["percent"])
		resetsAt, _ := parse.AsString(entry["resets_at"])
		categories = append(categories, parse.Category{
			Key:           key,
			Name:          name,
			Utilization:   percent,
			ResetsAt:      resetsAt,
			WindowSeconds: 7 * 24 * 3600,
		})
	}
	return categories
}

// parseClaude parses the raw Claude usage API response into categories and
// optional extra usage.
func parseClaude(data map[string]any) ([]parse.Category, *parse.ExtraUsage) {
	var categories []parse.Category
	seen := make(map[string]bool)
	takenNames := make(map[string]bool)
	takenKeys := make(map[string]bool)

	add := func(key string) {
		entry, ok := data[key].(map[string]any)
		if !ok || claudeIsCredit(entry) {
			return
		}
		if category, ok := claudeCategory(key, entry); ok {
			categories = append(categories, category)
			takenNames[strings.ToLower(category.Name)] = true
			takenKeys[category.Key] = true
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

	categories = append(categories, claudeScopedLimits(data, takenNames, takenKeys)...)

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
