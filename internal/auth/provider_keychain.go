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

type keychainProvider struct{}

func (keychainProvider) name() string { return "keychain" }

func (keychainProvider) getToken() (string, error) {
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
