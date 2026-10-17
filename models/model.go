package models

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/styles"
)

type ItemType int

const (
	ItemTypeHeader ItemType = iota
	// The other values equal the matching docker.CleanupItemType.
	ItemTypeImage
	ItemTypeContainer
	ItemTypeVolume
	ItemTypeNetwork
	ItemTypeBuildCache
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
	// Name is an image's first tag or a container's name ("<none>" if it
	// has neither), for naming the item in messages.
	Name string
}

type Model struct {
	items        []SelectableItem
	cursor       int
	selected     map[string]struct{} // Docker IDs of selected items
	dockerClient docker.API
	images       []dockerClient.APIImages
	containers   []dockerClient.APIContainers
	errorMsg     string
	cleaning     bool
	refreshing   int // fetches still outstanding after the refresh key
	keys         keyMap
	help         help.Model
	width        int // terminal size; 0 until the first tea.WindowSizeMsg
	height       int
	offset       int // index of the first list row on screen
	// confirm is the delete awaiting the user's answer; nil when the
	// confirmation dialog is closed.
	confirm     *pendingDelete
	confirmKeys confirmKeyMap
	// report describes the last cleanup; nil before the first one and
	// while a cleanup runs.
	report *cleanupReport
}

// cleanupReport is what the model shows about a finished cleanup. It is
// built when the result arrives, while the failed items are still listed, so
// their names are known.
type cleanupReport struct {
	summary  string   // e.g. "Deleted 4 · Failed 1"
	failures []string // one "<short id> <name>: <reason>" line per failure
	// retry holds the failed images a forced removal may delete, with
	// Force set; f retries them.
	retry []docker.CleanupItem
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

// InitialModel creates a new Model connected to the Docker daemon, found
// through DOCKER_HOST or the current Docker context (see
// docker.ResolveEndpoint). If Docker cannot be reached, an error is returned
// (and a diagnostic naming the endpoint is written to stderr). The caller is
// responsible for exiting the program on error.
func InitialModel() (Model, error) {
	client, endpoint, err := docker.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to connect to Docker daemon:", err)
		return Model{}, err
	}

	if err := client.Ping(); err != nil {
		fmt.Fprintln(os.Stderr, styles.ErrorMessage.Render(fmt.Sprintf("Cannot reach Docker at %s. Is it running?", endpoint)))
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
			m.toggleAtCursor()

		case key.Matches(msg, m.keys.SelectAll):
			m.setSelected(m.items, true)

		case key.Matches(msg, m.keys.SelectNone):
			m.selected = make(map[string]struct{})

		case key.Matches(msg, m.keys.Refresh):
			// A cleanup already refreshes the lists when it finishes.
			if !m.cleaning {
				m.errorMsg = ""
				m.report = nil
				m.refreshing = 2 // images and containers
				return m, m.refreshCmd()
			}

		case key.Matches(msg, m.keys.PageUp):
			m.cursor = max(m.cursor-m.pageSize(), 0)

		case key.Matches(msg, m.keys.PageDown):
			m.cursor = max(min(m.cursor+m.pageSize(), len(m.items)-1), 0)

		case key.Matches(msg, m.keys.Home):
			m.cursor = 0

		case key.Matches(msg, m.keys.End):
			m.cursor = max(len(m.items)-1, 0)

		case key.Matches(msg, m.keys.ForceRetry):
			// Enabled only while m.report.retry is non-empty.
			return m.startCleanup(m.report.retry)

		case key.Matches(msg, m.keys.Delete):
			// Open the confirmation dialog; nothing is deleted until the
			// user answers it (see updateConfirm).
			if len(m.selected) > 0 {
				m.confirm = m.newPendingDelete()
				m.confirmKeys.Force.SetEnabled(m.confirm.running > 0)
			}
		}
		m.scrollToCursor()

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.scrollToCursor()
		return m, nil

	case docker.ImagesFetchedMsg:
		m.images = msg.Images
		m.refreshing = max(0, m.refreshing-1)
		m.updateItems()
		return m, nil

	case docker.ContainersFetchedMsg:
		m.containers = msg.Containers
		m.refreshing = max(0, m.refreshing-1)
		m.updateItems()
		return m, nil

	case docker.CleanupMsg:
		return m, docker.PerformCleanup(m.dockerClient, msg.ItemsToClean)

	case docker.CleanupResultMsg:
		m.cleaning = false
		// Failed items stay selected so the user can retry them.
		for _, item := range msg.Deleted {
			delete(m.selected, item.ID)
		}
		m.setReport(m.newReport(msg))
		// Refresh even after failures: some items may be gone regardless.
		return m, m.refreshCmd()

	case docker.ErrMsg:
		m.errorMsg = msg.Error()
		m.cleaning = false
		m.refreshing = 0
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
	m.setReport(nil)
	m.cleaning = true
	return m, func() tea.Msg {
		return docker.CleanupMsg{ItemsToClean: items}
	}
}

// setReport replaces the cleanup report, enabling the force-retry key only
// while the report has images to retry.
func (m *Model) setReport(r *cleanupReport) {
	m.report = r
	m.keys.ForceRetry.SetEnabled(r != nil && len(r.retry) > 0)
}

// newReport turns a cleanup result into the lines the model shows, naming
// each failed item from its row.
func (m *Model) newReport(res docker.CleanupResultMsg) *cleanupReport {
	names := make(map[string]string, len(m.items))
	for _, item := range m.items {
		if item.Type != ItemTypeHeader {
			names[item.ID] = item.Name
		}
	}

	r := &cleanupReport{summary: res.Summary(), retry: res.ForceRetry()}
	for _, f := range res.Failed {
		label := docker.ShortID(f.Item.ID)
		if name := names[f.Item.ID]; name != "" {
			label += " " + name
		}
		r.failures = append(r.failures, fmt.Sprintf("%s: %s", label, f.Reason))
	}
	return r
}

// retryHint offers the forced retry, e.g. "Press f to force-remove 2 images
// that are in use or have several tags".
func (r *cleanupReport) retryHint() string {
	if len(r.retry) == 1 {
		return "Press f to force-remove 1 image that is in use or has several tags"
	}
	return fmt.Sprintf("Press f to force-remove %d images that are in use or have several tags", len(r.retry))
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
	// Scroll on a copy too: status lines added since the last Update shrink
	// the room for the list and could otherwise push the cursor off screen.
	m.scrollToCursor()

	var s strings.Builder
	s.WriteString(m.titleView())
	s.WriteString("\n")
	s.WriteString(m.listView())
	s.WriteString(m.footerView())
	return s.String()
}

// titleView renders the title block above the list.
func (m Model) titleView() string {
	return styles.Title.Render("🐳 Docker Cleaner")
}

// footerView renders everything below the list: the help footer and any
// status messages.
func (m Model) footerView() string {
	var s strings.Builder

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

	if m.refreshing > 0 {
		s.WriteString("\n")
		s.WriteString(styles.Cleaning.Render("🔄 Refreshing..."))
	}

	if r := m.report; r != nil {
		s.WriteString("\n")
		if len(r.failures) == 0 {
			s.WriteString(styles.SuccessMessage.Render("✅ " + r.summary))
		} else {
			s.WriteString(styles.ErrorMessage.Render("❌ " + r.summary))
		}
		for _, line := range r.failures {
			s.WriteString("\n")
			s.WriteString(styles.Warning.Render("  " + line))
		}
		if len(r.retry) > 0 {
			s.WriteString("\n")
			s.WriteString(styles.Confirm.Render(r.retryHint()))
		}
	}

	if m.errorMsg != "" {
		s.WriteString("\n")
		s.WriteString(styles.ErrorMessage.Render("❌ " + m.errorMsg))
	}

	return s.String()
}

// getSelectionForCleanup returns the selected items by type in
// docker.RemovalOrder, containers first so an image, volume or network is
// never removed before a container that uses it, and otherwise in list order.
// Iterating m.items rather than the selection map keeps the order
// deterministic.
func (m *Model) getSelectionForCleanup() []docker.CleanupItem {
	var items []docker.CleanupItem
	for _, t := range docker.RemovalOrder() {
		typ := ItemType(t)
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

// toggleAtCursor flips the selection of the row under the cursor. On a
// section header it toggles the whole section instead: if every item in it is
// already selected they are all deselected, otherwise they are all selected.
func (m *Model) toggleAtCursor() {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return
	}
	if m.items[m.cursor].Type == ItemTypeHeader {
		section := m.sectionItems(m.cursor)
		m.setSelected(section, !m.allSelected(section))
		return
	}
	id := m.items[m.cursor].ID
	_, ok := m.selected[id]
	m.setSelected(m.items[m.cursor:m.cursor+1], !ok)
}

// sectionItems returns the rows belonging to the section whose header is at
// index header: everything up to the next header or the end of the list.
func (m *Model) sectionItems(header int) []SelectableItem {
	end := header + 1
	for end < len(m.items) && m.items[end].Type != ItemTypeHeader {
		end++
	}
	return m.items[header+1 : end]
}

// allSelected reports whether every selectable row in items is selected. It
// is false for a list with no selectable rows, so toggling an empty section
// tries to select (a no-op) rather than deselect.
func (m *Model) allSelected(items []SelectableItem) bool {
	found := false
	for _, item := range items {
		if item.Type == ItemTypeHeader {
			continue
		}
		if _, ok := m.selected[item.ID]; !ok {
			return false
		}
		found = true
	}
	return found
}

// setSelected selects (on) or deselects every non-header row in items.
// Header rows are skipped so they never enter the selection.
func (m *Model) setSelected(items []SelectableItem, on bool) {
	if m.selected == nil {
		m.selected = make(map[string]struct{})
	}
	for _, item := range items {
		if item.Type == ItemTypeHeader {
			continue
		}
		if on {
			m.selected[item.ID] = struct{}{}
		} else {
			delete(m.selected, item.ID)
		}
	}
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
			Name:    name,
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
			Name:    name,
		})
	}

	m.reconcileSelection()
	m.scrollToCursor()
}
