package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadTestConfig(t *testing.T, contents string) (config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return loadConfig(path)
}

func assertProviderDurations(t *testing.T, cfg config, id string, wantInterval, wantMaxAge time.Duration) {
	t.Helper()
	pc, ok := cfg.Providers[id]
	if !ok {
		t.Fatalf("missing provider %s", id)
	}
	interval, maxAge, err := pc.durations()
	if err != nil {
		t.Fatalf("provider %s durations: %v", id, err)
	}
	if interval != wantInterval || maxAge != wantMaxAge {
		t.Fatalf("provider %s: interval/max_age = %s/%s, want %s/%s", id, interval, maxAge, wantInterval, wantMaxAge)
	}
}

// defaultDurations is each provider's default interval and max_age.
var defaultDurations = map[string][2]time.Duration{
	"codex":       {10 * time.Minute, 35 * time.Minute},
	"claude":      {20 * time.Minute, 75 * time.Minute},
	"antigravity": {20 * time.Minute, 40 * time.Minute},
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Listen != ":9101" {
		t.Fatalf("listen = %q, want :9101", cfg.Listen)
	}
	for id, d := range defaultDurations {
		assertProviderDurations(t, cfg, id, d[0], d[1])
	}
	if cfg.CLIProxy.URL != "" || cfg.CLIProxy.KeyService != "cliproxyapi-management" || cfg.CLIProxy.relistInterval() != 5*time.Minute || cfg.CLIProxy.maxAge() != 6*time.Hour {
		t.Fatalf("cliproxy defaults = %#v", cfg.CLIProxy)
	}
}

func TestLoadConfigDefaultsAndOverrides(t *testing.T) {
	for _, tt := range []struct {
		name     string
		yaml     string
		provider string
		interval time.Duration
		maxAge   time.Duration
	}{
		{"empty file", "", "codex", 10 * time.Minute, 35 * time.Minute},
		{"empty document", "{}", "codex", 10 * time.Minute, 35 * time.Minute},
		{"empty providers", "providers: {}", "codex", 10 * time.Minute, 35 * time.Minute},
		{"null providers", "providers: null", "codex", 10 * time.Minute, 35 * time.Minute},
		{"empty Codex entry", "providers:\n  codex: {}", "codex", 10 * time.Minute, 35 * time.Minute},
		{"null Codex entry", "providers:\n  codex: null", "codex", 10 * time.Minute, 35 * time.Minute},
		{"Codex max_age only", "providers:\n  codex:\n    max_age: 45m", "codex", 10 * time.Minute, 45 * time.Minute},
		{"Codex interval only", "providers:\n  codex:\n    interval: 12m", "codex", 12 * time.Minute, 35 * time.Minute},
		{"Codex full override", "providers:\n  codex:\n    interval: 8m\n    max_age: 30m", "codex", 8 * time.Minute, 30 * time.Minute},
		{"shorter age than interval", "providers:\n  codex:\n    interval: 10m\n    max_age: 1m", "codex", 10 * time.Minute, time.Minute},
		{"Claude empty entry", "providers:\n  claude: {}", "claude", 20 * time.Minute, 75 * time.Minute},
		{"Claude max_age only", "providers:\n  claude:\n    max_age: 55m", "claude", 20 * time.Minute, 55 * time.Minute},
		{"Claude interval only", "providers:\n  claude:\n    interval: 7m", "claude", 7 * time.Minute, 75 * time.Minute},
		{"antigravity interval fallback", "providers:\n  antigravity:\n    interval: 15m", "antigravity", 15 * time.Minute, 30 * time.Minute},
		{"other full override", "providers:\n  antigravity:\n    interval: 15m\n    max_age: 50m", "antigravity", 15 * time.Minute, 50 * time.Minute},
		{"unknown empty entry", "providers:\n  custom: {}", "custom", 20 * time.Minute, 40 * time.Minute},
		{"unknown interval fallback", "providers:\n  custom:\n    interval: 3m", "custom", 3 * time.Minute, 6 * time.Minute},
		{"unknown max_age only", "providers:\n  custom:\n    max_age: 55m", "custom", 20 * time.Minute, 55 * time.Minute},
		{"null durations retain defaults", "providers:\n  codex:\n    interval: null\n    max_age: null", "codex", 10 * time.Minute, 35 * time.Minute},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadTestConfig(t, tt.yaml)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Listen != ":9101" {
				t.Fatalf("listen = %q, want :9101", cfg.Listen)
			}
			assertProviderDurations(t, cfg, tt.provider, tt.interval, tt.maxAge)
			for id, d := range defaultDurations {
				if id == tt.provider {
					continue
				}
				assertProviderDurations(t, cfg, id, d[0], d[1])
			}
		})
	}
}

func TestLoadConfigListenAndIndependentDefaults(t *testing.T) {
	cfg, err := loadTestConfig(t, "listen: ':9999'\nproviders:\n  codex:\n    max_age: 40m\n  claude:\n    interval: 5m\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9999" {
		t.Fatalf("listen = %q, want :9999", cfg.Listen)
	}
	assertProviderDurations(t, cfg, "codex", 10*time.Minute, 40*time.Minute)
	assertProviderDurations(t, cfg, "claude", 5*time.Minute, 75*time.Minute)
	assertProviderDurations(t, cfg, "antigravity", 20*time.Minute, 40*time.Minute)
	// A previous load must not mutate the defaults used for the next load.
	assertProviderDurations(t, defaultConfig(), "codex", 10*time.Minute, 35*time.Minute)
}

func TestLoadConfigRejectsInvalidDurations(t *testing.T) {
	for _, field := range []string{"interval", "max_age"} {
		for _, value := range []string{"0", "0s", "-1m", "nonsense", "", "999999999999999999999h"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				contents := "providers:\n  codex:\n    " + field + ": '" + value + "'\n"
				_, err := loadTestConfig(t, contents)
				if err == nil || !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), field) {
					t.Fatalf("loadConfig error = %v, want provider and %s validation error", err, field)
				}
			})
		}
	}
	// Claude has a default max_age, so only providers without one derive it.
	for _, id := range []string{"antigravity", "custom"} {
		t.Run(id+"/overflow fallback age", func(t *testing.T) {
			_, err := loadTestConfig(t, "providers:\n  "+id+":\n    interval: 2000000h")
			if err == nil || !strings.Contains(err.Error(), "overflows max_age") {
				t.Fatalf("loadConfig error = %v, want fallback max_age overflow", err)
			}
		})
	}
}

func TestLoadConfigErrors(t *testing.T) {
	if _, err := loadTestConfig(t, "providers: ["); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("malformed YAML error = %v", err)
	}
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if !os.IsNotExist(err) {
		t.Fatalf("missing file error = %v, want os.IsNotExist", err)
	}
	assertProviderDurations(t, cfg, "codex", 10*time.Minute, 35*time.Minute)
}

func TestLoadConfigCLIProxy(t *testing.T) {
	cfg, err := loadTestConfig(t, "cliproxy:\n  url: http://127.0.0.1:8317\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CLIProxy.URL != "http://127.0.0.1:8317" || cfg.CLIProxy.KeyService != "cliproxyapi-management" ||
		cfg.CLIProxy.relistInterval() != 5*time.Minute || cfg.CLIProxy.maxAge() != 6*time.Hour {
		t.Fatalf("cliproxy = %#v", cfg.CLIProxy)
	}

	cfg, err = loadTestConfig(t, "cliproxy:\n  url: http://proxy:1\n  key_service: other\n  relist: 90s\n  max_age: 2h\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CLIProxy.KeyService != "other" || cfg.CLIProxy.relistInterval() != 90*time.Second || cfg.CLIProxy.maxAge() != 2*time.Hour {
		t.Fatalf("cliproxy = %#v", cfg.CLIProxy)
	}
	// The remote max_age is the proxy's own; provider timings stay put.
	assertProviderDurations(t, cfg, "claude", 20*time.Minute, 75*time.Minute)

	for _, yaml := range []string{"", "cliproxy: null", "cliproxy: {}", "cliproxy:\n  url: ''"} {
		cfg, err := loadTestConfig(t, yaml)
		if err != nil {
			t.Fatalf("%q: %v", yaml, err)
		}
		if cfg.CLIProxy.URL != "" {
			t.Fatalf("%q: cliproxy enabled with url %q", yaml, cfg.CLIProxy.URL)
		}
	}
}

func TestLoadConfigRejectsBadCLIProxy(t *testing.T) {
	for _, tt := range []struct{ name, yaml, want string }{
		{"zero relist", "cliproxy:\n  relist: 0s", "relist"},
		{"bad relist", "cliproxy:\n  relist: soon", "relist"},
		{"zero max_age", "cliproxy:\n  max_age: 0s", "max_age"},
		{"bad max_age", "cliproxy:\n  max_age: forever", "max_age"},
		{"empty max_age", "cliproxy:\n  max_age: ''", "max_age"},
		{"no scheme", "cliproxy:\n  url: 127.0.0.1:8317", "url"},
		{"no host", "cliproxy:\n  url: 'http://'", "url"},
		{"empty key service", "cliproxy:\n  url: http://127.0.0.1:8317\n  key_service: ''", "key_service"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadTestConfig(t, tt.yaml)
			if err == nil || !strings.Contains(err.Error(), "cliproxy") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("loadConfig error = %v, want a cliproxy %s error", err, tt.want)
			}
		})
	}
}

func TestUnknownProviders(t *testing.T) {
	cfg, err := loadTestConfig(t, "providers:\n  claude: {}\n  zeta: {}\n  alibaba:\n    interval: 5m\n")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.unknownProviders()
	if len(got) != 2 || got[0] != "alibaba" || got[1] != "zeta" {
		t.Fatalf("unknownProviders = %v, want [alibaba zeta]", got)
	}
	if len(defaultConfig().unknownProviders()) != 0 {
		t.Fatal("defaults name an unknown provider")
	}
}

func TestProviderConfigFallback(t *testing.T) {
	interval, maxAge, err := defaultConfig().providerConfig("unlisted").durations()
	if err != nil || interval != 20*time.Minute || maxAge != 40*time.Minute {
		t.Fatalf("fallback = %s/%s, %v; want 20m/40m", interval, maxAge, err)
	}
}

func TestBuildRevisionPrefersLinkerValue(t *testing.T) {
	old := revision
	t.Cleanup(func() { revision = old })
	revision = "abc123"
	if got := buildRevision(); got != "abc123" {
		t.Fatalf("buildRevision = %q, want the -X value", got)
	}
	revision = ""
	if got := buildRevision(); got == "" {
		t.Fatal("buildRevision is empty without a -X value")
	}
}
