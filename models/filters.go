package models

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/filter"
)

// olderThanSteps are the ages the older-than key cycles through, starting
// from off.
var olderThanSteps = []time.Duration{0, 7 * filter.Day, 30 * filter.Day, 90 * filter.Day}

// nextOlderThan returns the older-than age after d in olderThanSteps.
func nextOlderThan(d time.Duration) time.Duration {
	i := slices.Index(olderThanSteps, d) // -1, so off, if d is not a step
	return olderThanSteps[(i+1)%len(olderThanSteps)]
}

// clock returns the time item ages are measured from.
func (m *Model) clock() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

// setFilters changes the filter options with change and rebuilds the list.
func (m *Model) setFilters(change func(*filter.Options)) {
	m.relist(func() { change(&m.filters) })
}

// setQuery changes the search query and rebuilds the list. Surrounding
// spaces are ignored, so a query of spaces searches for nothing.
func (m *Model) setQuery(q string) {
	if q = strings.TrimSpace(q); q != m.query {
		m.relist(func() { m.query = q })
	}
}

// clearFilters turns every filter and the search off. The sort order stays.
func (m *Model) clearFilters() {
	m.search.Reset()
	if m.filters.Active() || m.query != "" {
		m.relist(func() { m.filters, m.query = filter.Options{}, "" })
	}
}

// relist applies change, which alters what the list shows, and rebuilds the
// list. The cursor stays on the row it was on; if that row is gone, it stays
// at the same index.
func (m *Model) relist(change func()) {
	key, ok := m.rowKey(m.cursor)
	change()
	m.updateItems()
	if !ok {
		return
	}
	for i := range m.items {
		if k, _ := m.rowKey(i); k == key {
			m.cursor = i
			return
		}
	}
}

// shown returns the rows of one section that the list shows: those matching
// the search (see matchesQuery).
func (m *Model) shown(rows []SelectableItem) []SelectableItem {
	if m.query == "" {
		return rows
	}
	return slices.DeleteFunc(rows, func(item SelectableItem) bool {
		return !m.matchesQuery(item)
	})
}

// matchesQuery reports whether item's name or text contains the search
// query, ignoring case.
func (m *Model) matchesQuery(item SelectableItem) bool {
	q := strings.ToLower(m.query)
	return strings.Contains(strings.ToLower(item.Name), q) ||
		strings.Contains(strings.ToLower(item.Display), q)
}

// fetchedIDs returns the ID of every resource in the last fetch that a row
// may stand for, whether or not it is listed: images, containers, volumes
// and networks, and the build cache while it has something reclaimable.
// Protected resources are included; callers check m.protected.
func (m *Model) fetchedIDs() map[string]struct{} {
	ids := make(map[string]struct{}, len(m.images)+len(m.containers)+len(m.volumes)+len(m.networks)+1)
	for _, img := range m.images {
		ids[img.ID] = struct{}{}
	}
	for _, c := range m.containers {
		ids[c.ID] = struct{}{}
	}
	for _, v := range m.volumes {
		ids[v.Name] = struct{}{}
	}
	for _, n := range m.networks {
		ids[n.ID] = struct{}{}
	}
	if m.diskUsageErr == nil && m.diskUsage.BuildCache.Reclaimable > 0 {
		ids[docker.BuildCacheID] = struct{}{}
	}
	return ids
}

// hiddenSelected counts the selected items the filters or the search hide.
// A delete leaves them alone.
func (m Model) hiddenSelected() int {
	n := len(m.selected)
	for _, item := range m.items {
		if item.Type == ItemTypeHeader {
			continue
		}
		if _, ok := m.selected[item.ID]; ok {
			n--
		}
	}
	return n
}

// filterBar is the status line describing what narrows the list, e.g.
// `Filters: dangling · older than 30d · "postgres" (esc clears)   +3
// selected hidden by filters`. It is empty when there is nothing to say.
// While the search input is open it shows the query itself, so the bar
// leaves it out.
func (m Model) filterBar() string {
	var parts []string

	labels := m.filters.Labels()
	if m.query != "" && !m.searching {
		labels = append(labels, `"`+m.query+`"`)
	}
	if len(labels) > 0 {
		bar := "Filters: " + strings.Join(labels, " · ")
		if !m.searching {
			bar += " (esc clears)"
		}
		parts = append(parts, bar)
	}

	if n := m.hiddenSelected(); n > 0 {
		parts = append(parts, fmt.Sprintf("+%d selected hidden by filters", n))
	}
	return strings.Join(parts, "   ")
}

// newSearchInput returns the search input. Its cursor does not blink, so
// the input needs no timer messages.
func newSearchInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "search names"
	ti.Cursor.SetMode(cursor.CursorStatic)
	return ti
}

// startSearch opens the search input on the current query.
func (m *Model) startSearch() tea.Cmd {
	m.searching = true
	m.search.SetValue(m.query)
	m.search.CursorEnd()
	return m.search.Focus()
}

// updateSearch handles a key press while the search input is open. Every key
// edits the query, and the list follows as it is typed, except: enter keeps
// the query and returns to the list, esc clears it and returns to the list,
// and ctrl+c quits.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case key.Matches(msg, m.searchKeys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.searchKeys.Apply):
		m.searching = false
		m.search.Blur()

	case key.Matches(msg, m.searchKeys.Cancel):
		m.searching = false
		m.search.Blur()
		m.search.Reset()
		m.setQuery("")

	default:
		m.search, cmd = m.search.Update(msg)
		m.setQuery(m.search.Value())
	}
	m.scrollToCursor()
	return m, cmd
}
