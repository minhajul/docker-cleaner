package models

import (
	"strings"
	"testing"

	"docker-cleaner/docker"
	dockerClient "github.com/fsouza/go-dockerclient"
)

func TestShortID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"exactly12ccc", "exactly12ccc"},     // 12 chars -> returned whole
		{"1234567890abcdef", "1234567890ab"}, // 16 chars -> first 12
	}
	for _, c := range cases {
		if got := docker.ShortID(c.in); got != c.want {
			t.Errorf("ShortID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortIDPanicsOnShortInput(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("ShortID should panic on short input")
		}
	}()
	docker.ShortID("short")
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

func TestUpdateItemsFiltersSystemImages(t *testing.T) {
	m := Model{
		images: []dockerClient.APIImages{
			{ID: "abcdef1234567890", RepoTags: []string{"alpine:latest"}},
			{ID: "kicbase1234567890", RepoTags: []string{"gcr.io/k8s-minikube/kicbase:v0.0.50"}},
		},
		containers: []dockerClient.APIContainers{
			{ID: "container1234abcd", Names: []string{"/web"}, Image: "nginx"},
			{ID: "pod1234567890ab", Names: []string{"k8s_POD_web_default_abc"}},
		},
	}
	m.updateItems()

	// k8s image filtered, k8s container filtered -> only user content shows
	for _, it := range m.items {
		if it.Type == ItemTypeImage && strings.Contains(it.Display, "kicbase") {
			t.Errorf("system image should be filtered, got %q", it.Display)
		}
		if it.Type == ItemTypeContainer && strings.Contains(it.Display, "k8s_") {
			t.Errorf("system container should be filtered, got %q", it.Display)
		}
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

	var headers, images, containers int
	var imageDisplays, containerDisplays []string
	for _, it := range m.items {
		switch it.Type {
		case ItemTypeHeader:
			headers++
		case ItemTypeImage:
			images++
			imageDisplays = append(imageDisplays, it.Display)
		case ItemTypeContainer:
			containers++
			containerDisplays = append(containerDisplays, it.Display)
		}
	}

	if headers != 2 {
		t.Errorf("expected 2 headers, got %d", headers)
	}
	if images != 2 {
		t.Errorf("expected 2 images, got %d", images)
	}
	if containers != 1 {
		t.Errorf("expected 1 container, got %d", containers)
	}

	// Orphan image (no RepoTags) should display "<none>"
	orphanFound := false
	for _, d := range imageDisplays {
		if strings.Contains(d, "<none>") {
			orphanFound = true
			break
		}
	}
	if !orphanFound {
		t.Errorf("expected orphan image to display <none>, got %v", imageDisplays)
	}

	// Container name should have leading slash stripped
	if len(containerDisplays) != 1 {
		t.Fatalf("expected 1 container display, got %d", len(containerDisplays))
	}
	d := containerDisplays[0]
	if !strings.Contains(d, "web") || strings.Contains(d, "/web") {
		t.Errorf("expected container name without leading slash, got %q", d)
	}
}
