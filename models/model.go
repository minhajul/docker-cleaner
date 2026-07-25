package models

import (
	"fmt"
	"os"
	"strings"

	"docker-cleaner/docker"
	"docker-cleaner/styles"
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

// InitialModel creates a new Model connected to the Docker daemon. If Docker
// cannot be reached, an error is returned (and a diagnostic is written to
// stderr). The caller is responsible for exiting the program on error.
func InitialModel() (Model, error) {
	client, err := dockerClient.NewClientFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to connect to Docker daemon:", err)
		return Model{}, err
	}

	if err := client.Ping(); err != nil {
		fmt.Fprintln(os.Stderr, styles.ErrorMessage.Render("Docker is not running or unreachable. Please start Docker and try again."))
		return Model{}, err
	}

	return Model{
		selected:     make(map[int]struct{}),
		dockerClient: client,
	}, nil
}

func (m Model) Init() tea.Cmd {
	return m.refreshCmd()
}

// refreshCmd returns a tea.Cmd that fetches both images and containers
// using the shared Docker client.
func (m Model) refreshCmd() tea.Cmd {
	return tea.Batch(docker.FetchDockerImages(m.dockerClient), docker.FetchDockerContainers(m.dockerClient))
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
		return m, m.refreshCmd()

	case docker.ErrMsg:
		m.errorMsg = msg.Error()
		m.successMsg = "" // Clear any previous success messages
		m.cleaning = false
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	var s strings.Builder

	s.WriteString(styles.Title.Render("🐳 Docker Cleaner"))
	s.WriteString("\n")

	for i, item := range m.items {
		var line strings.Builder

		// Cursor indicator
		if m.cursor == i {
			line.WriteString(styles.Cursor.Render("▶ "))
		} else {
			line.WriteString("  ")
		}

		// Handle different item types
		if item.Type == ItemTypeHeader {
			// Headers don't have checkboxes
			line.WriteString(styles.Header.Render(item.Display))
		} else {
			// Checkbox for selectable items
			_, isSelected := m.selected[i]
			if isSelected {
				line.WriteString(styles.SelectedCheckbox.Render("✓ "))
			} else {
				line.WriteString(styles.UnselectedCheckbox.Render("☐ "))
			}

			// Item text with appropriate styling
			if isSelected {
				line.WriteString(styles.SelectedItem.Render(item.Display))
			} else {
				line.WriteString(styles.Item.Render(item.Display))
			}
		}

		s.WriteString(line.String())
		s.WriteString("\n")
	}

	// Instructions
	s.WriteString(styles.Instructions.Render("\n↑/↓ or j/k: navigate • space: select/deselect • d: delete selected • q: quit"))

	// Status messages
	if m.cleaning {
		s.WriteString("\n")
		s.WriteString(styles.Cleaning.Render("🔄 Cleaning up..."))
	}

	if m.successMsg != "" {
		s.WriteString("\n")
		s.WriteString(styles.SuccessMessage.Render("✅ " + m.successMsg))
	}

	if m.errorMsg != "" {
		s.WriteString("\n")
		s.WriteString(styles.ErrorMessage.Render("❌ " + m.errorMsg))
	}

	return s.String()
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

	images := docker.FilterUserImages(m.images)
	containers := docker.FilterUserContainers(m.containers)

	// Add images section
	m.items = append(m.items, SelectableItem{
		Type:    ItemTypeHeader,
		Display: "📦 Docker Images",
	})

	for _, img := range images {
		name := "<none>"
		if len(img.RepoTags) > 0 && img.RepoTags[0] != "<none>:<none>" {
			name = img.RepoTags[0]
		}
		m.items = append(m.items, SelectableItem{
			ID:      img.ID,
			Type:    ItemTypeImage,
			Display: fmt.Sprintf("%s (ID: %s, Size: %s)", name, docker.ShortID(img.ID), docker.HumanSize(img.Size)),
		})
	}

	// Add containers section
	m.items = append(m.items, SelectableItem{
		Type:    ItemTypeHeader,
		Display: "🏃 Docker Containers",
	})

	for _, container := range containers {
		name := "<none>"
		if len(container.Names) > 0 {
			name = container.Names[0]
			// Remove leading slash from container name
			if strings.HasPrefix(name, "/") {
				name = name[1:]
			}
		}
		// SizeRw is the writable layer; SizeRootFs is the read-only image.
		// Sum them so the user sees the actual disk footprint of removing
		// this container.
		totalSize := container.SizeRw + container.SizeRootFs
		m.items = append(m.items, SelectableItem{
			ID:      container.ID,
			Type:    ItemTypeContainer,
			Display: fmt.Sprintf("%s (ID: %s, Image: %s, Size: %s)", name, docker.ShortID(container.ID), container.Image, docker.HumanSize(totalSize)),
		})
	}
}
