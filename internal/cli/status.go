package cli

import (
	"fmt"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
)

// RunStatus fetches usage data from all providers and prints a human-readable
// budget-headroom summary to stdout.
func RunStatus() error {
	var allCats []parse.Category
	var extra *parse.ExtraUsage
	hasMultipleProviders := false

	// Probe and fetch from all providers.
	available := make(map[string]bool)
	for _, p := range provider.All {
		if p.Probe == nil || p.Probe() {
			available[p.ID] = true
		}
	}

	providerCount := 0
	for _, p := range provider.All {
		if !available[p.ID] {
			continue
		}
		res, _, err := p.Fetch()
		if err != nil {
			return err
		}
		if res == nil {
			continue
		}
		providerCount++
		allCats = append(allCats, res.Categories...)
		if res.Extra != nil {
			extra = res.Extra
		}
	}

	if providerCount > 1 {
		hasMultipleProviders = true
	}

	// Prefix names when multiple providers present.
	if hasMultipleProviders {
		allCats = prefixCategoryNames(allCats)
	}

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

	if extra != nil {
		usagePct := extra.Utilization
		limitDollars := extra.MonthlyLimit / 100
		usedDollars := extra.UsedCredits / 100

		var status string
		if usagePct >= 80 {
			status = "warning"
		} else {
			status = "on track"
		}

		fmt.Printf("Extra usage: %.0f%% used ($%.2f / $%.2f) | %s\n",
			usagePct, usedDollars, limitDollars, status)
	}

	return nil
}

// prefixCategoryNames adds provider prefixes to category names when multiple
// providers are present (e.g. "Session" → "Claude session").
func prefixCategoryNames(cats []parse.Category) []parse.Category {
	for i := range cats {
		p := providerForKey(cats[i].Key)
		if p != nil {
			cats[i].Name = p.DisplayName + " " + lowercaseFirst(cats[i].Name)
		}
	}
	return cats
}

// providerForKey returns the provider that owns the given category key.
func providerForKey(key string) *provider.Provider {
	for i := range provider.All {
		for _, k := range provider.All[i].CategoryIDs {
			if k == key {
				return &provider.All[i]
			}
		}
	}
	return nil
}

// lowercaseFirst lowercases the first character of s.
func lowercaseFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}
