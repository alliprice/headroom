package provider

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alliprice/headroom/internal/parse"
)

// Codex is the provider for Codex API usage data.
var Codex = Provider{
	ID:          "codex",
	DisplayName: "Codex",
	CategoryIDs: []string{"codex_primary", "codex_secondary"},
	Probe:       probeCodex,
	Fetch:       fetchCodex,
}

func probeCodex() bool {
	data, _ := fetchCodexRPC()
	return data != nil
}

func fetchCodex() (*FetchResult, bool, error) {
	data, err := fetchCodexRPC()
	if err != nil {
		return nil, false, err
	}
	if data == nil {
		return &FetchResult{}, false, nil
	}
	cats := parseCodex(data)
	return &FetchResult{Categories: cats}, false, nil
}

// fetchCodexRPC retrieves usage data from Codex via the app-server JSON-RPC
// interface. Returns (nil, nil) if the codex binary is not installed.
func fetchCodexRPC() (map[string]any, error) {
	initMsg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "initialize",
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "usage-probe",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		},
	})

	limitsMsg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "2",
		"method":  "account/rateLimits/read",
		"params":  nil,
	})

	cmd := exec.Command("codex", "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("Codex error: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("Codex error: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		// exec.LookPath wraps ErrNotFound; check the error string as a fallback
		var exitErr *exec.Error
		if errors.As(err, &exitErr) && errors.Is(exitErr.Err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("Codex error: %w", err)
	}

	defer func() {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)

	// Send initialize request and read the response.
	if _, err := fmt.Fprintf(stdin, "%s\n", initMsg); err != nil {
		return nil, fmt.Errorf("Codex error: %w", err)
	}
	if !scanner.Scan() {
		return nil, fmt.Errorf("No init response from codex app-server")
	}
	if strings.TrimSpace(scanner.Text()) == "" {
		return nil, fmt.Errorf("No init response from codex app-server")
	}

	// Send rateLimits request and read the response with a timeout.
	if _, err := fmt.Fprintf(stdin, "%s\n", limitsMsg); err != nil {
		return nil, fmt.Errorf("Codex error: %w", err)
	}

	type scanResult struct {
		line string
		ok   bool
	}
	ch := make(chan scanResult, 1)
	go func() {
		ok := scanner.Scan()
		ch <- scanResult{line: scanner.Text(), ok: ok}
	}()

	var resultLine string
	select {
	case res := <-ch:
		if !res.ok || strings.TrimSpace(res.line) == "" {
			return nil, fmt.Errorf("Codex app-server timed out on rateLimits")
		}
		resultLine = res.line
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("Codex app-server timed out on rateLimits")
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(resultLine), &data); err != nil {
		return nil, fmt.Errorf("Failed to parse Codex response")
	}
	return data, nil
}

// parseCodex parses the raw Codex usage API response into categories.
func parseCodex(data map[string]any) []parse.Category {
	var categories []parse.Category

	result, _ := data["result"].(map[string]any)
	rateLimits, _ := result["rateLimits"].(map[string]any)

	type mapping struct {
		rpcKey         string
		catKey         string
		name           string
		defaultWinMins int
	}

	mappings := []mapping{
		{"primary", "codex_primary", "Session", 300},
		{"secondary", "codex_secondary", "Weekly", 10080},
	}

	for _, m := range mappings {
		raw, ok := rateLimits[m.rpcKey]
		if !ok {
			continue
		}
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		windowMins := m.defaultWinMins
		if wm, ok := entry["windowDurationMins"].(float64); ok {
			windowMins = int(wm)
		}

		var resetsISO string
		if resetsUnix, ok := entry["resetsAt"].(float64); ok && resetsUnix != 0 {
			t := time.Unix(int64(resetsUnix), 0).UTC()
			resetsISO = t.Format(time.RFC3339)
		}

		utilization, _ := parse.AsFloat64(entry["usedPercent"])

		categories = append(categories, parse.Category{
			Key:           m.catKey,
			Name:          m.name,
			Utilization:   utilization,
			ResetsAt:      resetsISO,
			WindowSeconds: windowMins * 60,
		})
	}

	return categories
}
