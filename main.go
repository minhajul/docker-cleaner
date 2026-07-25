package main

import (
	"fmt"
	"log"
	"os"

	"docker-cleaner/models"
	"docker-cleaner/styles"

	"github.com/charmbracelet/bubbletea"
)

func main() {
	m, err := models.InitialModel()
	if err != nil {
		os.Exit(1)
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		msg := styles.ErrorMessage.Render(fmt.Sprintf("There's been an error: %v", err))
		log.Fatalf("%s", msg)
	}
}
