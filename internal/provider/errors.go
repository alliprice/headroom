package provider

import (
	"fmt"
	"strings"
)

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
