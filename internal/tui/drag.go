package tui

import (
	"image"

	tea "charm.land/bubbletea/v2"
)

// dragPhase tracks the current phase of a drag interaction.
type dragPhase int

const (
	dragIdle    dragPhase = iota
	dragPending           // mousedown received, waiting for movement threshold
	dragActive            // dragging — past movement threshold
)

// dragTarget identifies what was grabbed.
type dragTarget int

const (
	dragTargetNone  dragTarget = iota
	dragTargetPanel            // grabbed a panel border/title
	dragTargetBar              // grabbed a category (title + bar region)
)

// dragState holds all state for an in-progress drag.
type dragState struct {
	phase  dragPhase
	target dragTarget

	// What was grabbed.
	panelID    string // "claude" or "codex" (for panel drag)
	barKey     string // category key (for bar drag)
	ghostLabel string // display name for the ghost indicator

	// Mouse positions.
	startX, startY int // initial click position
	currX, currY   int // current mouse position
}

const dragThreshold = 2 // cells of movement before drag activates

// handleMouseDown processes a mouse click. It performs hit-testing against
// stored layout geometry and starts a pending drag if something was hit.
func (m Model) handleMouseDown(msg tea.MouseClickMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}

	x, y := mouse.X, mouse.Y

	if m.layout == nil {
		return m, nil
	}

	// Hit-test category regions first (more specific), then panels.
	// Check Claude categories.
	for _, bg := range m.layout.claudeBars {
		if image.Pt(x, y).In(bg.bounds) {
			m.drag = dragState{
				phase:      dragPending,
				target:     dragTargetBar,
				barKey:     bg.key,
				ghostLabel: m.catDisplayName(bg.key),
				startX:     x, startY: y,
				currX: x, currY: y,
			}
			return m, nil
		}
	}
	// Check Codex categories.
	for _, bg := range m.layout.codexBars {
		if image.Pt(x, y).In(bg.bounds) {
			m.drag = dragState{
				phase:      dragPending,
				target:     dragTargetBar,
				barKey:     bg.key,
				ghostLabel: m.catDisplayName(bg.key),
				startX:     x, startY: y,
				currX: x, currY: y,
			}
			return m, nil
		}
	}
	// Check panels (border/empty space — anything not a bar).
	if image.Pt(x, y).In(m.layout.claudePanel) {
		m.drag = dragState{
			phase:      dragPending,
			target:     dragTargetPanel,
			panelID:    "claude",
			ghostLabel: "Claude",
			startX:     x, startY: y,
			currX: x, currY: y,
		}
		return m, nil
	}
	if image.Pt(x, y).In(m.layout.codexPanel) {
		m.drag = dragState{
			phase:      dragPending,
			target:     dragTargetPanel,
			panelID:    "codex",
			ghostLabel: "Codex",
			startX:     x, startY: y,
			currX: x, currY: y,
		}
		return m, nil
	}

	return m, nil
}

// handleMouseMove processes mouse motion. Promotes pending → active if the
// movement threshold is exceeded, then live-reorders items as the cursor moves.
func (m Model) handleMouseMove(msg tea.MouseMotionMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	if m.drag.phase == dragIdle {
		return m, nil
	}

	m.drag.currX = mouse.X
	m.drag.currY = mouse.Y

	if m.drag.phase == dragPending {
		dx := m.drag.currX - m.drag.startX
		dy := m.drag.currY - m.drag.startY
		if abs(dx) >= dragThreshold || abs(dy) >= dragThreshold {
			m.drag.phase = dragActive
		}
	}

	// Live reorder during active drag.
	if m.drag.phase == dragActive && m.layout != nil {
		switch m.drag.target {
		case dragTargetBar:
			m.liveReorderBar()
		case dragTargetPanel:
			m.liveReorderPanel()
		}
	}

	return m, nil
}

// liveReorderBar moves the dragged bar to the slot under the cursor,
// causing other bars to shift like phone app icons. The target slot is
// computed by counting non-dragged items whose midpoint is above the
// cursor — this avoids oscillation from the dragged item's own geometry.
func (m *Model) liveReorderBar() {
	// Find which panel owns the dragged bar.
	var order *[]string
	var bars []barGeom
	for _, bg := range m.layout.claudeBars {
		if bg.key == m.drag.barKey {
			order = &m.layoutState.claudeCatOrder
			bars = m.layout.claudeBars
			break
		}
	}
	if order == nil {
		for _, bg := range m.layout.codexBars {
			if bg.key == m.drag.barKey {
				order = &m.layoutState.codexCatOrder
				bars = m.layout.codexBars
				break
			}
		}
	}

	if order == nil || len(bars) < 2 {
		return
	}

	// Count how many non-dragged items have their midpoint above the cursor.
	// This gives the insertion slot without interference from the dragged
	// item's own geometry.
	slot := 0
	for _, bg := range bars {
		if bg.key == m.drag.barKey {
			continue
		}
		midY := (bg.bounds.Min.Y + bg.bounds.Max.Y) / 2
		if m.drag.currY >= midY {
			slot++
		}
	}

	newOrder := moveToSlot(*order, m.layoutState.hidden, m.drag.barKey, slot)
	if newOrder != nil {
		*order = newOrder
	}
}

// liveReorderPanel swaps panel order when the cursor crosses the midpoint
// of the total panel area.
func (m *Model) liveReorderPanel() {
	if len(m.layoutState.panelOrder) < 2 {
		return
	}

	// Compute midpoint of the total panel area (stable regardless of order).
	areaMinY := min(m.layout.claudePanel.Min.Y, m.layout.codexPanel.Min.Y)
	areaMaxY := max(m.layout.claudePanel.Max.Y, m.layout.codexPanel.Max.Y)
	midY := (areaMinY + areaMaxY) / 2

	// Find current position of the dragged panel.
	currentIdx := 0
	for i, pid := range m.layoutState.panelOrder {
		if pid == m.drag.panelID {
			currentIdx = i
			break
		}
	}

	if currentIdx == 0 && m.drag.currY >= midY {
		// Top panel dragged to or below midpoint → swap.
		m.layoutState.panelOrder[0], m.layoutState.panelOrder[1] =
			m.layoutState.panelOrder[1], m.layoutState.panelOrder[0]
	} else if currentIdx == 1 && m.drag.currY < midY {
		// Bottom panel dragged above midpoint → swap.
		m.layoutState.panelOrder[0], m.layoutState.panelOrder[1] =
			m.layoutState.panelOrder[1], m.layoutState.panelOrder[0]
	}
}

// handleMouseUp processes mouse release. Reordering already happened live
// during drag; this just handles hide-on-release and clears drag state.
func (m Model) handleMouseUp(msg tea.MouseReleaseMsg) (Model, tea.Cmd) {
	if m.drag.phase == dragIdle {
		return m, nil
	}

	// Bar dragged to screen edge → hide. Terminal mouse coords are clamped
	// to [0, width-1], so we use a small threshold from the edges.
	if m.drag.phase == dragActive && m.drag.target == dragTargetBar {
		mouse := msg.Mouse()
		if mouse.X <= 1 || mouse.X >= m.width-2 {
			m.layoutState.hidden[m.drag.barKey] = true
		}
	}

	m.drag = dragState{}
	return m, nil
}

// catDisplayName looks up the display name for a category key.
func (m Model) catDisplayName(key string) string {
	for _, c := range m.categories {
		if c.Key == key {
			return c.Name
		}
	}
	return key
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
