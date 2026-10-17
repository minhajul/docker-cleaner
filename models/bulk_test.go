package models

import (
	"slices"
	"strings"
	"testing"

	"docker-cleaner/docker/dockertest"
	tea "github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
)

const ctrY = "yyyyyyyyyyyyyyyy"

// bulkModel returns a model listing imgA, imgB, ctrX and ctrY.
func bulkModel() Model {
	m := newModel(nil)
	m = updated(m, images(imgA, imgB))
	return updated(m, containers(ctrX, ctrY))
}

// headerRow returns the index of the n-th (0-based) section header.
func headerRow(t *testing.T, m Model, n int) int {
	t.Helper()
	seen := 0
	for i, it := range m.items {
		if it.Type != ItemTypeHeader {
			continue
		}
		if seen == n {
			return i
		}
		seen++
	}
	t.Fatalf("no header #%d in %v", n, m.items)
	return -1
}

func TestSelectAll(t *testing.T) {
	m := bulkModel()
	m = toggle(t, m, imgB)

	m = press(m, "a")

	if got, want := selectedIDs(m), slices.Sorted(slices.Values([]string{imgA, imgB, ctrX, ctrY})); !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
	if _, ok := m.selected[""]; ok {
		t.Error("a header row (empty ID) ended up in the selection")
	}
}

func TestSelectNone(t *testing.T) {
	m := bulkModel()
	m = press(m, "a")

	m = press(m, "n")

	if len(m.selected) != 0 {
		t.Errorf("selected = %v, want empty", selectedIDs(m))
	}
}

func TestToggleSectionHeader(t *testing.T) {
	cases := []struct {
		name    string
		section int      // 0 = images, 1 = containers
		before  []string // selected before pressing space on the header
		want    []string
	}{
		{"none selected selects section", 0, nil, []string{imgA, imgB}},
		{"partially selected selects rest", 0, []string{imgA}, []string{imgA, imgB}},
		{"all selected deselects section", 0, []string{imgA, imgB}, nil},
		{"leaves other section alone", 1, []string{imgA, ctrX}, []string{ctrX, ctrY, imgA}},
		{"deselect leaves other section alone", 1, []string{imgB, ctrX, ctrY}, []string{imgB}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := bulkModel()
			for _, id := range c.before {
				m = toggle(t, m, id)
			}

			m.cursor = headerRow(t, m, c.section)
			m = press(m, " ")

			if got, want := selectedIDs(m), slices.Sorted(slices.Values(c.want)); !slices.Equal(got, want) {
				t.Errorf("selected = %v, want %v", got, want)
			}
			if _, ok := m.selected[""]; ok {
				t.Error("a header row (empty ID) ended up in the selection")
			}
		})
	}
}

func TestToggleEmptySectionHeader(t *testing.T) {
	m := newModel(nil)
	m = updated(m, images(imgA))
	m = updated(m, containers())
	m = toggle(t, m, imgA)

	// The containers section has a header and nothing under it.
	m.cursor = headerRow(t, m, 1)
	m = press(m, " ")

	if got, want := selectedIDs(m), []string{imgA}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
}

// run executes cmd, which may be a tea.Batch, and feeds every resulting
// message back into the model.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = run(t, m, c)
		}
		return m
	}
	return updated(m, msg)
}

func fakeImages(ids ...string) []dockerClient.APIImages {
	return images(ids...).Images
}

func fakeContainers(ids ...string) []dockerClient.APIContainers {
	return containers(ids...).Containers
}

func hasItem(m Model, id string) bool {
	return slices.ContainsFunc(m.items, func(it SelectableItem) bool {
		return it.ID == id && it.Type != ItemTypeHeader
	})
}

func TestRefreshKey(t *testing.T) {
	fake := &dockertest.Fake{
		Images:     fakeImages(imgA, imgB),
		Containers: fakeContainers(ctrX),
	}
	m := newModel(fake)
	m = run(t, m, m.Init())
	for _, id := range []string{imgA, imgB, ctrX} {
		m = toggle(t, m, id)
	}
	m.report = &cleanupReport{summary: "Deleted 1"}
	m.errorMsg = "stale error"

	// imgA vanished and ctrY appeared outside the app.
	fake.Images = fakeImages(imgB, imgC)
	fake.Containers = fakeContainers(ctrX, ctrY)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(Model)
	if m.report != nil || m.errorMsg != "" {
		t.Errorf("refresh should clear messages, got report=%v error=%q", m.report, m.errorMsg)
	}
	if !strings.Contains(m.View(), "Refreshing") {
		t.Errorf("view should show a refreshing indicator, got:\n%s", m.View())
	}

	m = run(t, m, cmd)

	if got, want := selectedIDs(m), slices.Sorted(slices.Values([]string{imgB, ctrX})); !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
	for _, id := range []string{imgB, imgC, ctrX, ctrY} {
		if !hasItem(m, id) {
			t.Errorf("%s missing from items after refresh: %v", id, m.items)
		}
	}
	if hasItem(m, imgA) {
		t.Errorf("vanished %s still listed: %v", imgA, m.items)
	}
	if strings.Contains(m.View(), "Refreshing") {
		t.Errorf("refreshing indicator should clear once both lists arrive, got:\n%s", m.View())
	}
}

func TestRefreshKeyIgnoredWhileCleaning(t *testing.T) {
	m := newModel(&dockertest.Fake{})
	m.cleaning = true

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})

	if cmd != nil {
		t.Error("refresh during cleanup should not start another fetch")
	}
	if next.(Model).refreshing != 0 {
		t.Error("refresh during cleanup should not show the refreshing indicator")
	}
}
