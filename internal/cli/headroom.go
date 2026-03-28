package cli

import (
	"math"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
)

// HeadroomCategory holds computed headroom data for a single usage category.
type HeadroomCategory struct {
	Name        string
	UsagePct    float64
	GlidePct    float64
	HeadroomPct float64
	Status      string // "plenty of room" | "slow down" | "conserve"
	Resets      string
}

// HeadroomExtra holds computed headroom data for extra (dollar) usage.
type HeadroomExtra struct {
	UsagePct     float64
	UsedDollars  float64
	LimitDollars float64
	Status       string // "on track" | "warning"
}

// HeadroomResult is the output of fetchHeadroom.
type HeadroomResult struct {
	Categories []HeadroomCategory
	Extra      *HeadroomExtra
}

// fetchHeadroom probes all providers, fetches usage data, and computes
// headroom status for each category and extra usage.
func fetchHeadroom() (*HeadroomResult, error) {
	// Probe
	available := make(map[string]bool)
	for _, p := range provider.All {
		if p.Probe == nil || p.Probe() {
			available[p.ID] = true
		}
	}

	// Fetch
	var allCats []parse.Category
	var extra *parse.ExtraUsage
	providerCount := 0
	for _, p := range provider.All {
		if !available[p.ID] {
			continue
		}
		res, _, err := p.Fetch()
		if err != nil {
			return nil, err
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

	// Prefix names when multiple providers present.
	if providerCount > 1 {
		allCats = prefixCategoryNames(allCats)
	}

	// Compute per-category headroom.
	result := &HeadroomResult{
		Categories: make([]HeadroomCategory, 0, len(allCats)),
	}
	for _, cat := range allCats {
		usagePct := cat.Utilization
		glidePct := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		headroom := usagePct - glidePct
		result.Categories = append(result.Categories, HeadroomCategory{
			Name:        cat.Name,
			UsagePct:    usagePct,
			GlidePct:    glidePct,
			HeadroomPct: headroom,
			Status:      categoryStatus(usagePct, headroom),
			Resets:      parse.FormatResetTime(cat.ResetsAt),
		})
	}

	// Compute extra usage headroom.
	if extra != nil {
		usagePct := extra.Utilization
		result.Extra = &HeadroomExtra{
			UsagePct:     usagePct,
			UsedDollars:  extra.UsedCredits / 100,
			LimitDollars: extra.MonthlyLimit / 100,
			Status:       extraStatus(usagePct),
		}
	}

	return result, nil
}

// categoryStatus returns the status string for a usage category.
func categoryStatus(usagePct, headroom float64) string {
	switch {
	case usagePct >= 80:
		return "conserve"
	case headroom <= 0:
		return "plenty of room"
	default:
		return "slow down"
	}
}

// extraStatus returns the status string for extra usage.
func extraStatus(usagePct float64) string {
	if usagePct >= 80 {
		return "warning"
	}
	return "on track"
}

// round1 rounds f to one decimal place.
func round1(f float64) float64 {
	return math.Round(f*10) / 10
}

// round2 rounds f to two decimal places.
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// prefixCategoryNames adds provider prefixes to category names when multiple
// providers are present (e.g. "Session" -> "Claude session").
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
