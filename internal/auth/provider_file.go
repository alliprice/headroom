package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type fileProvider struct{}

func (fileProvider) name() string { return "file" }

func (fileProvider) getToken() (string, error) {
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
