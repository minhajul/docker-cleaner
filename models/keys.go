package models

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every key binding the TUI responds to. It is the single source
// for both input handling (key.Matches in Update) and the help footer, so a
// binding is never described in one place and handled differently elsewhere.
type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	Delete key.Binding
	Help   key.Binding
	Quit   key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Toggle: key.NewBinding(
			key.WithKeys(" ", "enter"),
			key.WithHelp("space", "select"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete selected"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "more keys"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp implements help.KeyMap: the one-line footer.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Delete, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap: the expanded view toggled by "?". Each
// inner slice is rendered as one column.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Toggle, k.Delete},
		{k.Help, k.Quit},
	}
}
