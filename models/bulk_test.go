package models

import (
	"slices"
	"testing"
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
