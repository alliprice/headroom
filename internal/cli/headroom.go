package cli

import (
	"fmt"
	"math"
	"strings"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
)

// HeadroomCategory holds computed headroom data for a single usage category.
type HeadroomCategory struct {
	Name                string
	UsagePct            float64
	GlidePct            float64
	HeadroomPct         float64
	SleepRecoveryPct    float64
	AdjustedHeadroomPct float64
	Status              string // "plenty of room" | "slow down" | "conserve"
	Resets              string
}

// HeadroomExtra holds computed headroom data for extra (dollar) usage.
type HeadroomExtra struct {
	UsagePct     float64
	UsedDollars  float64
	LimitDollars float64
	Status       string // "on track" | "warning"
}

// HeadroomFailure records a provider that could not be reached, so one dead
// provider is reported rather than hiding every healthy one.
type HeadroomFailure struct {
	Provider string
	Message  string
}

// HeadroomResult is the output of fetchHeadroom.
type HeadroomResult struct {
	Categories []HeadroomCategory
	Extra      *HeadroomExtra
	Failures   []HeadroomFailure
}

// failureMessage collapses a provider error onto one line and drops the
// provider name the error already carries, since the caller prints it. Some
// CLIs hand back a whole JSON document as their error text.
func failureMessage(displayName, msg string, limit int) string {
	msg = strings.Join(strings.Fields(msg), " ")
	msg = strings.TrimPrefix(msg, displayName+": ")
	if limit > 0 && len(msg) > limit {
		msg = strings.TrimSpace(msg[:limit-3]) + "..."
	}
	return msg
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

	// Fetch. A provider that fails is recorded and skipped: some credentials
	// cannot be refreshed without a person, and one of those going stale must
	// not take the healthy providers down with it.
	var allCats []parse.Category
	var extra *parse.ExtraUsage
	var failures []HeadroomFailure
	providerCount := 0
	attempted := 0
	for _, p := range provider.All {
		if !available[p.ID] {
			continue
		}
		attempted++
		res, _, err := p.Fetch()
		if err != nil {
			failures = append(failures, HeadroomFailure{
				Provider: p.DisplayName,
				Message:  failureMessage(p.DisplayName, err.Error(), 200),
			})
			continue
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

	// Only a total blackout is an error. Anything less still has something
	// worth printing.
	if providerCount == 0 && attempted > 0 && len(failures) == attempted {
		return nil, fmt.Errorf("every provider failed; first: %s: %s",
			failures[0].Provider, failures[0].Message)
	}

	// Prefix names when multiple providers present.
	if providerCount > 1 {
		allCats = prefixCategoryNames(allCats)
	}

	// Compute per-category headroom.
	result := &HeadroomResult{
		Categories: make([]HeadroomCategory, 0, len(allCats)),
		Failures:   failures,
	}
	for _, cat := range allCats {
		usagePct := cat.Utilization
		glidePct := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		headroom := usagePct - glidePct
		sleepRecovery := parse.CalcSleepRecoveryPct(cat.ResetsAt, cat.WindowSeconds)
		adjustedHeadroom := headroom - sleepRecovery
		result.Categories = append(result.Categories, HeadroomCategory{
			Name:                cat.Name,
			UsagePct:            usagePct,
			GlidePct:            glidePct,
			HeadroomPct:         headroom,
			SleepRecoveryPct:    sleepRecovery,
			AdjustedHeadroomPct: adjustedHeadroom,
			Status:              categoryStatus(usagePct, headroom, adjustedHeadroom),
			Resets:              parse.FormatResetTime(cat.ResetsAt),
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
func categoryStatus(usagePct, headroom, adjustedHeadroom float64) string {
	switch {
	case usagePct >= 80:
		return "conserve"
	case adjustedHeadroom > 0:
		return "conserve"
	case headroom > 0:
		return "slow down"
	default:
		return "plenty of room"
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
//
// Only a bare window name is lowercased. A name that already qualifies itself
// with a model group ("Gemini Session") keeps its own capitalisation, so it
// reads as "Antigravity Gemini Session" rather than "Antigravity gemini
// Session".
func prefixCategoryNames(cats []parse.Category) []parse.Category {
	for i := range cats {
		p := providerForKey(cats[i].Key)
		if p == nil {
			continue
		}
		name := cats[i].Name
		if !strings.Contains(name, " ") {
			name = lowercaseFirst(name)
		}
		cats[i].Name = p.DisplayName + " " + name
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
