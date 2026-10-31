package provider

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestHumanizeError(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		err         error
		want        string
	}{
		{
			name:        "timeout",
			displayName: "Claude",
			err:         errors.New("request timeout occurred"),
			want:        "Claude: timed out - will retry",
		},
		{
			name:        "deadline exceeded",
			displayName: "Antigravity",
			err:         errors.New("context deadline exceeded"),
			want:        "Antigravity: timed out - will retry",
		},
		{
			name:        "no such host",
			displayName: "Claude",
			err:         errors.New("dial tcp: lookup api.example.com: no such host"),
			want:        "Claude: DNS lookup failed - check connection",
		},
		{
			name:        "DNS error",
			displayName: "Codex",
			err:         errors.New("DNS resolution failed"),
			want:        "Codex: DNS lookup failed - check connection",
		},
		{
			name:        "connection refused",
			displayName: "Claude",
			err:         errors.New("dial tcp 127.0.0.1:8080: connection refused"),
			want:        "Claude: connection refused",
		},
		{
			name:        "TLS error",
			displayName: "Antigravity",
			err:         errors.New("TLS handshake failed"),
			want:        "Antigravity: TLS error - check network",
		},
		{
			name:        "certificate error",
			displayName: "Claude",
			err:         errors.New("x509: certificate signed by unknown authority"),
			want:        "Claude: TLS error - check network",
		},
		{
			name:        "EOF",
			displayName: "Codex",
			err:         errors.New("unexpected EOF"),
			want:        "Codex: connection lost - will retry",
		},
		{
			name:        "connection reset",
			displayName: "Claude",
			err:         errors.New("read tcp: connection reset by peer"),
			want:        "Claude: connection lost - will retry",
		},
		{
			name:        "expired token",
			displayName: "Claude",
			err:         errors.New("access token expired"),
			want:        "access token expired",
		},
		{
			name:        "authenticate required",
			displayName: "Antigravity",
			err:         errors.New("please authenticate to continue"),
			want:        "please authenticate to continue",
		},
		{
			name:        "re-authenticate",
			displayName: "Claude",
			err:         errors.New("session invalid, re-authenticate required"),
			want:        "session invalid, re-authenticate required",
		},
		{
			name:        "rate limit 429",
			displayName: "Claude",
			err:         errors.New("HTTP 429: too many requests"),
			want:        "Claude: rate limited - will retry",
		},
		{
			name:        "server error 500",
			displayName: "Antigravity",
			err:         errors.New("HTTP 500: internal server error"),
			want:        "Antigravity: server error - will retry",
		},
		{
			name:        "server error 502",
			displayName: "Claude",
			err:         errors.New("502 Bad Gateway"),
			want:        "Claude: server error - will retry",
		},
		{
			name:        "server error 503",
			displayName: "Codex",
			err:         errors.New("503 Service Unavailable"),
			want:        "Codex: server error - will retry",
		},
		{
			name:        "server error 504",
			displayName: "Antigravity",
			err:         errors.New("504 Gateway Timeout"),
			want:        "Antigravity: server error - will retry",
		},
		{
			name:        "short generic error",
			displayName: "Claude",
			err:         errors.New("something went wrong"),
			want:        "Claude: something went wrong",
		},
		{
			name:        "long error truncated",
			displayName: "Antigravity",
			err:         errors.New("this is a very long error message that exceeds sixty characters and should be truncated"),
			want:        "Antigravity: this is a very long error message that exceeds sixty char...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanizeError(tt.displayName, tt.err)
			if got != tt.want {
				t.Errorf("humanizeError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.October, 31, 22, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		value  string
		want   time.Duration
		wantOK bool
	}{
		{"seconds", "2593", 2593 * time.Second, true},
		{"seconds with space", " 120 ", 2 * time.Minute, true},
		{"zero", "0", 0, true},
		{"seconds capped", "86400", 6 * time.Hour, true},
		{"huge seconds capped", "99999999999999999", 6 * time.Hour, true},
		{"http date", now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{"http date capped", now.Add(48 * time.Hour).Format(http.TimeFormat), 6 * time.Hour, true},
		{"http date in the past", now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{"empty", "", 0, false},
		{"negative", "-5", 0, false},
		{"garbage", "soon", 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.value, now)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("parseRetryAfter(%q) = %s, %v; want %s, %v", tt.value, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestRetryAfterThroughWrapping(t *testing.T) {
	inner := &RetryAfterError{Wait: 2593 * time.Second, Err: errors.New("API error: 429 Too Many Requests")}
	wrapped := fmt.Errorf("outer: %w", inner)
	if wait, ok := RetryAfter(wrapped); !ok || wait != 2593*time.Second {
		t.Fatalf("RetryAfter(wrapped) = %s, %v", wait, ok)
	}
	if _, ok := RetryAfter(errors.New("plain")); ok {
		t.Fatal("plain error reported a retry wait")
	}
	if _, ok := RetryAfter(nil); ok {
		t.Fatal("nil error reported a retry wait")
	}
}

// humanize used to be fmt.Errorf("%s", humanizeError(...)), which dropped the
// error's type and with it the server's Retry-After.
func TestHumanizeKeepsRetryAfter(t *testing.T) {
	err := humanize("Claude", &RetryAfterError{Wait: 43 * time.Minute, Err: errors.New("API error: 429 Too Many Requests")})
	if got, want := err.Error(), "Claude: rate limited - will retry"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	if wait, ok := RetryAfter(err); !ok || wait != 43*time.Minute {
		t.Fatalf("RetryAfter(humanized) = %s, %v; want 43m, true", wait, ok)
	}
	if humanize("Claude", nil) != nil {
		t.Fatal("humanize(nil) is not nil")
	}
}
