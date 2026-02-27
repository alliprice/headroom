package tui

// layoutCmd represents a reversible mutation to the layout state.
type layoutCmd interface {
	Execute(ls *layoutState)
	Undo(ls *layoutState)
}

// hideBarCmd hides a single bar.
type hideBarCmd struct {
	barKey string
}

func (c hideBarCmd) Execute(ls *layoutState) {
	ls.hidden[c.barKey] = true
}

func (c hideBarCmd) Undo(ls *layoutState) {
	delete(ls.hidden, c.barKey)
}

// hidePanelCmd hides all bars in a panel.
type hidePanelCmd struct {
	panelID string
	keys    []string // bars that were hidden (for undo)
}

func (c hidePanelCmd) Execute(ls *layoutState) {
	for _, k := range c.keys {
		ls.hidden[k] = true
	}
}

func (c hidePanelCmd) Undo(ls *layoutState) {
	for _, k := range c.keys {
		delete(ls.hidden, k)
	}
}

// swapPanelsCmd swaps panelOrder[0] and panelOrder[1]. Self-inverse.
type swapPanelsCmd struct{}

func (c swapPanelsCmd) Execute(ls *layoutState) {
	if len(ls.panelOrder) >= 2 {
		ls.panelOrder[0], ls.panelOrder[1] = ls.panelOrder[1], ls.panelOrder[0]
	}
}

func (c swapPanelsCmd) Undo(ls *layoutState) {
	c.Execute(ls) // self-inverse
}

// reorderBarCmd captures a bar reorder within a panel.
type reorderBarCmd struct {
	panelID  string
	oldOrder []string
	newOrder []string
}

func (c reorderBarCmd) Execute(ls *layoutState) {
	ls.catOrder[c.panelID] = make([]string, len(c.newOrder))
	copy(ls.catOrder[c.panelID], c.newOrder)
}

func (c reorderBarCmd) Undo(ls *layoutState) {
	ls.catOrder[c.panelID] = make([]string, len(c.oldOrder))
	copy(ls.catOrder[c.panelID], c.oldOrder)
}

// restoreAllCmd captures a full layout reset (the "0" key).
type restoreAllCmd struct {
	prevState layoutState
	newState  layoutState
}

func (c restoreAllCmd) Execute(ls *layoutState) {
	*ls = c.newState.clone()
}

func (c restoreAllCmd) Undo(ls *layoutState) {
	*ls = c.prevState.clone()
}

const maxCmdHistory = 50
