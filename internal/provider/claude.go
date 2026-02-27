package provider

import (
	"strings"

	"github.com/alliprice/headroom/internal/auth"
	"github.com/alliprice/headroom/internal/fetch"
	"github.com/alliprice/headroom/internal/parse"
)

// Claude is the provider for Claude API usage data.
var Claude = Provider{
	ID:          "claude",
	DisplayName: "Claude",
	CategoryIDs: []string{"five_hour", "seven_day", "seven_day_opus"},
	Probe:       nil, // always attempted
	Fetch:       fetchClaude,
}

func fetchClaude() (*FetchResult, bool, error) {
	token, err := auth.GetAccessToken()
	if err != nil {
		return nil, true, err
	}

	data, err := fetch.FetchClaude(token)
	if err != nil {
		isAuth := strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "authenticate")
		return nil, isAuth, err
	}

	cats, extra := parse.ParseClaude(data)
	return &FetchResult{Categories: cats, Extra: extra}, false, nil
}
