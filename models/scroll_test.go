package models

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/minhajul/docker-cleaner/docker"
)

// manyImages returns a fetch message for n images with distinct IDs.
func manyImages(n int) docker.ImagesFetchedMsg {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("sha256:%012d%052d", i, 0)
	}
	return images(ids...)
}

// sized returns a model listing n images in a terminal of the given height.
func sized(n, height int) Model {
	m := newModel(nil)
	m = updated(m, tea.WindowSizeMsg{Width: 80, Height: height})
	m = updated(m, manyImages(n))
	return updated(m, containers("cccccccccccccccc0", "cccccccccccccccc1"))
}

// checkFits fails the test unless View fits in the terminal, keeps the title
// and footer on screen, and draws the cursor on the row it points at.
func checkFits(t *testing.T, m Model) {
	t.Helper()
	view := m.View()
	if got := lipgloss.Height(view); got > m.height {
		t.Fatalf("cursor %d: view is %d lines, terminal is %d:\n%s", m.cursor, got, m.height, view)
	}
	if !strings.Contains(view, "Docker Cleaner") {
		t.Fatalf("cursor %d: title missing:\n%s", m.cursor, view)
	}
	if !strings.Contains(view, "quit") {
		t.Fatalf("cursor %d: footer missing:\n%s", m.cursor, view)
	}

	var cursorLine string
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "▶") {
			cursorLine = line
		}
	}
	item := m.items[m.cursor]
	want := item.Display
	if item.Type != ItemTypeHeader {
		want = docker.ShortID(item.ID)
	}
	if !strings.Contains(cursorLine, want) {
		t.Fatalf("cursor %d (%q) not drawn; cursor line %q in:\n%s", m.cursor, want, cursorLine, view)
	}
}

func TestScrollKeepsCursorOnScreen(t *testing.T) {
	m := sized(200, 20)
	last := len(m.items) - 1

	for m.cursor < last {
		m = press(m, "j")
		checkFits(t, m)
	}
	for m.cursor > 0 {
		m = press(m, "k")
		checkFits(t, m)
	}
}

func TestScrollJumps(t *testing.T) {
	pgdown := tea.KeyMsg{Type: tea.KeyPgDown}
	pgup := tea.KeyMsg{Type: tea.KeyPgUp}

	tests := []struct {
		name string
		keys []tea.Msg
		want func(m Model) int // expected cursor
	}{
		{"G goes to last row", []tea.Msg{keyMsg("G")}, func(m Model) int { return len(m.items) - 1 }},
		{"end goes to last row", []tea.Msg{tea.KeyMsg{Type: tea.KeyEnd}}, func(m Model) int { return len(m.items) - 1 }},
		{"g goes to first row", []tea.Msg{keyMsg("G"), keyMsg("g")}, func(Model) int { return 0 }},
		{"home goes to first row", []tea.Msg{keyMsg("G"), tea.KeyMsg{Type: tea.KeyHome}}, func(Model) int { return 0 }},
		{"pgdown moves a page", []tea.Msg{pgdown}, func(m Model) int { return sized(200, 20).pageSize() }},
		{"pgdown stops at last row", repeat(pgdown, 100), func(m Model) int { return len(m.items) - 1 }},
		{"pgup stops at first row", append(repeat(pgdown, 3), repeat(pgup, 10)...), func(Model) int { return 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := sized(200, 20)
			for _, msg := range tt.keys {
				m = updated(m, msg)
				checkFits(t, m)
			}
			if want := tt.want(m); m.cursor != want {
				t.Errorf("cursor = %d, want %d", m.cursor, want)
			}
		})
	}
}

func TestScrollPinsSectionHeader(t *testing.T) {
	m := sized(200, 20)
	m = press(m, "G")
	view := m.View()
	if !strings.Contains(view, "Docker Containers") {
		t.Errorf("containers header should be on screen:\n%s", view)
	}

	m.cursor = 100
	m = press(m, "k") // any key re-scrolls to the cursor
	view = m.View()
	if !strings.Contains(view, "Docker Images") {
		t.Errorf("images header should be pinned mid-section:\n%s", view)
	}
	if !strings.Contains(view, "↑") || !strings.Contains(view, "↓") {
		t.Errorf("both scroll indicators should show mid-list:\n%s", view)
	}
}

func TestScrollOffsetClampsWhenListShrinks(t *testing.T) {
	m := sized(200, 20)
	m = press(m, "G")
	if m.offset == 0 {
		t.Fatal("offset should have moved after G")
	}

	m = updated(m, manyImages(3))
	if m.offset != 0 {
		t.Errorf("offset = %d after shrinking to a list that fits, want 0", m.offset)
	}
	checkFits(t, m)

	m = updated(m, images())
	m = updated(m, containers())
	if m.offset != 0 {
		t.Errorf("offset = %d for a list of headers, want 0", m.offset)
	}
	checkFits(t, m)
}

func TestScrollFollowsTerminalHeight(t *testing.T) {
	m := sized(200, 20)
	m = press(m, "G")
	m = press(m, "?") // full help: a taller footer
	checkFits(t, m)

	m = updated(m, tea.WindowSizeMsg{Width: 80, Height: 12})
	checkFits(t, m)
	m = updated(m, tea.WindowSizeMsg{Width: 80, Height: 300})
	if m.offset != 0 {
		t.Errorf("offset = %d once everything fits, want 0", m.offset)
	}
	checkFits(t, m)
}

func TestNoHeightRendersEverything(t *testing.T) {
	m := newModel(nil)
	m = updated(m, manyImages(200))
	m = press(m, "G")
	view := m.View()
	for _, item := range m.items {
		want := item.Display
		if item.Type != ItemTypeHeader {
			want = docker.ShortID(item.ID)
		}
		if !strings.Contains(view, want) {
			t.Fatalf("row %q missing before the terminal size is known", want)
		}
	}
	if strings.ContainsAny(view, "↑↓") {
		t.Errorf("no scroll indicators expected without a height:\n%s", view)
	}
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func repeat(msg tea.Msg, n int) []tea.Msg {
	msgs := make([]tea.Msg, n)
	for i := range msgs {
		msgs[i] = msg
	}
	return msgs
}
