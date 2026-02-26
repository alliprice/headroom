package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/alliprice/headroom/internal/parse"
)

// View implements tea.Model. It renders the complete TUI screen.
func (m Model) View() string {
	// Sleep mode
	if m.state == stateSleeping {
		return RenderSleep(m.width, m.height, m.sleepFrame)
	}

	w := m.width
	h := m.height
	if h < 1 || w < 5 {
		return ""
	}

	margin := 2
	if w < 20 {
		margin = 0
	}
	contentWidth := w - margin*2
	pad := strings.Repeat(" ", margin)

	// Separate core from extras
	var core, extraCats []parse.Category
	for _, c := range m.categories {
		if parse.CoreKeys[c.Key] {
			core = append(core, c)
		} else {
			extraCats = append(extraCats, c)
		}
	}
	if len(core) == 0 {
		core = m.categories
		extraCats = nil
	}
	n := len(core)

	// === Progressive compaction ===
	used := n // bars always shown
	level := 0

	// Build steps slice matching the Python algorithm:
	//   steps[0]        = max(n-1, 0)  -- spacing between bars (level 1)
	//   steps[1..2*n]   = alternating 1s for title[i] and reset[i]
	//   steps[2*n+1]    = 2            -- header (title + blank line)
	steps := []int{max(n-1, 0)}
	for i := 0; i < n; i++ {
		steps = append(steps, 1) // title for item i
		steps = append(steps, 1) // reset for item i
	}
	steps = append(steps, 2) // header

	for _, cost := range steps {
		if used+cost <= h {
			level++
			used += cost
		}
	}

	showSpacing := level >= 1
	showHeader := level >= 2+n*2

	showTitle := make([]bool, n)
	showReset := make([]bool, n)
	for i := 0; i < n; i++ {
		showTitle[i] = level >= 2+i*2
		showReset[i] = level >= 3+i*2
	}

	// Extra categories: each needs title + reset + bar + optional spacing
	var visibleExtra []parse.Category
	for _, cat := range extraCats {
		cost := 3
		if showSpacing {
			cost = 4
		}
		if used+cost <= h {
			visibleExtra = append(visibleExtra, cat)
			used += cost
		}
	}

	// Extra usage
	showExtraUsage := false
	if m.extra != nil {
		cost := 3
		if showSpacing {
			cost = 4
		}
		if used+cost <= h {
			showExtraUsage = true
			used += cost
		}
	}

	showFooter := used+1 <= h

	// === Build output ===
	var lines []string

	// Header
	if showHeader {
		hasCodex := false
		for _, c := range m.categories {
			if strings.HasPrefix(c.Key, "codex_") {
				hasCodex = true
				break
			}
		}
		title := "Claude Usage Monitor"
		if hasCodex {
			title = "Usage Monitor"
		}
		updated := parse.FormatUpdatedAgo(m.lastFetchTime)

		headerLine := pad + titleStyle.Render(title)
		gap := contentWidth - lipgloss.Width(title) - lipgloss.Width(updated)
		if gap > 0 {
			headerLine += strings.Repeat(" ", gap) + normalStyle.Render(updated)
		}
		lines = append(lines, headerLine, "")
	}

	// Error
	if m.errorMsg != "" {
		lines = append(lines, pad+errorStyle.Render(truncate(m.errorMsg, contentWidth)), "")
	}

	// Empty state
	if len(m.categories) == 0 && m.errorMsg == "" {
		lines = append(lines, pad+normalStyle.Render("No usage data available"))
	}

	// Core categories
	for idx, cat := range core {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)

		if showTitle[idx] {
			name := boldStyle.Render(cat.Name)
			usagePctStr := fmt.Sprintf("%.0f%% used", usage)
			usageStr := normalStyle.Render(usagePctStr)
			gap := contentWidth - lipgloss.Width(cat.Name) - lipgloss.Width(usagePctStr)
			line := pad + name
			if gap > 0 {
				line += strings.Repeat(" ", gap) + usageStr
			}
			lines = append(lines, line)
		}

		if showReset[idx] {
			resetStr := parse.FormatResetTime(cat.ResetsAt)
			if resetStr != "" {
				lines = append(lines, pad+dimStyle.Render(resetStr))
			} else {
				lines = append(lines, "")
			}
		}

		lines = append(lines, pad+RenderBar(contentWidth, usage, glide))

		if showSpacing && idx < n-1 {
			lines = append(lines, "")
		}
	}

	// Extra categories (full detail)
	for _, cat := range visibleExtra {
		if showSpacing {
			lines = append(lines, "")
		}
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		resetStr := parse.FormatResetTime(cat.ResetsAt)

		namePct := fmt.Sprintf("%.0f%% used", usage)
		name := boldStyle.Render(cat.Name)
		usageStr := normalStyle.Render(namePct)
		gap := contentWidth - lipgloss.Width(cat.Name) - lipgloss.Width(namePct)
		line := pad + name
		if gap > 0 {
			line += strings.Repeat(" ", gap) + usageStr
		}
		lines = append(lines, line)

		if resetStr != "" {
			lines = append(lines, pad+dimStyle.Render(resetStr))
		} else {
			lines = append(lines, "")
		}

		lines = append(lines, pad+RenderBar(contentWidth, usage, glide))
	}

	// Extra usage (monthly billing)
	if showExtraUsage {
		if showSpacing {
			lines = append(lines, "")
		}
		limitDollars := m.extra.MonthlyLimit / 100
		usedDollars := m.extra.UsedCredits / 100
		name := "Extra usage (monthly)"
		usageStr := fmt.Sprintf("$%.2f / $%.2f", usedDollars, limitDollars)

		nameSt := boldStyle.Render(name)
		usageSt := normalStyle.Render(usageStr)
		gap := contentWidth - lipgloss.Width(name) - lipgloss.Width(usageStr)
		line := pad + nameSt
		if gap > 0 {
			line += strings.Repeat(" ", gap) + usageSt
		}
		lines = append(lines, line, "")
		lines = append(lines, pad+RenderBar(contentWidth, m.extra.Utilization, 100))
	}

	// Footer (or input prompt replaces footer)
	if m.inputMode == inputInterval {
		prompt := boldStyle.Render("Refresh interval (seconds): ") + normalStyle.Render(m.inputBuf)
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, pad+prompt)
	} else if showFooter {
		footerText := fmt.Sprintf("q: quit  r: refresh  t: interval (%ds)", m.refreshFocused)
		footer := dimStyle.Render(footerText)
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		footerGap := contentWidth - lipgloss.Width(footerText)
		if footerGap > 0 {
			lines = append(lines, pad+strings.Repeat(" ", footerGap)+footer)
		} else {
			lines = append(lines, pad+footer)
		}
	}

	return strings.Join(lines, "\n")
}

// truncate shortens s to at most maxLen bytes. It does not account for
// multi-byte runes, but is safe for ASCII error messages.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
