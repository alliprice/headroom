package provider

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"
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

// expiry tests

func TestTokenAtUnexpired(t *testing.T) {
	var creds claudeCredentials
	creds.ClaudeAiOauth.AccessToken = "sk-live"
	creds.ClaudeAiOauth.ExpiresAt = time.Now().Add(time.Hour).UnixMilli()

	tok, err := creds.tokenAt(time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "sk-live" {
		t.Fatalf("got %q, want %q", tok, "sk-live")
	}
}

func TestTokenAtExpired(t *testing.T) {
	var creds claudeCredentials
	creds.ClaudeAiOauth.AccessToken = "sk-stale"
	creds.ClaudeAiOauth.ExpiresAt = time.Now().Add(-time.Hour).UnixMilli()

	_, err := creds.tokenAt(time.Now())
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

// Older credentials files carry no expiresAt. Absent must not mean expired.
func TestTokenAtNoExpiryRecorded(t *testing.T) {
	var creds claudeCredentials
	creds.ClaudeAiOauth.AccessToken = "sk-no-expiry"

	tok, err := creds.tokenAt(time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "sk-no-expiry" {
		t.Fatalf("got %q, want %q", tok, "sk-no-expiry")
	}
}

func TestFileProviderExpiredToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	data := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"sk-stale","expiresAt":%d}}`,
		time.Now().Add(-time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	p := claudeFileProvider{}
	_, err := p.getToken()
	if err == nil {
		t.Fatal("expected error for expired token in credentials file")
	}
}

// The regression: a dead credentials file used to satisfy the chain and hide
// the keychain behind it, so every fetch 401ed until the file was removed.
func TestChainSkipsExpiredFileForKeychain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	data := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"sk-stale","expiresAt":%d}}`,
		time.Now().Add(-time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	chain := []claudeCredentialProvider{
		claudeFileProvider{},
		mockCredentialProvider{tok: "sk-keychain"},
	}
	tok, err := claudeGetAccessTokenFromChain(chain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "sk-keychain" {
		t.Fatalf("got %q, want %q", tok, "sk-keychain")
	}
}

// keychain account selection

// Several keychain items may share one service name, separated only by
// account, and an unqualified lookup returns an arbitrary one. Claude Code
// writes under the macOS username, so that has to be asked for by name or a
// stale duplicate filed under another account shadows the live credential.
func TestKeychainAccountsAskForUsernameFirst(t *testing.T) {
	accounts := claudeKeychainAccounts()
	if len(accounts) == 0 {
		t.Fatal("expected at least one account to try")
	}
	if last := accounts[len(accounts)-1]; last != "" {
		t.Errorf("last account = %q, want the unqualified lookup as fallback", last)
	}

	u, err := user.Current()
	if err != nil || u.Username == "" {
		t.Skip("no current user to compare against")
	}
	if accounts[0] != u.Username {
		t.Errorf("first account = %q, want the macOS username %q", accounts[0], u.Username)
	}
	if len(accounts) != 2 {
		t.Errorf("accounts = %v, want the username then the unqualified fallback", accounts)
	}
}
