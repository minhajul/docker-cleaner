package models

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
)

const (
	imgPostgres = "sha256:pppppppppppppppp"
	ctrDB       = "dddddddddddddddd"
	ctrPod      = "kkkkkkkkkkkkkkkk"
)

// protectModel returns a model protecting patterns, listing postgres:16
// (imgPostgres), alpine:latest (imgA), and the containers my-db (ctrDB), web
// (ctrX) and a Kubernetes pod (ctrPod).
func protectModel(t *testing.T, patterns ...string) Model {
	t.Helper()
	p, err := docker.NewProtector(patterns)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(nil)
	m.protector = p
	m = updated(m, docker.ImagesFetchedMsg{Images: []dockerClient.APIImages{
		{ID: imgPostgres, RepoTags: []string{"postgres:16"}},
		{ID: imgA, RepoTags: []string{"alpine:latest"}},
	}})
	return updated(m, docker.ContainersFetchedMsg{Containers: []dockerClient.APIContainers{
		{ID: ctrDB, Names: []string{"/my-db"}},
		{ID: ctrX, Names: []string{"/web"}},
		{ID: ctrPod, Names: []string{"/k8s_POD_web_default"}},
	}})
}

// listedIDs returns the IDs of the non-header rows, in list order.
func listedIDs(m Model) []string {
	var ids []string
	for _, it := range m.items {
		if it.Type != ItemTypeHeader {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// rowOf returns the index of the row holding id, failing the test if none.
func rowOf(t *testing.T, m Model, id string) int {
	t.Helper()
	for i, it := range m.items {
		if it.ID == id && it.Type != ItemTypeHeader {
			return i
		}
	}
	t.Fatalf("no row with ID %q in %v", id, m.items)
	return -1
}

func TestProtectedHiddenByDefault(t *testing.T) {
	m := protectModel(t, "postgres:*", "my-db")

	if got, want := listedIDs(m), []string{imgA, ctrX}; !slices.Equal(got, want) {
		t.Errorf("listed = %v, want %v", got, want)
	}
	if got := len(m.protected); got != 3 {
		t.Errorf("%d protected, want 3 (postgres, my-db, the pod)", got)
	}
	if view := m.View(); !strings.Contains(view, "🔒 3 protected items hidden · press p to show") {
		t.Errorf("view lacks the hidden-items hint:\n%s", view)
	}
}

func TestProtectedHint(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		reveal   bool
		want     string
	}{
		{"system only", nil, false, "🔒 1 protected item hidden · press p to show"},
		{"revealed", []string{"postgres"}, true, "🔒 2 protected items shown read-only · press p to hide"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := protectModel(t, c.patterns...)
			if c.reveal {
				m = press(m, "p")
			}
			if got := m.protectedHint(); got != c.want {
				t.Errorf("hint = %q, want %q", got, c.want)
			}
		})
	}
}

func TestProtectedHintAbsentWhenNothingProtected(t *testing.T) {
	m := bulkModel()
	if hint := m.protectedHint(); hint != "" {
		t.Errorf("hint = %q, want none", hint)
	}
	if view := m.View(); strings.Contains(view, "protected") {
		t.Errorf("view mentions protected items though there are none:\n%s", view)
	}
}

func TestRevealProtected(t *testing.T) {
	m := protectModel(t, "postgres:*", "my-db")

	m = press(m, "p")

	if got, want := listedIDs(m), []string{imgPostgres, imgA, ctrDB, ctrX, ctrPod}; !slices.Equal(got, want) {
		t.Fatalf("listed = %v, want %v", got, want)
	}
	for id, reason := range map[string]string{imgPostgres: "protect-list", ctrDB: "protect-list", ctrPod: "system"} {
		row := m.rowView(rowOf(t, m, id))
		if !strings.Contains(row, "🔒") || !strings.Contains(row, "· "+reason) {
			t.Errorf("protected row %s = %q, want a lock and %q", id, row, reason)
		}
		if strings.Contains(row, "☐") || strings.Contains(row, "✓") {
			t.Errorf("protected row %s = %q has a checkbox", id, row)
		}
	}
	if row := m.rowView(rowOf(t, m, imgA)); strings.Contains(row, "🔒") || !strings.Contains(row, "☐") {
		t.Errorf("unprotected row = %q, want a checkbox and no lock", row)
	}

	m = press(m, "p")

	if got, want := listedIDs(m), []string{imgA, ctrX}; !slices.Equal(got, want) {
		t.Errorf("after hiding again, listed = %v, want %v", got, want)
	}
}

func TestRevealKeepsCursorOnRow(t *testing.T) {
	m := protectModel(t, "postgres:*")
	m.cursor = rowOf(t, m, imgA)

	m = press(m, "p")
	if got := m.items[m.cursor].ID; got != imgA {
		t.Errorf("after revealing, cursor on %q, want %q", got, imgA)
	}

	m = press(m, "p")
	if got := m.items[m.cursor].ID; got != imgA {
		t.Errorf("after hiding, cursor on %q, want %q", got, imgA)
	}
}

func TestProtectedRowsUnselectable(t *testing.T) {
	cases := []struct {
		name string
		act  func(t *testing.T, m Model) Model
		want []string
	}{
		{"space", func(t *testing.T, m Model) Model {
			m.cursor = rowOf(t, m, imgPostgres)
			return press(m, " ")
		}, nil},
		{"select all", func(_ *testing.T, m Model) Model {
			return press(m, "a")
		}, []string{ctrX, imgA}},
		{"section toggle", func(t *testing.T, m Model) Model {
			m.cursor = headerRow(t, m, 1) // containers
			return press(m, " ")
		}, []string{ctrX}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := press(protectModel(t, "postgres:*", "my-db"), "p")
			m = c.act(t, m)
			if got, want := selectedIDs(m), slices.Sorted(slices.Values(c.want)); !slices.Equal(got, want) {
				t.Errorf("selected = %v, want %v", got, want)
			}
		})
	}
}

func TestSectionToggleIgnoresProtectedRows(t *testing.T) {
	m := press(protectModel(t, "my-db"), "p")
	m.cursor = headerRow(t, m, 1) // containers: my-db and the pod are protected

	m = press(m, " ")
	if got, want := selectedIDs(m), []string{ctrX}; !slices.Equal(got, want) {
		t.Fatalf("after first toggle, selected = %v, want %v", got, want)
	}

	// Every selectable row is selected, so the toggle clears the section
	// even though the protected rows are not selected.
	m = press(m, " ")
	if len(m.selected) != 0 {
		t.Errorf("after second toggle, selected = %v, want empty", selectedIDs(m))
	}
}

func TestSelectAllThenDeleteSkipsProtected(t *testing.T) {
	for _, reveal := range []bool{false, true} {
		name := "hidden"
		if reveal {
			name = "revealed"
		}
		t.Run(name, func(t *testing.T) {
			m := protectModel(t, "postgres:*", "my-db")
			if reveal {
				m = press(m, "p")
			}

			m = press(m, "a")
			m = press(m, "d")

			if m.confirm == nil {
				t.Fatal("d did not open the confirmation")
			}
			want := []docker.CleanupItem{
				{ID: ctrX, Type: docker.CleanupItemTypeContainer},
				{ID: imgA, Type: docker.CleanupItemTypeImage},
			}
			if !slices.Equal(m.confirm.items, want) {
				t.Errorf("confirm items = %v, want %v", m.confirm.items, want)
			}
			if m.confirm.images != 1 || m.confirm.containers != 1 {
				t.Errorf("confirm counts %d images, %d containers, want 1 and 1", m.confirm.images, m.confirm.containers)
			}
		})
	}
}

func TestGetSelectionForCleanupSkipsProtected(t *testing.T) {
	m := press(protectModel(t, "postgres:*"), "p")
	// Force a protected ID into the selection, bypassing the keys.
	m.selected[imgPostgres] = struct{}{}
	m.selected[imgA] = struct{}{}

	got := m.getSelectionForCleanup()
	want := []docker.CleanupItem{{ID: imgA, Type: docker.CleanupItemTypeImage}}
	if !slices.Equal(got, want) {
		t.Errorf("getSelectionForCleanup() = %v, want %v", got, want)
	}
}

func TestSelectionDropsItemsThatBecomeProtected(t *testing.T) {
	m := protectModel(t, "postgres:*")
	m = toggle(t, m, imgA)

	// alpine was retagged outside the app and now matches the protect-list.
	m = updated(m, docker.ImagesFetchedMsg{Images: []dockerClient.APIImages{
		{ID: imgA, RepoTags: []string{"postgres:alpine"}},
	}})

	if len(m.selected) != 0 {
		t.Errorf("selected = %v, want the newly protected image dropped", selectedIDs(m))
	}
}

func TestScrollFitsWithProtectedRows(t *testing.T) {
	m := sized(50, 20)
	m = updated(m, docker.ContainersFetchedMsg{Containers: []dockerClient.APIContainers{
		{ID: ctrX, Names: []string{"/web"}},
		{ID: ctrPod, Names: []string{"/k8s_POD_web_default"}},
	}})
	m = press(m, "p")
	if !strings.Contains(m.View(), "shown read-only") {
		t.Fatalf("view lacks the protected hint:\n%s", m.View())
	}

	for last := len(m.items) - 1; m.cursor < last; {
		m = press(m, "j")
		checkFits(t, m)
	}
	if row := m.items[m.cursor]; row.ID != ctrPod {
		t.Errorf("last row is %q, want the protected pod", row.ID)
	}
}

func TestInitialModelRejectsBadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("protect:\n  - \"db-[0-9\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The config is read before Docker is contacted, so this needs no daemon.
	_, err := InitialModel(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("InitialModel(%q) = %v, want an error naming the file", path, err)
	}
}

func TestFullHelpListsShowProtected(t *testing.T) {
	m := press(newModel(nil), "?")
	if view := m.View(); !strings.Contains(view, "show/hide protected") {
		t.Errorf("full help lacks the p key:\n%s", view)
	}
}
