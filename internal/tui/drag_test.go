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
		dragKey  string
		slot     int
		expected []string
		wantNil  bool
	}{
		{
			name:     "Forward: drag first to last",
			order:    []string{"a", "b", "c"},
			dragKey:  "a",
			slot:     2,
			expected: []string{"b", "c", "a"},
		},
		{
			name:     "Backward: drag last to first",
			order:    []string{"a", "b", "c"},
			dragKey:  "c",
			slot:     0,
			expected: []string{"c", "a", "b"},
		},
		{
			name:    "Same slot returns nil",
			order:   []string{"a", "b", "c"},
			dragKey: "a",
			slot:    0,
			wantNil: true,
		},
		{
			name:     "Slot beyond bounds clamped",
			order:    []string{"a", "b", "c"},
			dragKey:  "a",
			slot:     10,
			expected: []string{"b", "c", "a"},
		},
		{
			name:     "Negative slot clamped to 0",
			order:    []string{"a", "b", "c"},
			dragKey:  "c",
			slot:     -5,
			expected: []string{"c", "a", "b"},
		},
		{
			name:    "Single item returns nil",
			order:   []string{"a"},
			dragKey: "a",
			slot:    0,
			wantNil: true,
		},
		{
			name:    "dragKey not found returns nil",
			order:   []string{"a", "b", "c"},
			dragKey: "x",
			wantNil: true,
		},
		{
			name:    "Empty order returns nil",
			order:   []string{},
			dragKey: "a",
			wantNil: true,
		},
		{
			name:     "Middle to first",
			order:    []string{"a", "b", "c"},
			dragKey:  "b",
			slot:     0,
			expected: []string{"b", "a", "c"},
		},
		{
			name:     "Middle to last",
			order:    []string{"a", "b", "c"},
			dragKey:  "b",
			slot:     2,
			expected: []string{"a", "c", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := moveToSlot(tt.order, tt.dragKey, tt.slot)
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

func TestDefaultLayoutState(t *testing.T) {
	catsByProvider := map[string][]string{
		"claude": {"five_hour", "seven_day"},
		"codex":  {"codex_primary", "codex_secondary"},
	}

	ls := defaultLayoutState(catsByProvider)

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
	claudeKeys := catsByProvider["claude"]
	codexKeys := catsByProvider["codex"]
	if len(ls.catOrder["claude"]) != len(claudeKeys) {
		t.Fatalf("claude catOrder length mismatch: expected %d, got %d", len(claudeKeys), len(ls.catOrder["claude"]))
	}
	for i := range claudeKeys {
		if ls.catOrder["claude"][i] != claudeKeys[i] {
			t.Errorf("claude catOrder[%d]: expected %q, got %q", i, claudeKeys[i], ls.catOrder["claude"][i])
		}
	}
	if len(ls.catOrder["codex"]) != len(codexKeys) {
		t.Fatalf("codex catOrder length mismatch: expected %d, got %d", len(codexKeys), len(ls.catOrder["codex"]))
	}
	for i := range codexKeys {
		if ls.catOrder["codex"][i] != codexKeys[i] {
			t.Errorf("codex catOrder[%d]: expected %q, got %q", i, codexKeys[i], ls.catOrder["codex"][i])
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
				catOrder: map[string][]string{
					"claude": {"five_hour", "seven_day"},
				},
				hidden: map[string]bool{},
			},
			expected: []string{"five_hour", "seven_day"},
		},
		{
			name:  "Omits hidden Claude keys",
			panel: "claude",
			ls: layoutState{
				catOrder: map[string][]string{
					"claude": {"five_hour", "seven_day", "extra"},
				},
				hidden: map[string]bool{"seven_day": true},
			},
			expected: []string{"five_hour", "extra"},
		},
		{
			name:  "Returns all Codex keys when nothing hidden",
			panel: "codex",
			ls: layoutState{
				catOrder: map[string][]string{
					"codex": {"codex_primary", "codex_secondary"},
				},
				hidden: map[string]bool{},
			},
			expected: []string{"codex_primary", "codex_secondary"},
		},
		{
			name:  "Omits hidden Codex keys",
			panel: "codex",
			ls: layoutState{
				catOrder: map[string][]string{
					"codex": {"codex_primary", "codex_secondary"},
				},
				hidden: map[string]bool{"codex_primary": true},
			},
			expected: []string{"codex_secondary"},
		},
		{
			name:     "Unknown panel returns nil",
			panel:    "unknown",
			ls:       layoutState{catOrder: map[string][]string{}},
			expected: nil,
		},
		{
			name:  "All hidden returns empty slice",
			panel: "claude",
			ls: layoutState{
				catOrder: map[string][]string{
					"claude": {"five_hour", "seven_day"},
				},
				hidden: map[string]bool{"five_hour": true, "seven_day": true},
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
		available:     map[string]bool{"claude": true, "codex": true},
		providerExtra: map[string]*parse.ExtraUsage{},
		layout: &layoutInfo{
			panels: map[string]image.Rectangle{
				"claude": image.Rect(10, 2, 70, 12),
				"codex":  image.Rect(10, 13, 70, 23),
			},
			bars: map[string][]barGeom{
				"claude": {
					{key: "five_hour", bounds: image.Rect(12, 4, 68, 6)},
					{key: "seven_day", bounds: image.Rect(12, 7, 68, 9)},
					{key: "extra_usage", bounds: image.Rect(12, 10, 68, 12)},
				},
				"codex": {
					{key: "codex_primary", bounds: image.Rect(12, 15, 68, 17)},
					{key: "codex_secondary", bounds: image.Rect(12, 18, 68, 20)},
				},
			},
			statusBar: image.Rect(0, 23, 80, 24),
			trashZone: trashZoneRect(80, 24),
		},
		layoutState: layoutState{
			panelOrder: []string{"claude", "codex"},
			catOrder: map[string][]string{
				"claude": {"five_hour", "seven_day"},
				"codex":  {"codex_primary", "codex_secondary"},
			},
			hidden: map[string]bool{},
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
			name:           "Click on extra_usage starts bar drag",
			x:              40,
			y:              11,
			expectedPhase:  dragPending,
			expectedTarget: dragTargetBar,
			expectedKey:    "extra_usage",
			expectedLabel:  "extra_usage",
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
		name           string
		startX, startY int
		moveX, moveY   int
		expectedPhase  dragPhase
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
		name         string
		dragTarget   dragTarget
		barKey       string
		panelID      string
		releaseX     int
		releaseY     int
		expectHide   bool
		expectHidden map[string]bool
	}{
		{
			name:       "Bar dragged to trash zone is hidden",
			dragTarget: dragTargetBar,
			barKey:     "five_hour",
			panelID:    "claude",
			releaseX:   70,
			releaseY:   16,
			expectHide: true,
			expectHidden: map[string]bool{
				"five_hour": true,
			},
		},
		{
			name:         "Bar released outside trash zone not hidden",
			dragTarget:   dragTargetBar,
			barKey:       "five_hour",
			panelID:      "claude",
			releaseX:     40,
			releaseY:     10,
			expectHide:   false,
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
			name:         "Panel released outside trash zone not hidden",
			dragTarget:   dragTargetPanel,
			panelID:      "claude",
			releaseX:     40,
			releaseY:     10,
			expectHide:   false,
			expectHidden: map[string]bool{},
		},
		{
			name:       "extra_usage dragged to trash zone is hidden",
			dragTarget: dragTargetBar,
			barKey:     "extra_usage",
			panelID:    "claude",
			releaseX:   70,
			releaseY:   16,
			expectHide: true,
			expectHidden: map[string]bool{
				"extra_usage": true,
			},
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
				panelID: "claude",
				barKey:  tt.dragKey,
				startX:  40,
				startY:  5,
				currX:   40,
				currY:   tt.currY,
			}

			initialOrder := append([]string(nil), m.layoutState.catOrder["claude"]...)
			m.liveReorderBar()

			if tt.expectNoChange {
				if len(m.layoutState.catOrder["claude"]) != len(initialOrder) {
					t.Fatalf("order changed when it shouldn't have")
				}
				for i := range m.layoutState.catOrder["claude"] {
					if m.layoutState.catOrder["claude"][i] != initialOrder[i] {
						t.Errorf("order changed at index %d: %q -> %q", i, initialOrder[i], m.layoutState.catOrder["claude"][i])
					}
				}
				return
			}

			if len(m.layoutState.catOrder["claude"]) != len(tt.expectedOrder) {
				t.Fatalf("order length: expected %d, got %d", len(tt.expectedOrder), len(m.layoutState.catOrder["claude"]))
			}
			for i := range m.layoutState.catOrder["claude"] {
				if m.layoutState.catOrder["claude"][i] != tt.expectedOrder[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expectedOrder[i], m.layoutState.catOrder["claude"][i])
				}
			}
		})
	}
}

func TestLiveReorderBarSingleBar(t *testing.T) {
	m := testModel()
	// Remove all but one bar from Claude panel
	m.layout.bars["claude"] = []barGeom{
		{key: "five_hour", bounds: image.Rect(12, 4, 68, 6)},
	}
	m.layoutState.catOrder["claude"] = []string{"five_hour"}

	m.drag = dragState{
		phase:   dragActive,
		target:  dragTargetBar,
		panelID: "claude",
		barKey:  "five_hour",
		startX:  40,
		startY:  5,
		currX:   40,
		currY:   10,
	}

	initialOrder := append([]string(nil), m.layoutState.catOrder["claude"]...)
	m.liveReorderBar()

	// Order should not change
	if len(m.layoutState.catOrder["claude"]) != len(initialOrder) {
		t.Fatalf("order changed when it shouldn't have")
	}
	for i := range m.layoutState.catOrder["claude"] {
		if m.layoutState.catOrder["claude"][i] != initialOrder[i] {
			t.Errorf("order changed at index %d: %q -> %q", i, initialOrder[i], m.layoutState.catOrder["claude"][i])
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
			currY:         5, // above claude's midpoint (7)
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

// ═══════════════════════════════════════════════════════════════════════════
// Gesture tests - end-to-end drag sequences through real handlers
// ═══════════════════════════════════════════════════════════════════════════

// simulateDrag replays a full mouse drag sequence through the real handlers:
// mousedown -> mousemove past threshold -> mousemove to target -> mouseup.
func simulateDrag(m Model, startX, startY, endX, endY int) Model {
	m, _ = m.handleMouseDown(tea.MouseClickMsg(tea.Mouse{
		X: startX, Y: startY, Button: tea.MouseLeft,
	}))
	// Move past threshold
	m, _ = m.handleMouseMove(tea.MouseMotionMsg(tea.Mouse{
		X: startX + dragThreshold + 1, Y: startY,
	}))
	// Move to target
	m, _ = m.handleMouseMove(tea.MouseMotionMsg(tea.Mouse{
		X: endX, Y: endY,
	}))
	// Release
	m, _ = m.handleMouseUp(tea.MouseReleaseMsg(tea.Mouse{
		X: endX, Y: endY,
	}))
	return m
}

func TestGesture_BarReorder(t *testing.T) {
	m := testModel()
	// Drag five_hour (Y=5) down past seven_day midpoint (Y=8)
	m = simulateDrag(m, 40, 5, 40, 8)

	// Order should flip
	got := m.layoutState.catOrder["claude"]
	want := []string{"seven_day", "five_hour"}
	if !slicesEqual(got, want) {
		t.Errorf("catOrder = %v, want %v", got, want)
	}
	// Should have exactly 1 reorderBarCmd
	if len(m.cmdHistory) != 1 {
		t.Fatalf("cmdHistory length = %d, want 1", len(m.cmdHistory))
	}
	if _, ok := m.cmdHistory[0].(reorderBarCmd); !ok {
		t.Errorf("cmd type = %T, want reorderBarCmd", m.cmdHistory[0])
	}
	// Undo restores original order
	m.undoCmd()
	got = m.layoutState.catOrder["claude"]
	want = []string{"five_hour", "seven_day"}
	if !slicesEqual(got, want) {
		t.Errorf("after undo: catOrder = %v, want %v", got, want)
	}
}

func TestGesture_PanelSwap(t *testing.T) {
	m := testModel()
	// Drag claude panel (Y=3, in panel header area) below codex midpoint (Y=18)
	m = simulateDrag(m, 40, 3, 40, 18)

	got := m.layoutState.panelOrder
	want := []string{"codex", "claude"}
	if !slicesEqual(got, want) {
		t.Errorf("panelOrder = %v, want %v", got, want)
	}
	if len(m.cmdHistory) != 1 {
		t.Fatalf("cmdHistory length = %d, want 1", len(m.cmdHistory))
	}
	if _, ok := m.cmdHistory[0].(reorderPanelsCmd); !ok {
		t.Errorf("cmd type = %T, want reorderPanelsCmd", m.cmdHistory[0])
	}
	// Undo restores
	m.undoCmd()
	got = m.layoutState.panelOrder
	want = []string{"claude", "codex"}
	if !slicesEqual(got, want) {
		t.Errorf("after undo: panelOrder = %v, want %v", got, want)
	}
}

func TestGesture_BarToTrash(t *testing.T) {
	m := testModel()
	tz := m.layout.trashZone
	// Drag five_hour to trash zone center
	tzX := (tz.Min.X + tz.Max.X) / 2
	tzY := (tz.Min.Y + tz.Max.Y) / 2
	m = simulateDrag(m, 40, 5, tzX, tzY)

	if !m.layoutState.hidden["five_hour"] {
		t.Error("five_hour should be hidden")
	}
	if len(m.cmdHistory) != 1 {
		t.Fatalf("cmdHistory length = %d, want 1", len(m.cmdHistory))
	}
	if _, ok := m.cmdHistory[0].(hideBarCmd); !ok {
		t.Errorf("cmd type = %T, want hideBarCmd", m.cmdHistory[0])
	}
	// Undo unhides
	m.undoCmd()
	if m.layoutState.hidden["five_hour"] {
		t.Error("after undo: five_hour should not be hidden")
	}
}

func TestGesture_PanelToTrash(t *testing.T) {
	m := testModel()
	tz := m.layout.trashZone
	tzX := (tz.Min.X + tz.Max.X) / 2
	tzY := (tz.Min.Y + tz.Max.Y) / 2
	// Drag claude panel header to trash
	m = simulateDrag(m, 40, 3, tzX, tzY)

	if !m.layoutState.hidden["five_hour"] || !m.layoutState.hidden["seven_day"] {
		t.Error("all claude bars should be hidden")
	}
	if len(m.cmdHistory) != 1 {
		t.Fatalf("cmdHistory length = %d, want 1", len(m.cmdHistory))
	}
	if _, ok := m.cmdHistory[0].(hidePanelCmd); !ok {
		t.Errorf("cmd type = %T, want hidePanelCmd", m.cmdHistory[0])
	}
	// Undo unhides all
	m.undoCmd()
	if m.layoutState.hidden["five_hour"] || m.layoutState.hidden["seven_day"] {
		t.Error("after undo: claude bars should not be hidden")
	}
}

func TestGesture_BelowThreshold(t *testing.T) {
	m := testModel()
	origOrder := copyStrings(m.layoutState.catOrder["claude"])
	origPanels := copyStrings(m.layoutState.panelOrder)

	// Click and release without exceeding threshold
	m, _ = m.handleMouseDown(tea.MouseClickMsg(tea.Mouse{
		X: 40, Y: 5, Button: tea.MouseLeft,
	}))
	m, _ = m.handleMouseUp(tea.MouseReleaseMsg(tea.Mouse{
		X: 40, Y: 5,
	}))

	if !slicesEqual(m.layoutState.catOrder["claude"], origOrder) {
		t.Error("catOrder should not change")
	}
	if !slicesEqual(m.layoutState.panelOrder, origPanels) {
		t.Error("panelOrder should not change")
	}
	if len(m.cmdHistory) != 0 {
		t.Errorf("cmdHistory should be empty, got %d", len(m.cmdHistory))
	}
}

func TestGesture_MultiUndo(t *testing.T) {
	m := testModel()

	// Gesture 1: reorder bars (drag five_hour past seven_day)
	m = simulateDrag(m, 40, 5, 40, 8)
	if !slicesEqual(m.layoutState.catOrder["claude"], []string{"seven_day", "five_hour"}) {
		t.Fatalf("after reorder: catOrder = %v", m.layoutState.catOrder["claude"])
	}

	// Gesture 2: hide seven_day (now first) to trash
	tz := m.layout.trashZone
	tzX := (tz.Min.X + tz.Max.X) / 2
	tzY := (tz.Min.Y + tz.Max.Y) / 2
	m = simulateDrag(m, 40, 5, tzX, tzY)
	// seven_day is first in the reordered list, so dragging from Y=5 (first bar position) grabs it
	// But we need to check what actually got grabbed - it depends on the bar geometry
	// The layout.bars still has the original geometry, so Y=5 hits five_hour
	if !m.layoutState.hidden["five_hour"] {
		t.Fatalf("five_hour should be hidden after trash gesture")
	}

	if len(m.cmdHistory) != 2 {
		t.Fatalf("cmdHistory length = %d, want 2", len(m.cmdHistory))
	}

	// Undo hide
	m.undoCmd()
	if m.layoutState.hidden["five_hour"] {
		t.Error("after first undo: five_hour should not be hidden")
	}

	// Undo reorder
	m.undoCmd()
	if !slicesEqual(m.layoutState.catOrder["claude"], []string{"five_hour", "seven_day"}) {
		t.Errorf("after second undo: catOrder = %v, want [five_hour seven_day]", m.layoutState.catOrder["claude"])
	}
}
