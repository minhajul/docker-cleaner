package main

import (
	"fmt"
	"log"
	"os"

	"github.com/minhajul/docker-cleaner/models"
	"github.com/minhajul/docker-cleaner/styles"

	"github.com/charmbracelet/bubbletea"
)

func main() {
	m, err := models.InitialModel()
	if err != nil {
		os.Exit(1)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		msg := styles.ErrorMessage.Render(fmt.Sprintf("There's been an error: %v", err))
		log.Fatalf("%s", msg)
	}
}
