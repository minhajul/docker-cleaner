package models

import (
	"fmt"
	"os"
	"strings"

	"docker-cleaner/docker"
	"docker-cleaner/styles"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
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
	selected     map[string]struct{} // Docker IDs of selected items
	dockerClient docker.API
	images       []dockerClient.APIImages
	containers   []dockerClient.APIContainers
	errorMsg     string
	successMsg   string
	cleaning     bool
	keys         keyMap
	help         help.Model
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

	return newModel(client), nil
}

// newModel returns a ready-to-use Model backed by cli. Tests pass a fake.
func newModel(cli docker.API) Model {
	return Model{
		selected:     make(map[string]struct{}),
		dockerClient: cli,
		keys:         defaultKeyMap(),
		help:         help.New(),
	}
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
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}

		case key.Matches(msg, m.keys.Toggle):
			// Only allow selection of non-header items
			if m.cursor < len(m.items) && m.items[m.cursor].Type != ItemTypeHeader {
				id := m.items[m.cursor].ID
				if _, ok := m.selected[id]; ok {
					delete(m.selected, id)
				} else {
					if m.selected == nil {
						m.selected = make(map[string]struct{})
					}
					m.selected[id] = struct{}{}
				}
			}
		case key.Matches(msg, m.keys.Delete):
			if len(m.selected) > 0 {
				// Clear previous messages when starting new cleanup
				m.errorMsg = ""
				m.successMsg = ""
				m.cleaning = true
				// Snapshot now: the command runs on another goroutine and
				// must not read m.selected while Update mutates it.
				items := m.getSelectionForCleanup()
				return m, func() tea.Msg {
					return docker.CleanupMsg{ItemsToClean: items}
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
		m.selected = make(map[string]struct{})
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
			_, isSelected := m.selected[item.ID]
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

	s.WriteString(styles.Instructions.Render(m.help.View(m.keys)))

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

// getSelectionForCleanup returns the selected items, containers first so an
// image is never removed before a container that uses it, and otherwise in
// list order. Iterating m.items rather than the selection map keeps the order
// deterministic.
func (m *Model) getSelectionForCleanup() []docker.CleanupItem {
	var items []docker.CleanupItem
	for _, typ := range []ItemType{ItemTypeContainer, ItemTypeImage} {
		for _, item := range m.items {
			if item.Type != typ {
				continue
			}
			if _, ok := m.selected[item.ID]; ok {
				items = append(items, docker.CleanupItem{
					ID:   item.ID,
					Type: docker.CleanupItemType(item.Type),
				})
			}
		}
	}
	return items
}

// reconcileSelection keeps the selection and cursor consistent with a freshly
// rebuilt m.items: selected IDs that are no longer listed are dropped, and the
// cursor is clamped into range (0 when the list is empty).
func (m *Model) reconcileSelection() {
	listed := make(map[string]struct{}, len(m.items))
	for _, item := range m.items {
		if item.Type != ItemTypeHeader {
			listed[item.ID] = struct{}{}
		}
	}
	for id := range m.selected {
		if _, ok := listed[id]; !ok {
			delete(m.selected, id)
		}
	}

	m.cursor = max(0, min(m.cursor, len(m.items)-1))
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
			// Remove leading slash from container name
			name = strings.TrimPrefix(container.Names[0], "/")
		}
		// Show the state so running containers, which removal will refuse,
		// stand out.
		state := container.State
		if state == "" {
			state = "unknown"
		}
		// Mirror `docker ps -s`: SizeRw is the container's writable layer,
		// which is all a delete frees; SizeRootFs (virtual) also counts the
		// image layers, which stay on disk until the image is removed.
		size := fmt.Sprintf("%s (virtual %s)", docker.HumanSize(container.SizeRw), docker.HumanSize(container.SizeRootFs))
		m.items = append(m.items, SelectableItem{
			ID:      container.ID,
			Type:    ItemTypeContainer,
			Display: fmt.Sprintf("%s (ID: %s, Image: %s, State: %s, Size: %s)", name, docker.ShortID(container.ID), container.Image, state, size),
		})
	}

	m.reconcileSelection()
}
