package provider

import (
	"bufio"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanCodexRPCResponseSkipsNotifications(t *testing.T) {
	input := strings.Join([]string{
		`{"method":"remoteControl/status/changed","params":{"status":"disabled"}}`,
		`{"id":"unrelated","result":{"ignored":true}}`,
		`{"id":"2","result":{"rateLimits":{"secondary":{"usedPercent":42}}}}`,
	}, "\n")

	got, err := scanCodexRPCResponse(bufio.NewScanner(strings.NewReader(input)), "2", time.Second)
	if err != nil {
		t.Fatalf("scanCodexRPCResponse returned error: %v", err)
	}
	if got["id"] != "2" {
		t.Fatalf("response id = %v, want 2", got["id"])
	}
}

func TestScanCodexRPCResponseReturnsErrorAtEOF(t *testing.T) {
	input := `{"method":"remoteControl/status/changed","params":{}}`

	_, err := scanCodexRPCResponse(bufio.NewScanner(strings.NewReader(input)), "2", time.Second)
	if err == nil {
		t.Fatal("scanCodexRPCResponse returned nil error, want EOF error")
	}
}

func TestFetchCodexRPCLive(t *testing.T) {
	if os.Getenv("HEADROOM_TEST_CODEX_LIVE") != "1" {
		t.Skip("set HEADROOM_TEST_CODEX_LIVE=1 to query the local Codex app-server")
	}

	data, err := fetchCodexRPC()
	if err != nil {
		t.Fatalf("fetchCodexRPC returned error: %v", err)
	}

	categories := parseCodex(data)
	if len(categories) == 0 {
		t.Fatalf("parseCodex returned no categories; response: %#v", data)
	}

	foundWeekly := false
	for _, category := range categories {
		if category.Name == "Weekly" {
			foundWeekly = true
		}
	}
	if !foundWeekly {
		t.Fatal("parseCodex did not return a Weekly category")
	}
}

func TestParseCodexUsesWindowDurationForModernPrimary(t *testing.T) {
	data := map[string]any{
		"result": map[string]any{
			"rateLimits": map[string]any{
				"limitId": "codex",
				"primary": map[string]any{
					"usedPercent":        float64(98),
					"windowDurationMins": float64(10080),
					"resetsAt":           float64(1789585795),
				},
				"secondary": nil,
			},
		},
	}

	categories := parseCodex(data)
	if len(categories) != 1 {
		t.Fatalf("parseCodex returned %d categories, want 1", len(categories))
	}
	if categories[0].Name != "Weekly" {
		t.Errorf("category name = %q, want Weekly", categories[0].Name)
	}
	if categories[0].WindowSeconds != 10080*60 {
		t.Errorf("window seconds = %d, want %d", categories[0].WindowSeconds, 10080*60)
	}
}

func TestParseCodexIncludesModelSpecificLimits(t *testing.T) {
	data := map[string]any{
		"result": map[string]any{
			"rateLimits": map[string]any{
				"limitId": "codex",
				"primary": map[string]any{"usedPercent": float64(50), "windowDurationMins": float64(10080)},
			},
			"rateLimitsByLimitId": map[string]any{
				"codex": map[string]any{
					"limitId": "codex",
					"primary": map[string]any{"usedPercent": float64(50), "windowDurationMins": float64(10080)},
				},
				"codex_bengalfox": map[string]any{
					"limitId":   "codex_bengalfox",
					"limitName": "GPT-5.3-Codex-Spark",
					"primary":   map[string]any{"usedPercent": float64(10), "windowDurationMins": float64(300)},
					"secondary": map[string]any{"usedPercent": float64(20), "windowDurationMins": float64(10080)},
				},
			},
		},
	}

	categories := parseCodex(data)
	if len(categories) != 3 {
		t.Fatalf("parseCodex returned %d categories, want 3", len(categories))
	}
	if categories[1].Name != "GPT-5.3-Codex-Spark Session" {
		t.Errorf("model-specific primary name = %q", categories[1].Name)
	}
	if categories[2].Name != "GPT-5.3-Codex-Spark Weekly" {
		t.Errorf("model-specific secondary name = %q", categories[2].Name)
	}
}

// The probe must not start the app-server: every run is an upstream
// rate-limit read, and the probe used to throw its answer away.
func TestProbeCodexDoesNotRunBinary(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\ntouch '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	if !probeCodex() {
		t.Fatal("probeCodex = false with codex on PATH")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("probeCodex ran the codex binary")
	}
}

func TestProbeCodexMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if probeCodex() {
		t.Fatal("probeCodex = true with no codex on PATH")
	}
}

func codexIDToken(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(payload)) + ".c2lnbmF0dXJl"
}

func writeCodexAuth(t *testing.T, dir, idToken string) {
	t.Helper()
	contents := `{"auth_mode":"chatgpt","tokens":{"id_token":"` + idToken + `","access_token":"at","refresh_token":"rt"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCodexLoginEmailFromCodexHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	writeCodexAuth(t, dir, codexIDToken(`{"email":"dprice@gmail.com","sub":"x"}`))
	if got := CodexLoginEmail(); got != "dprice@gmail.com" {
		t.Fatalf("CodexLoginEmail = %q", got)
	}
}

func TestCodexLoginEmailDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	writeCodexAuth(t, filepath.Join(home, ".codex"), codexIDToken(`{"email":"alli@sibilance.io"}`))
	if got := CodexLoginEmail(); got != "alli@sibilance.io" {
		t.Fatalf("CodexLoginEmail = %q", got)
	}
}

func TestCodexLoginEmailUnknown(t *testing.T) {
	for _, tt := range []struct {
		name    string
		idToken string // "-" means no auth.json
	}{
		{"missing file", "-"},
		{"no id token", ""},
		{"not a JWT", "opaque"},
		{"bad base64", "a.!!!.c"},
		{"no email claim", codexIDToken(`{"sub":"x"}`)},
		{"payload not JSON", codexIDToken(`nope`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CODEX_HOME", dir)
			if tt.idToken != "-" {
				writeCodexAuth(t, dir, tt.idToken)
			}
			if got := CodexLoginEmail(); got != "" {
				t.Fatalf("CodexLoginEmail = %q, want empty", got)
			}
		})
	}
}
