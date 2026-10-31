package provider

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os/exec"
	"sort"
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
	Demo:        demoCodex,
}

func probeCodex() bool {
	data, _ := fetchCodexRPC()
	return data != nil
}

func fetchCodex() (*FetchResult, bool, error) {
	data, err := fetchCodexRPC()
	if err != nil {
		return nil, false, fmt.Errorf("%s", humanizeError("Codex", err))
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

	// Send initialize request and read its response. The app-server may emit
	// notifications at any point, so match responses by JSON-RPC id instead of
	// assuming the next line belongs to the request we just sent.
	if _, err := fmt.Fprintf(stdin, "%s\n", initMsg); err != nil {
		return nil, fmt.Errorf("Codex error: %w", err)
	}
	if _, err := scanCodexRPCResponse(scanner, "1", 10*time.Second); err != nil {
		return nil, fmt.Errorf("No init response from codex app-server")
	}

	// Send rateLimits request and read the matching response with a timeout.
	if _, err := fmt.Fprintf(stdin, "%s\n", limitsMsg); err != nil {
		return nil, fmt.Errorf("Codex error: %w", err)
	}
	data, err := scanCodexRPCResponse(scanner, "2", 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Codex app-server timed out on rateLimits")
	}
	return data, nil
}

// scanCodexRPCResponse skips app-server notifications and unrelated responses
// until it finds the JSON-RPC response with expectedID.
func scanCodexRPCResponse(scanner *bufio.Scanner, expectedID string, timeout time.Duration) (map[string]any, error) {
	type scanResult struct {
		data map[string]any
		err  error
	}

	ch := make(chan scanResult, 1)
	go func() {
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var data map[string]any
			if err := json.Unmarshal([]byte(line), &data); err != nil {
				continue
			}
			if fmt.Sprint(data["id"]) != expectedID {
				continue
			}
			ch <- scanResult{data: data}
			return
		}

		err := scanner.Err()
		if err == nil {
			err = errors.New("Codex app-server closed before sending a response")
		}
		ch <- scanResult{err: err}
	}()

	select {
	case result := <-ch:
		return result.data, result.err
	case <-time.After(timeout):
		return nil, errors.New("Codex app-server response timed out")
	}
}

// parseCodex parses the raw Codex usage API response into categories.
func parseCodex(data map[string]any) []parse.Category {
	var categories []parse.Category

	result, _ := data["result"].(map[string]any)
	rateLimits, _ := result["rateLimits"].(map[string]any)
	categories = append(categories, parseCodexLimit(rateLimits, "codex", "")...)

	// Newer app-server versions expose additional model-specific limits here.
	// The top-level rateLimits object duplicates the ordinary "codex" entry, so
	// skip that id and add only genuinely separate limits.
	limitsByID, _ := result["rateLimitsByLimitId"].(map[string]any)
	mainLimitID, _ := rateLimits["limitId"].(string)
	limitIDs := make([]string, 0, len(limitsByID))
	for limitID := range limitsByID {
		if limitID != mainLimitID {
			limitIDs = append(limitIDs, limitID)
		}
	}
	sort.Strings(limitIDs)

	for _, limitID := range limitIDs {
		limit, ok := limitsByID[limitID].(map[string]any)
		if !ok {
			continue
		}
		limitName, _ := limit["limitName"].(string)
		if limitName == "" {
			limitName = limitID
		}
		categories = append(categories, parseCodexLimit(limit, limitID, limitName)...)
	}

	return categories
}

func parseCodexLimit(rateLimits map[string]any, keyPrefix, namePrefix string) []parse.Category {
	type mapping struct {
		rpcKey         string
		defaultName    string
		defaultWinMins int
	}
	mappings := []mapping{
		{rpcKey: "primary", defaultName: "Session", defaultWinMins: 300},
		{rpcKey: "secondary", defaultName: "Weekly", defaultWinMins: 10080},
	}

	var categories []parse.Category
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
		windowName := codexWindowName(windowMins, m.defaultName)
		categoryName := windowName
		if namePrefix != "" {
			categoryName = namePrefix + " " + windowName
		}

		var resetsISO string
		if resetsUnix, ok := entry["resetsAt"].(float64); ok && resetsUnix != 0 {
			t := time.Unix(int64(resetsUnix), 0).UTC()
			resetsISO = t.Format(time.RFC3339)
		}

		utilization, _ := parse.AsFloat64(entry["usedPercent"])

		categories = append(categories, parse.Category{
			Key:           keyPrefix + "_" + m.rpcKey,
			Name:          categoryName,
			Utilization:   utilization,
			ResetsAt:      resetsISO,
			WindowSeconds: windowMins * 60,
		})
	}

	return categories
}

func codexWindowName(windowMins int, fallback string) string {
	switch windowMins {
	case 300:
		return "Session"
	case 10080:
		return "Weekly"
	default:
		return fallback
	}
}

func demoCodex() *FetchResult {
	now := time.Now().UTC()
	return &FetchResult{
		Categories: []parse.Category{
			{
				Key:           "codex_primary",
				Name:          "Session",
				Utilization:   20 + rand.Float64()*40,
				ResetsAt:      now.Add(time.Duration(1+rand.Intn(4)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: 300 * 60,
			},
			{
				Key:           "codex_secondary",
				Name:          "Weekly",
				Utilization:   40 + rand.Float64()*35,
				ResetsAt:      now.Add(time.Duration(24+rand.Intn(120)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: 10080 * 60,
			},
		},
	}
}
