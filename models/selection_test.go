package models

import (
	"maps"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
)

const (
	imgA = "sha256:aaaaaaaaaaaaaaaa"
	imgB = "sha256:bbbbbbbbbbbbbbbb"
	imgC = "sha256:cccccccccccccccc"
	ctrX = "xxxxxxxxxxxxxxxx"
)

// updated feeds msg to the model and returns the updated Model.
func updated(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// press sends a key press to the model and returns the updated Model.
func press(m Model, key string) Model {
	return updated(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

// toggle moves the cursor to the row holding id and presses space, failing
// the test if no such row exists.
func toggle(t *testing.T, m Model, id string) Model {
	t.Helper()
	for i, it := range m.items {
		if it.ID == id && it.Type != ItemTypeHeader {
			m.cursor = i
			return press(m, " ")
		}
	}
	t.Fatalf("no row with ID %q in %v", id, m.items)
	return m
}

func selectedIDs(m Model) []string {
	return slices.Sorted(maps.Keys(m.selected))
}

func images(ids ...string) docker.ImagesFetchedMsg {
	var imgs []dockerClient.APIImages
	for _, id := range ids {
		imgs = append(imgs, dockerClient.APIImages{ID: id})
	}
	return docker.ImagesFetchedMsg{Images: imgs}
}

func containers(ids ...string) docker.ContainersFetchedMsg {
	var ctrs []dockerClient.APIContainers
	for _, id := range ids {
		ctrs = append(ctrs, dockerClient.APIContainers{ID: id})
	}
	return docker.ContainersFetchedMsg{Containers: ctrs}
}

func TestSelectionFollowsIDAcrossRefresh(t *testing.T) {
	m := newModel(nil)
	m = updated(m, images(imgA, imgB))
	m = toggle(t, m, imgB)

	// Refresh with a new image first and the old ones reversed: every row
	// index now points at a different item.
	m = updated(m, images(imgC, imgB, imgA))
	m = updated(m, containers(ctrX))

	if got, want := selectedIDs(m), []string{imgB}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
	got := m.getSelectionForCleanup()
	want := []docker.CleanupItem{{ID: imgB, Type: docker.CleanupItemTypeImage}}
	if !slices.Equal(got, want) {
		t.Errorf("getSelectionForCleanup() = %v, want %v", got, want)
	}
}

func TestToggleDeselectsByIDAfterRefresh(t *testing.T) {
	m := newModel(nil)
	m = updated(m, images(imgA))
	m = toggle(t, m, imgA)

	m = updated(m, images(imgC, imgA))
	m = toggle(t, m, imgA)

	if len(m.selected) != 0 {
		t.Errorf("expected toggle to deselect %s, selected = %v", imgA, selectedIDs(m))
	}
}

func TestSelectionDropsVanishedIDs(t *testing.T) {
	m := newModel(nil)
	m = updated(m, images(imgA, imgB))
	m = updated(m, containers(ctrX))
	for _, id := range []string{imgA, imgB, ctrX} {
		m = toggle(t, m, id)
	}

	// imgA and the container were removed outside the app.
	m = updated(m, images(imgB))
	m = updated(m, containers())

	if got, want := selectedIDs(m), []string{imgB}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
}

func TestCursorClampsWhenListShrinks(t *testing.T) {
	cases := []struct {
		name string
		msg  docker.ImagesFetchedMsg
		want int
	}{
		// header + 1 image + header = 3 rows; cursor clamps to the last row.
		{"fewer images", images(imgA), 2},
		// header + header = 2 rows.
		{"no images", images(), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newModel(nil)
			m = updated(m, images(imgA, imgB, imgC))
			m.cursor = len(m.items) - 1

			m = updated(m, c.msg)

			if m.cursor != c.want {
				t.Errorf("cursor = %d, want %d (rows: %d)", m.cursor, c.want, len(m.items))
			}
		})
	}
}

func TestGetSelectionForCleanupContainersFirst(t *testing.T) {
	const ctrY = "yyyyyyyyyyyyyyyy"
	m := newModel(nil)
	m = updated(m, images(imgA, imgB))
	m = updated(m, containers(ctrX, ctrY))
	for _, id := range []string{imgB, ctrY, imgA, ctrX} {
		m = toggle(t, m, id)
	}

	got := m.getSelectionForCleanup()
	want := []docker.CleanupItem{
		{ID: ctrX, Type: docker.CleanupItemTypeContainer},
		{ID: ctrY, Type: docker.CleanupItemTypeContainer},
		{ID: imgA, Type: docker.CleanupItemTypeImage},
		{ID: imgB, Type: docker.CleanupItemTypeImage},
	}
	if !slices.Equal(got, want) {
		t.Errorf("getSelectionForCleanup() = %v, want %v", got, want)
	}
}

func TestReconcileSelectionEmptyList(t *testing.T) {
	m := newModel(nil)
	m.cursor = 5
	m.selected[imgA] = struct{}{}
	m.reconcileSelection()
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 for an empty list", m.cursor)
	}
	if len(m.selected) != 0 {
		t.Errorf("selected = %v, want empty", selectedIDs(m))
	}
}
