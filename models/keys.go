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

// confirmKeyMap holds the keys of the delete confirmation dialog. While the
// dialog is open, Update handles only these and the footer renders them in
// place of keyMap.
type confirmKeyMap struct {
	Yes   key.Binding
	Force key.Binding // enabled only while a selected container is running
	No    key.Binding
	Quit  key.Binding
}

func defaultConfirmKeyMap() confirmKeyMap {
	return confirmKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("y", "delete"),
		),
		Force: key.NewBinding(
			key.WithKeys("f", "F"),
			key.WithHelp("f", "force delete"),
		),
		// Any key other than Yes, Force and Quit cancels; No just names the
		// obvious ones in the footer.
		No: key.NewBinding(
			key.WithKeys("n", "N", "esc"),
			key.WithHelp("n/esc", "cancel"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}

// ShortHelp implements help.KeyMap. help.Model leaves out disabled bindings,
// so Force only shows when it applies.
func (k confirmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Yes, k.Force, k.No, k.Quit}
}

// FullHelp implements help.KeyMap. The dialog has so few keys that the full
// view is the short one.
func (k confirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
