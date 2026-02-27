package tui

import (
	"testing"
	"time"

	"github.com/alliprice/headroom/internal/parse"
)

var t0 = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

func TestTickRefreshFocused(t *testing.T) {
	s := newRefreshScheduler(0)
	ft := t0
	s.recordFetch(ft)

	// Too soon - no refresh.
	got := s.tick(t0.Add(100 * time.Second))
	if got != refreshNone {
		t.Fatalf("expected refreshNone at 100s, got %d", got)
	}

	// At the focused interval boundary - should refresh.
	got = s.tick(t0.Add(time.Duration(parse.RefreshFocused) * time.Second))
	if got != refreshFetch {
		t.Fatalf("expected refreshFetch at %ds, got %d", parse.RefreshFocused, got)
	}
}

func TestTickRefreshUnfocused(t *testing.T) {
	s := newRefreshScheduler(0)
	ft := t0
	s.recordFetch(ft)
	s.setFocus(false, t0)

	// At focused interval - should NOT refresh (unfocused uses longer interval).
	got := s.tick(t0.Add(time.Duration(parse.RefreshFocused) * time.Second))
	if got != refreshNone {
		t.Fatalf("expected refreshNone at focused interval when unfocused, got %d", got)
	}

	// At unfocused interval - should refresh.
	got = s.tick(t0.Add(time.Duration(parse.RefreshUnfocused) * time.Second))
	if got != refreshFetch {
		t.Fatalf("expected refreshFetch at %ds, got %d", parse.RefreshUnfocused, got)
	}
}

func TestTickNoRefreshTooSoon(t *testing.T) {
	s := newRefreshScheduler(0)
	ft := t0
	s.recordFetch(ft)

	got := s.tick(t0.Add(10 * time.Second))
	if got != refreshNone {
		t.Fatalf("expected refreshNone, got %d", got)
	}
}

func TestTickSleepTransition(t *testing.T) {
	s := newRefreshScheduler(0)
	ft := t0
	s.recordFetch(ft)
	s.setFocus(false, t0)

	// Just before sleep threshold.
	got := s.tick(t0.Add(time.Duration(parse.SleepAfterUnfocusedSeconds-1) * time.Second))
	if got == refreshSleep {
		t.Fatal("should not sleep before threshold")
	}

	// At sleep threshold.
	got = s.tick(t0.Add(time.Duration(parse.SleepAfterUnfocusedSeconds) * time.Second))
	if got != refreshSleep {
		t.Fatalf("expected refreshSleep, got %d", got)
	}
}

func TestTickNoSleepWhenFocused(t *testing.T) {
	s := newRefreshScheduler(0)
	ft := t0
	s.recordFetch(ft)

	// Even after a long time, focused means no sleep.
	got := s.tick(t0.Add(time.Duration(parse.SleepAfterUnfocusedSeconds+3600) * time.Second))
	// Should be refreshFetch (enough time passed), never refreshSleep.
	if got == refreshSleep {
		t.Fatal("should never sleep when focused")
	}
}

func TestTickAuthErrorRetry(t *testing.T) {
	s := newRefreshScheduler(0)
	s.recordFetchAttempt(t0)
	s.recordError(true)

	// Before auth error interval - no retry.
	got := s.tick(t0.Add(time.Duration(parse.RefreshOnAuthError-1) * time.Second))
	if got != refreshNone {
		t.Fatalf("expected refreshNone before auth retry interval, got %d", got)
	}

	// At auth error interval - should retry.
	got = s.tick(t0.Add(time.Duration(parse.RefreshOnAuthError) * time.Second))
	if got != refreshFetch {
		t.Fatalf("expected refreshFetch at auth retry interval, got %d", got)
	}
}

func TestTickErrorRetry(t *testing.T) {
	s := newRefreshScheduler(0)
	s.recordFetchAttempt(t0)
	s.recordError(false) // non-auth error

	// Non-auth errors retry at the focused interval.
	got := s.tick(t0.Add(time.Duration(parse.RefreshFocused-1) * time.Second))
	if got != refreshNone {
		t.Fatalf("expected refreshNone before retry interval, got %d", got)
	}

	got = s.tick(t0.Add(time.Duration(parse.RefreshFocused) * time.Second))
	if got != refreshFetch {
		t.Fatalf("expected refreshFetch at retry interval, got %d", got)
	}
}

func TestSetInterval(t *testing.T) {
	s := newRefreshScheduler(0)
	ft := t0
	s.recordFetch(ft)

	// Set custom interval of 60s.
	s.setInterval(60)

	// At 59s - should not refresh.
	got := s.tick(t0.Add(59 * time.Second))
	if got != refreshNone {
		t.Fatalf("expected refreshNone at 59s with 60s interval, got %d", got)
	}

	// At 60s - should refresh.
	got = s.tick(t0.Add(60 * time.Second))
	if got != refreshFetch {
		t.Fatalf("expected refreshFetch at 60s, got %d", got)
	}
}

func TestRecordFetchClearsError(t *testing.T) {
	s := newRefreshScheduler(0)
	s.recordFetchAttempt(t0)
	s.recordError(true) // auth error

	// Simulate successful fetch.
	s.clearError()
	fetchTime := t0.Add(time.Duration(parse.RefreshOnAuthError) * time.Second)
	s.recordFetch(fetchTime)

	// Even well past the auth retry interval, should not trigger fetch
	// because we now have a lastFetchTime and the error is cleared.
	got := s.tick(fetchTime.Add(1 * time.Second))
	if got != refreshNone {
		t.Fatalf("expected refreshNone after clearing error and recording fetch, got %d", got)
	}

	// Should refresh at the normal focused interval from the fetch time.
	got = s.tick(fetchTime.Add(time.Duration(parse.RefreshFocused) * time.Second))
	if got != refreshFetch {
		t.Fatalf("expected refreshFetch at normal interval, got %d", got)
	}
}
