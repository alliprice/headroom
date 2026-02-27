package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileProviderValid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	data := `{"claudeAiOauth":{"accessToken":"sk-file-token"}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	p := fileProvider{}
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
	p := fileProvider{}
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
	p := fileProvider{}
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
	p := fileProvider{}
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
	p := fileProvider{}
	tok, err := p.getToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "sk-override-token" {
		t.Fatalf("got %q, want %q", tok, "sk-override-token")
	}
}
