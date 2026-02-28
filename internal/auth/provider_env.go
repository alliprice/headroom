package auth

import (
	"fmt"
	"os"
)

type envProvider struct{}

func (envProvider) getToken() (string, error) {
	token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if token == "" {
		return "", fmt.Errorf("CLAUDE_CODE_OAUTH_TOKEN not set")
	}
	return token, nil
}
