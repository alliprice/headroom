package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type keychainCreds struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// GetAccessToken retrieves the Claude access token from the macOS Keychain.
// It returns the token string and a nil error on success, or an empty string
// and a descriptive error on failure.
func GetAccessToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("keychain access timed out")
		}

		var notFound *exec.Error
		if errors.As(err, &notFound) && errors.Is(notFound.Err, exec.ErrNotFound) {
			return "", fmt.Errorf("'security' command not found (macOS only)")
		}

		// exec.ExitError means the command ran but exited non-zero.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("keychain error - run 'claude' to re-authenticate")
		}

		return "", fmt.Errorf("keychain error: %w", err)
	}

	var creds keychainCreds
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &creds); err != nil {
		return "", fmt.Errorf("invalid credentials - run 'claude' to re-authenticate")
	}

	token := creds.ClaudeAiOauth.AccessToken
	if token == "" {
		return "", fmt.Errorf("no token found - run 'claude' to authenticate")
	}

	return token, nil
}
