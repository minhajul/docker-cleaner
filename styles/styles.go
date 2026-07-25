// Package styles defines shared lipgloss styles used across the application.
package styles

import "github.com/charmbracelet/lipgloss"

var (
	// Title is the style for the main title.
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1)

	// Header is the style for section headers.
	Header = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#3B82F6")).
		MarginTop(1)

	// Cursor is the style for the cursor indicator.
	Cursor = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Bold(true)

	// SelectedCheckbox is the style for a checked checkbox.
	SelectedCheckbox = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#10B981")).
				Bold(true)

	// UnselectedCheckbox is the style for an unchecked checkbox.
	UnselectedCheckbox = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280"))

	// Item is the default style for list items.
	Item = lipgloss.NewStyle().
		PaddingLeft(1)

	// SelectedItem is the style for selected list items.
	SelectedItem = lipgloss.NewStyle().
			PaddingLeft(1).
			Foreground(lipgloss.Color("#10B981"))

	// ErrorMessage is the style for error messages.
	ErrorMessage = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#EF4444")).
			Padding(0, 1)

	// SuccessMessage is the style for success messages.
	SuccessMessage = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#10B981")).
			Padding(0, 1).
			MarginTop(1)

	// Instructions is the style for the help text.
	Instructions = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			MarginTop(1)

	// Cleaning is the style for the "in progress" indicator.
	Cleaning = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Bold(true).
			MarginTop(1)
)
