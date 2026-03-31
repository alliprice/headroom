package provider

import (
	"errors"
	"testing"
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
			displayName: "Gemini",
			err:         errors.New("context deadline exceeded"),
			want:        "Gemini: timed out - will retry",
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
			displayName: "Gemini",
			err:         errors.New("TLS handshake failed"),
			want:        "Gemini: TLS error - check network",
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
			displayName: "Gemini",
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
			displayName: "Gemini",
			err:         errors.New("HTTP 500: internal server error"),
			want:        "Gemini: server error - will retry",
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
			displayName: "Gemini",
			err:         errors.New("504 Gateway Timeout"),
			want:        "Gemini: server error - will retry",
		},
		{
			name:        "short generic error",
			displayName: "Claude",
			err:         errors.New("something went wrong"),
			want:        "Claude: something went wrong",
		},
		{
			name:        "long error truncated",
			displayName: "Gemini",
			err:         errors.New("this is a very long error message that exceeds sixty characters and should be truncated"),
			want:        "Gemini: this is a very long error message that exceeds sixty char...",
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
