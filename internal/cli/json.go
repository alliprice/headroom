package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

// RunJSON fetches usage data from all providers and writes a JSON
// budget-headroom summary to stdout.
func RunJSON() error {
	hr, err := fetchHeadroom()
	if err != nil {
		return err
	}

	type categoryEntry struct {
		Name        string  `json:"name"`
		UsagePct    float64 `json:"usage_pct"`
		GlidePct    float64 `json:"glide_pct"`
		HeadroomPct float64 `json:"headroom_pct"`
		Status      string  `json:"status"`
		Resets      string  `json:"resets"`
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
		Categories: make([]categoryEntry, 0, len(hr.Categories)),
	}

	for _, cat := range hr.Categories {
		out.Categories = append(out.Categories, categoryEntry{
			Name:        cat.Name,
			UsagePct:    round1(cat.UsagePct),
			GlidePct:    round1(cat.GlidePct),
			HeadroomPct: round1(cat.HeadroomPct),
			Status:      cat.Status,
			Resets:      cat.Resets,
		})
	}

	if hr.Extra != nil {
		out.ExtraUsage = &extraEntry{
			UsagePct:     round1(hr.Extra.UsagePct),
			UsedDollars:  round2(hr.Extra.UsedDollars),
			LimitDollars: round2(hr.Extra.LimitDollars),
			Status:       hr.Extra.Status,
		}
	}

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("json encoding failed: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s\n", encoded)
	return nil
}
