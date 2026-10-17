package models

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestConfirmQQuits(t *testing.T) {
	fake := newFake()
	m := loaded(t, fake, imgA)
	m = send(t, m, runes("d"))

	_, cmd := m.Update(runes("q"))
	if cmd == nil {
		t.Fatal("q should quit while confirming")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q returned %T, want tea.QuitMsg", cmd())
	}
	if len(fake.Removed) != 0 {
		t.Errorf("Removed = %v, want nothing", fake.Removed)
	}
}

func TestConfirmFooterNamesQ(t *testing.T) {
	m := loaded(t, newFake(), imgA)
	m = send(t, m, runes("d"))
	view := m.View()

	if !strings.Contains(view, "q quit") {
		t.Errorf("dialog footer should offer q to quit, got:\n%s", view)
	}
	if strings.Contains(view, "ctrl+c") {
		t.Errorf("dialog footer should name q, not ctrl+c, got:\n%s", view)
	}
}

func TestHeaderCursorBesideText(t *testing.T) {
	m := newModel(nil)
	m = updated(m, images(imgA))
	m = updated(m, containers(ctrX))

	for i, item := range m.items {
		if item.Type != ItemTypeHeader {
			continue
		}
		m.cursor = -1
		plain := m.rowView(i)
		m.cursor = i
		row := m.rowView(i)

		lines := strings.Split(row, "\n")
		last := lines[len(lines)-1]
		if !strings.Contains(last, "▶") || !strings.Contains(last, item.Display) {
			t.Errorf("header %q: cursor should share the header text's line, got %q", item.Display, row)
		}
		for _, l := range lines[:len(lines)-1] {
			if strings.TrimSpace(l) != "" {
				t.Errorf("header %q: margin line %q should be blank", item.Display, l)
			}
		}
		// The scroll logic sizes rows by height, so the cursor must not
		// change it.
		if got, want := lipgloss.Height(row), lipgloss.Height(plain); got != want || want != 2 {
			t.Errorf("header %q: height with cursor = %d, without = %d, want both 2", item.Display, got, want)
		}
	}
}

func TestConfirmWarningNamesStates(t *testing.T) {
	cases := []struct {
		p    pendingDelete
		want string
	}{
		{pendingDelete{containers: 1}, ""},
		{pendingDelete{containers: 1, running: 1}, "⚠ 1 container is running and won't be removed with y; press f to force-remove it"},
		{pendingDelete{containers: 2, paused: 1}, "⚠ 1 container is paused and won't be removed with y; press f to force-remove it"},
		{pendingDelete{containers: 2, restarting: 2}, "⚠ 2 containers are restarting and won't be removed with y; press f to force-remove them"},
		{pendingDelete{containers: 3, running: 2, paused: 1}, "⚠ 3 containers (2 running, 1 paused) won't be removed with y; press f to force-remove them"},
		{pendingDelete{containers: 3, running: 1, paused: 1, restarting: 1}, "⚠ 3 containers (1 running, 1 paused, 1 restarting) won't be removed with y; press f to force-remove them"},
	}
	for _, c := range cases {
		if got := c.p.warning(); got != c.want {
			t.Errorf("warning() for %+v = %q, want %q", c.p, got, c.want)
		}
	}
}

func TestConfirmForceOfferedForPausedAndRestarting(t *testing.T) {
	for _, state := range []string{"paused", "restarting"} {
		t.Run(state, func(t *testing.T) {
			fake := newFake()
			for i := range fake.Containers {
				if fake.Containers[i].ID == ctrStopped {
					fake.Containers[i].State = state
				}
			}
			m := loaded(t, fake, ctrStopped)

			m = send(t, m, runes("d"))
			want := "⚠ 1 container is " + state + " and won't be removed with y; press f to force-remove it"
			if view := m.View(); !strings.Contains(view, want) || !strings.Contains(view, "force delete") {
				t.Errorf("view should warn %q and offer f, got:\n%s", want, view)
			}

			send(t, m, runes("f"))
			if len(fake.RemoveContainerOpts) != 1 || !fake.RemoveContainerOpts[0].Force {
				t.Errorf("f should force-remove the %s container, got %v", state, fake.RemoveContainerOpts)
			}
		})
	}
}
