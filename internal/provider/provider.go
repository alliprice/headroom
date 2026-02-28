package provider

import "github.com/alliprice/headroom/internal/parse"

// FetchResult carries parsed usage data from a single provider.
type FetchResult struct {
	Categories []parse.Category
	Extra      *parse.ExtraUsage // nil if provider doesn't have it
}

// Provider is a declarative description of a usage data source.
// Each provider lives in its own file and is registered in All.
type Provider struct {
	ID          string   // "claude", "codex"
	DisplayName string   // "Claude", "Codex"
	CategoryIDs []string // known keys in display order

	// Probe returns true if the provider is available. nil = always available.
	Probe func() bool

	// Fetch retrieves and parses usage data in one step.
	Fetch func() (result *FetchResult, isAuthErr bool, err error)

	// Demo returns plausible fake data for demo mode. nil = no demo data.
	Demo func() *FetchResult
}
