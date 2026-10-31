package provider

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
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

// Each app-server call is a real upstream rate-limit read, so the probe only
// looks for the binary. It used to run the whole RPC and throw the answer
// away, which doubled the calls against the account.
func probeCodex() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

const (
	codexInitTimeout = 10 * time.Second
	// The rateLimits read goes upstream to chatgpt.com, which can be slower
	// than the local initialize handshake.
	codexRateLimitsTimeout = 30 * time.Second
)

// CodexLoginEmail returns the email of the account the Codex CLI is logged in
// as, from the email claim of tokens.id_token in $CODEX_HOME/auth.json
// (default ~/.codex/auth.json). The JWT payload is read without verifying its
// signature: this names the account for a label, and trusts nothing to it.
// It returns "" when the file, the token or the claim is missing.
func CodexLoginEmail() string {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".codex")
	}

	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		return ""
	}
	var auth struct {
		Tokens struct {
			IDToken string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return ""
	}
	return jwtEmail(auth.Tokens.IDToken)
}

// jwtEmail decodes the payload segment of a JWT and returns its email claim.
func jwtEmail(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Email)
}

func fetchCodex() (*FetchResult, bool, error) {
	data, err := fetchCodexRPC()
	if err != nil {
		return nil, false, humanize("Codex", err)
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
	if _, err := scanCodexRPCResponse(scanner, "1", codexInitTimeout); err != nil {
		return nil, fmt.Errorf("No init response from codex app-server")
	}

	// Send rateLimits request and read the matching response with a timeout.
	if _, err := fmt.Fprintf(stdin, "%s\n", limitsMsg); err != nil {
		return nil, fmt.Errorf("Codex error: %w", err)
	}
	data, err := scanCodexRPCResponse(scanner, "2", codexRateLimitsTimeout)
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
