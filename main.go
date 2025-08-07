package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"log"

	"docker-cleaner/models"
	"github.com/charmbracelet/bubbletea"
)

var errorMessageStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#FAFAFA")).
	Background(lipgloss.Color("#EF4444")).
	Padding(0, 1)

func main() {
	p := tea.NewProgram(models.InitialModel())
	if _, err := p.Run(); err != nil {
		log.Fatalf(errorMessageStyle.Render(fmt.Sprintf("Alas, there's been an error: %v", err)))
	}
}
