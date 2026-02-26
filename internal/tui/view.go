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

	// Panel layout constants.
	const panelMargin = 2 // columns of space on each side of the panel
	hFrame := panelStyle.GetHorizontalFrameSize()
	vFrame := panelStyle.GetVerticalFrameSize()

	// lipgloss Width() includes padding but NOT borders, so subtract
	// the border width from our target to get the correct Width() arg.
	panelWidth := w - panelMargin*2
	borderW := panelStyle.GetBorderLeftSize() + panelStyle.GetBorderRightSize()
	panelContentWidth := panelWidth - hFrame
	widthArg := panelWidth - borderW // Width() excludes borders

	var panels string
	if len(codexCats) > 0 {
		// Stacked: split vertical space evenly between two panels.
		eachHeight := (panelAreaHeight - vFrame*2) / 2

		claudeContent := renderPanel(claudeCats, m.extra, panelContentWidth, eachHeight)
		claudePanel := panelStyle.Width(widthArg).Render(claudeContent)
		claudePanel = embedBorderTitle(claudePanel, "Claude", panelWidth)

		codexContent := renderPanel(codexCats, nil, panelContentWidth, eachHeight)
		codexPanel := panelStyle.Width(widthArg).Render(codexContent)
		codexPanel = embedBorderTitle(codexPanel, "Codex", panelWidth)

		panels = lipgloss.JoinVertical(lipgloss.Left, claudePanel, codexPanel)
	} else {
		claudeContent := renderPanel(claudeCats, m.extra, panelContentWidth, panelAreaHeight-vFrame)
		panels = panelStyle.Width(widthArg).Render(claudeContent)
		panels = embedBorderTitle(panels, "Claude", panelWidth)
	}
	panels = lipgloss.PlaceHorizontal(w, lipgloss.Center, panels)

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
		helpView := m.help.View(m.keys)
		right = dimStyle.Render(updated) + "  " + helpView
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
// The panel title (e.g. "Claude") is rendered in the border by the caller.
func renderPanel(cats []parse.Category, extra *parse.ExtraUsage, width int, maxHeight int) string {
	n := len(cats)
	if n == 0 {
		return normalStyle.Render("No data")
	}

	// Progressive compaction.
	// Base: one bar per category (n lines).
	used := n

	// Step 0: add title lines for all categories (+n lines).
	showTitles := false
	if used+n <= maxHeight {
		showTitles = true
		used += n
	}

	// Step 1: spacing between categories (+n-1 lines).
	showSpacing := false
	if showTitles && used+max(n-1, 0) <= maxHeight {
		showSpacing = true
		used += max(n-1, 0)
	}

	// Extra usage (Claude panel only, shown when space allows).
	showExtra := false
	if extra != nil {
		cost := 1 // bar
		if showTitles {
			cost += 2 // title + dollar subtitle
		}
		if showSpacing {
			cost++
		}
		if used+cost <= maxHeight {
			showExtra = true
		}
	}

	// Build the content lines.
	var lines []string

	for idx, cat := range cats {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)

		if showTitles {
			resetStr := parse.FormatResetTime(cat.ResetsAt)
			lines = append(lines, alignRow(boldStyle.Render(cat.Name), dimStyle.Render(resetStr), width))
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
		if showTitles {
			limitDollars := extra.MonthlyLimit / 100
			usedDollars := extra.UsedCredits / 100
			name := "Extra usage (monthly)"
			resetStr := parse.FormatMonthReset()
			lines = append(lines, alignRow(boldStyle.Render(name), dimStyle.Render(resetStr), width))

			usageStr := fmt.Sprintf("$%.2f / $%.2f", usedDollars, limitDollars)
			lines = append(lines, dimStyle.Render(usageStr))
		}
		lines = append(lines, RenderBar(width, extra.Utilization, parse.CalcMonthGlide()))
	}

	return strings.Join(lines, "\n")
}

// alignRow places left on the left and right on the right within the given
// width, filling the gap with spaces. Both left and right should already be
// styled (the gap uses no styling).
func alignRow(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap > 0 {
		return left + strings.Repeat(" ", gap) + right
	}
	return left
}

// embedBorderTitle replaces the top border line of a rendered panel with one
// that embeds the given title, e.g. ╭─ Claude ──────────────╮
func embedBorderTitle(rendered, title string, width int) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}

	border := lipgloss.RoundedBorder()
	borderSt := lipgloss.NewStyle().Foreground(colorBorder)
	styledTitle := titleStyle.Render(" " + title + " ")
	titleWidth := lipgloss.Width(styledTitle)

	// ╭(1) + ─(1) + styledTitle(titleWidth) + ─×N + ╮(1)
	rightDashes := width - 3 - titleWidth
	if rightDashes < 0 {
		rightDashes = 0
	}

	topLine := borderSt.Render(border.TopLeft+border.Top) +
		styledTitle +
		borderSt.Render(strings.Repeat(border.Top, rightDashes)+border.TopRight)

	lines[0] = topLine
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

