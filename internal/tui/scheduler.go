package tui

import (
	"time"

	"github.com/alliprice/headroom/internal/parse"
)

// refreshAction is the result of a scheduler tick.
type refreshAction int

const (
	refreshNone  refreshAction = iota
	refreshFetch                       // time to fetch new data
	refreshSleep                       // enter sleep mode
)

// refreshScheduler owns all refresh/sleep timing state and exposes a single
// tick method that returns the next action. The model feeds it events (focus
// change, fetch result, errors) and reads back what to do on each tick.
type refreshScheduler struct {
	focusedInterval   int // seconds between refreshes when focused
	unfocusedInterval int // seconds between refreshes when unfocused
	authErrorInterval int // seconds between retries on auth error
	sleepAfter        int // seconds unfocused before entering sleep

	focused       bool
	lastFocusLost time.Time
	lastFetchTime *time.Time // nil = never fetched successfully
	lastFetchAttempt time.Time
	hasError    bool
	isAuthError bool
}

// newRefreshScheduler returns a scheduler with defaults from parse constants.
// focusedInterval overrides the default focused refresh interval if > 0.
func newRefreshScheduler(focusedInterval int) refreshScheduler {
	fi := parse.RefreshFocused
	if focusedInterval > 0 {
		fi = focusedInterval
	}
	return refreshScheduler{
		focusedInterval:   fi,
		unfocusedInterval: parse.RefreshUnfocused,
		authErrorInterval: parse.RefreshOnAuthError,
		sleepAfter:        parse.SleepAfterUnfocusedSeconds,
		focused:           true,
	}
}

// tick evaluates the current time against all timing thresholds and returns
// the action the model should take. Called once per second from tickMsg.
func (s *refreshScheduler) tick(now time.Time) refreshAction {
	// Sleep transition: unfocused for too long.
	if !s.focused {
		elapsed := now.Sub(s.lastFocusLost).Seconds()
		if elapsed >= float64(s.sleepAfter) {
			return refreshSleep
		}
	}

	// Normal refresh: last successful fetch is old enough.
	if s.lastFetchTime != nil {
		interval := s.focusedInterval
		if !s.focused {
			interval = s.unfocusedInterval
		}
		if now.Sub(*s.lastFetchTime).Seconds() >= float64(interval) {
			return refreshFetch
		}
		return refreshNone
	}

	// Error retry: never fetched successfully, but have tried and failed.
	if s.hasError {
		retryInterval := s.focusedInterval
		if s.isAuthError {
			retryInterval = s.authErrorInterval
		}
		if now.Sub(s.lastFetchAttempt).Seconds() >= float64(retryInterval) {
			return refreshFetch
		}
	}

	return refreshNone
}

// recordFetch records a successful fetch time.
func (s *refreshScheduler) recordFetch(t time.Time) {
	s.lastFetchTime = &t
}

// recordFetchAttempt records that a fetch was attempted (success or failure).
func (s *refreshScheduler) recordFetchAttempt(t time.Time) {
	s.lastFetchAttempt = t
}

// recordError records that the last fetch returned an error.
func (s *refreshScheduler) recordError(isAuth bool) {
	s.hasError = true
	s.isAuthError = isAuth
}

// clearError clears the error state after a successful fetch.
func (s *refreshScheduler) clearError() {
	s.hasError = false
	s.isAuthError = false
}

// setFocus updates focus state and records when focus was lost.
func (s *refreshScheduler) setFocus(focused bool, now time.Time) {
	if s.focused && !focused {
		s.lastFocusLost = now
	}
	s.focused = focused
}

// setInterval updates the focused refresh interval.
func (s *refreshScheduler) setInterval(seconds int) {
	if seconds > 0 {
		s.focusedInterval = seconds
	}
}
