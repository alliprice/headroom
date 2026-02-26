package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/alliprice/headroom/internal/parse"
)

// View implements tea.Model. It renders the complete TUI screen.
func (m Model) View() string {
	// Sleep mode
	if m.state == stateSleeping {
		return RenderPlasma(m.width, m.height, m.sleepFrame)
	}

	w := m.width
	h := m.height
	if h < 1 || w < 5 {
		return ""
	}

	// Split categories into Claude vs Codex groups.
	var claudeCats, codexCats []parse.Category
	for _, c := range m.categories {
		if strings.HasPrefix(c.Key, "codex_") {
			codexCats = append(codexCats, c)
		} else {
			claudeCats = append(claudeCats, c)
		}
	}

	// Status bar (always rendered at the bottom).
	statusBar := m.renderStatusBar()

	// Error line (rendered above panels when present).
	var errorLine string
	if m.errorMsg != "" {
		errorLine = errorStyle.Render(truncate(m.errorMsg, w-4))
	}

	// Empty state.
	if len(m.categories) == 0 && m.errorMsg == "" {
		body := normalStyle.Render("No usage data available")
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.PlaceVertical(h-1, lipgloss.Center, body),
			statusBar,
		)
	}

	// Small terminal fallback — no borders.
	if w < 40 || h < 12 {
		return m.renderFlat(claudeCats, codexCats, statusBar, errorLine)
	}

	// Calculate available height for the panel area.
	panelAreaHeight := h - 1 // subtract status bar row
	if errorLine != "" {
		panelAreaHeight -= 2 // error line + trailing blank line
	}

	// Panel border+padding overhead:
	//   2 rows for top/bottom border + 2 rows for top/bottom padding = 4 rows
	//   4 columns for left/right border (1 each) + left/right padding (2 each) = 6 cols
	const borderRowOverhead = 4
	const borderColOverhead = 6

	// panelStyle.Width() includes border+padding, so pass w for full-width
	// panels. The actual text content width is w minus the overhead.
	panelContentWidth := w - borderColOverhead

	var panels string
	if len(codexCats) > 0 {
		// Stacked: split vertical space evenly between two panels.
		eachHeight := (panelAreaHeight - borderRowOverhead*2) / 2

		claudeContent := renderPanel("Claude", claudeCats, m.extra, panelContentWidth, eachHeight, m.lastFetchTime)
		claudePanel := panelStyle.Width(w).Render(claudeContent)

		codexContent := renderPanel("Codex", codexCats, nil, panelContentWidth, eachHeight, m.lastFetchTime)
		codexPanel := panelStyle.Width(w).Render(codexContent)

		panels = lipgloss.JoinVertical(lipgloss.Left, claudePanel, codexPanel)
	} else {
		claudeContent := renderPanel("Claude", claudeCats, m.extra, panelContentWidth, panelAreaHeight-borderRowOverhead, m.lastFetchTime)
		panels = panelStyle.Width(w).Render(claudeContent)
	}

	// Compose final view.
	var sections []string
	if errorLine != "" {
		sections = append(sections, errorLine, "")
	}
	sections = append(sections, panels)

	body := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Pad remaining height so the status bar is always at the bottom.
	bodyHeight := lipgloss.Height(body)
	if bodyHeight < h-1 {
		body += strings.Repeat("\n", h-1-bodyHeight)
	}

	return body + "\n" + statusBar
}

// renderStatusBar builds the full-width status bar displayed at the bottom of
// the screen.
func (m Model) renderStatusBar() string {
	brand := statusBrandStyle.Render(" headroom ")

	var right string
	if m.inputMode == inputInterval {
		right = boldStyle.Render("Interval (seconds): ") + normalStyle.Render(m.inputBuf+"_")
	} else {
		updated := parse.FormatUpdatedAgo(m.lastFetchTime)
		hints := fmt.Sprintf("q:quit r:refresh t:interval(%ds)", m.refreshFocused)
		right = dimStyle.Render(updated + "  " + hints)
	}

	brandWidth := lipgloss.Width(brand)
	rightOnBg := statusBarStyle.Render(right + " ")
	gapWidth := m.width - brandWidth - lipgloss.Width(rightOnBg)
	if gapWidth < 0 {
		gapWidth = 0
	}
	gap := statusBarStyle.Render(strings.Repeat(" ", gapWidth))

	return brand + gap + rightOnBg
}

// renderPanel builds the content string for a single bordered panel. It
// applies progressive compaction so the content fits within maxHeight rows.
// width is the inner content width (border and padding already subtracted).
func renderPanel(title string, cats []parse.Category, extra *parse.ExtraUsage, width int, maxHeight int, lastFetchTime *time.Time) string {
	n := len(cats)
	if n == 0 {
		return normalStyle.Render("No data")
	}

	// Panel title line.
	panelTitle := titleStyle.Render(title)

	// Progressive compaction.
	// Always shown: panel title (1) + one bar per category (n).
	used := 1 + n
	level := 0

	// Steps:
	//   step 0    : spacing between categories  (n-1 lines)
	//   steps 1..n: info line per category       (reset time + percentage)
	steps := []int{max(n-1, 0)}
	for i := 0; i < n; i++ {
		steps = append(steps, 1)
	}

	for _, cost := range steps {
		if used+cost <= maxHeight {
			level++
			used += cost
		}
	}

	showSpacing := level >= 1
	showInfo := make([]bool, n)
	for i := 0; i < n; i++ {
		showInfo[i] = level >= 2+i
	}

	// Extra usage (Claude panel only, shown when space allows).
	showExtra := false
	if extra != nil {
		cost := 3 // title line + blank line + bar
		if showSpacing {
			cost++
		}
		if used+cost <= maxHeight {
			showExtra = true
		}
	}

	// Build the content lines.
	var lines []string
	lines = append(lines, panelTitle)

	for idx, cat := range cats {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)

		if showInfo[idx] {
			resetStr := parse.FormatResetTime(cat.ResetsAt)
			pctStr := fmt.Sprintf("%.0f%%", usage)
			left := dimStyle.Render(resetStr)
			right := dimStyle.Render(pctStr)
			gap := width - lipgloss.Width(resetStr) - lipgloss.Width(pctStr)
			line := left
			if gap > 0 {
				line += strings.Repeat(" ", gap) + right
			}
			lines = append(lines, line)
		}

		lines = append(lines, RenderBar(width, usage, glide))

		if showSpacing && idx < n-1 {
			lines = append(lines, "")
		}
	}

	// Extra usage (monthly billing).
	if showExtra {
		if showSpacing {
			lines = append(lines, "")
		}
		limitDollars := extra.MonthlyLimit / 100
		usedDollars := extra.UsedCredits / 100
		name := "Extra usage (monthly)"
		usageStr := fmt.Sprintf("$%.2f / $%.2f", usedDollars, limitDollars)

		nameSt := boldStyle.Render(name)
		usageSt := dimStyle.Render(usageStr)
		gap := width - lipgloss.Width(name) - lipgloss.Width(usageStr)
		line := nameSt
		if gap > 0 {
			line += strings.Repeat(" ", gap) + usageSt
		}
		lines = append(lines, line, "")
		lines = append(lines, RenderBar(width, extra.Utilization, 100))
	}

	return strings.Join(lines, "\n")
}

// renderFlat renders a simplified layout with no borders, used when the
// terminal is too small to fit the full panel layout (width < 40 or height < 12).
func (m Model) renderFlat(claudeCats, codexCats []parse.Category, statusBar, errorLine string) string {
	w := m.width
	h := m.height
	allCats := append(claudeCats, codexCats...)

	var lines []string
	if errorLine != "" {
		lines = append(lines, errorLine)
	}

	for _, cat := range allCats {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		lines = append(lines, RenderBar(w, usage, glide))
	}

	body := strings.Join(lines, "\n")
	bodyHeight := lipgloss.Height(body)
	if bodyHeight < h-1 {
		body += strings.Repeat("\n", h-1-bodyHeight)
	}
	return body + "\n" + statusBar
}

// truncate shortens s to at most maxLen bytes. It does not account for
// multi-byte runes, but is safe for ASCII error messages.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

