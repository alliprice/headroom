package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alliprice/headroom/internal/parse"
)

// Gemini is the provider for Gemini CLI usage data.
var Gemini = Provider{
	ID:          "gemini",
	DisplayName: "Gemini",
	CategoryIDs: nil, // dynamic - discovered at fetch time
	Probe:       probeGemini,
	Fetch:       fetchGemini,
	Demo:        demoGemini,
}

// Gemini CLI's public OAuth client credentials (embedded in the CLI source).
// These are "installed application" credentials - safe to include per
// https://developers.google.com/identity/protocols/oauth2/native-app
const (
	geminiClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	geminiClientSecret = "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"
	geminiTokenURL     = "https://oauth2.googleapis.com/token"
)

// geminiProjectID caches the project ID across fetch cycles within a session.
var geminiProjectID string

// geminiCredsPath returns the path to the Gemini CLI's OAuth credentials file.
func geminiCredsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "oauth_creds.json")
}

// geminiCreds represents the OAuth credentials stored by the Gemini CLI.
type geminiCreds struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiryDate   float64 `json:"expiry_date"` // milliseconds since epoch
}

// readGeminiCreds reads and returns the Gemini OAuth credentials.
func readGeminiCreds() (*geminiCreds, error) {
	data, err := os.ReadFile(geminiCredsPath())
	if err != nil {
		return nil, fmt.Errorf("Gemini creds not found: %w", err)
	}
	var creds geminiCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("Gemini creds invalid: %w", err)
	}
	return &creds, nil
}

// refreshGeminiToken refreshes the access token if it's expired or about to
// expire (within 60s). Writes the updated credentials back to disk.
func refreshGeminiToken(creds *geminiCreds) error {
	expiryTime := time.UnixMilli(int64(creds.ExpiryDate))
	if time.Now().Before(expiryTime.Add(-60 * time.Second)) {
		return nil // token still valid
	}

	form := url.Values{
		"client_id":     {geminiClientID},
		"client_secret": {geminiClientSecret},
		"refresh_token": {creds.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm(geminiTokenURL, form)
	if err != nil {
		return fmt.Errorf("Gemini token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Gemini token refresh: %s", resp.Status)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"` // seconds
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("Gemini token refresh parse error: %w", err)
	}

	creds.AccessToken = result.AccessToken
	creds.ExpiryDate = float64(time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).UnixMilli())

	// Write updated creds back to disk.
	data, _ := json.Marshal(creds)
	if err := os.WriteFile(geminiCredsPath(), data, 0600); err != nil {
		return fmt.Errorf("Gemini creds write error: %w", err)
	}
	return nil
}

// geminiAPIPost makes an authenticated POST to the Gemini internal API.
func geminiAPIPost(token, endpoint string, payload any) (map[string]any, error) {
	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 10 * time.Second}
	apiURL := "https://cloudcode-pa.googleapis.com/v1internal:" + endpoint
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Gemini API error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini API error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("Gemini token expired - 401/403")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini API error: %s", resp.Status)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("Gemini API parse error: %w", err)
	}
	return data, nil
}

func probeGemini() bool {
	_, err := os.Stat(geminiCredsPath())
	return err == nil
}

func fetchGemini() (*FetchResult, bool, error) {
	data, err := fetchGeminiAPI()
	if err != nil {
		isAuth := strings.Contains(err.Error(), "expired") ||
			strings.Contains(err.Error(), "authenticate") ||
			strings.Contains(err.Error(), "401") ||
			strings.Contains(err.Error(), "403")
		return nil, isAuth, err
	}
	cats := parseGemini(data)
	return &FetchResult{Categories: cats}, false, nil
}

// fetchGeminiAPI retrieves quota data from the Gemini internal API.
func fetchGeminiAPI() (map[string]any, error) {
	creds, err := readGeminiCreds()
	if err != nil {
		return nil, err
	}

	if err := refreshGeminiToken(creds); err != nil {
		return nil, err
	}

	// Get project ID (cached across calls within a session).
	if geminiProjectID == "" {
		payload := map[string]any{
			"metadata": map[string]any{
				"ideType":    "IDE_UNSPECIFIED",
				"platform":   "PLATFORM_UNSPECIFIED",
				"pluginType": "GEMINI",
			},
		}
		data, err := geminiAPIPost(creds.AccessToken, "loadCodeAssist", payload)
		if err != nil {
			return nil, err
		}
		pid, _ := data["cloudaicompanionProject"].(string)
		if pid == "" {
			return nil, fmt.Errorf("Gemini API: no project ID in loadCodeAssist response")
		}
		geminiProjectID = pid
	}

	// Fetch quota.
	payload := map[string]any{
		"project": geminiProjectID,
	}
	return geminiAPIPost(creds.AccessToken, "retrieveUserQuota", payload)
}

// gemini-specific display names

var geminiFamilyDisplayName = map[string]string{
	"flash":      "Flash",
	"flash-lite": "Flash Lite",
	"pro":        "Pro",
}

// geminiFamily extracts the model family from a Gemini model ID.
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

// parseGemini parses the raw Gemini retrieveUserQuota API response into
// categories. Buckets are grouped by model family (flash, pro) with the
// most-used value per family.
func parseGemini(data map[string]any) []parse.Category {
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

		remaining, hasRemaining := parse.AsFloat64(bucket["remainingFraction"])
		if !hasRemaining {
			continue
		}

		modelID, _ := parse.AsString(bucket["modelId"])

		// Skip _vertex routing variants - they mirror the base model quota.
		if strings.HasSuffix(modelID, "_vertex") {
			continue
		}

		// Skip untouched models.
		if remaining >= 1 {
			continue
		}

		family := geminiFamily(modelID)
		resetTime, _ := parse.AsString(bucket["resetTime"])

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
	var categories []parse.Category
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

		// Gemini quotas always reset on a 24-hour cycle.
		windowSeconds := 86400

		categories = append(categories, parse.Category{
			Key:           "gemini_" + family,
			Name:          name,
			Utilization:   utilization,
			ResetsAt:      fd.resetTime,
			WindowSeconds: windowSeconds,
		})
	}

	return categories
}

func demoGemini() *FetchResult {
	now := time.Now().UTC()
	return &FetchResult{
		Categories: []parse.Category{
			{
				Key:           "gemini_flash",
				Name:          "Flash",
				Utilization:   30 + rand.Float64()*40,
				ResetsAt:      now.Add(time.Duration(12+rand.Intn(12)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: 86400,
			},
		},
	}
}
