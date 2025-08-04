package main

import (
	"log"

	"docker-cleaner/models"
	"github.com/charmbracelet/bubbletea"
)

func main() {
	p := tea.NewProgram(models.InitialModel())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Alas, there's been an error: %v", err)
	}
}
