package models

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/minhajul/docker-cleaner/styles"
)

// window is the part of the list View draws: rows [start, end), preceded by
// the header of their section when start is not itself a header.
type window struct {
	start, end int
	pinned     int // index of the pinned section header, or -1
}

// hiddenAbove is the number of rows above the window that are not on screen.
func (w window) hiddenAbove() int {
	if w.pinned >= 0 {
		return w.start - 1
	}
	return w.start
}

// rowView renders list row i on its own; section headers take two lines
// because of their top margin.
func (m Model) rowView(i int) string {
	item := m.items[i]
	var line strings.Builder

	if item.Type == ItemTypeHeader {
		// Write the header's top margin as blank lines of its own, so the
		// cursor lands beside the header text rather than on the margin.
		line.WriteString(strings.Repeat("\n", styles.Header.GetMarginTop()))
	}

	// Cursor indicator
	if m.cursor == i {
		line.WriteString(styles.Cursor.Render("▶ "))
	} else {
		line.WriteString("  ")
	}

	// Handle different item types
	if item.Type == ItemTypeHeader {
		// Headers don't have checkboxes
		line.WriteString(styles.Header.MarginTop(0).Render(item.Display))
		return line.String()
	}

	if m.isProtected(item) {
		line.WriteString(m.protectedRowView(item))
		return line.String()
	}

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
	return line.String()
}

// pinnedView renders section header i pinned above the window. It drops the
// header's top margin so it costs a single line.
func (m Model) pinnedView(i int) string {
	return "  " + styles.Header.MarginTop(0).Render(m.items[i].Display)
}

// moreView renders a "↑ N more" / "↓ N more" scroll indicator.
func moreView(arrow string, n int) string {
	return styles.ScrollIndicator.Render(fmt.Sprintf("  %s %d more", arrow, n))
}

// listHeight returns how many terminal lines the list may use: the terminal
// height minus the title and footer as currently rendered, so it follows the
// footer as full help or status messages come and go. ok is false until the
// terminal size is known, in which case the whole list is drawn.
func (m Model) listHeight() (lines int, ok bool) {
	if m.height <= 0 {
		return 0, false
	}
	chrome := lipgloss.Height(m.titleView()) + lipgloss.Height(m.footerView())
	return max(m.height-chrome, 0), true
}

// windowAt lays out the list starting at row start in avail lines. The pinned
// header and the scroll indicators are counted against avail. At least one row
// is always included, even if it does not fit.
func (m Model) windowAt(start, avail int) window {
	w := window{start: start, end: start, pinned: -1}
	if start >= len(m.items) {
		return w
	}

	used := 0
	if m.items[start].Type != ItemTypeHeader {
		for i := start - 1; i >= 0; i-- {
			if m.items[i].Type == ItemTypeHeader {
				w.pinned = i
				used += lipgloss.Height(m.pinnedView(i))
				break
			}
		}
	}
	if w.hiddenAbove() > 0 {
		used++ // "↑ N more"
	}

	heights := make([]int, 0, avail)
	for w.end < len(m.items) {
		h := lipgloss.Height(m.rowView(w.end))
		if used+h > avail {
			break
		}
		heights = append(heights, h)
		used += h
		w.end++
	}
	if w.end < len(m.items) {
		// Rows are hidden below: give up rows until "↓ N more" fits.
		for used+1 > avail && len(heights) > 0 {
			used -= heights[len(heights)-1]
			heights = heights[:len(heights)-1]
			w.end--
		}
	}
	w.end = max(w.end, start+1)
	return w
}

// window returns the part of the list View draws at the current offset.
func (m Model) window() window {
	avail, ok := m.listHeight()
	if !ok {
		return window{start: 0, end: len(m.items), pinned: -1}
	}
	return m.windowAt(m.offset, avail)
}

// scrollToCursor clamps m.offset into the list and scrolls by the minimum
// needed to keep the cursor on screen. If the list ends above the bottom of
// the screen it scrolls back up to fill the space, e.g. after the list
// shrinks or the terminal grows.
func (m *Model) scrollToCursor() {
	avail, ok := m.listHeight()
	if !ok || len(m.items) == 0 {
		m.offset = 0
		return
	}
	m.offset = max(0, min(m.offset, len(m.items)-1))

	visible := func(start int) bool {
		return m.cursor < m.windowAt(start, avail).end
	}
	switch {
	case m.cursor < m.offset:
		m.offset = m.cursor
	case !visible(m.offset):
		// Every row takes at least one line, so no window starting more
		// than avail rows above the cursor can reach it.
		start := max(m.offset+1, m.cursor-avail+1)
		for start < m.cursor && !visible(start) {
			start++
		}
		m.offset = start
	}

	for m.offset > 0 && m.windowAt(m.offset-1, avail).end == len(m.items) {
		m.offset--
	}
}

// pageSize is how far pgup/pgdown move the cursor: the number of rows on
// screen, or the whole list before the terminal size is known.
func (m Model) pageSize() int {
	w := m.window()
	return max(w.end-w.start, 1)
}

// listView renders the visible rows, each followed by a newline, with the
// pinned header and scroll indicators.
func (m Model) listView() string {
	w := m.window()

	var s strings.Builder
	if n := w.hiddenAbove(); n > 0 {
		s.WriteString(moreView("↑", n))
		s.WriteString("\n")
	}
	if w.pinned >= 0 {
		s.WriteString(m.pinnedView(w.pinned))
		s.WriteString("\n")
	}
	for i := w.start; i < w.end; i++ {
		s.WriteString(m.rowView(i))
		s.WriteString("\n")
	}
	if n := len(m.items) - w.end; n > 0 {
		s.WriteString(moreView("↓", n))
		s.WriteString("\n")
	}
	return s.String()
}
