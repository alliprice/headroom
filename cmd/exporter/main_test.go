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

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Listen != ":9101" {
		t.Fatalf("listen = %q, want :9101", cfg.Listen)
	}
	assertProviderDurations(t, cfg, "codex", 10*time.Minute, 35*time.Minute)
	assertProviderDurations(t, cfg, "claude", 20*time.Minute, 40*time.Minute)
	assertProviderDurations(t, cfg, "antigravity", 20*time.Minute, 40*time.Minute)
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
		{"other empty entry", "providers:\n  claude: {}", "claude", 20 * time.Minute, 40 * time.Minute},
		{"other max_age only", "providers:\n  claude:\n    max_age: 55m", "claude", 20 * time.Minute, 55 * time.Minute},
		{"other interval fallback", "providers:\n  claude:\n    interval: 7m", "claude", 7 * time.Minute, 14 * time.Minute},
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
			for _, id := range []string{"codex", "claude", "antigravity"} {
				if id == tt.provider {
					continue
				}
				if id == "codex" {
					assertProviderDurations(t, cfg, id, 10*time.Minute, 35*time.Minute)
				} else {
					assertProviderDurations(t, cfg, id, 20*time.Minute, 40*time.Minute)
				}
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
	assertProviderDurations(t, cfg, "claude", 5*time.Minute, 10*time.Minute)
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
	for _, id := range []string{"claude", "antigravity", "custom"} {
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
