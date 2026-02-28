package tui

import "testing"

func TestHideBarCmd_ExecuteUndo(t *testing.T) {
	ls := layoutState{hidden: map[string]bool{}}
	cmd := hideBarCmd{barKey: "a"}
	cmd.Execute(&ls)
	if !ls.hidden["a"] {
		t.Error("Execute should hide bar 'a'")
	}
	cmd.Undo(&ls)
	if ls.hidden["a"] {
		t.Error("Undo should unhide bar 'a'")
	}
}

func TestHidePanelCmd_ExecuteUndo(t *testing.T) {
	ls := layoutState{
		catOrder: map[string][]string{"p1": {"a", "b", "c"}},
		hidden:   map[string]bool{},
	}
	cmd := hidePanelCmd{panelID: "p1", keys: []string{"a", "b", "c"}}
	cmd.Execute(&ls)
	for _, k := range []string{"a", "b", "c"} {
		if !ls.hidden[k] {
			t.Errorf("Execute should hide bar %q", k)
		}
	}
	cmd.Undo(&ls)
	for _, k := range []string{"a", "b", "c"} {
		if ls.hidden[k] {
			t.Errorf("Undo should unhide bar %q", k)
		}
	}
}

func TestReorderPanelsCmd_ExecuteUndo(t *testing.T) {
	ls := layoutState{
		panelOrder: []string{"a", "b", "c"},
		catOrder:   map[string][]string{},
		hidden:     map[string]bool{},
	}
	cmd := reorderPanelsCmd{
		oldOrder: []string{"a", "b", "c"},
		newOrder: []string{"c", "a", "b"},
	}
	cmd.Execute(&ls)
	if ls.panelOrder[0] != "c" || ls.panelOrder[1] != "a" || ls.panelOrder[2] != "b" {
		t.Errorf("Execute: got %v, want [c a b]", ls.panelOrder)
	}
	cmd.Undo(&ls)
	if ls.panelOrder[0] != "a" || ls.panelOrder[1] != "b" || ls.panelOrder[2] != "c" {
		t.Errorf("Undo: got %v, want [a b c]", ls.panelOrder)
	}
}

func TestReorderBarCmd_ExecuteUndo(t *testing.T) {
	ls := layoutState{
		catOrder: map[string][]string{"p1": {"a", "b", "c"}},
		hidden:   map[string]bool{},
	}
	cmd := reorderBarCmd{
		panelID:  "p1",
		oldOrder: []string{"a", "b", "c"},
		newOrder: []string{"b", "a", "c"},
	}
	cmd.Execute(&ls)
	if ls.catOrder["p1"][0] != "b" || ls.catOrder["p1"][1] != "a" {
		t.Errorf("Execute: got %v, want [b a c]", ls.catOrder["p1"])
	}
	cmd.Undo(&ls)
	if ls.catOrder["p1"][0] != "a" || ls.catOrder["p1"][1] != "b" {
		t.Errorf("Undo: got %v, want [a b c]", ls.catOrder["p1"])
	}
}

func TestRestoreAllCmd_ExecuteUndo(t *testing.T) {
	prev := layoutState{
		panelOrder: []string{"a", "b"},
		catOrder:   map[string][]string{"a": {"x", "y"}},
		hidden:     map[string]bool{"x": true},
	}
	newLS := layoutState{
		panelOrder: []string{"a", "b"},
		catOrder:   map[string][]string{"a": {"x", "y"}},
		hidden:     map[string]bool{},
	}
	ls := prev.clone()
	cmd := restoreAllCmd{prevState: prev.clone(), newState: newLS.clone()}
	cmd.Execute(&ls)
	if ls.hidden["x"] {
		t.Error("Execute should clear hidden")
	}
	cmd.Undo(&ls)
	if !ls.hidden["x"] {
		t.Error("Undo should restore hidden")
	}
}

func TestCmdHistory_Undo(t *testing.T) {
	m := Model{
		layoutState: layoutState{hidden: map[string]bool{}},
	}
	// Push two commands
	cmd1 := hideBarCmd{barKey: "a"}
	cmd1.Execute(&m.layoutState)
	m.pushCmd(cmd1)
	cmd2 := hideBarCmd{barKey: "b"}
	cmd2.Execute(&m.layoutState)
	m.pushCmd(cmd2)

	if len(m.cmdHistory) != 2 {
		t.Fatalf("history length = %d, want 2", len(m.cmdHistory))
	}

	// Undo last
	m.undoCmd()
	if m.layoutState.hidden["b"] {
		t.Error("undo should unhide 'b'")
	}
	if !m.layoutState.hidden["a"] {
		t.Error("'a' should still be hidden")
	}

	// Undo first
	m.undoCmd()
	if m.layoutState.hidden["a"] {
		t.Error("undo should unhide 'a'")
	}

	// Undo on empty does nothing
	m.undoCmd()
	if len(m.cmdHistory) != 0 {
		t.Error("history should be empty")
	}
}

func TestCmdHistory_Cap(t *testing.T) {
	m := Model{
		layoutState: layoutState{hidden: map[string]bool{}},
	}
	for i := 0; i < 60; i++ {
		m.pushCmd(hideBarCmd{barKey: "x"})
	}
	if len(m.cmdHistory) != maxCmdHistory {
		t.Errorf("history length = %d, want %d", len(m.cmdHistory), maxCmdHistory)
	}
}
