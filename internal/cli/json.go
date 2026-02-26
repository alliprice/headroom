package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/alliprice/headroom/internal/auth"
	"github.com/alliprice/headroom/internal/fetch"
	"github.com/alliprice/headroom/internal/parse"
)

// round1 rounds f to one decimal place.
func round1(f float64) float64 {
	return math.Round(f*10) / 10
}

// round2 rounds f to two decimal places.
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// RunJSON fetches usage data from Claude (and optionally Codex) and writes a
// JSON budget-headroom summary to stdout.
func RunJSON() error {
	token, err := auth.GetAccessToken()
	if err != nil {
		return err
	}

	data, err := fetch.FetchClaude(token)
	if err != nil {
		return err
	}

	claudeCats, extraUsage := parse.ParseClaude(data)

	var codexCats []parse.Category
	codexData, codexErr := fetch.FetchCodex()
	if codexErr == nil && codexData != nil {
		codexCats = parse.ParseCodex(codexData)
	}

	allCats := CombineCategories(claudeCats, codexCats)

	type categoryEntry struct {
		Name       string  `json:"name"`
		UsagePct   float64 `json:"usage_pct"`
		GlidePct   float64 `json:"glide_pct"`
		HeadroomPct float64 `json:"headroom_pct"`
		Status     string  `json:"status"`
		Resets     string  `json:"resets"`
	}

	type extraEntry struct {
		UsagePct     float64 `json:"usage_pct"`
		UsedDollars  float64 `json:"used_dollars"`
		LimitDollars float64 `json:"limit_dollars"`
		Status       string  `json:"status"`
	}

	type output struct {
		Categories []categoryEntry `json:"categories"`
		ExtraUsage *extraEntry     `json:"extra_usage,omitempty"`
	}

	out := output{
		Categories: make([]categoryEntry, 0, len(allCats)),
	}

	for _, cat := range allCats {
		usagePct := cat.Utilization
		glidePct := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		resetStr := parse.FormatResetTime(cat.ResetsAt)
		headroom := glidePct - usagePct

		var status string
		switch {
		case usagePct >= 80:
			status = "conserve"
		case headroom >= 0:
			status = "plenty of room"
		default:
			status = "slow down"
		}

		out.Categories = append(out.Categories, categoryEntry{
			Name:        cat.Name,
			UsagePct:    round1(usagePct),
			GlidePct:    round1(glidePct),
			HeadroomPct: round1(headroom),
			Status:      status,
			Resets:      resetStr,
		})
	}

	if extraUsage != nil {
		usagePct := extraUsage.Utilization
		limitDollars := extraUsage.MonthlyLimit / 100
		usedDollars := extraUsage.UsedCredits / 100

		var status string
		if usagePct >= 80 {
			status = "warning"
		} else {
			status = "on track"
		}

		out.ExtraUsage = &extraEntry{
			UsagePct:     round1(usagePct),
			UsedDollars:  round2(usedDollars),
			LimitDollars: round2(limitDollars),
			Status:       status,
		}
	}

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("json encoding failed: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s\n", encoded)
	return nil
}
