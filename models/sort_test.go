package models

import (
	"slices"
	"strings"
	"testing"
)

func TestSortLargestImageFirst(t *testing.T) {
	m := filterModel(t, filterFake())
	if got := visibleIDs(m)[0]; got == imgDangling {
		t.Fatalf("the largest image is already first before sorting; the test proves nothing")
	}

	m = send(t, m, runes("s"))
	if got := visibleIDs(m)[0]; got != imgDangling {
		t.Errorf("first image after s = %s, want the largest, %s", got, imgDangling)
	}
	if got, want := m.filterBar(), "Sort: size"; got != want {
		t.Errorf("filter bar = %q, want %q", got, want)
	}
	if !strings.Contains(m.View(), "Sort: size") {
		t.Errorf("view should show the sort order, got:\n%s", m.View())
	}
}

func TestSortOrders(t *testing.T) {
	cases := []struct {
		name    string
		presses int
		bar     string
		want    []string
	}{
		{"default", 0, "",
			[]string{imgA, imgB, imgDangling, ctrRunning, ctrStopped, ctrX, volData, volCache, netApp, netSpare}},
		// Containers have no size here, so they keep the daemon's order.
		{"size, largest first", 1, "Sort: size",
			[]string{imgDangling, imgA, imgB, ctrRunning, ctrStopped, ctrX, volData, volCache, netApp, netSpare}},
		// Networks have no creation time, so they keep the daemon's order.
		{"age, oldest first", 2, "Sort: age",
			[]string{imgDangling, imgB, imgA, ctrStopped, ctrX, ctrRunning, volData, volCache, netApp, netSpare}},
		{"name", 3, "Sort: name",
			[]string{imgDangling, imgA, imgB, ctrStopped, ctrX, ctrRunning, volCache, volData, netApp, netSpare}},
		{"back to default", 4, "",
			[]string{imgA, imgB, imgDangling, ctrRunning, ctrStopped, ctrX, volData, volCache, netApp, netSpare}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := filterModel(t, filterFake())
			m = pressAll(t, m, strings.Repeat("s", c.presses))
			if got := visibleIDs(m); !slices.Equal(got, c.want) {
				t.Errorf("listed = %v, want %v", got, c.want)
			}
			if got := m.filterBar(); got != c.bar {
				t.Errorf("filter bar = %q, want %q", got, c.bar)
			}
		})
	}
}

func TestSortSurvivesRefreshAndFilters(t *testing.T) {
	m := pressAll(t, filterModel(t, filterFake()), "sU")
	m = send(t, m, runes("r"))
	if got, want := visibleIDs(m)[:2], []string{imgDangling, imgB}; !slices.Equal(got, want) {
		t.Errorf("images = %v, want %v, still largest first", got, want)
	}
	if got, want := m.filterBar(), "Filters: unused (esc clears)   Sort: size"; got != want {
		t.Errorf("filter bar = %q, want %q", got, want)
	}
	// esc clears the filters but keeps the order.
	m = send(t, m, keyEsc)
	if got, want := m.filterBar(), "Sort: size"; got != want {
		t.Errorf("filter bar after esc = %q, want %q", got, want)
	}
}

func TestSortKeepsCursorOnRow(t *testing.T) {
	m := filterModel(t, filterFake())
	for i, it := range m.items {
		if it.ID == imgB {
			m.cursor = i
		}
	}
	m = send(t, m, runes("s"))
	if got := m.items[m.cursor].ID; got != imgB {
		t.Errorf("cursor on %q after sorting, want %q", got, imgB)
	}
}

func TestRowsShowAge(t *testing.T) {
	m := filterModel(t, filterFake())
	want := map[string]string{
		imgA:        "alpine:latest (ID: aaaaaaaaaaaa, Size: 8.0 MB, Created: 2 days ago)",
		imgB:        "busybox:latest (ID: bbbbbbbbbbbb, Size: 4.0 MB, Created: 5 weeks ago)",
		imgDangling: "<none> (ID: dddddddddddd, Size: 1.0 GB, Created: 3 months ago)",
		ctrRunning:  "web (ID: rrrrrrrrrrrr, Image: alpine, State: running, Size: 0 B (virtual 0 B), Created: 1 day ago)",
		ctrX:        "job (ID: xxxxxxxxxxxx, Image: alpine:latest, State: created, Size: 0 B (virtual 0 B), Created: 10 days ago)",
		volData:     "pgdata (Driver: local, Size: 48.0 MB, Used by: 1 container, Created: 2 months ago)",
		volCache:    "cache (Driver: local, Size: 1.0 MB, Unused, Created: 3 days ago)",
		// Docker does not report a network's creation time.
		netSpare: "spare (ID: bbbb1111bbbb, Driver: bridge, Containers: 0)",
	}
	for id, display := range want {
		if got := rowDisplay(m, id); got != display {
			t.Errorf("row %s = %q, want %q", id, got, display)
		}
	}
}

func TestSortKeyInFullHelp(t *testing.T) {
	m := send(t, filterModel(t, filterFake()), runes("?"))
	if !strings.Contains(m.View(), "sort: size/age/name") {
		t.Errorf("full help should list the sort key, got:\n%s", m.View())
	}
}
