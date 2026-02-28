package tui

import (
	"fmt"
	"image"
	"strings"

	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
)

// windowTitle is the terminal window title in vaporwave full-width characters.
const windowTitle = "ｈｅａｄｒｏｏｍ"

// newView creates a tea.View with the standard settings for headroom.
func newView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = windowTitle
	return v
}

// View implements tea.Model. It renders the complete TUI screen.
func (m Model) View() tea.View {
	var content string

	if m.state == stateSleeping {
		content = RenderPlasma(m.width, m.height, m.sleepFrame, "press any key to wake")
		return newView(content)
	}

	if m.state == stateLoading {
		var fadeT float64
		if m.sleepFrame >= 20 {
			fadeT = 1.0
		} else {
			fadeT = float64(m.sleepFrame) / 20.0
		}
		content = RenderLoadingFrame(m.bgGrid, m.width, m.height, m.sleepFrame, fadeT)
		return newView(content)
	}

	w := m.width
	h := m.height
	if h < 1 || w < 5 {
		return newView("")
	}

	// Build category lookup map.
	catMap := make(map[string]parse.Category, len(m.categories))
	for _, c := range m.categories {
		catMap[c.Key] = c
	}

	// Get ordered, visible category keys and resolved structs for each panel.
	type panelCats struct {
		pid  string
		cats []parse.Category
	}
	var allPanelCats []panelCats
	for _, pid := range m.layoutState.panelOrder {
		keys := m.layoutState.orderedCats(pid)
		var cats []parse.Category
		for _, k := range keys {
			if c, ok := catMap[k]; ok {
				cats = append(cats, c)
			}
		}
		allPanelCats = append(allPanelCats, panelCats{pid, cats})
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
		return newView(content)
	}

	// Small terminal fallback - no borders.
	if w < 40 || h < 12 {
		var flatCats []parse.Category
		for _, pc := range allPanelCats {
			flatCats = append(flatCats, pc.cats...)
		}
		content = m.renderFlatGeneric(flatCats, statusBar, errorLine)
		return newView(content)
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

	// Build animation callback for the loading->running bar sweep phase.
	animFn := m.anim.buildAnimFunc(m.sleepFrame)

	// Determine panel rendering order.
	type panelDef struct {
		pid   string
		name  string
		cats  []parse.Category
		extra *parse.ExtraUsage
	}
	var panelDefs []panelDef
	for _, pc := range allPanelCats {
		if len(pc.cats) > 0 {
			p := provider.ByID(pc.pid)
			displayName := pc.pid
			if p != nil {
				displayName = p.DisplayName
			}
			var extra *parse.ExtraUsage
			if e := m.providerExtra[pc.pid]; e != nil && !m.layoutState.hidden["extra_usage"] {
				extra = e
			}
			panelDefs = append(panelDefs, panelDef{pc.pid, displayName, pc.cats, extra})
		}
	}

	// Handle all-hidden case.
	if len(panelDefs) == 0 && len(m.categories) > 0 {
		panelDefs = append(panelDefs, panelDef{"headroom", "headroom", nil, nil})
	}

	var panels string
	var panelBarInfos [][]barLineInfo // per-panel bar line info
	var panelPIDs []string            // provider IDs in render order
	var partHeights []int             // actual rendered height of each panel
	eachHeight := 0
	if len(panelDefs) > 1 {
		n := len(panelDefs)
		gaps := n - 1
		eachHeight = (panelAreaHeight - vFrame*n - gaps) / n
		var parts []string
		for _, pd := range panelDefs {
			var content string
			var barInfos []barLineInfo
			if len(pd.cats) == 0 {
				content = dimStyle.Render("All bars hidden - press ") + titleStyle.Render("0") + dimStyle.Render(" to restore")
			} else {
				content, barInfos = renderPanelWithGeom(pd.cats, pd.extra, panelContentWidth, eachHeight, animFn)
			}
			p := panelStyle.Width(widthArg).Render(content)
			p = embedBorderTitle(p, pd.name, panelWidth)
			parts = append(parts, p)
			partHeights = append(partHeights, lipgloss.Height(p))
			panelBarInfos = append(panelBarInfos, barInfos)
			panelPIDs = append(panelPIDs, pd.pid)
		}
		// Join all panels with a blank-line gap between each pair.
		var joinArgs []string
		for i, p := range parts {
			if i > 0 {
				joinArgs = append(joinArgs, "")
			}
			joinArgs = append(joinArgs, p)
		}
		panels = lipgloss.JoinVertical(lipgloss.Left, joinArgs...)
	} else if len(panelDefs) == 1 {
		pd := panelDefs[0]
		var content string
		var barInfos []barLineInfo
		if len(pd.cats) == 0 {
			content = dimStyle.Render("All bars hidden - press ") + titleStyle.Render("0") + dimStyle.Render(" to restore")
		} else {
			content, barInfos = renderPanelWithGeom(pd.cats, pd.extra, panelContentWidth, panelAreaHeight-vFrame, animFn)
		}
		panels = panelStyle.Width(widthArg).Render(content)
		panels = embedBorderTitle(panels, pd.name, panelWidth)
		panelBarInfos = append(panelBarInfos, barInfos)
		panelPIDs = append(panelPIDs, pd.pid)
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

			// Reset bar geometry for all panels.
			for pid := range m.layout.bars {
				m.layout.bars[pid] = nil
			}

			if len(panelDefs) > 1 {
				for pi := range panelDefs {
					pid := panelPIDs[pi]
					// Sum all preceding panel heights + 1 gap row each.
					pyOff := panelY
					for j := 0; j < pi; j++ {
						pyOff += partHeights[j] + 1
					}

					if pi < len(panelBarInfos) {
						for _, bi := range panelBarInfos[pi] {
							absY := pyOff + contentOffY + bi.relY
							r := image.Rect(contentOffX, absY, contentOffX+panelContentWidth, absY+bi.height)
							bg := barGeom{key: bi.key, bounds: r, pinned: bi.pinned}
							m.layout.bars[pid] = append(m.layout.bars[pid], bg)
						}
					}

					// Panel bounds (use actual rendered height).
					pH := partHeights[pi]
					pRect := image.Rect(panelX, pyOff, panelX+panelVisualWidth, pyOff+pH)
					m.layout.panels[pid] = pRect
				}
			} else if len(panelDefs) == 1 {
				pid := panelPIDs[0]
				pH := lipgloss.Height(panels)
				pRect := image.Rect(panelX, panelY, panelX+panelVisualWidth, panelY+pH)
				m.layout.panels[pid] = pRect

				if len(panelBarInfos) > 0 {
					for _, bi := range panelBarInfos[0] {
						absY := panelY + contentOffY + bi.relY
						r := image.Rect(contentOffX, absY, contentOffX+panelContentWidth, absY+bi.height)
						bg := barGeom{key: bi.key, bounds: r, pinned: bi.pinned}
						m.layout.bars[pid] = append(m.layout.bars[pid], bg)
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

		// Ghost layer during active drag - bordered card following the cursor.
		if m.drag.phase == dragActive && m.drag.ghostLabel != "" {
			ghostStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDim).
				Foreground(colorNormal).
				Padding(0, 1)
			ghost := ghostStyle.Render(m.drag.ghostLabel)
			ghostW := lipgloss.Width(ghost)
			ghostX := m.drag.currX - ghostW/2
			if ghostX < 0 {
				ghostX = 0
			}
			ghostLayer := lipgloss.NewLayer(ghost).
				X(ghostX).Y(m.drag.currY).Z(10).ID("ghost")
			comp.AddLayers(ghostLayer)
		}

		// Trash zone layer during active drag.
		if m.drag.phase == dragActive {
			tz := trashZoneRect(w, h)
			m.layout.trashZone = tz
			hovering := image.Pt(m.drag.currX, m.drag.currY).In(tz)
			trashContent := renderTrashZone(hovering)
			trashLayer := lipgloss.NewLayer(trashContent).
				X(tz.Min.X).Y(tz.Min.Y).Z(5).ID("trash")
			comp.AddLayers(trashLayer)
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

	return newView(content)
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
		updated := parse.FormatUpdatedAgo(m.sched.lastFetchTime)
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

// renderFlatGeneric renders a simplified layout with no borders, used when the
// terminal is too small to fit the full panel layout (width < 40 or height < 12).
func (m Model) renderFlatGeneric(allCats []parse.Category, statusBar, errorLine string) string {
	w := m.width
	h := m.height

	var lines []string
	if errorLine != "" {
		lines = append(lines, errorLine)
	}

	for _, cat := range allCats {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		lines = append(lines, RenderBar(w, usage, glide, 1.0))
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

// barLineInfo records the region within a panel's content area that
// corresponds to a category (title line + bar line), for hit-testing.
type barLineInfo struct {
	key    string
	relY   int  // first line of this category region (title or bar)
	height int  // number of lines this region spans
	pinned bool // true = absorbs clicks but can't be dragged
}

// panelCompaction holds the progressive compaction decisions for a panel.
// It is a pure value computed from (numCats, hasExtra, maxHeight).
type panelCompaction struct {
	showTitles  bool
	showSpacing bool
	showExtra   bool
}

// computeCompaction determines which visual elements fit within maxHeight rows.
// numCats is the number of category bars; hasExtra indicates whether an extra
// usage bar is available; maxHeight is the maximum number of content lines.
func computeCompaction(numCats int, hasExtra bool, maxHeight int) panelCompaction {
	used := numCats

	showTitles := false
	if used+numCats <= maxHeight {
		showTitles = true
		used += numCats
	}

	showSpacing := false
	if showTitles && used+max(numCats-1, 0) <= maxHeight {
		showSpacing = true
		used += max(numCats-1, 0)
	}

	showExtra := false
	if hasExtra {
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

	return panelCompaction{showTitles, showSpacing, showExtra}
}

// renderPanelWithGeom builds the content string for a single bordered panel
// and returns the relative line positions of each bar for hit-testing.
// animFn, if non-nil, overrides bar values for the sweep animation.
func renderPanelWithGeom(cats []parse.Category, extra *parse.ExtraUsage, width int, maxHeight int, animFn barAnimFunc) (string, []barLineInfo) {
	n := len(cats)
	if n == 0 {
		return normalStyle.Render("No data"), nil
	}

	c := computeCompaction(n, extra != nil, maxHeight)

	var lines []string
	var barInfos []barLineInfo
	lineIdx := 0

	for idx, cat := range cats {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)

		catStartY := lineIdx
		if c.showTitles {
			resetStr := parse.FormatResetTime(cat.ResetsAt)
			lines = append(lines, alignRow(boldStyle.Render(cat.Name), dimStyle.Render(resetStr), width))
			lineIdx++
		}

		animUsage, animGlide, opacity := usage, glide, 1.0
		if animFn != nil {
			animUsage, animGlide, opacity = animFn(cat.Key, usage, glide)
		}
		lines = append(lines, RenderBar(width, animUsage, animGlide, opacity))
		lineIdx++
		barInfos = append(barInfos, barLineInfo{key: cat.Key, relY: catStartY, height: lineIdx - catStartY})

		if c.showSpacing && idx < n-1 {
			lines = append(lines, "")
			lineIdx++
		}
	}

	if c.showExtra {
		if c.showSpacing {
			lines = append(lines, "")
			lineIdx++
		}
		extraStartY := lineIdx
		if c.showTitles {
			limitDollars := extra.MonthlyLimit / 100
			usedDollars := extra.UsedCredits / 100
			name := "Extra usage"
			rightStr := fmt.Sprintf("$%.2f / $%.2f  %s", usedDollars, limitDollars, parse.FormatMonthReset())
			lines = append(lines, alignRow(boldStyle.Render(name), dimStyle.Render(rightStr), width))
			lineIdx++
		}
		extraUsage := extra.Utilization
		extraGlide := parse.CalcMonthGlide()
		extraOpacity := 1.0
		if animFn != nil {
			extraUsage, extraGlide, extraOpacity = animFn("extra_usage", extraUsage, extraGlide)
		}
		lines = append(lines, RenderBar(width, extraUsage, extraGlide, extraOpacity))
		lineIdx++
		barInfos = append(barInfos, barLineInfo{key: "extra_usage", relY: extraStartY, height: lineIdx - extraStartY})
	}

	return strings.Join(lines, "\n"), barInfos
}

// renderTrashZone renders a block-pixel trashcan icon.
// When hovering is true, it renders in red; otherwise dim lavender.
func renderTrashZone(hovering bool) string {
	fg := colorDim
	if hovering {
		fg = colorError
	}
	s := lipgloss.NewStyle().Foreground(fg)

	//     ▄▄▄        flat knob handle
	//   ▄█████▄      lid (overhangs body)
	//   ▐█████▌      lid rim
	//    █▌█▌█       body with ridges
	//    █▌█▌█       body with ridges
	//    █▌█▌█       body with ridges
	//    ▀▀▀▀▀       base
	lines := []string{
		"    " + s.Render("▄▄▄"),
		"  " + s.Render("▄█████▄"),
		"  " + s.Render("▐█████▌"),
		"   " + s.Render("█▌█▌█"),
		"   " + s.Render("█▌█▌█"),
		"   " + s.Render("█▌█▌█"),
		"   " + s.Render("▀▀▀▀▀"),
	}
	return strings.Join(lines, "\n")
}
