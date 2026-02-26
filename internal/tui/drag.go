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
	dragTargetBar              // grabbed a bar row
)

// dragState holds all state for an in-progress drag.
type dragState struct {
	phase  dragPhase
	target dragTarget

	// What was grabbed.
	panelID string // "claude" or "codex" (for panel drag)
	barKey  string // category key (for bar drag)

	// Mouse positions.
	startX, startY int // initial click position
	currX, currY   int // current mouse position
}

const dragThreshold = 2 // pixels of movement before drag activates

// handleMouseDown processes a mouse click. It performs hit-testing against
// stored layout geometry and starts a pending drag if something was hit.
func (m Model) handleMouseDown(msg tea.MouseClickMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}

	x, y := mouse.X, mouse.Y

	// Hit-test bars first (more specific), then panels.
	if m.layout != nil {
		// Check Claude bars.
		for _, bg := range m.layout.claudeBars {
			if image.Pt(x, y).In(bg.bounds) {
				m.drag = dragState{
					phase:  dragPending,
					target: dragTargetBar,
					barKey: bg.key,
					startX: x, startY: y,
					currX: x, currY: y,
				}
				return m, nil
			}
		}
		// Check Codex bars.
		for _, bg := range m.layout.codexBars {
			if image.Pt(x, y).In(bg.bounds) {
				m.drag = dragState{
					phase:  dragPending,
					target: dragTargetBar,
					barKey: bg.key,
					startX: x, startY: y,
					currX: x, currY: y,
				}
				return m, nil
			}
		}
		// Check panels.
		if image.Pt(x, y).In(m.layout.claudePanel) {
			m.drag = dragState{
				phase:   dragPending,
				target:  dragTargetPanel,
				panelID: "claude",
				startX:  x, startY: y,
				currX: x, currY: y,
			}
			return m, nil
		}
		if image.Pt(x, y).In(m.layout.codexPanel) {
			m.drag = dragState{
				phase:   dragPending,
				target:  dragTargetPanel,
				panelID: "codex",
				startX:  x, startY: y,
				currX: x, currY: y,
			}
			return m, nil
		}
	}

	return m, nil
}

// handleMouseMove processes mouse motion. Promotes pending → active if the
// movement threshold is exceeded.
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

	return m, nil
}

// handleMouseUp processes mouse release. Applies the drag action if active,
// or cancels if still pending (click with no drag).
func (m Model) handleMouseUp(msg tea.MouseReleaseMsg) (Model, tea.Cmd) {
	if m.drag.phase == dragIdle {
		return m, nil
	}

	defer func() { m.drag = dragState{} }()

	if m.drag.phase == dragPending {
		// Click without enough movement — no-op.
		m.drag = dragState{}
		return m, nil
	}

	// Active drag — apply action.
	mouse := msg.Mouse()
	m.drag.currX = mouse.X
	m.drag.currY = mouse.Y

	switch m.drag.target {
	case dragTargetPanel:
		m = m.applyPanelDrop()
	case dragTargetBar:
		m = m.applyBarDrop()
	}

	m.drag = dragState{}
	return m, nil
}

// applyPanelDrop swaps panel order if the panel was dragged past the midpoint.
func (m Model) applyPanelDrop() Model {
	if len(m.layoutState.panelOrder) < 2 {
		return m
	}

	if m.layout == nil {
		return m
	}

	// Find the other panel's bounds.
	var otherBounds image.Rectangle
	switch m.drag.panelID {
	case "claude":
		otherBounds = m.layout.codexPanel
	case "codex":
		otherBounds = m.layout.claudePanel
	}

	// If cursor is within the other panel's vertical range, swap.
	midY := (otherBounds.Min.Y + otherBounds.Max.Y) / 2
	if m.drag.panelID == m.layoutState.panelOrder[0] {
		// Top panel dragged down — swap if past midpoint of bottom panel.
		if m.drag.currY >= midY {
			m.layoutState.panelOrder[0], m.layoutState.panelOrder[1] =
				m.layoutState.panelOrder[1], m.layoutState.panelOrder[0]
		}
	} else {
		// Bottom panel dragged up — swap if above midpoint of top panel.
		if m.drag.currY <= midY {
			m.layoutState.panelOrder[0], m.layoutState.panelOrder[1] =
				m.layoutState.panelOrder[1], m.layoutState.panelOrder[0]
		}
	}

	return m
}

// applyBarDrop reorders a bar within its panel or hides it if dragged off-screen.
func (m Model) applyBarDrop() Model {
	// Drag off screen edge → hide.
	if m.drag.currX < 0 || m.drag.currX >= m.width {
		m.layoutState.hidden[m.drag.barKey] = true
		return m
	}

	// Find which panel and position within the order.
	var order *[]string
	var bars []barGeom
	if m.layout != nil {
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
	}

	if order == nil || len(bars) < 2 {
		return m
	}

	// Find the target position based on cursor Y.
	targetIdx := -1
	for i, bg := range bars {
		midY := (bg.bounds.Min.Y + bg.bounds.Max.Y) / 2
		if m.drag.currY <= midY {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		targetIdx = len(bars) - 1
	}

	// Find current index in the order slice.
	srcIdx := -1
	for i, k := range *order {
		if k == m.drag.barKey {
			srcIdx = i
			break
		}
	}

	if srcIdx < 0 || srcIdx == targetIdx {
		return m
	}

	// Move the element.
	key := (*order)[srcIdx]
	*order = append((*order)[:srcIdx], (*order)[srcIdx+1:]...)
	// Insert at target position (adjust if src was before target).
	if targetIdx > srcIdx {
		targetIdx--
	}
	if targetIdx > len(*order) {
		targetIdx = len(*order)
	}
	*order = append((*order)[:targetIdx], append([]string{key}, (*order)[targetIdx:]...)...)

	return m
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
