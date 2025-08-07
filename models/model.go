package models

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"os"
	"strings"

	"docker-cleaner/docker"
	"github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
)

type ItemType int

const (
	ItemTypeHeader ItemType = iota
	ItemTypeImage
	ItemTypeContainer
)

type SelectableItem struct {
	ID      string
	Type    ItemType
	Display string
}

type Model struct {
	items        []SelectableItem
	cursor       int
	selected     map[int]struct{}
	dockerClient *dockerClient.Client
	images       []dockerClient.APIImages
	containers   []dockerClient.APIContainers
	errorMsg     string
	successMsg   string
	cleaning     bool
}

var errorMessageStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#FAFAFA")).
	Background(lipgloss.Color("#eb4d4b"))

func InitialModel() Model {
	client, err := dockerClient.NewClientFromEnv()

	if err != nil {
		fmt.Printf("Unable to connect to Docker daemon: %v\n", err)
		os.Exit(1)
	}

	err = client.Ping()

	if err != nil {
		fmt.Println(errorMessageStyle.Render("Docker is not running or unreachable. Please start Docker and try again."))
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
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}

		case "enter", " ":
			// Only allow selection of non-header items
			if m.cursor < len(m.items) && m.items[m.cursor].Type != ItemTypeHeader {
				_, ok := m.selected[m.cursor]
				if ok {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = struct{}{}
				}
			}
		case "d": // Delete selected items
			if len(m.selected) > 0 {
				// Clear previous messages when starting new cleanup
				m.errorMsg = ""
				m.successMsg = ""
				m.cleaning = true
				return m, func() tea.Msg {
					return docker.CleanupMsg{ItemsToClean: m.getSelectionForCleanup()}
				}
			}
		}

	case docker.ImagesFetchedMsg:
		m.images = msg.Images
		m.updateItems()
		return m, nil

	case docker.ContainersFetchedMsg:
		m.containers = msg.Containers
		m.updateItems()
		return m, nil

	case docker.CleanupMsg:
		return m, docker.PerformCleanup(m.dockerClient, msg.ItemsToClean)

	case docker.CleanupDoneMsg:
		m.cleaning = false
		m.selected = make(map[int]struct{})
		m.errorMsg = "" // Clear any previous errors
		m.successMsg = msg.Message
		return m, tea.Batch(docker.FetchDockerImages, docker.FetchDockerContainers)

	case docker.ErrMsg:
		m.errorMsg = msg.Error()
		m.successMsg = "" // Clear any previous success messages
		m.cleaning = false
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	s := "Docker Cleaner\n\n"

	for i, item := range m.items {
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		// Don't show checkbox for headers
		if item.Type == ItemTypeHeader {
			s += fmt.Sprintf("%s    %s\n", cursor, item.Display)
		} else {
			s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, item.Display)
		}
	}

	s += "\nPress q to quit. Press space to select/deselect. Press d to delete selected.\n"
	if m.cleaning {
		s += "Cleaning up...\n"
	}
	if m.successMsg != "" {
		s += fmt.Sprintf("✓ %s\n", m.successMsg)
	}
	if m.errorMsg != "" {
		s += fmt.Sprintf("✗ Error: %s\n", m.errorMsg)
	}

	return s
}

func (m *Model) getSelectionForCleanup() []docker.CleanupItem {
	var items []docker.CleanupItem
	for i := range m.selected {
		if i < len(m.items) {
			item := m.items[i]
			if item.Type != ItemTypeHeader {
				items = append(items, docker.CleanupItem{
					ID:   item.ID,
					Type: docker.CleanupItemType(item.Type),
				})
			}
		}
	}
	return items
}

func (m *Model) updateItems() {
	m.items = []SelectableItem{}

	// Add images section
	m.items = append(m.items, SelectableItem{
		Type:    ItemTypeHeader,
		Display: "Docker Images",
	})

	for _, img := range m.images {
		name := "<none>"
		if len(img.RepoTags) > 0 && img.RepoTags[0] != "<none>:<none>" {
			name = img.RepoTags[0]
		}
		m.items = append(m.items, SelectableItem{
			ID:      img.ID,
			Type:    ItemTypeImage,
			Display: fmt.Sprintf("  %s (ID: %s)", name, img.ID[:12]),
		})
	}

	// Add containers section
	m.items = append(m.items, SelectableItem{
		Type:    ItemTypeHeader,
		Display: "\nDocker Containers",
	})

	for _, container := range m.containers {
		name := "<none>"
		if len(container.Names) > 0 {
			name = container.Names[0]
			// Remove leading slash from container name
			if strings.HasPrefix(name, "/") {
				name = name[1:]
			}
		}
		m.items = append(m.items, SelectableItem{
			ID:      container.ID,
			Type:    ItemTypeContainer,
			Display: fmt.Sprintf("  %s (ID: %s, Image: %s)", name, container.ID[:12], container.Image),
		})
	}
}
