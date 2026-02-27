package auth

import (
	"fmt"
	"testing"
)

type mockProvider struct {
	n   string
	tok string
	err error
}

func (m mockProvider) name() string             { return m.n }
func (m mockProvider) getToken() (string, error) { return m.tok, m.err }

func TestChainFirstWins(t *testing.T) {
	chain := []credentialProvider{
		mockProvider{n: "a", tok: "token-a"},
		mockProvider{n: "b", tok: "token-b"},
	}
	tok, err := getTokenFromChain(chain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "token-a" {
		t.Fatalf("got %q, want %q", tok, "token-a")
	}
}

func TestChainSkipsFailures(t *testing.T) {
	chain := []credentialProvider{
		mockProvider{n: "fail", err: fmt.Errorf("nope")},
		mockProvider{n: "ok", tok: "token-ok"},
	}
	tok, err := getTokenFromChain(chain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "token-ok" {
		t.Fatalf("got %q, want %q", tok, "token-ok")
	}
}

func TestChainAllFail(t *testing.T) {
	chain := []credentialProvider{
		mockProvider{n: "a", err: fmt.Errorf("fail-a")},
		mockProvider{n: "b", err: fmt.Errorf("fail-b")},
	}
	_, err := getTokenFromChain(chain)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestChainEmpty(t *testing.T) {
	_, err := getTokenFromChain(nil)
	if err == nil {
		t.Fatal("expected error for empty chain")
	}
}
