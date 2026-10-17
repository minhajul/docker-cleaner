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
	// Size is the disk space deleting the item frees: an image's Size, or a
	// container's writable layer (SizeRw).
	Size int64
	// State is a container's state as reported by Docker (e.g. "running");
	// empty for images and headers.
	State string
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
	// confirm is the delete awaiting the user's answer; nil when the
	// confirmation dialog is closed.
	confirm     *pendingDelete
	confirmKeys confirmKeyMap
}

// pendingDelete is a snapshot of the selection taken when the user presses
// delete, along with the totals the confirmation dialog shows. The selection
// cannot change while the dialog is open, so the snapshot stays accurate.
type pendingDelete struct {
	items      []docker.CleanupItem
	images     int
	containers int
	size       int64 // bytes freed: images' Size plus containers' SizeRw
	running    int   // selected containers that are running
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
		confirmKeys:  defaultConfirmKeyMap(),
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
		if m.confirm != nil {
			return m.updateConfirm(msg)
		}
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
			// Open the confirmation dialog; nothing is deleted until the
			// user answers it (see updateConfirm).
			if len(m.selected) > 0 {
				m.confirm = m.newPendingDelete()
				m.confirmKeys.Force.SetEnabled(m.confirm.running > 0)
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

// updateConfirm handles a key press while the confirmation dialog is open.
// The dialog closes on any key: y deletes, f (offered only when a selected
// container is running) deletes with containers force-removed, ctrl+c quits,
// and everything else cancels.
func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pending := m.confirm
	m.confirm = nil
	switch {
	case key.Matches(msg, m.confirmKeys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.confirmKeys.Force):
		for i := range pending.items {
			if pending.items[i].Type == docker.CleanupItemTypeContainer {
				pending.items[i].Force = true
			}
		}
		return m.startCleanup(pending.items)

	case key.Matches(msg, m.confirmKeys.Yes):
		return m.startCleanup(pending.items)
	}
	return m, nil
}

// startCleanup clears the previous status and returns the command that
// removes items. items must be a snapshot: the command runs on another
// goroutine and must not read m.selected while Update mutates it.
func (m Model) startCleanup(items []docker.CleanupItem) (Model, tea.Cmd) {
	m.errorMsg = ""
	m.successMsg = ""
	m.cleaning = true
	return m, func() tea.Msg {
		return docker.CleanupMsg{ItemsToClean: items}
	}
}

// newPendingDelete snapshots the current selection for the confirmation
// dialog, totalling it from the per-row Size and State set by updateItems.
func (m *Model) newPendingDelete() *pendingDelete {
	rows := make(map[string]SelectableItem, len(m.items))
	for _, item := range m.items {
		if item.Type != ItemTypeHeader {
			rows[item.ID] = item
		}
	}

	p := &pendingDelete{items: m.getSelectionForCleanup()}
	for _, it := range p.items {
		row := rows[it.ID]
		p.size += row.Size
		switch row.Type {
		case ItemTypeImage:
			p.images++
		case ItemTypeContainer:
			p.containers++
			if row.State == "running" {
				p.running++
			}
		}
	}
	return p
}

// prompt is the dialog's question, e.g. "Delete 3 images, 2 containers
// (1.4 GB)? y/N".
func (p *pendingDelete) prompt() string {
	var parts []string
	if p.images > 0 {
		parts = append(parts, plural(p.images, "image"))
	}
	if p.containers > 0 {
		parts = append(parts, plural(p.containers, "container"))
	}
	return fmt.Sprintf("Delete %s (%s)? y/N", strings.Join(parts, ", "), docker.HumanSize(p.size))
}

// warning explains what happens to running containers, or returns "" when
// none are selected.
func (p *pendingDelete) warning() string {
	switch p.running {
	case 0:
		return ""
	case 1:
		return "⚠ 1 container is running and won't be removed with y; press f to force-remove it"
	default:
		return fmt.Sprintf("⚠ %d containers are running and won't be removed with y; press f to force-remove them", p.running)
	}
}

// plural formats a count with its noun, adding an "s" unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
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

	if m.confirm != nil {
		s.WriteString("\n")
		s.WriteString(styles.Confirm.Render(m.confirm.prompt()))
		if w := m.confirm.warning(); w != "" {
			s.WriteString("\n")
			s.WriteString(styles.Warning.Render(w))
		}
		s.WriteString(styles.Instructions.Render(m.help.View(m.confirmKeys)))
	} else {
		s.WriteString(styles.Instructions.Render(m.help.View(m.keys)))
	}

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
			Size:    img.Size,
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
			Size:    container.SizeRw,
			State:   container.State,
		})
	}

	m.reconcileSelection()
}
