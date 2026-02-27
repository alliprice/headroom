package tui

import (
	"charm.land/bubbles/v2/key"
)

type keyMap struct {
	Quit     key.Binding
	Refresh  key.Binding
	Interval key.Binding
	Reset    key.Binding
	Undo     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "Q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("r", "refresh"),
		),
		Interval: key.NewBinding(
			key.WithKeys("t", "T"),
			key.WithHelp("t", "interval"),
		),
		Reset: key.NewBinding(
			key.WithKeys("0"),
			key.WithHelp("0", "restore"),
		),
		Undo: key.NewBinding(
			key.WithKeys("ctrl+z"),
			key.WithHelp("^z", "undo"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Refresh, k.Interval, k.Reset, k.Undo}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Refresh, k.Interval, k.Reset, k.Undo},
	}
}
