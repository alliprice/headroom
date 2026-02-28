package auth

import (
	"os"
	"path/filepath"
)

// credentialProvider retrieves an OAuth access token from a single source.
type credentialProvider interface {
	getToken() (string, error)
}

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
