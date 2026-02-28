package provider

import (
	"os"
	"strings"

	"github.com/alliprice/headroom/internal/fetch"
	"github.com/alliprice/headroom/internal/parse"
)

// Gemini is the provider for Gemini CLI usage data.
var Gemini = Provider{
	ID:          "gemini",
	DisplayName: "Gemini",
	CategoryIDs: nil, // dynamic - discovered at fetch time
	Probe:       probeGemini,
	Fetch:       fetchGemini,
}

func probeGemini() bool {
	_, err := os.Stat(fetch.GeminiCredsPath())
	return err == nil
}

func fetchGemini() (*FetchResult, bool, error) {
	data, err := fetch.FetchGemini()
	if err != nil {
		isAuth := strings.Contains(err.Error(), "expired") ||
			strings.Contains(err.Error(), "authenticate") ||
			strings.Contains(err.Error(), "401") ||
			strings.Contains(err.Error(), "403")
		return nil, isAuth, err
	}
	cats := parse.ParseGemini(data)
	return &FetchResult{Categories: cats}, false, nil
}
