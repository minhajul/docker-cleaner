package models

import (
	"errors"
	"strings"
	"testing"

	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker/dockertest"
	"github.com/moby/moby/api/types/volume"
)

const (
	volData  = "pgdata"
	volCache = "cache"
	// volAnon is an anonymous volume: its name is a random hex ID.
	volAnon = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// withVolumes adds three volumes to fake: pgdata (48 MB, used by the
// stopped container db), cache (1 GB, unused) and an anonymous one whose
// driver does not report its size.
func withVolumes(fake *dockertest.Fake) *dockertest.Fake {
	fake.Volumes = []dockerClient.Volume{
		{Name: volData, Driver: "local"},
		{Name: volCache, Driver: "local"},
		{Name: volAnon, Driver: "nfs"},
	}
	fake.Usage.Volumes.Items = []volume.Volume{
		{Name: volData, UsageData: &volume.UsageData{Size: 48 << 20, RefCount: 1}},
		{Name: volCache, UsageData: &volume.UsageData{Size: 1 << 30, RefCount: 0}},
		{Name: volAnon, UsageData: &volume.UsageData{Size: -1, RefCount: -1}},
	}
	for i, c := range fake.Containers {
		if c.ID == ctrStopped {
			fake.Containers[i].Mounts = []dockerClient.APIMount{{Type: "volume", Name: volData}}
		}
	}
	return fake
}

// rowDisplay returns the Display of the row holding id, or "" if none does.
func rowDisplay(m Model, id string) string {
	for _, it := range m.items {
		if it.ID == id && it.Type != ItemTypeHeader {
			return it.Display
		}
	}
	return ""
}

func TestVolumesSection(t *testing.T) {
	m := loaded(t, withVolumes(newFake()))

	want := map[string]string{
		volData:  "pgdata (Driver: local, Size: 48.0 MB, Used by: 1 container)",
		volCache: "cache (Driver: local, Size: 1.0 GB, Unused)",
		// Unknown size and ref count: the use comes from the mounts.
		volAnon: "0123456789ab (Driver: nfs, Size: unknown, Unused)",
	}
	for id, display := range want {
		if got := rowDisplay(m, id); got != display {
			t.Errorf("row %s = %q, want %q", id, got, display)
		}
	}
	view := m.View()
	if !strings.Contains(view, "💾 Volumes") || strings.Contains(view, "sizes unavailable") {
		t.Errorf("view should show the Volumes header without a note, got:\n%s", view)
	}
}

func TestVolumesSectionHiddenWhenEmpty(t *testing.T) {
	m := loaded(t, newFake())
	if strings.Contains(m.View(), volumesHeader) {
		t.Errorf("view should not show an empty Volumes section, got:\n%s", m.View())
	}
}

func TestVolumesSkipSystemVolumes(t *testing.T) {
	fake := withVolumes(newFake())
	fake.Volumes = append(fake.Volumes, dockerClient.Volume{
		Name:   "minikube",
		Labels: map[string]string{"created_by.minikube.sigs.k8s.io": "true"},
	})
	m := loaded(t, fake)
	if listed(m, "minikube") {
		t.Errorf("the minikube volume should be hidden, rows: %v", m.items)
	}
}

func TestDiskUsageErrorKeepsListsWorking(t *testing.T) {
	fake := withVolumes(newFake())
	fake.DiskUsageErr = errors.New("df not supported")
	m := loaded(t, fake)

	for _, id := range []string{imgA, imgB, ctrRunning, ctrStopped, volData, volCache} {
		if !listed(m, id) {
			t.Errorf("%s should still be listed, rows: %v", id, m.items)
		}
	}
	// Without disk usage the volume's use is counted from the mounts.
	if got, want := rowDisplay(m, volData), "pgdata (Driver: local, Size: unknown, Used by: 1 container)"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
	view := m.View()
	if !strings.Contains(view, "💾 Volumes (sizes unavailable)") {
		t.Errorf("view should note the missing sizes, got:\n%s", view)
	}
	if m.errorMsg != "" || strings.Contains(view, "❌") {
		t.Errorf("a disk usage error should not show as an error, got:\n%s", view)
	}

	// Refreshing still finishes, and deleting still works.
	m = send(t, m, runes("r"))
	if strings.Contains(m.View(), "Refreshing") {
		t.Errorf("refresh should finish despite the disk usage error, got:\n%s", m.View())
	}
	m = toggle(t, m, imgA)
	m = send(t, m, runes("d"))
	send(t, m, runes("y"))
	if want := "image:" + imgA; len(fake.Removed) != 1 || fake.Removed[0] != want {
		t.Errorf("Removed = %v, want [%s]", fake.Removed, want)
	}
}

func TestDeleteVolumes(t *testing.T) {
	fake := withVolumes(newFake())
	fake.RemoveErrs = map[string]error{volData: dockerClient.ErrVolumeInUse}
	m := loaded(t, fake, imgB, volData, volCache)

	m = send(t, m, runes("d"))
	if want := "Delete 1 image, 2 volumes (1.5 GB)? y/N"; !strings.Contains(m.View(), want) {
		t.Errorf("view should contain %q, got:\n%s", want, m.View())
	}
	m = send(t, m, runes("y"))

	view := m.View()
	for _, want := range []string{
		"❌ Deleted 2 · Failed 1",
		"pgdata: volume is in use by a container; remove the container first",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view should contain %q, got:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Press f") {
		t.Errorf("an in-use volume should not offer a forced retry, got:\n%s", view)
	}
	if listed(m, volCache) || !listed(m, volData) {
		t.Errorf("after the refresh, cache should be gone and pgdata listed, rows: %v", m.items)
	}
}
