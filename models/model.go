package models

import (
	"fmt"
	"os"
	"strings"

	"docker-cleaner/docker"
	"github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
)

type Model struct {
	choices      []string
	cursor       int
	selected     map[int]struct{}
	dockerClient *dockerClient.Client
	images       []dockerClient.APIImages
	containers   []dockerClient.APIContainers
	errorMsg     string
	cleaning     bool
}

func InitialModel() Model {
	client, err := dockerClient.NewClientFromEnv()

	if err != nil {
		fmt.Printf("Unable to connect to Docker daemon: %v\n", err)
		os.Exit(1)
	}

	err = client.Ping()

	if err != nil {
		fmt.Println("Docker is not running or unreachable. Please start Docker and try again.")
		os.Exit(1)
	}

	return Model{
		selected:     make(map[int]struct{}),
		dockerClient: client,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(docker.FetchDockerImages, docker.FetchDockerContainers)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		case "enter", " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}
		case "d": // Delete selected items
			if len(m.selected) > 0 {
				m.cleaning = true
				return m, func() tea.Msg {
					return docker.CleanupMsg{ItemsToClean: m.getSelectionForCleanup()}
				}
			}
		}

	case docker.ImagesFetchedMsg:
		m.images = msg.Images
		m.updateChoices()
		return m, nil

	case docker.ContainersFetchedMsg:
		m.containers = msg.Containers
		m.updateChoices()
		return m, nil

	case docker.CleanupMsg:
		return m, docker.PerformCleanup(m.dockerClient, msg.ItemsToClean)

	case docker.CleanupDoneMsg:
		m.cleaning = false
		m.selected = make(map[int]struct{})
		return m, tea.Batch(docker.FetchDockerImages, docker.FetchDockerContainers)

	case docker.ErrMsg:
		m.errorMsg = msg.Error()
		m.cleaning = false
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	s := "Docker Cleaner\n\n"

	for i, choice := range m.choices {
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
	}

	s += "\nPress q to quit. Press space to select/deselect. Press d to delete selected.\n"
	if m.cleaning {
		s += "Cleaning up...\n"
	}
	if m.errorMsg != "" {
		s += fmt.Sprintf("Error: %s\n", m.errorMsg)
	}

	return s
}

func (m *Model) getSelectionForCleanup() []string {
	var items []string
	for i := range m.selected {
		item := m.choices[i]
		if strings.Contains(item, "(ID:") {
			parts := strings.Split(item, "(ID: ")
			id := strings.TrimSuffix(parts[1], ")")
			items = append(items, id)
		}
	}
	return items
}

func (m *Model) updateChoices() {
	m.choices = []string{}
	m.choices = append(m.choices, "Docker Images")
	for _, img := range m.images {
		name := "<none>"
		if len(img.RepoTags) > 0 {
			name = img.RepoTags[0]
		}
		m.choices = append(m.choices, fmt.Sprintf("  %s (ID: %s)", name, img.ID[:12]))
	}

	m.choices = append(m.choices, "\nDocker Containers")
	for _, container := range m.containers {
		name := container.Names[0]
		m.choices = append(m.choices, fmt.Sprintf("  %s (ID: %s, Image: %s)", name, container.ID[:12], container.Image))
	}
}
