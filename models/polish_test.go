package models

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
