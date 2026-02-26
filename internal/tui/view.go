package tui

import (
	"fmt"
	"image"
	"strings"

	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/parse"
)

// View implements tea.Model. It renders the complete TUI screen.
func (m Model) View() tea.View {
	var content string

	if m.state == stateSleeping {
		content = RenderPlasma(m.width, m.height, m.sleepFrame)
		v := tea.NewView(content)
		v.AltScreen = true
		v.ReportFocus = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	w := m.width
	h := m.height
	if h < 1 || w < 5 {
		v := tea.NewView("")
		v.AltScreen = true
		v.ReportFocus = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	// Build category lookup map.
	catMap := make(map[string]parse.Category, len(m.categories))
	for _, c := range m.categories {
		catMap[c.Key] = c
	}

	// Get ordered, visible category keys for each panel.
	claudeKeys := m.layoutState.orderedCats("claude")
	codexKeys := m.layoutState.orderedCats("codex")

	// Resolve keys to category structs.
	var claudeCats, codexCats []parse.Category
	for _, k := range claudeKeys {
		if c, ok := catMap[k]; ok {
			claudeCats = append(claudeCats, c)
		}
	}
	for _, k := range codexKeys {
		if c, ok := catMap[k]; ok {
			codexCats = append(codexCats, c)
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
		content = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.PlaceVertical(h-1, lipgloss.Center, body),
			statusBar,
		)
		v := tea.NewView(content)
		v.AltScreen = true
		v.ReportFocus = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	// Small terminal fallback — no borders.
	if w < 40 || h < 12 {
		content = m.renderFlat(claudeCats, codexCats, statusBar, errorLine)
		v := tea.NewView(content)
		v.AltScreen = true
		v.ReportFocus = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
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

	panelWidth := w - panelMargin*2
	panelContentWidth := panelWidth - hFrame
	widthArg := panelWidth // v2: Width() includes borders and padding

	// Determine panel rendering order.
	type panelDef struct {
		name  string
		cats  []parse.Category
		extra *parse.ExtraUsage
	}
	var panelDefs []panelDef
	for _, pid := range m.layoutState.panelOrder {
		switch pid {
		case "claude":
			if len(claudeCats) > 0 {
				panelDefs = append(panelDefs, panelDef{"Claude", claudeCats, m.extra})
			}
		case "codex":
			if len(codexCats) > 0 {
				panelDefs = append(panelDefs, panelDef{"Codex", codexCats, nil})
			}
		}
	}

	// Handle all-hidden case.
	if len(panelDefs) == 0 && len(m.categories) > 0 {
		panelDefs = append(panelDefs, panelDef{"headroom", nil, nil})
	}

	var panels string
	var panelBarInfos [][]barLineInfo // per-panel bar line info
	eachHeight := 0
	if len(panelDefs) > 1 {
		eachHeight = (panelAreaHeight - vFrame*2 - 1) / 2
		var parts []string
		for _, pd := range panelDefs {
			var content string
			var barInfos []barLineInfo
			if len(pd.cats) == 0 {
				content = dimStyle.Render("All bars hidden (press 0 to reset)")
			} else {
				content, barInfos = renderPanelWithGeom(pd.cats, pd.extra, panelContentWidth, eachHeight)
			}
			p := panelStyle.Width(widthArg).Render(content)
			p = embedBorderTitle(p, pd.name, panelWidth)
			parts = append(parts, p)
			panelBarInfos = append(panelBarInfos, barInfos)
		}
		panels = lipgloss.JoinVertical(lipgloss.Left, parts[0], "", parts[1])
	} else if len(panelDefs) == 1 {
		pd := panelDefs[0]
		var content string
		var barInfos []barLineInfo
		if len(pd.cats) == 0 {
			content = dimStyle.Render("All bars hidden (press 0 to reset)")
		} else {
			content, barInfos = renderPanelWithGeom(pd.cats, pd.extra, panelContentWidth, panelAreaHeight-vFrame)
		}
		panels = panelStyle.Width(widthArg).Render(content)
		panels = embedBorderTitle(panels, pd.name, panelWidth)
		panelBarInfos = append(panelBarInfos, barInfos)
	} else {
		// No panels at all (no data).
		panels = ""
	}

	// Canvas compositing with background.
	if len(m.bgGrid) > 0 {
		bgContent := renderBackground(m.bgGrid, w, h)

		// Compute panel position for centering.
		panelVisualWidth := 0
		for _, line := range strings.Split(panels, "\n") {
			if strings.TrimSpace(line) != "" {
				pw := lipgloss.Width(line)
				if pw > 0 {
					panelVisualWidth = pw
					break
				}
			}
		}
		panelX := 0
		if panelVisualWidth > 0 && w > panelVisualWidth {
			panelX = (w - panelVisualWidth) / 2
		}

		// Vertical centering of panels in content area.
		panelLines := strings.Split(panels, "\n")
		errorRows := 0
		if errorLine != "" {
			errorRows = 2
		}
		contentRows := h - 1
		remainingRows := contentRows - errorRows
		panelY := errorRows
		if len(panelLines) < remainingRows {
			panelY = errorRows + (remainingRows-len(panelLines))/2
		}

		// Populate layout geometry for hit-testing.
		if m.layout != nil {
			borderTop := panelStyle.GetBorderTopSize()
			paddingTop := panelStyle.GetPaddingTop()
			borderLeft := panelStyle.GetBorderLeftSize()
			paddingLeft := panelStyle.GetPaddingLeft()
			contentOffX := panelX + borderLeft + paddingLeft
			contentOffY := borderTop + paddingTop

			m.layout.claudeBars = nil
			m.layout.codexBars = nil

			if len(panelDefs) > 1 {
				for pi, pd := range panelDefs {
					var pyOff int
					if pi == 0 {
						pyOff = panelY
					} else {
						// Second panel offset: first panel height + 1 gap row
						firstH := eachHeight + vFrame
						pyOff = panelY + firstH + 1
					}

					if pi < len(panelBarInfos) {
						for _, bi := range panelBarInfos[pi] {
							absY := pyOff + contentOffY + bi.relY
							r := image.Rect(contentOffX, absY, contentOffX+panelContentWidth, absY+1)
							bg := barGeom{key: bi.key, bounds: r}
							if pd.name == "Claude" {
								m.layout.claudeBars = append(m.layout.claudeBars, bg)
							} else {
								m.layout.codexBars = append(m.layout.codexBars, bg)
							}
						}
					}

					// Panel bounds.
					pH := eachHeight + vFrame
					pRect := image.Rect(panelX, pyOff, panelX+panelVisualWidth, pyOff+pH)
					if pd.name == "Claude" {
						m.layout.claudePanel = pRect
					} else {
						m.layout.codexPanel = pRect
					}
				}
			} else if len(panelDefs) == 1 {
				pd := panelDefs[0]
				pH := lipgloss.Height(panels)
				pRect := image.Rect(panelX, panelY, panelX+panelVisualWidth, panelY+pH)
				if pd.name == "Claude" {
					m.layout.claudePanel = pRect
				} else {
					m.layout.codexPanel = pRect
				}

				if len(panelBarInfos) > 0 {
					for _, bi := range panelBarInfos[0] {
						absY := panelY + contentOffY + bi.relY
						r := image.Rect(contentOffX, absY, contentOffX+panelContentWidth, absY+1)
						bg := barGeom{key: bi.key, bounds: r}
						if pd.name == "Claude" {
							m.layout.claudeBars = append(m.layout.claudeBars, bg)
						} else {
							m.layout.codexBars = append(m.layout.codexBars, bg)
						}
					}
				}
			}

			m.layout.statusBar = image.Rect(0, h-1, w, h)
		}

		// Build layers via Compositor (handles X/Y/Z positioning).
		bgLayer := lipgloss.NewLayer(bgContent).Z(0).ID("bg")
		panelLayer := lipgloss.NewLayer(panels).X(panelX).Y(panelY).Z(1).ID("panels")
		statusLayer := lipgloss.NewLayer(statusBar).X(0).Y(h - 1).Z(2).ID("status")

		comp := lipgloss.NewCompositor(bgLayer, panelLayer, statusLayer)

		if errorLine != "" {
			errorLayer := lipgloss.NewLayer(errorLine).X(0).Y(0).Z(2).ID("error")
			comp.AddLayers(errorLayer)
		}

		// Ghost layer during active drag.
		if m.drag.phase == dragActive {
			var ghostLabel string
			switch m.drag.target {
			case dragTargetPanel:
				ghostLabel = m.drag.panelID
			case dragTargetBar:
				ghostLabel = m.drag.barKey
			}
			if ghostLabel != "" {
				ghost := dimStyle.Render(" " + ghostLabel + " ")
				ghostLayer := lipgloss.NewLayer(ghost).
					X(m.drag.currX).Y(m.drag.currY).Z(10).ID("ghost")
				comp.AddLayers(ghostLayer)
			}
		}

		content = comp.Render()
	} else {
		// Fallback: no background grid yet.
		panels = lipgloss.PlaceHorizontal(w, lipgloss.Center, panels)

		var sections []string
		if errorLine != "" {
			sections = append(sections, errorLine, "")
		}
		sections = append(sections, panels)

		body := lipgloss.JoinVertical(lipgloss.Left, sections...)

		bodyHeight := lipgloss.Height(body)
		if bodyHeight < h-1 {
			body += strings.Repeat("\n", h-1-bodyHeight)
		}

		content = body + "\n" + statusBar
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderStatusBar builds the full-width status bar displayed at the bottom of
// the screen. Every character uses an explicit background so there are no
// unstyled gaps between styled segments.
func (m Model) renderStatusBar() string {
	brand := statusBrandStyle.Render(" headroom ")

	var right string
	if m.inputMode == inputInterval {
		right = helpKeyStyle.Render("Interval (seconds): ") + helpDescStyle.Render(m.inputBuf+"_ ")
	} else {
		updated := parse.FormatUpdatedAgo(m.lastFetchTime)
		right = helpDescStyle.Render(updated+"  ") + renderHelp(m.keys) + helpDescStyle.Render(" ")
	}

	brandWidth := lipgloss.Width(brand)
	rightWidth := lipgloss.Width(right)
	gapWidth := m.width - brandWidth - rightWidth
	if gapWidth < 0 {
		gapWidth = 0
	}
	gap := statusBarStyle.Render(strings.Repeat(" ", gapWidth))

	return brand + gap + right
}

// renderHelp builds the key-binding help text with every character (including
// spaces) inside a styled Render call so the status bar background is
// continuous. This replaces bubbles/help.View which leaves unstyled gaps.
func renderHelp(k keyMap) string {
	bindings := k.ShortHelp()
	var parts []string
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts, helpKeyStyle.Render(h.Key)+helpDescStyle.Render(" "+h.Desc))
	}
	return strings.Join(parts, helpSepStyle.Render(" • "))
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
			cost++ // title
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
			name := "Extra usage"
			rightStr := fmt.Sprintf("$%.2f / $%.2f  %s", usedDollars, limitDollars, parse.FormatMonthReset())
			lines = append(lines, alignRow(boldStyle.Render(name), dimStyle.Render(rightStr), width))
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

// barLineInfo records which line within a panel's content area corresponds
// to a bar, for hit-testing.
type barLineInfo struct {
	key  string
	relY int // line offset within panel content
}

// renderPanelWithGeom is like renderPanel but also returns the relative line
// index of each bar within the panel content area.
func renderPanelWithGeom(cats []parse.Category, extra *parse.ExtraUsage, width int, maxHeight int) (string, []barLineInfo) {
	n := len(cats)
	if n == 0 {
		return normalStyle.Render("No data"), nil
	}

	// Progressive compaction (same as renderPanel).
	used := n
	showTitles := false
	if used+n <= maxHeight {
		showTitles = true
		used += n
	}
	showSpacing := false
	if showTitles && used+max(n-1, 0) <= maxHeight {
		showSpacing = true
		used += max(n-1, 0)
	}
	showExtra := false
	if extra != nil {
		cost := 1
		if showTitles {
			cost++
		}
		if showSpacing {
			cost++
		}
		if used+cost <= maxHeight {
			showExtra = true
		}
	}

	var lines []string
	var barInfos []barLineInfo
	lineIdx := 0

	for idx, cat := range cats {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)

		if showTitles {
			resetStr := parse.FormatResetTime(cat.ResetsAt)
			lines = append(lines, alignRow(boldStyle.Render(cat.Name), dimStyle.Render(resetStr), width))
			lineIdx++
		}

		lines = append(lines, RenderBar(width, usage, glide))
		barInfos = append(barInfos, barLineInfo{key: cat.Key, relY: lineIdx})
		lineIdx++

		if showSpacing && idx < n-1 {
			lines = append(lines, "")
			lineIdx++
		}
	}

	if showExtra {
		if showSpacing {
			lines = append(lines, "")
			lineIdx++
		}
		if showTitles {
			limitDollars := extra.MonthlyLimit / 100
			usedDollars := extra.UsedCredits / 100
			name := "Extra usage"
			rightStr := fmt.Sprintf("$%.2f / $%.2f  %s", usedDollars, limitDollars, parse.FormatMonthReset())
			lines = append(lines, alignRow(boldStyle.Render(name), dimStyle.Render(rightStr), width))
			lineIdx++
		}
		lines = append(lines, RenderBar(width, extra.Utilization, parse.CalcMonthGlide()))
		barInfos = append(barInfos, barLineInfo{key: "extra_usage", relY: lineIdx})
		lineIdx++
	}

	return strings.Join(lines, "\n"), barInfos
}
