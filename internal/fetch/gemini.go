package fetch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

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

// GeminiCredsPath returns the path to the Gemini CLI's OAuth credentials file.
func GeminiCredsPath() string {
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
	data, err := os.ReadFile(GeminiCredsPath())
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
	if err := os.WriteFile(GeminiCredsPath(), data, 0600); err != nil {
		return fmt.Errorf("Gemini creds write error: %w", err)
	}
	return nil
}

// geminiAPIPost makes an authenticated POST to the Gemini internal API.
func geminiAPIPost(token, endpoint string, payload any) (map[string]any, error) {
	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 10 * time.Second}
	url := "https://cloudcode-pa.googleapis.com/v1internal:" + endpoint
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
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

// FetchGemini retrieves quota data from the Gemini internal API.
// Returns the raw retrieveUserQuota response as map[string]any.
func FetchGemini() (map[string]any, error) {
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
