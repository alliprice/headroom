package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// mock credential provider for chain tests

type mockCredentialProvider struct {
	tok string
	err error
}

func (m mockCredentialProvider) getToken() (string, error) { return m.tok, m.err }

func TestChainFirstWins(t *testing.T) {
	chain := []claudeCredentialProvider{
		mockCredentialProvider{tok: "token-a"},
		mockCredentialProvider{tok: "token-b"},
	}
	tok, err := claudeGetAccessTokenFromChain(chain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "token-a" {
		t.Fatalf("got %q, want %q", tok, "token-a")
	}
}

func TestChainSkipsFailures(t *testing.T) {
	chain := []claudeCredentialProvider{
		mockCredentialProvider{err: fmt.Errorf("nope")},
		mockCredentialProvider{tok: "token-ok"},
	}
	tok, err := claudeGetAccessTokenFromChain(chain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "token-ok" {
		t.Fatalf("got %q, want %q", tok, "token-ok")
	}
}

func TestChainAllFail(t *testing.T) {
	chain := []claudeCredentialProvider{
		mockCredentialProvider{err: fmt.Errorf("fail-a")},
		mockCredentialProvider{err: fmt.Errorf("fail-b")},
	}
	_, err := claudeGetAccessTokenFromChain(chain)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestChainEmpty(t *testing.T) {
	_, err := claudeGetAccessTokenFromChain(nil)
	if err == nil {
		t.Fatal("expected error for empty chain")
	}
}

// env provider tests

func TestEnvProviderSet(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-test-token")
	p := claudeEnvProvider{}
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
	p := claudeEnvProvider{}
	_, err := p.getToken()
	if err == nil {
		t.Fatal("expected error for empty env var")
	}
}

// file provider tests

func TestFileProviderValid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	data := `{"claudeAiOauth":{"accessToken":"sk-file-token"}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	p := claudeFileProvider{}
	tok, err := p.getToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "sk-file-token" {
		t.Fatalf("got %q, want %q", tok, "sk-file-token")
	}
}

func TestFileProviderMissing(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	p := claudeFileProvider{}
	_, err := p.getToken()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFileProviderInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	p := claudeFileProvider{}
	_, err := p.getToken()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFileProviderEmptyToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	data := `{"claudeAiOauth":{"accessToken":""}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	p := claudeFileProvider{}
	_, err := p.getToken()
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestFileProviderConfigDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	data := `{"claudeAiOauth":{"accessToken":"sk-override-token"}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	p := claudeFileProvider{}
	tok, err := p.getToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "sk-override-token" {
		t.Fatalf("got %q, want %q", tok, "sk-override-token")
	}
}
