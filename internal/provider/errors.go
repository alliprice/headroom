package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRetryAfter caps how long a server can push the next poll out. A wait
// longer than this is more likely a bad header than a real instruction, and a
// poller that sleeps for a day looks dead.
const maxRetryAfter = 6 * time.Hour

// RetryAfterError is a failed request whose server said when to try again.
type RetryAfterError struct {
	Wait time.Duration
	Err  error
}

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("%v (retry after %s)", e.Err, e.Wait)
}

func (e *RetryAfterError) Unwrap() error { return e.Err }

// HTTPStatusError is a non-200 answer from a provider's API.
type HTTPStatusError struct {
	Code   int
	Status string // e.g. "429 Too Many Requests"
}

func (e *HTTPStatusError) Error() string { return "API error: " + e.Status }

// RateLimited reports whether err's chain holds a 429.
func RateLimited(err error) bool {
	var status *HTTPStatusError
	return errors.As(err, &status) && status.Code == http.StatusTooManyRequests
}

// RetryAfter reports the wait a server asked for anywhere in err's chain.
func RetryAfter(err error) (time.Duration, bool) {
	var ra *RetryAfterError
	if errors.As(err, &ra) {
		return ra.Wait, true
	}
	return 0, false
}

// parseRetryAfter reads a Retry-After header value: delay seconds or an
// HTTP-date. A date already past means retry now.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}

	var wait time.Duration
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > int64(maxRetryAfter/time.Second) {
			return maxRetryAfter, true
		}
		wait = time.Duration(secs) * time.Second
	} else if at, err := http.ParseTime(v); err == nil {
		// Whole seconds, as the header itself is. It also keeps the wait's
		// String() free of digit runs that humanizeError would read as a
		// status code.
		wait = at.Sub(now).Round(time.Second)
	} else {
		return 0, false
	}

	if wait < 0 {
		wait = 0
	}
	if wait > maxRetryAfter {
		wait = maxRetryAfter
	}
	return wait, true
}

// humanizedError carries a short message for people while keeping the
// original error reachable through errors.As, so a caller can still read a
// RetryAfterError's wait.
type humanizedError struct {
	msg string
	err error
}

func (e *humanizedError) Error() string { return e.msg }
func (e *humanizedError) Unwrap() error { return e.err }

func humanize(displayName string, err error) error {
	if err == nil {
		return nil
	}
	return &humanizedError{msg: humanizeError(displayName, err), err: err}
}

func humanizeError(displayName string, err error) string {
	msg := err.Error()
	msgLower := strings.ToLower(msg)

	if strings.Contains(msg, "500") || strings.Contains(msg, "502") || strings.Contains(msg, "503") || strings.Contains(msg, "504") {
		return fmt.Sprintf("%s: server error - will retry", displayName)
	}

	if strings.Contains(msg, "429") {
		return fmt.Sprintf("%s: rate limited - will retry", displayName)
	}

	if strings.Contains(msgLower, "timeout") || strings.Contains(msgLower, "deadline exceeded") {
		return fmt.Sprintf("%s: timed out - will retry", displayName)
	}

	if strings.Contains(msgLower, "no such host") || strings.Contains(msgLower, "dns") {
		return fmt.Sprintf("%s: DNS lookup failed - check connection", displayName)
	}

	if strings.Contains(msgLower, "connection refused") {
		return fmt.Sprintf("%s: connection refused", displayName)
	}

	if strings.Contains(msgLower, "tls") || strings.Contains(msgLower, "certificate") {
		return fmt.Sprintf("%s: TLS error - check network", displayName)
	}

	if strings.Contains(msgLower, "eof") || strings.Contains(msgLower, "connection reset") {
		return fmt.Sprintf("%s: connection lost - will retry", displayName)
	}

	if strings.Contains(msgLower, "expired") || strings.Contains(msgLower, "authenticate") || strings.Contains(msgLower, "re-authenticate") {
		return msg
	}

	truncated := msg
	if len(msg) > 60 {
		truncated = msg[:57] + "..."
	}

	return fmt.Sprintf("%s: %s", displayName, truncated)
}
