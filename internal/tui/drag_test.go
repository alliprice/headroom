package tui

import (
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/parse"
)

// ═══════════════════════════════════════════════════════════════════════════
// Pure function tests
// ═══════════════════════════════════════════════════════════════════════════

func TestMoveToSlot(t *testing.T) {
	tests := []struct {
		name     string
		order    []string
		hidden   map[string]bool
		dragKey  string
		slot     int
		expected []string
		wantNil  bool
	}{
		{
			name:     "Forward: drag first to last",
			order:    []string{"a", "b", "c"},
			hidden:   map[string]bool{},
			dragKey:  "a",
			slot:     2,
			expected: []string{"b", "c", "a"},
		},
		{
			name:     "Backward: drag last to first",
			order:    []string{"a", "b", "c"},
			hidden:   map[string]bool{},
			dragKey:  "c",
			slot:     0,
			expected: []string{"c", "a", "b"},
		},
		{
			name:     "Same slot returns nil",
			order:    []string{"a", "b", "c"},
			hidden:   map[string]bool{},
			dragKey:  "a",
			slot:     0,
			wantNil:  true,
		},
		{
			name:     "Slot beyond bounds clamped",
			order:    []string{"a", "b", "c"},
			hidden:   map[string]bool{},
			dragKey:  "a",
			slot:     10,
			expected: []string{"b", "c", "a"},
		},
		{
			name:     "Negative slot clamped to 0",
			order:    []string{"a", "b", "c"},
			hidden:   map[string]bool{},
			dragKey:  "c",
			slot:     -5,
			expected: []string{"c", "a", "b"},
		},
		{
			name:     "Single item returns nil",
			order:    []string{"a"},
			hidden:   map[string]bool{},
			dragKey:  "a",
			slot:     0,
			wantNil:  true,
		},
		{
			name:     "Hidden entries preserved",
			order:    []string{"a", "hidden", "b", "c"},
			hidden:   map[string]bool{"hidden": true},
			dragKey:  "a",
			slot:     2,
			expected: []string{"b", "hidden", "c", "a"},
		},
		{
			name:    "dragKey not found returns nil",
			order:   []string{"a", "b", "c"},
			hidden:  map[string]bool{},
			dragKey: "x",
			wantNil: true,
		},
		{
			name:    "Empty order returns nil",
			order:   []string{},
			hidden:  map[string]bool{},
			dragKey: "a",
			wantNil: true,
		},
		{
			name:     "Middle to first",
			order:    []string{"a", "b", "c"},
			hidden:   map[string]bool{},
			dragKey:  "b",
			slot:     0,
			expected: []string{"b", "a", "c"},
		},
		{
			name:     "Middle to last",
			order:    []string{"a", "b", "c"},
			hidden:   map[string]bool{},
			dragKey:  "b",
			slot:     2,
			expected: []string{"a", "c", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := moveToSlot(tt.order, tt.hidden, tt.dragKey, tt.slot)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected non-nil result, got nil")
			}
			if len(result) != len(tt.expected) {
				t.Fatalf("length mismatch: expected %d, got %d", len(tt.expected), len(result))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expected[i], result[i])
				}
			}
		})
	}
}

func TestTrashZoneRect(t *testing.T) {
	tests := []struct {
		name     string
		w, h     int
		expected image.Rectangle
	}{
		{
			name:     "Standard terminal 80x24",
			w:        80,
			h:        24,
			expected: image.Rect(67, 14, 78, 21),
		},
		{
			name:     "Large terminal 120x40",
			w:        120,
			h:        40,
			expected: image.Rect(107, 30, 118, 37),
		},
		{
			name: "Small terminal (may have negative coords)",
			w:    5,
			h:    5,
			// x1 = 5 - 11 - 2 = -8
			// y1 = 5 - 1 - 7 - 2 = -5
			expected: image.Rect(-8, -5, 3, 2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trashZoneRect(tt.w, tt.h)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHideAllBarsInPanel(t *testing.T) {
	tests := []struct {
		name         string
		panelID      string
		claudeCats   []string
		codexCats    []string
		initialHide  map[string]bool
		expectedHide map[string]bool
	}{
		{
			name:       "Hide all Claude bars",
			panelID:    "claude",
			claudeCats: []string{"five_hour", "seven_day"},
			codexCats:  []string{"codex_primary"},
			initialHide: map[string]bool{
				"codex_primary": true,
			},
			expectedHide: map[string]bool{
				"five_hour":     true,
				"seven_day":     true,
				"codex_primary": true,
			},
		},
		{
			name:       "Hide all Codex bars",
			panelID:    "codex",
			claudeCats: []string{"five_hour"},
			codexCats:  []string{"codex_primary", "codex_secondary"},
			initialHide: map[string]bool{
				"five_hour": true,
			},
			expectedHide: map[string]bool{
				"five_hour":        true,
				"codex_primary":    true,
				"codex_secondary":  true,
			},
		},
		{
			name:       "Unknown panel is no-op",
			panelID:    "unknown",
			claudeCats: []string{"five_hour"},
			codexCats:  []string{"codex_primary"},
			initialHide: map[string]bool{
				"five_hour": true,
			},
			expectedHide: map[string]bool{
				"five_hour": true,
			},
		},
		{
			name:       "Already-hidden bars stay hidden",
			panelID:    "claude",
			claudeCats: []string{"five_hour", "seven_day"},
			codexCats:  []string{},
			initialHide: map[string]bool{
				"five_hour": true,
			},
			expectedHide: map[string]bool{
				"five_hour": true,
				"seven_day": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ls := layoutState{
				claudeCatOrder: tt.claudeCats,
				codexCatOrder:  tt.codexCats,
				hidden:         tt.initialHide,
			}
			ls.hideAllBarsInPanel(tt.panelID)
			if len(ls.hidden) != len(tt.expectedHide) {
				t.Fatalf("hidden map size mismatch: expected %d, got %d", len(tt.expectedHide), len(ls.hidden))
			}
			for k, v := range tt.expectedHide {
				if ls.hidden[k] != v {
					t.Errorf("key %q: expected %v, got %v", k, v, ls.hidden[k])
				}
			}
		})
	}
}

func TestDefaultLayoutState(t *testing.T) {
	claudeKeys := []string{"five_hour", "seven_day"}
	codexKeys := []string{"codex_primary", "codex_secondary"}

	ls := defaultLayoutState(claudeKeys, codexKeys)

	// Panel order should be ["claude", "codex"]
	if len(ls.panelOrder) != 2 {
		t.Fatalf("expected 2 panels, got %d", len(ls.panelOrder))
	}
	if ls.panelOrder[0] != "claude" {
		t.Errorf("panelOrder[0]: expected 'claude', got %q", ls.panelOrder[0])
	}
	if ls.panelOrder[1] != "codex" {
		t.Errorf("panelOrder[1]: expected 'codex', got %q", ls.panelOrder[1])
	}

	// Cat orders should match input
	if len(ls.claudeCatOrder) != len(claudeKeys) {
		t.Fatalf("claudeCatOrder length mismatch: expected %d, got %d", len(claudeKeys), len(ls.claudeCatOrder))
	}
	for i := range claudeKeys {
		if ls.claudeCatOrder[i] != claudeKeys[i] {
			t.Errorf("claudeCatOrder[%d]: expected %q, got %q", i, claudeKeys[i], ls.claudeCatOrder[i])
		}
	}
	if len(ls.codexCatOrder) != len(codexKeys) {
		t.Fatalf("codexCatOrder length mismatch: expected %d, got %d", len(codexKeys), len(ls.codexCatOrder))
	}
	for i := range codexKeys {
		if ls.codexCatOrder[i] != codexKeys[i] {
			t.Errorf("codexCatOrder[%d]: expected %q, got %q", i, codexKeys[i], ls.codexCatOrder[i])
		}
	}

	// Hidden map should be empty (not nil)
	if ls.hidden == nil {
		t.Fatal("hidden map is nil, expected empty map")
	}
	if len(ls.hidden) != 0 {
		t.Errorf("hidden map should be empty, got %d entries", len(ls.hidden))
	}
}

func TestOrderedCats(t *testing.T) {
	tests := []struct {
		name     string
		panel    string
		ls       layoutState
		expected []string
	}{
		{
			name:  "Returns all Claude keys when nothing hidden",
			panel: "claude",
			ls: layoutState{
				claudeCatOrder: []string{"five_hour", "seven_day"},
				hidden:         map[string]bool{},
			},
			expected: []string{"five_hour", "seven_day"},
		},
		{
			name:  "Omits hidden Claude keys",
			panel: "claude",
			ls: layoutState{
				claudeCatOrder: []string{"five_hour", "seven_day", "extra"},
				hidden:         map[string]bool{"seven_day": true},
			},
			expected: []string{"five_hour", "extra"},
		},
		{
			name:  "Returns all Codex keys when nothing hidden",
			panel: "codex",
			ls: layoutState{
				codexCatOrder: []string{"codex_primary", "codex_secondary"},
				hidden:        map[string]bool{},
			},
			expected: []string{"codex_primary", "codex_secondary"},
		},
		{
			name:  "Omits hidden Codex keys",
			panel: "codex",
			ls: layoutState{
				codexCatOrder: []string{"codex_primary", "codex_secondary"},
				hidden:        map[string]bool{"codex_primary": true},
			},
			expected: []string{"codex_secondary"},
		},
		{
			name:     "Unknown panel returns nil",
			panel:    "unknown",
			ls:       layoutState{},
			expected: nil,
		},
		{
			name:  "All hidden returns empty slice",
			panel: "claude",
			ls: layoutState{
				claudeCatOrder: []string{"five_hour", "seven_day"},
				hidden:         map[string]bool{"five_hour": true, "seven_day": true},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.ls.orderedCats(tt.panel)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected non-nil result, got nil")
			}
			if len(result) != len(tt.expected) {
				t.Fatalf("length mismatch: expected %d, got %d", len(tt.expected), len(result))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expected[i], result[i])
				}
			}
		})
	}
}

func TestMergeOrder(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		incoming []string
		expected []string
	}{
		{
			name:     "New keys appended",
			existing: []string{"a", "b"},
			incoming: []string{"a", "b", "c", "d"},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "Existing keys preserved in order",
			existing: []string{"c", "a", "b"},
			incoming: []string{"a", "b", "c"},
			expected: []string{"c", "a", "b"},
		},
		{
			name:     "Duplicates in incoming ignored",
			existing: []string{"a"},
			incoming: []string{"a", "a", "b", "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "Empty existing",
			existing: []string{},
			incoming: []string{"a", "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "Empty incoming",
			existing: []string{"a", "b"},
			incoming: []string{},
			expected: []string{"a", "b"},
		},
		{
			name:     "Both empty",
			existing: []string{},
			incoming: []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeOrder(tt.existing, tt.incoming)
			if len(result) != len(tt.expected) {
				t.Fatalf("length mismatch: expected %d, got %d", len(tt.expected), len(result))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expected[i], result[i])
				}
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Hit-testing tests (Model-based)
// ═══════════════════════════════════════════════════════════════════════════

// testModel creates a Model with populated layout geometry for hit-testing.
func testModel() Model {
	m := Model{
		state:  stateRunning,
		width:  80,
		height: 24,
		keys:   newKeyMap(),
		categories: []parse.Category{
			{Key: "five_hour", Name: "Session"},
			{Key: "seven_day", Name: "Weekly"},
			{Key: "codex_primary", Name: "Session"},
			{Key: "codex_secondary", Name: "Weekly"},
		},
		layout: &layoutInfo{
			claudePanel: image.Rect(10, 2, 70, 12),
			codexPanel:  image.Rect(10, 13, 70, 23),
			statusBar:   image.Rect(0, 23, 80, 24),
			claudeBars: []barGeom{
				{key: "five_hour", bounds: image.Rect(12, 4, 68, 6)},
				{key: "seven_day", bounds: image.Rect(12, 7, 68, 9)},
				{key: "extra_usage", bounds: image.Rect(12, 10, 68, 12), pinned: true},
			},
			codexBars: []barGeom{
				{key: "codex_primary", bounds: image.Rect(12, 15, 68, 17)},
				{key: "codex_secondary", bounds: image.Rect(12, 18, 68, 20)},
			},
			trashZone: trashZoneRect(80, 24),
		},
		layoutState: layoutState{
			panelOrder:     []string{"claude", "codex"},
			claudeCatOrder: []string{"five_hour", "seven_day"},
			codexCatOrder:  []string{"codex_primary", "codex_secondary"},
			hidden:         map[string]bool{},
		},
	}
	return m
}

func TestHandleMouseDown(t *testing.T) {
	tests := []struct {
		name           string
		x, y           int
		expectedPhase  dragPhase
		expectedTarget dragTarget
		expectedKey    string
		expectedPanel  string
		expectedLabel  string
	}{
		{
			name:           "Click on Claude bar",
			x:              40,
			y:              5,
			expectedPhase:  dragPending,
			expectedTarget: dragTargetBar,
			expectedKey:    "five_hour",
			expectedLabel:  "Session",
		},
		{
			name:           "Click on Codex bar",
			x:              40,
			y:              16,
			expectedPhase:  dragPending,
			expectedTarget: dragTargetBar,
			expectedKey:    "codex_primary",
			expectedLabel:  "Session",
		},
		{
			name:          "Click on extra_usage (pinned) does not start drag",
			x:             40,
			y:             11,
			expectedPhase: dragIdle,
		},
		{
			name:           "Click on Claude panel not on bar",
			x:              40,
			y:              3,
			expectedPhase:  dragPending,
			expectedTarget: dragTargetPanel,
			expectedPanel:  "claude",
			expectedLabel:  "Claude",
		},
		{
			name:          "Click outside all panels",
			x:             5,
			y:             5,
			expectedPhase: dragIdle,
		},
		{
			name:          "Click on status bar",
			x:             40,
			y:             23,
			expectedPhase: dragIdle,
		},
		{
			name:           "Click on Codex panel not on bar",
			x:              40,
			y:              14,
			expectedPhase:  dragPending,
			expectedTarget: dragTargetPanel,
			expectedPanel:  "codex",
			expectedLabel:  "Codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel()
			msg := tea.MouseClickMsg(tea.Mouse{
				X:      tt.x,
				Y:      tt.y,
				Button: tea.MouseLeft,
			})
			result, _ := m.handleMouseDown(msg)

			if result.drag.phase != tt.expectedPhase {
				t.Errorf("phase: expected %v, got %v", tt.expectedPhase, result.drag.phase)
			}
			if tt.expectedPhase == dragIdle {
				return
			}

			if result.drag.target != tt.expectedTarget {
				t.Errorf("target: expected %v, got %v", tt.expectedTarget, result.drag.target)
			}
			if result.drag.startX != tt.x {
				t.Errorf("startX: expected %d, got %d", tt.x, result.drag.startX)
			}
			if result.drag.startY != tt.y {
				t.Errorf("startY: expected %d, got %d", tt.y, result.drag.startY)
			}
			if result.drag.currX != tt.x {
				t.Errorf("currX: expected %d, got %d", tt.x, result.drag.currX)
			}
			if result.drag.currY != tt.y {
				t.Errorf("currY: expected %d, got %d", tt.y, result.drag.currY)
			}

			if tt.expectedTarget == dragTargetBar {
				if result.drag.barKey != tt.expectedKey {
					t.Errorf("barKey: expected %q, got %q", tt.expectedKey, result.drag.barKey)
				}
			}
			if tt.expectedTarget == dragTargetPanel {
				if result.drag.panelID != tt.expectedPanel {
					t.Errorf("panelID: expected %q, got %q", tt.expectedPanel, result.drag.panelID)
				}
			}
			if result.drag.ghostLabel != tt.expectedLabel {
				t.Errorf("ghostLabel: expected %q, got %q", tt.expectedLabel, result.drag.ghostLabel)
			}
		})
	}
}

func TestHandleMouseMove(t *testing.T) {
	tests := []struct {
		name          string
		startX, startY int
		moveX, moveY  int
		expectedPhase dragPhase
	}{
		{
			name:          "Small movement stays pending",
			startX:        40,
			startY:        5,
			moveX:         41,
			moveY:         5,
			expectedPhase: dragPending,
		},
		{
			name:          "Movement exceeding threshold activates drag",
			startX:        40,
			startY:        5,
			moveX:         43,
			moveY:         5,
			expectedPhase: dragActive,
		},
		{
			name:          "Vertical movement exceeding threshold activates drag",
			startX:        40,
			startY:        5,
			moveX:         40,
			moveY:         8,
			expectedPhase: dragActive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel()
			// Start drag
			clickMsg := tea.MouseClickMsg(tea.Mouse{
				X:      tt.startX,
				Y:      tt.startY,
				Button: tea.MouseLeft,
			})
			m, _ = m.handleMouseDown(clickMsg)

			// Move mouse
			moveMsg := tea.MouseMotionMsg(tea.Mouse{
				X: tt.moveX,
				Y: tt.moveY,
			})
			result, _ := m.handleMouseMove(moveMsg)

			if result.drag.phase != tt.expectedPhase {
				t.Errorf("phase: expected %v, got %v", tt.expectedPhase, result.drag.phase)
			}
			if result.drag.currX != tt.moveX {
				t.Errorf("currX: expected %d, got %d", tt.moveX, result.drag.currX)
			}
			if result.drag.currY != tt.moveY {
				t.Errorf("currY: expected %d, got %d", tt.moveY, result.drag.currY)
			}
		})
	}
}

func TestHandleMouseUpTrash(t *testing.T) {
	tests := []struct {
		name        string
		dragTarget  dragTarget
		barKey      string
		panelID     string
		releaseX    int
		releaseY    int
		expectHide  bool
		expectHidden map[string]bool
	}{
		{
			name:       "Bar dragged to trash zone is hidden",
			dragTarget: dragTargetBar,
			barKey:     "five_hour",
			releaseX:   70,
			releaseY:   16,
			expectHide: true,
			expectHidden: map[string]bool{
				"five_hour": true,
			},
		},
		{
			name:       "Bar released outside trash zone not hidden",
			dragTarget: dragTargetBar,
			barKey:     "five_hour",
			releaseX:   40,
			releaseY:   10,
			expectHide: false,
			expectHidden: map[string]bool{},
		},
		{
			name:       "Panel dragged to trash zone hides all bars",
			dragTarget: dragTargetPanel,
			panelID:    "claude",
			releaseX:   70,
			releaseY:   16,
			expectHide: true,
			expectHidden: map[string]bool{
				"five_hour": true,
				"seven_day": true,
			},
		},
		{
			name:       "Panel released outside trash zone not hidden",
			dragTarget: dragTargetPanel,
			panelID:    "claude",
			releaseX:   40,
			releaseY:   10,
			expectHide: false,
			expectHidden: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel()

			// Set up active drag state
			m.drag = dragState{
				phase:   dragActive,
				target:  tt.dragTarget,
				barKey:  tt.barKey,
				panelID: tt.panelID,
				startX:  40,
				startY:  5,
				currX:   tt.releaseX,
				currY:   tt.releaseY,
			}

			// Release mouse
			releaseMsg := tea.MouseReleaseMsg(tea.Mouse{
				X: tt.releaseX,
				Y: tt.releaseY,
			})
			result, _ := m.handleMouseUp(releaseMsg)

			// Drag should be cleared
			if result.drag.phase != dragIdle {
				t.Errorf("drag phase after release: expected dragIdle, got %v", result.drag.phase)
			}

			// Check hidden state
			if len(result.layoutState.hidden) != len(tt.expectHidden) {
				t.Fatalf("hidden map size: expected %d, got %d", len(tt.expectHidden), len(result.layoutState.hidden))
			}
			for k, v := range tt.expectHidden {
				if result.layoutState.hidden[k] != v {
					t.Errorf("hidden[%q]: expected %v, got %v", k, v, result.layoutState.hidden[k])
				}
			}
		})
	}
}

func TestHandleMouseUpPending(t *testing.T) {
	m := testModel()

	// Start drag (pending phase)
	clickMsg := tea.MouseClickMsg(tea.Mouse{
		X:      40,
		Y:      5,
		Button: tea.MouseLeft,
	})
	m, _ = m.handleMouseDown(clickMsg)

	// Verify pending
	if m.drag.phase != dragPending {
		t.Fatalf("expected dragPending, got %v", m.drag.phase)
	}

	// Release without activating
	releaseMsg := tea.MouseReleaseMsg(tea.Mouse{
		X: 40,
		Y: 5,
	})
	result, _ := m.handleMouseUp(releaseMsg)

	// Drag should be cleared, nothing hidden
	if result.drag.phase != dragIdle {
		t.Errorf("drag phase after release: expected dragIdle, got %v", result.drag.phase)
	}
	if len(result.layoutState.hidden) != 0 {
		t.Errorf("hidden map should be empty, got %d entries", len(result.layoutState.hidden))
	}
}

func TestLiveReorderBar(t *testing.T) {
	tests := []struct {
		name           string
		dragKey        string
		currY          int
		expectedOrder  []string
		expectNoChange bool
	}{
		{
			name:          "Drag down: first bar past second midpoint",
			dragKey:       "five_hour",
			currY:         8, // past seven_day midpoint (7+9)/2 = 8
			expectedOrder: []string{"seven_day", "five_hour"},
		},
		{
			name:          "Drag up: second bar past first midpoint",
			dragKey:       "seven_day",
			currY:         4, // before five_hour midpoint (4+6)/2 = 5
			expectedOrder: []string{"seven_day", "five_hour"},
		},
		{
			name:           "Stay in same slot",
			dragKey:        "five_hour",
			currY:          5, // still in first slot
			expectNoChange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel()
			m.drag = dragState{
				phase:   dragActive,
				target:  dragTargetBar,
				barKey:  tt.dragKey,
				startX:  40,
				startY:  5,
				currX:   40,
				currY:   tt.currY,
			}

			initialOrder := append([]string(nil), m.layoutState.claudeCatOrder...)
			m.liveReorderBar()

			if tt.expectNoChange {
				if len(m.layoutState.claudeCatOrder) != len(initialOrder) {
					t.Fatalf("order changed when it shouldn't have")
				}
				for i := range m.layoutState.claudeCatOrder {
					if m.layoutState.claudeCatOrder[i] != initialOrder[i] {
						t.Errorf("order changed at index %d: %q -> %q", i, initialOrder[i], m.layoutState.claudeCatOrder[i])
					}
				}
				return
			}

			if len(m.layoutState.claudeCatOrder) != len(tt.expectedOrder) {
				t.Fatalf("order length: expected %d, got %d", len(tt.expectedOrder), len(m.layoutState.claudeCatOrder))
			}
			for i := range m.layoutState.claudeCatOrder {
				if m.layoutState.claudeCatOrder[i] != tt.expectedOrder[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expectedOrder[i], m.layoutState.claudeCatOrder[i])
				}
			}
		})
	}
}

func TestLiveReorderBarSingleBar(t *testing.T) {
	m := testModel()
	// Remove all but one bar from Claude panel
	m.layout.claudeBars = []barGeom{
		{key: "five_hour", bounds: image.Rect(12, 4, 68, 6)},
	}
	m.layoutState.claudeCatOrder = []string{"five_hour"}

	m.drag = dragState{
		phase:   dragActive,
		target:  dragTargetBar,
		barKey:  "five_hour",
		startX:  40,
		startY:  5,
		currX:   40,
		currY:   10,
	}

	initialOrder := append([]string(nil), m.layoutState.claudeCatOrder...)
	m.liveReorderBar()

	// Order should not change
	if len(m.layoutState.claudeCatOrder) != len(initialOrder) {
		t.Fatalf("order changed when it shouldn't have")
	}
	for i := range m.layoutState.claudeCatOrder {
		if m.layoutState.claudeCatOrder[i] != initialOrder[i] {
			t.Errorf("order changed at index %d: %q -> %q", i, initialOrder[i], m.layoutState.claudeCatOrder[i])
		}
	}
}

func TestLiveReorderPanel(t *testing.T) {
	tests := []struct {
		name          string
		panelID       string
		currY         int
		expectedOrder []string
	}{
		{
			name:          "Top panel dragged below midpoint swaps",
			panelID:       "claude",
			currY:         18, // below midpoint (2+23)/2 = 12.5
			expectedOrder: []string{"codex", "claude"},
		},
		{
			name:          "Bottom panel dragged above midpoint swaps",
			panelID:       "codex",
			currY:         10, // above midpoint
			expectedOrder: []string{"codex", "claude"},
		},
		{
			name:          "Top panel stays above midpoint, no swap",
			panelID:       "claude",
			currY:         8,
			expectedOrder: []string{"claude", "codex"},
		},
		{
			name:          "Bottom panel stays below midpoint, no swap",
			panelID:       "codex",
			currY:         18,
			expectedOrder: []string{"claude", "codex"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel()
			m.drag = dragState{
				phase:   dragActive,
				target:  dragTargetPanel,
				panelID: tt.panelID,
				startX:  40,
				startY:  10,
				currX:   40,
				currY:   tt.currY,
			}

			m.liveReorderPanel()

			if len(m.layoutState.panelOrder) != len(tt.expectedOrder) {
				t.Fatalf("panelOrder length: expected %d, got %d", len(tt.expectedOrder), len(m.layoutState.panelOrder))
			}
			for i := range m.layoutState.panelOrder {
				if m.layoutState.panelOrder[i] != tt.expectedOrder[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expectedOrder[i], m.layoutState.panelOrder[i])
				}
			}
		})
	}
}
