package models

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker/dockertest"
	"github.com/minhajul/docker-cleaner/filter"
	"github.com/moby/moby/api/types/volume"
)

// testNow is the fixed time the tests measure ages from.
var testNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

const imgDangling = "sha256:dddddddddddddddd"

var keyEnter = tea.KeyMsg{Type: tea.KeyEnter}

// ago is the time d before testNow.
func ago(d time.Duration) time.Time {
	return testNow.Add(-d)
}

// filterFake returns a daemon with resources of every kind for the filters
// to tell apart:
//   - images: alpine (2 days old, used by web and job), busybox (40 days,
//     unused) and a dangling one (100 days, unused);
//   - containers: web (running, 1 day), db (exited, 35 days) and job
//     (created, 10 days);
//   - volumes: pgdata (60 days, mounted by db) and cache (3 days, unused);
//   - networks: app_default (web is attached) and spare (unused).
func filterFake() *dockertest.Fake {
	fake := &dockertest.Fake{
		Images: []dockerClient.APIImages{
			{ID: imgA, RepoTags: []string{"alpine:latest"}, Size: 8 << 20, Created: ago(2 * filter.Day).Unix()},
			{ID: imgB, RepoTags: []string{"busybox:latest"}, Size: 4 << 20, Created: ago(40 * filter.Day).Unix()},
			{ID: imgDangling, RepoTags: []string{"<none>:<none>"}, Size: 1 << 30, Created: ago(100 * filter.Day).Unix()},
		},
		Containers: []dockerClient.APIContainers{
			{ID: ctrRunning, Names: []string{"/web"}, Image: "alpine", State: "running", Created: ago(filter.Day).Unix(),
				Networks: dockerClient.NetworkList{Networks: map[string]dockerClient.ContainerNetwork{"app_default": {NetworkID: netApp}}}},
			{ID: ctrStopped, Names: []string{"/db"}, Image: "postgres:16", State: "exited", Created: ago(35 * filter.Day).Unix(),
				Mounts: []dockerClient.APIMount{{Type: "volume", Name: volData}}},
			{ID: ctrX, Names: []string{"/job"}, Image: "alpine:latest", State: "created", Created: ago(10 * filter.Day).Unix()},
		},
		Volumes: []dockerClient.Volume{
			{Name: volData, Driver: "local", CreatedAt: ago(60 * filter.Day)},
			{Name: volCache, Driver: "local", CreatedAt: ago(3 * filter.Day)},
		},
		Networks: []dockerClient.Network{
			{ID: netApp, Name: "app_default", Driver: "bridge"},
			{ID: netSpare, Name: "spare", Driver: "bridge"},
		},
	}
	fake.Usage.Volumes.Items = []volume.Volume{
		{Name: volData, UsageData: &volume.UsageData{Size: 48 << 20, RefCount: 1}},
		{Name: volCache, UsageData: &volume.UsageData{Size: 1 << 20, RefCount: 0}},
	}
	return fake
}

// filterModel returns a model backed by fake, with its lists fetched and ids
// selected, whose clock reads testNow.
func filterModel(t *testing.T, fake *dockertest.Fake, ids ...string) Model {
	t.Helper()
	m := newModel(fake)
	m.now = func() time.Time { return testNow }
	m = settle(t, m, m.Init())
	for _, id := range ids {
		m = toggle(t, m, id)
	}
	return m
}

// visibleIDs returns the IDs of the listed items, in list order.
func visibleIDs(m Model) []string {
	var ids []string
	for _, it := range m.items {
		if it.Type != ItemTypeHeader {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// pressAll sends each key of keys in turn.
func pressAll(t *testing.T, m Model, keys string) Model {
	t.Helper()
	for _, k := range keys {
		m = send(t, m, runes(string(k)))
	}
	return m
}

func TestFilters(t *testing.T) {
	everything := []string{imgA, imgB, imgDangling, ctrRunning, ctrStopped, ctrX, volData, volCache, netApp, netSpare}
	cases := []struct {
		name string
		keys string
		want []string
		bar  string
	}{
		{"none", "", everything, ""},
		{"dangling narrows images only", "D",
			[]string{imgDangling, ctrRunning, ctrStopped, ctrX, volData, volCache, netApp, netSpare},
			"Filters: dangling (esc clears)"},
		{"stopped narrows containers only", "S",
			[]string{imgA, imgB, imgDangling, ctrStopped, ctrX, volData, volCache, netApp, netSpare},
			"Filters: stopped (esc clears)"},
		{"unused narrows images, volumes and networks", "U",
			[]string{imgB, imgDangling, ctrRunning, ctrStopped, ctrX, volCache, netSpare},
			"Filters: unused (esc clears)"},
		{"older than 7d", "O",
			[]string{imgB, imgDangling, ctrStopped, ctrX, volData, netApp, netSpare},
			"Filters: older than 7d (esc clears)"},
		{"older than 30d", "OO",
			[]string{imgB, imgDangling, ctrStopped, volData, netApp, netSpare},
			"Filters: older than 30d (esc clears)"},
		{"older than 90d", "OOO",
			[]string{imgDangling, netApp, netSpare},
			"Filters: older than 90d (esc clears)"},
		{"older than cycles back to off", "OOOO", everything, ""},
		{"a second press turns a filter off", "DD", everything, ""},
		{"filters combine", "UOS",
			[]string{imgB, imgDangling, ctrStopped, ctrX, netSpare},
			"Filters: stopped · unused · older than 7d (esc clears)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := pressAll(t, filterModel(t, filterFake()), c.keys)
			if got := visibleIDs(m); !slices.Equal(got, c.want) {
				t.Errorf("listed = %v, want %v", got, c.want)
			}
			if got := m.filterBar(); got != c.bar {
				t.Errorf("filter bar = %q, want %q", got, c.bar)
			}
			if c.bar != "" && !strings.Contains(m.View(), c.bar) {
				t.Errorf("view should show %q, got:\n%s", c.bar, m.View())
			}
		})
	}
}

func TestFilterLeavesEmptySectionsOut(t *testing.T) {
	fake := filterFake()
	fake.Networks = fake.Networks[:1] // app_default only, which is in use
	m := pressAll(t, filterModel(t, fake), "U")
	view := m.View()
	if strings.Contains(view, networksHeader) {
		t.Errorf("an emptied Networks section should be left out, got:\n%s", view)
	}
	if !strings.Contains(view, "📦 Docker Images") || !strings.Contains(view, "🏃 Docker Containers") {
		t.Errorf("the image and container headers should stay, got:\n%s", view)
	}
}

func TestEscClearsFilters(t *testing.T) {
	m := pressAll(t, filterModel(t, filterFake()), "DSUO")
	m = send(t, m, runes("/"))
	m = pressAll(t, m, "zzz")
	m = send(t, m, keyEnter)

	m = send(t, m, keyEsc)
	if m.filters.Active() || m.query != "" {
		t.Errorf("esc should clear the filters and search, got %+v and %q", m.filters, m.query)
	}
	if got := len(visibleIDs(m)); got != 10 {
		t.Errorf("listed %d items after esc, want all 10", got)
	}
	if bar := m.filterBar(); bar != "" {
		t.Errorf("filter bar = %q, want empty", bar)
	}
	// Opening the search again starts from an empty query.
	m = send(t, m, runes("/"))
	if v := m.search.Value(); v != "" {
		t.Errorf("search input = %q after esc, want empty", v)
	}
}

func TestSearch(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"by image tag", "busy", []string{imgB}},
		{"ignores case", "ALPINE", []string{imgA, ctrRunning, ctrX}}, // web and job show their image
		{"by container name", "db", []string{ctrStopped}},
		{"by volume name", "cache", []string{volCache}},
		{"by network name", "spare", []string{netSpare}},
		{"by display text", "exited", []string{ctrStopped}},
		{"no match", "nothing-matches", nil},
		{"spaces only", "  ", []string{imgA, imgB, imgDangling, ctrRunning, ctrStopped, ctrX, volData, volCache, netApp, netSpare}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := send(t, filterModel(t, filterFake()), runes("/"))
			m = send(t, m, runes(c.query))
			if got := visibleIDs(m); !slices.Equal(got, c.want) {
				t.Errorf("while typing: listed = %v, want %v", got, c.want)
			}
			m = send(t, m, keyEnter)
			if m.searching {
				t.Fatal("enter should close the search input")
			}
			if got := visibleIDs(m); !slices.Equal(got, c.want) {
				t.Errorf("after enter: listed = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSearchCombinesWithFilters(t *testing.T) {
	m := pressAll(t, filterModel(t, filterFake()), "S")
	m = send(t, m, runes("/"))
	m = send(t, m, runes("job"))
	m = send(t, m, keyEnter)
	if got, want := visibleIDs(m), []string{ctrX}; !slices.Equal(got, want) {
		t.Errorf("listed = %v, want %v", got, want)
	}
	if got, want := m.filterBar(), `Filters: stopped · "job" (esc clears)`; got != want {
		t.Errorf("filter bar = %q, want %q", got, want)
	}
}

func TestSearchInputTakesKeys(t *testing.T) {
	fake := filterFake()
	m := filterModel(t, fake, imgB)
	m = send(t, m, runes("/"))
	if !m.searching {
		t.Fatal("/ should open the search input")
	}
	view := m.View()
	if !strings.Contains(view, "/ ") || !strings.Contains(view, "keep search") {
		t.Errorf("view should show the input and its keys, got:\n%s", view)
	}

	// Keys that act on the list are typed instead: q does not quit, d does
	// not delete, a does not select all.
	next, cmd := m.Update(runes("q"))
	m = next.(Model)
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q quit while searching")
		}
	}
	m = pressAll(t, m, "uad")
	if m.confirm != nil {
		t.Error("d opened the delete dialog while searching")
	}
	if got, want := m.search.Value(), "quad"; got != want {
		t.Errorf("search input = %q, want %q", got, want)
	}
	if got, want := selectedIDs(m), []string{imgB}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}

	// esc clears the search and closes the input; esc again does nothing.
	m = send(t, m, keyEsc)
	if m.searching || m.query != "" || len(visibleIDs(m)) != 10 {
		t.Errorf("esc should clear the search, got searching %v, query %q, %d rows", m.searching, m.query, len(visibleIDs(m)))
	}

	// ctrl+c still quits.
	m = send(t, m, runes("/"))
	if _, cmd := m.Update(keyCtrlC); cmd == nil {
		t.Error("ctrl+c should quit while searching")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c should quit while searching")
	}
}

func TestSearchTakesPaste(t *testing.T) {
	m := send(t, filterModel(t, filterFake()), runes("/"))
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("busybox"), Paste: true})
	if got, want := visibleIDs(m), []string{imgB}; !slices.Equal(got, want) {
		t.Errorf("listed = %v, want %v", got, want)
	}
}

func TestSearchReopensOnQuery(t *testing.T) {
	m := send(t, filterModel(t, filterFake()), runes("/"))
	m = send(t, m, runes("busy"))
	m = send(t, m, keyEnter)
	m = send(t, m, runes("/"))
	m = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if got, want := m.query, "bus"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
}

func TestFilteredSelectionStaysSelected(t *testing.T) {
	fake := filterFake()
	m := filterModel(t, fake, imgA, imgDangling)

	// alpine is in use, so the unused filter hides it.
	m = pressAll(t, m, "U")
	if got, want := selectedIDs(m), []string{imgA, imgDangling}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v: filtering must not drop a selection", got, want)
	}
	want := "Filters: unused (esc clears)   +1 selected hidden by filters"
	if got := m.filterBar(); got != want {
		t.Errorf("filter bar = %q, want %q", got, want)
	}

	// Only the visible selection is deleted, and the dialog says so.
	m = send(t, m, runes("d"))
	view := m.View()
	for _, s := range []string{"Delete 1 image (1.0 GB)? y/N", "1 selected item hidden by filters will be kept"} {
		if !strings.Contains(view, s) {
			t.Errorf("dialog should show %q, got:\n%s", s, view)
		}
	}
	m = send(t, m, runes("y"))
	if want := []string{"image:" + imgDangling}; !slices.Equal(fake.Removed, want) {
		t.Errorf("Removed = %v, want %v", fake.Removed, want)
	}

	// Clearing the filter brings the hidden selection back.
	m = send(t, m, keyEsc)
	if got, want := selectedIDs(m), []string{imgA}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
	if got := m.filterBar(); got != "" {
		t.Errorf("filter bar = %q, want empty", got)
	}
}

func TestDeleteIgnoresOnlyHiddenSelection(t *testing.T) {
	m := filterModel(t, filterFake(), imgA)
	m = pressAll(t, m, "D") // alpine is tagged: hidden
	m = send(t, m, runes("d"))
	if m.confirm != nil {
		t.Errorf("d should not open the dialog when every selected item is hidden, got prompt %q", m.confirm.prompt())
	}
}

func TestSelectionDropsVanishedWhileFiltered(t *testing.T) {
	fake := filterFake()
	m := filterModel(t, fake, imgA, imgB)
	m = pressAll(t, m, "D") // hides both

	// alpine is removed outside the app.
	fake.Images = fake.Images[1:]
	m = send(t, m, runes("r"))
	if got, want := selectedIDs(m), []string{imgB}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v: a vanished item should be dropped, a hidden one kept", got, want)
	}
}

func TestSelectAllSelectsVisibleOnly(t *testing.T) {
	m := pressAll(t, filterModel(t, filterFake()), "Sa")
	want := slices.Sorted(slices.Values([]string{imgA, imgB, imgDangling, ctrStopped, ctrX, volData, volCache, netApp, netSpare}))
	if got := selectedIDs(m); !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v (all but the hidden running container)", got, want)
	}
}

func TestFilterKeepsCursorOnRow(t *testing.T) {
	m := filterModel(t, filterFake())
	for i, it := range m.items {
		if it.ID == imgDangling {
			m.cursor = i
		}
	}
	m = pressAll(t, m, "D")
	if got := m.items[m.cursor].ID; got != imgDangling {
		t.Errorf("cursor on %q after filtering, want %q", got, imgDangling)
	}
}

func TestFilterKeysInFullHelp(t *testing.T) {
	m := filterModel(t, filterFake())
	m = send(t, m, runes("?"))
	view := m.View()
	for _, s := range []string{"search", "clear filters", "dangling images", "stopped containers", "unused", "older than 7/30/90d"} {
		if !strings.Contains(view, s) {
			t.Errorf("full help should list %q, got:\n%s", s, view)
		}
	}
}
