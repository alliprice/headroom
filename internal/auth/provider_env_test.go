package auth

import "testing"

func TestEnvProviderSet(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-test-token")
	p := envProvider{}
	tok, err := p.getToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "sk-test-token" {
		t.Fatalf("got %q, want %q", tok, "sk-test-token")
	}
}

func TestEnvProviderUnset(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	p := envProvider{}
	_, err := p.getToken()
	if err == nil {
		t.Fatal("expected error for empty env var")
	}
}
