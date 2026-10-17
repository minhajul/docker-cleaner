package models

import (
	"strings"
	"testing"
)

func TestHelpToggle(t *testing.T) {
	m := newModel(nil)
	if m.help.ShowAll {
		t.Fatal("full help should start hidden")
	}
	if !strings.Contains(m.View(), "delete selected") {
		t.Errorf("short help should list the delete key, got:\n%s", m.View())
	}

	m = press(m, "?")
	if !m.help.ShowAll {
		t.Fatal("? should show full help")
	}
	if !strings.Contains(m.View(), "up") {
		t.Errorf("full help should list navigation keys, got:\n%s", m.View())
	}

	m = press(m, "?")
	if m.help.ShowAll {
		t.Error("? again should hide full help")
	}
}
