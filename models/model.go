package models

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/config"
	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/filter"
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
	// Size is the disk space deleting the item frees: an image's Size, a
	// container's writable layer (SizeRw), a volume's size (0 if
	// unknown) or the build cache's reclaimable space.
	Size int64
	// State is a container's state as reported by Docker (e.g. "running");
	// empty for other items and headers.
	State string
	// Name is an image's first tag or a container's name ("<none>" if it
	// has neither), or a volume's or network's name, for naming the item
	// in messages.
	Name string
	// Created is when the item was created; zero if unknown, as it is for
	// networks and the build cache.
	Created time.Time
}

type Model struct {
	items        []SelectableItem
	cursor       int
	selected     map[string]struct{} // Docker IDs of selected items
	dockerClient docker.API
	images       []dockerClient.APIImages
	containers   []dockerClient.APIContainers
	volumes      []dockerClient.Volume
	networks     []dockerClient.Network
	// diskUsage adds volume sizes and the build cache to the lists;
	// diskUsageErr is why it could not be fetched, if it could not.
	diskUsage    docker.DiskUsage
	diskUsageErr error
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
	// protector decides which resources are protected from deletion.
	protector docker.Protector
	// protected maps the ID of every protected resource in the last fetch,
	// listed or not, to the reason it is protected.
	protected map[string]docker.Reason
	// showProtected lists protected resources as read-only rows instead of
	// hiding them.
	showProtected bool
	// filters narrow the lists (see filter.Options), and a non-empty query
	// keeps only the rows whose name or text contains it, ignoring case.
	// Selected items they hide stay selected but are not deleted (see
	// reconcileSelection).
	filters filter.Options
	query   string
	// search is the search input, which takes every key while searching
	// is set; searchKeys are its keys.
	search     textinput.Model
	searching  bool
	searchKeys searchKeyMap
	// now is the clock item ages are measured from; nil means time.Now.
	now func() time.Time
	// sortOrder orders the rows within each section.
	sortOrder filter.SortOrder
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
	volumes    int
	networks   int
	buildCache bool
	size       int64 // bytes freed: the selected rows' Size (see SelectableItem)
	// Selected containers Docker won't remove without force, by state.
	running    int
	paused     int
	restarting int
	// hidden is the number of selected items the filters or the search
	// hide; they are not deleted.
	hidden int
}

// active is the number of selected containers Docker won't remove without
// force: running, paused or restarting ones.
func (p *pendingDelete) active() int {
	return p.running + p.paused + p.restarting
}

// InitialModel creates a new Model connected to the Docker daemon, found
// through DOCKER_HOST or the current Docker context (see
// docker.ResolveEndpoint), protecting the resources listed in the config
// file at configPath (the default location if empty; see config.Load). If
// the config file is invalid or Docker cannot be reached, an error is
// returned (and for Docker a diagnostic naming the endpoint is written to
// stderr). The caller is responsible for exiting the program on error.
func InitialModel(configPath string) (Model, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return Model{}, err
	}
	protector, err := docker.NewProtector(cfg.Protect)
	if err != nil {
		return Model{}, err
	}

	client, endpoint, err := docker.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to connect to Docker daemon:", err)
		return Model{}, err
	}

	if err := client.Ping(); err != nil {
		fmt.Fprintln(os.Stderr, styles.ErrorMessage.Render(fmt.Sprintf("Cannot reach Docker at %s. Is it running?", endpoint)))
		return Model{}, err
	}

	m := newModel(client)
	m.protector = protector
	return m, nil
}

// newModel returns a ready-to-use Model backed by cli. Tests pass a fake.
func newModel(cli docker.API) Model {
	return Model{
		selected:     make(map[string]struct{}),
		dockerClient: cli,
		keys:         defaultKeyMap(),
		help:         help.New(),
		confirmKeys:  defaultConfirmKeyMap(),
		search:       newSearchInput(),
		searchKeys:   defaultSearchKeyMap(),
		now:          time.Now,
	}
}

func (m Model) Init() tea.Cmd {
	return m.refreshCmd()
}

// refreshCmd returns a tea.Cmd that runs every fetch (see fetchCmds) using
// the shared Docker client.
func (m Model) refreshCmd() tea.Cmd {
	return tea.Batch(m.fetchCmds()...)
}

// fetchCmds returns the fetches that refresh the lists. Each answers with
// exactly one message, which Update counts against m.refreshing.
func (m Model) fetchCmds() []tea.Cmd {
	return []tea.Cmd{
		docker.FetchDockerImages(m.dockerClient),
		docker.FetchDockerContainers(m.dockerClient),
		docker.FetchVolumes(m.dockerClient),
		docker.FetchNetworks(m.dockerClient),
		docker.FetchDiskUsage(m.dockerClient),
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirm != nil {
			return m.updateConfirm(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
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
				m.refreshing = len(m.fetchCmds())
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

		case key.Matches(msg, m.keys.ShowProtected):
			m.toggleShowProtected()

		case key.Matches(msg, m.keys.Delete):
			// Open the confirmation dialog; nothing is deleted until the
			// user answers it (see updateConfirm). Selected items hidden
			// by the filters are not deleted, so there may be nothing to
			// ask about.
			if p := m.newPendingDelete(); len(p.items) > 0 {
				m.confirm = p
				m.confirmKeys.Force.SetEnabled(m.confirm.active() > 0)
			}

		case key.Matches(msg, m.keys.Search):
			return m, m.startSearch()

		case key.Matches(msg, m.keys.ClearFilters):
			m.clearFilters()

		case key.Matches(msg, m.keys.FilterDangling):
			m.setFilters(func(o *filter.Options) { o.Dangling = !o.Dangling })

		case key.Matches(msg, m.keys.FilterStopped):
			m.setFilters(func(o *filter.Options) { o.Stopped = !o.Stopped })

		case key.Matches(msg, m.keys.FilterUnused):
			m.setFilters(func(o *filter.Options) { o.Unused = !o.Unused })

		case key.Matches(msg, m.keys.FilterOlderThan):
			m.setFilters(func(o *filter.Options) { o.OlderThan = nextOlderThan(o.OlderThan) })

		case key.Matches(msg, m.keys.Sort):
			m.relist(func() { m.sortOrder = m.sortOrder.Next() })
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

	case docker.VolumesFetchedMsg:
		m.volumes = msg.Volumes
		m.refreshing = max(0, m.refreshing-1)
		m.updateItems()
		return m, nil

	case docker.NetworksFetchedMsg:
		m.networks = msg.Networks
		m.refreshing = max(0, m.refreshing-1)
		m.updateItems()
		return m, nil

	case docker.DiskUsageFetchedMsg:
		// On error keep nothing from an earlier fetch: stale sizes would
		// mislead more than missing ones.
		m.diskUsage, m.diskUsageErr = msg.Usage, msg.Err
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
// container is running, paused or restarting) deletes with containers
// force-removed, q or ctrl+c
// quits, and everything else cancels.
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
		r.failures = append(r.failures, fmt.Sprintf("%s: %s", failureLabel(f.Item, names[f.Item.ID]), f.Reason))
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

	p := &pendingDelete{items: m.getSelectionForCleanup(), hidden: m.hiddenSelected()}
	for _, it := range p.items {
		row := rows[it.ID]
		p.size += row.Size
		switch row.Type {
		case ItemTypeImage:
			p.images++
		case ItemTypeVolume:
			p.volumes++
		case ItemTypeNetwork:
			p.networks++
		case ItemTypeBuildCache:
			p.buildCache = true
		case ItemTypeContainer:
			p.containers++
			switch row.State {
			case "running":
				p.running++
			case "paused":
				p.paused++
			case "restarting":
				p.restarting++
			}
		}
	}
	return p
}

// prompt is the dialog's question, e.g. "Delete 3 images, 2 containers
// (1.4 GB)? y/N" or "Delete 2 images, 1 volume, build cache (3.4 GB)? y/N".
// Only the types selected are named.
func (p *pendingDelete) prompt() string {
	var parts []string
	if p.images > 0 {
		parts = append(parts, plural(p.images, "image"))
	}
	if p.containers > 0 {
		parts = append(parts, plural(p.containers, "container"))
	}
	if p.volumes > 0 {
		parts = append(parts, plural(p.volumes, "volume"))
	}
	if p.networks > 0 {
		parts = append(parts, plural(p.networks, "network"))
	}
	if p.buildCache {
		parts = append(parts, "build cache")
	}
	return fmt.Sprintf("Delete %s (%s)? y/N", strings.Join(parts, ", "), docker.HumanSize(p.size))
}

// warning explains what happens to running, paused and restarting
// containers, or returns "" when none are selected. It names their state:
// "⚠ 2 containers are paused and won't be removed with y; press f to
// force-remove them", or with mixed states "⚠ 3 containers (2 running,
// 1 paused) won't be removed with y; …".
func (p *pendingDelete) warning() string {
	n := p.active()
	if n == 0 {
		return ""
	}
	var states, counts []string
	for _, s := range []struct {
		count int
		state string
	}{{p.running, "running"}, {p.paused, "paused"}, {p.restarting, "restarting"}} {
		if s.count > 0 {
			states = append(states, s.state)
			counts = append(counts, fmt.Sprintf("%d %s", s.count, s.state))
		}
	}

	what := fmt.Sprintf("%d containers (%s)", n, strings.Join(counts, ", "))
	if len(states) == 1 {
		verb := "are"
		if n == 1 {
			verb = "is"
		}
		what = fmt.Sprintf("%s %s %s and", plural(n, "container"), verb, states[0])
	}
	it := "them"
	if n == 1 {
		it = "it"
	}
	return fmt.Sprintf("⚠ %s won't be removed with y; press f to force-remove %s", what, it)
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

	if m.searching {
		s.WriteString(m.search.View())
		s.WriteString("\n")
	}
	if bar := m.filterBar(); bar != "" {
		s.WriteString(styles.FilterBar.Render(bar))
		s.WriteString("\n")
	}

	if hint := m.protectedHint(); hint != "" {
		// Directly under the list, above the help's top margin.
		s.WriteString(styles.ProtectedHint.Render(hint))
		s.WriteString("\n")
	}

	switch {
	case m.confirm != nil:
		s.WriteString("\n")
		s.WriteString(styles.Confirm.Render(m.confirm.prompt()))
		if w := m.confirm.warning(); w != "" {
			s.WriteString("\n")
			s.WriteString(styles.Warning.Render(w))
		}
		if n := m.confirm.hidden; n > 0 {
			s.WriteString("\n")
			s.WriteString(styles.FilterBar.Render(plural(n, "selected item") + " hidden by filters will be kept"))
		}
		s.WriteString(styles.Instructions.Render(m.help.View(m.confirmKeys)))
	case m.searching:
		s.WriteString(styles.Instructions.Render(m.help.View(m.searchKeys)))
	default:
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
// deterministic. Protected items are never included, even if selected.
func (m *Model) getSelectionForCleanup() []docker.CleanupItem {
	var items []docker.CleanupItem
	for _, t := range docker.RemovalOrder() {
		typ := ItemType(t)
		for _, item := range m.items {
			if item.Type != typ || m.isProtected(item) {
				continue
			}
			if _, ok := m.selected[item.ID]; ok {
				items = append(items, docker.CleanupItem{
					ID:   item.ID,
					Type: docker.CleanupItemType(item.Type),
					// Prune with All only when a plain prune would
					// leave part of the reclaimable space shown.
					Force: typ == ItemTypeBuildCache && m.diskUsage.BuildCache.NeedsAll,
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
// tries to select (a no-op) rather than deselect. Protected rows are not
// selectable.
func (m *Model) allSelected(items []SelectableItem) bool {
	found := false
	for _, item := range items {
		if item.Type == ItemTypeHeader || m.isProtected(item) {
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
// Header and protected rows are skipped so they never enter the selection.
func (m *Model) setSelected(items []SelectableItem, on bool) {
	if m.selected == nil {
		m.selected = make(map[string]struct{})
	}
	for _, item := range items {
		if item.Type == ItemTypeHeader || m.isProtected(item) {
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
// rebuilt m.items: selected IDs whose resource is gone from Docker, or is now
// protected, are dropped, and the cursor is clamped into range (0 when the
// list is empty). Selected items that are only hidden by the filters or the
// search stay selected, so clearing them brings the selection back; until
// then they are left out of a delete (see getSelectionForCleanup) and
// counted in the filter bar (see hiddenSelected).
func (m *Model) reconcileSelection() {
	present := m.fetchedIDs()
	for id := range m.selected {
		if _, ok := present[id]; !ok || m.protected[id] != docker.NotProtected {
			delete(m.selected, id)
		}
	}

	m.cursor = max(0, min(m.cursor, len(m.items)-1))
}

func (m *Model) updateItems() {
	m.items = []SelectableItem{}

	m.protected = make(map[string]docker.Reason)
	now := m.clock()
	images := m.filters.Images(m.listedImages(), m.usage(), now)
	containers := m.filters.Containers(m.listedContainers(), now)

	// Add images section
	m.items = append(m.items, SelectableItem{
		Type:    ItemTypeHeader,
		Display: "📦 Docker Images",
	})

	var rows []SelectableItem
	for _, img := range images {
		name := "<none>"
		if len(img.RepoTags) > 0 && img.RepoTags[0] != "<none>:<none>" {
			name = img.RepoTags[0]
		}
		created := filter.ImageCreated(img)
		rows = append(rows, SelectableItem{
			ID:   img.ID,
			Type: ItemTypeImage,
			Display: fmt.Sprintf("%s (ID: %s, Size: %s%s)",
				name, docker.ShortID(img.ID), docker.HumanSize(img.Size), ageSuffix(created, now)),
			Size:    img.Size,
			Name:    name,
			Created: created,
		})
	}
	m.items = append(m.items, m.shown(rows)...)

	// Add containers section
	m.items = append(m.items, SelectableItem{
		Type:    ItemTypeHeader,
		Display: "🏃 Docker Containers",
	})

	rows = nil
	for _, container := range containers {
		name := "<none>"
		if len(container.Names) > 0 {
			// Remove leading slash from container name
			name = strings.TrimPrefix(container.Names[0], "/")
		}
		// Show the state so running and paused containers, which removal
		// will refuse, stand out.
		state := container.State
		if state == "" {
			state = "unknown"
		}
		// Mirror `docker ps -s`: SizeRw is the container's writable layer,
		// which is all a delete frees; SizeRootFs (virtual) also counts the
		// image layers, which stay on disk until the image is removed.
		size := fmt.Sprintf("%s (virtual %s)", docker.HumanSize(container.SizeRw), docker.HumanSize(container.SizeRootFs))
		created := filter.ContainerCreated(container)
		rows = append(rows, SelectableItem{
			ID:   container.ID,
			Type: ItemTypeContainer,
			Display: fmt.Sprintf("%s (ID: %s, Image: %s, State: %s, Size: %s%s)",
				name, docker.ShortID(container.ID), container.Image, state, size, ageSuffix(created, now)),
			Size:    container.SizeRw,
			State:   container.State,
			Name:    name,
			Created: created,
		})
	}
	m.items = append(m.items, m.shown(rows)...)

	m.items = append(m.items, m.volumeRows()...)
	m.items = append(m.items, m.networkRows()...)
	m.items = append(m.items, m.buildCacheRows()...)

	m.reconcileSelection()
	m.scrollToCursor()
}
