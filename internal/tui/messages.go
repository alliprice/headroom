package tui

import (
	"time"

	"github.com/alliprice/headroom/internal/parse"
)

// fetchResultMsg carries the result of a data fetch.
type fetchResultMsg struct {
	categories    []parse.Category
	extra         *parse.ExtraUsage              // backward compat: last non-nil extra
	providerExtra map[string]*parse.ExtraUsage   // provider ID → extra usage
	errorMsg      string
	isAuthErr     bool
	fetchTime     time.Time // when the fetch completed
}

// tickMsg fires every 1 second to update the "Updated: Xs ago" display
// and check if a refresh is needed.
type tickMsg time.Time

// sleepTickMsg fires every 500ms to animate the sleep mode frames.
type sleepTickMsg time.Time
