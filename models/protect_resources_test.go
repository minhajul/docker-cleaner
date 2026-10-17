package models

import (
	"slices"
	"strings"
	"testing"

	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
)

// resourcesModel returns a model protecting patterns that lists the volumes
// pgdata and scratch and the networks bridge (predefined), app_net and
// prod_net.
func resourcesModel(t *testing.T, patterns ...string) Model {
	t.Helper()
	p, err := docker.NewProtector(patterns)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(nil)
	m.protector = p
	m = updated(m, docker.VolumesFetchedMsg{Volumes: []dockerClient.Volume{
		{Name: "pgdata", Driver: "local"},
		{Name: "scratch", Driver: "local"},
	}})
	return updated(m, docker.NetworksFetchedMsg{Networks: []dockerClient.Network{
		{ID: "bbbbbbbbbbbbbbbb", Name: "bridge", Driver: "bridge"},
		{ID: "aaaaaaaaaaaaaaaa", Name: "app_net", Driver: "bridge"},
		{ID: "pppppppppppppppp", Name: "prod_net", Driver: "bridge"},
	}})
}

func TestProtectListCoversVolumesAndNetworks(t *testing.T) {
	m := resourcesModel(t, "pgdata", "prod_*")

	listed := listedIDs(m)
	for _, hidden := range []string{"pgdata", "pppppppppppppppp"} {
		if slices.Contains(listed, hidden) {
			t.Errorf("protected %s should be hidden, listed %v", hidden, listed)
		}
	}
	for _, shown := range []string{"scratch", "aaaaaaaaaaaaaaaa"} {
		if !slices.Contains(listed, shown) {
			t.Errorf("%s should be listed, got %v", shown, listed)
		}
	}
	if got := m.protected["pgdata"]; got != docker.ReasonProtectList {
		t.Errorf("pgdata reason = %q, want %q", got, docker.ReasonProtectList)
	}

	m = press(m, "a")
	for _, it := range m.getSelectionForCleanup() {
		if it.ID == "pgdata" || it.ID == "pppppppppppppppp" {
			t.Errorf("selection includes protected %s", it.ID)
		}
	}
}

func TestPredefinedNetworksNeitherListedNorCounted(t *testing.T) {
	m := resourcesModel(t)
	if len(m.protected) != 0 {
		t.Errorf("nothing should be protected, got %v", m.protected)
	}
	if strings.Contains(m.View(), "protected") {
		t.Errorf("no protected hint expected, got:\n%s", m.View())
	}

	m = press(m, "p")
	if slices.Contains(listedIDs(m), "bbbbbbbbbbbbbbbb") {
		t.Error("the predefined bridge network should never be listed, even when revealing protected items")
	}
}
