package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alliprice/headroom/internal/cliproxy"
	"github.com/alliprice/headroom/internal/provider"
)

// claudeProfileFor returns the resolved profile whose cleaned dir matches,
// for assertions.
func claudeProfileFor(cfg config, dir string) (claudeProfile, bool) {
	want := provider.ClaudeCleanConfigDir(dir)
	for _, p := range cfg.ClaudeProfiles {
		if p.ConfigDir == want {
			return p, true
		}
	}
	return claudeProfile{}, false
}

func TestDefaultConfigHasOneChainProfile(t *testing.T) {
	cfg := defaultConfig()
	if len(cfg.ClaudeProfiles) != 1 {
		t.Fatalf("got %d profiles, want 1", len(cfg.ClaudeProfiles))
	}
	p := cfg.ClaudeProfiles[0]
	if !p.Chain || p.Refresh {
		t.Fatalf("implicit profile = %#v, want a chain profile that is not refreshed", p)
	}
}

func TestImplicitProfileFollowsConfigDirEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	p := implicitClaudeProfile()
	if p.ConfigDir != provider.ClaudeCleanConfigDir(dir) || !p.Chain {
		t.Fatalf("implicit profile = %#v, want the env dir as a chain profile", p)
	}
}

func TestLoadConfigClaudeProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultDir := filepath.Join(home, ".claude")
	alliDir := filepath.Join(home, ".claude-alli")
	for _, d := range []string{defaultDir, alliDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	yaml := "claude_profiles:\n" +
		"  - config_dir: " + defaultDir + "\n" +
		"  - config_dir: " + alliDir + "\n" +
		"    refresh: true\n"
	cfg, err := loadTestConfig(t, yaml)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ClaudeProfiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(cfg.ClaudeProfiles))
	}
	def, ok := claudeProfileFor(cfg, defaultDir)
	if !ok || !def.Chain || def.Refresh {
		t.Fatalf("default-location profile = %#v, want a chain profile, not refreshed", def)
	}
	alli, ok := claudeProfileFor(cfg, alliDir)
	if !ok || alli.Chain || !alli.Refresh {
		t.Fatalf("alli profile = %#v, want a refreshing keychain profile", alli)
	}
}

func TestLoadConfigClaudeProfilesTildeAndTrailingSlash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".claude-alli"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadTestConfig(t, "claude_profiles:\n  - config_dir: ~/.claude-alli/\n    refresh: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := claudeProfileFor(cfg, filepath.Join(home, ".claude-alli")); !ok {
		t.Fatalf("~ and trailing slash did not resolve to %s: %#v", filepath.Join(home, ".claude-alli"), cfg.ClaudeProfiles)
	}
}

func TestLoadConfigClaudeProfilesErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	existing := filepath.Join(home, ".claude-alli")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	defaultDir := filepath.Join(home, ".claude")
	if err := os.Mkdir(defaultDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct{ name, yaml, want string }{
		{"empty config_dir", "claude_profiles:\n  - config_dir: ''\n", "must not be empty"},
		{"missing config_dir key", "claude_profiles:\n  - refresh: true\n", "must not be empty"},
		{"missing dir", "claude_profiles:\n  - config_dir: " + filepath.Join(home, "nope") + "\n", "not a directory"},
		{"duplicate", "claude_profiles:\n  - config_dir: " + existing + "\n  - config_dir: " + existing + "\n", "duplicate"},
		{"refresh on default", "claude_profiles:\n  - config_dir: " + defaultDir + "\n    refresh: true\n", "default profile"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadTestConfig(t, tt.yaml)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestClaudeProviderForChain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	// The default profile's identity file is ~/.claude.json in the home root.
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"dprice@gmail.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	prov, identity := claudeProviderFor(claudeProfile{ConfigDir: filepath.Join(home, ".claude"), Chain: true})
	if prov.ID != "claude" {
		t.Fatalf("provider id = %q", prov.ID)
	}
	if got := identity(); got != "dprice@gmail.com" {
		t.Fatalf("chain identity = %q, want the env-aware home login", got)
	}
}

func TestClaudeProviderForKeychainProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	alliDir := filepath.Join(home, ".claude-alli")
	if err := os.Mkdir(alliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeClaudeIdentity(t, alliDir, "alli@sibilance.io")

	_, identity := claudeProviderFor(claudeProfile{ConfigDir: provider.ClaudeCleanConfigDir(alliDir), Refresh: true})
	if got := identity(); got != "alli@sibilance.io" {
		t.Fatalf("profile identity = %q, want the profile's own login", got)
	}
}

func writeClaudeIdentity(t *testing.T, dir, email string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"oauthAccount":{"emailAddress":"` + email + `"}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A local profile for an account the proxy also holds suppresses the remote
// header state for that account, through planRemote's local-login check.
func TestLocalClaudeProfileSuppressesRemote(t *testing.T) {
	h := newRemoteHarness(t)
	h.mc.addLocal(provider.NewClaude(provider.ClaudeSource{}), func() string { return "alli@sibilance.io" }, 20*time.Minute, 75*time.Minute)

	h.lister.set([]cliproxy.Credential{
		withQuota(claudeCred("claude-alli", "alli@sibilance.io"), time.Now(), "0.3", "0.08"),
		withQuota(claudeCred("claude-other", "other@example.com"), time.Now(), "0.1", "0.2"),
	}, nil)
	h.reconcile(t)

	if got := h.remoteNames(); strings.Join(got, ",") != "claude-other" {
		t.Fatalf("remote = %v, want only other (alli is local)", got)
	}
}

// -refresh-profile refreshes exactly the named profile, writing the item back
// through the real keychain-source path with an injected endpoint.
func TestRunRefreshProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	alliDir := filepath.Join(home, ".claude-alli")
	writeClaudeIdentity(t, alliDir, "alli@sibilance.io")

	cfg := defaultConfig()
	cfg.ClaudeProfiles = []claudeProfile{
		{ConfigDir: provider.ClaudeCleanConfigDir(alliDir), Refresh: true},
		{ConfigDir: provider.ClaudeCleanConfigDir(filepath.Join(home, ".claude")), Chain: true},
	}

	// A profile that is not configured, or not refreshing, is refused.
	if err := runRefreshProfile(cfg, filepath.Join(home, ".claude")); err == nil || !strings.Contains(err.Error(), "refresh: true") {
		t.Fatalf("refreshing the chain profile err = %v", err)
	}
	if err := runRefreshProfile(cfg, filepath.Join(home, "nope")); err == nil || !strings.Contains(err.Error(), "no configured") {
		t.Fatalf("unknown profile err = %v", err)
	}
}
