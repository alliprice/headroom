package provider

import (
	"github.com/alliprice/headroom/internal/fetch"
	"github.com/alliprice/headroom/internal/parse"
)

// Codex is the provider for Codex API usage data.
var Codex = Provider{
	ID:          "codex",
	DisplayName: "Codex",
	CategoryIDs: []string{"codex_primary", "codex_secondary"},
	Probe:       probeCodex,
	Fetch:       fetchCodex,
}

func probeCodex() bool {
	data, _ := fetch.FetchCodex()
	return data != nil
}

func fetchCodex() (*FetchResult, bool, error) {
	data, err := fetch.FetchCodex()
	if err != nil {
		return nil, false, err
	}
	if data == nil {
		return &FetchResult{}, false, nil
	}
	cats := parse.ParseCodex(data)
	return &FetchResult{Categories: cats}, false, nil
}
