package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alliprice/headroom/internal/parse"
)

const antigravityFetchTimeout = 90 * time.Second

// Antigravity is the provider for Google Antigravity quota, the successor to
// Gemini Code Assist for individuals.
var Antigravity = Provider{
	ID:          "antigravity",
	DisplayName: "Antigravity",
	CategoryIDs: []string{
		"antigravity_gemini_5h",
		"antigravity_gemini_weekly",
		"antigravity_3p_5h",
		"antigravity_3p_weekly",
	},
	Probe: probeAntigravity,
	Fetch: fetchAntigravity,
	Demo:  demoAntigravity,
}

// Quota comes from the CLI rather than cloudcode-pa.googleapis.com. The HTTP
// endpoint answers, but reports remainingFraction 1 for every bucket whatever
// the real usage, and reaching it at all needs a scraped OAuth client secret
// and a spoofed user agent. The CLI reports true numbers and owns its own
// token refresh.
var antigravityArgs = []string{"-p", "/usage", "--output-format", "json", "--print-timeout", "90s"}

// antigravityBucket is one quota window within a model group.
type antigravityBucket struct {
	ID                string  `json:"id"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         string  `json:"reset_time"`
}

// antigravityReport is the envelope printed by `agy -p /usage`.
type antigravityReport struct {
	Status  string `json:"status"`
	Command struct {
		Data struct {
			Groups []struct {
				Buckets []antigravityBucket `json:"buckets"`
			} `json:"groups"`
		} `json:"data"`
	} `json:"command"`
}

// antigravityCategory is the display identity for one known bucket. Buckets
// are matched by id so an unrecognised one is skipped rather than rendered
// under a raw id.
type antigravityCategory struct {
	key  string
	name string
}

var antigravityCategories = map[string]antigravityCategory{
	"gemini-5h":     {key: "antigravity_gemini_5h", name: "Gemini Session"},
	"gemini-weekly": {key: "antigravity_gemini_weekly", name: "Gemini Weekly"},
	"3p-5h":         {key: "antigravity_3p_5h", name: "Claude/GPT Session"},
	"3p-weekly":     {key: "antigravity_3p_weekly", name: "Claude/GPT Weekly"},
}

// antigravityCategoryOrder fixes bar order regardless of how the CLI groups
// them, so the two session bars sit above the two weekly bars.
var antigravityCategoryOrder = []string{"gemini-5h", "gemini-weekly", "3p-5h", "3p-weekly"}

// antigravityWindowSeconds maps the bucket's own window field, so a new group
// on an existing window needs no change here.
var antigravityWindowSeconds = map[string]int{
	"5h":     5 * 3600,
	"weekly": 7 * 24 * 3600,
}

// antigravityCommandPath locates the Antigravity CLI. The launchd agent that
// runs the exporter has a PATH without ~/.local/bin, which is where the
// installer puts agy, so LookPath alone is not enough.
func antigravityCommandPath() (string, error) {
	if path, err := exec.LookPath("agy"); err == nil {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".local", "bin", "agy")
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}

	return "", errors.New("Antigravity CLI not found")
}

func probeAntigravity() bool {
	_, err := antigravityCommandPath()
	return err == nil
}

func fetchAntigravity() (*FetchResult, bool, error) {
	command, err := antigravityCommandPath()
	if err != nil {
		return nil, false, humanize("Antigravity", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), antigravityFetchTimeout)
	defer cancel()

	// Run from a directory the CLI cannot mistake for a workspace: it resolves
	// a project from the working directory and would otherwise adopt whatever
	// repo headroom happens to be launched in.
	cmd := exec.CommandContext(ctx, command, antigravityArgs...)
	cmd.Dir = os.TempDir()

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, false, fmt.Errorf("Antigravity quota request timed out")
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		isAuth := antigravityAuthError(message)
		return nil, isAuth, humanize("Antigravity", errors.New(message))
	}

	categories, err := parseAntigravity(output)
	if err != nil {
		return nil, antigravityAuthError(err.Error()), fmt.Errorf("Antigravity quota response: %w", err)
	}
	return &FetchResult{Categories: categories}, false, nil
}

func antigravityAuthError(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "not logged in") ||
		strings.Contains(message, "login") ||
		strings.Contains(message, "auth") ||
		strings.Contains(message, "credential") ||
		strings.Contains(message, "token expired")
}

// parseAntigravity turns the CLI usage report into categories, one per known
// bucket, in a fixed display order.
func parseAntigravity(data []byte) ([]parse.Category, error) {
	var report antigravityReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}

	if report.Status != "" && report.Status != "SUCCESS" {
		return nil, fmt.Errorf("status %s", report.Status)
	}

	// Flatten the groups: bucket ids are unique across them.
	buckets := make(map[string]antigravityBucket)
	for _, group := range report.Command.Data.Groups {
		for _, bucket := range group.Buckets {
			if _, known := antigravityCategories[bucket.ID]; known {
				buckets[bucket.ID] = bucket
			}
		}
	}

	var categories []parse.Category
	for _, id := range antigravityCategoryOrder {
		bucket, ok := buckets[id]
		if !ok {
			continue
		}
		display := antigravityCategories[id]

		utilization := (1 - bucket.RemainingFraction) * 100
		if utilization < 0 {
			utilization = 0
		}
		if utilization > 100 {
			utilization = 100
		}

		// An untouched bucket has no window running yet, but the CLI still
		// reports a reset of now plus the window. Pacing against that reads as
		// a fresh window with zero used, so leave the reset out and the bar
		// unpaced until usage starts the clock.
		resetsAt := bucket.ResetTime
		if bucket.RemainingFraction >= 1 {
			resetsAt = ""
		}

		categories = append(categories, parse.Category{
			Key:           display.key,
			Name:          display.name,
			Utilization:   utilization,
			ResetsAt:      resetsAt,
			WindowSeconds: antigravityWindowSeconds[bucket.Window],
		})
	}

	return categories, nil
}

func demoAntigravity() *FetchResult {
	now := time.Now().UTC()
	fiveHour := now.Add(time.Duration(1+rand.Intn(4)) * time.Hour).Format(time.RFC3339)
	weekly := now.Add(time.Duration(2+rand.Intn(5)) * 24 * time.Hour).Format(time.RFC3339)

	return &FetchResult{
		Categories: []parse.Category{
			{
				Key:           "antigravity_gemini_5h",
				Name:          "Gemini Session",
				Utilization:   20 + rand.Float64()*40,
				ResetsAt:      fiveHour,
				WindowSeconds: 5 * 3600,
			},
			{
				Key:           "antigravity_gemini_weekly",
				Name:          "Gemini Weekly",
				Utilization:   20 + rand.Float64()*40,
				ResetsAt:      weekly,
				WindowSeconds: 7 * 24 * 3600,
			},
			{
				Key:           "antigravity_3p_5h",
				Name:          "Claude/GPT Session",
				Utilization:   20 + rand.Float64()*40,
				ResetsAt:      fiveHour,
				WindowSeconds: 5 * 3600,
			},
			{
				Key:           "antigravity_3p_weekly",
				Name:          "Claude/GPT Weekly",
				Utilization:   20 + rand.Float64()*40,
				ResetsAt:      weekly,
				WindowSeconds: 7 * 24 * 3600,
			},
		},
	}
}
