package cli

import (
	"fmt"

	"github.com/alliprice/headroom/internal/auth"
	"github.com/alliprice/headroom/internal/fetch"
	"github.com/alliprice/headroom/internal/parse"
)

// CombineCategories merges Claude and Codex categories into display order.
// When both sources are present, names are prefixed ("Claude session", "Codex session",
// etc.) for clarity. The ordering is: claude core (five_hour, seven_day) + codex +
// claude extras (everything else).
func CombineCategories(claudeCats, codexCats []parse.Category) []parse.Category {
	if len(codexCats) > 0 {
		// Rename Claude categories to carry the "Claude" prefix.
		for i := range claudeCats {
			switch claudeCats[i].Key {
			case "five_hour":
				claudeCats[i].Name = "Claude session"
			case "seven_day":
				claudeCats[i].Name = "Claude weekly all"
			case "seven_day_opus":
				claudeCats[i].Name = "Claude weekly Opus"
			}
		}
		// Rename Codex categories to carry the "Codex" prefix.
		for i := range codexCats {
			switch codexCats[i].Key {
			case "codex_primary":
				codexCats[i].Name = "Codex session"
			case "codex_secondary":
				codexCats[i].Name = "Codex weekly"
			}
		}
	}

	coreKeys := map[string]bool{"five_hour": true, "seven_day": true}

	var core, extras []parse.Category
	for _, c := range claudeCats {
		if coreKeys[c.Key] {
			core = append(core, c)
		} else {
			extras = append(extras, c)
		}
	}

	result := make([]parse.Category, 0, len(core)+len(codexCats)+len(extras))
	result = append(result, core...)
	result = append(result, codexCats...)
	result = append(result, extras...)
	return result
}

// RunStatus fetches usage data from Claude (and optionally Codex) and prints
// a human-readable budget-headroom summary to stdout.
func RunStatus() error {
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

	for _, cat := range allCats {
		usagePct := cat.Utilization
		glidePct := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		resetStr := parse.FormatResetTime(cat.ResetsAt)
		headroom := glidePct - usagePct

		var pace string
		switch {
		case usagePct >= 80:
			pace = fmt.Sprintf("%+.0f%% vs pace, conserve usage", headroom)
		case headroom >= 0:
			pace = fmt.Sprintf("%.0f%% under pace, plenty of room", headroom)
		default:
			pace = fmt.Sprintf("%.0f%% over pace, slow down", -headroom)
		}

		fmt.Printf("%s: %.0f%% used | %s | %s\n", cat.Name, usagePct, pace, resetStr)
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

		fmt.Printf("Extra usage (monthly): %.0f%% used ($%.2f / $%.2f) | %s\n",
			usagePct, usedDollars, limitDollars, status)
	}

	return nil
}
