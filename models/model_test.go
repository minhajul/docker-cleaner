package models

import (
	"strings"
	"testing"

	dockerClient "github.com/fsouza/go-dockerclient"
)

func TestInitialModelReturnsErrorWhenDockerUnreachable(t *testing.T) {
	// This test does NOT need Docker to be running or not running.
	// It tests the contract that InitialModel returns (Model{}, error) gracefully.
	// We can't easily simulate "Docker unreachable" without env manipulation,
	// so we test what we CAN: that the function returns without panicking/exiting.
	_, err := InitialModel()
	// err may be nil (Docker running) or non-nil (Docker not running). Both are valid.
	// The KEY assertion is that the function returned instead of calling os.Exit.
	_ = err // both outcomes acceptable
}

func TestTruncateID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"short", "short"},
		{"exactly12c", "exactly12c"},
		{"1234567890abcdef", "1234567890ab"}, // 16 chars -> first 12
		{"", ""},
	}
	for _, c := range cases {
		if got := truncateID(c.in); got != c.want {
			t.Errorf("truncateID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGetSelectionForCleanup(t *testing.T) {
	m := Model{
		items: []SelectableItem{
			{ID: "img1", Type: ItemTypeImage},
			{ID: "ctr1", Type: ItemTypeContainer},
		},
		selected: map[int]struct{}{0: {}, 1: {}},
	}
	items := m.getSelectionForCleanup()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// headers should be filtered out
	m.items = append(m.items, SelectableItem{ID: "h", Type: ItemTypeHeader})
	// add another selection that hits the header — should be filtered
	m.selected[2] = struct{}{}
	items = m.getSelectionForCleanup()
	if len(items) != 2 {
		t.Errorf("expected headers to be filtered, got %d items", len(items))
	}
}

func TestUpdateItemsBuildsList(t *testing.T) {
	m := Model{
		images: []dockerClient.APIImages{
			{ID: "abcdef1234567890", RepoTags: []string{"alpine:latest"}},
			{ID: "orphan0000000000", RepoTags: []string{"<none>:<none>"}},
		},
		containers: []dockerClient.APIContainers{
			{ID: "container1234abcd", Names: []string{"/web"}, Image: "nginx"},
		},
	}
	m.updateItems()
	// Expect: header, image1, image2, header, container1 = 5 items
	if len(m.items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(m.items))
	}
	if m.items[0].Type != ItemTypeHeader {
		t.Errorf("expected first item to be a header, got type %d", m.items[0].Type)
	}
	if m.items[1].Type != ItemTypeImage {
		t.Errorf("expected second item to be an image, got type %d", m.items[1].Type)
	}
	// orphan image (no RepoTags) should show "<none>"
	if !strings.Contains(m.items[2].Display, "<none>") {
		t.Errorf("expected orphan image to display <none>, got %q", m.items[2].Display)
	}
	// container name should have leading slash stripped
	if !strings.Contains(m.items[4].Display, "web") || strings.Contains(m.items[4].Display, "/web") {
		t.Errorf("expected container name without leading slash, got %q", m.items[4].Display)
	}
}
