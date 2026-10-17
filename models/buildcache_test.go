package models

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"
	"github.com/moby/moby/api/types/build"
	mobyclient "github.com/moby/moby/client"
)

// withBuildCache gives fake 312 build cache records, 3 GB of them
// reclaimable. frontend says whether an unused frontend record is among
// them, which only a prune with All removes.
func withBuildCache(fake *dockertest.Fake, frontend bool) *dockertest.Fake {
	items := []build.CacheRecord{{Type: "regular", Size: 3 << 30}}
	if frontend {
		items = append(items, build.CacheRecord{Type: "frontend", Size: 40 << 20})
	}
	fake.Usage.BuildCache = mobyclient.BuildCacheDiskUsage{
		TotalCount:  312,
		Reclaimable: 3 << 30,
		Items:       items,
	}
	return fake
}

func TestBuildCacheSection(t *testing.T) {
	m := loaded(t, withBuildCache(newFake(), false))

	if got, want := rowDisplay(m, docker.BuildCacheID), "Build cache: 312 entries, 3.0 GB reclaimable"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
	// It comes last, under its own header.
	if n := len(m.items); n < 2 || m.items[n-2].Display != "🧱 Build cache" || m.items[n-1].ID != docker.BuildCacheID {
		t.Errorf("the build cache should be the last section, rows: %v", m.items)
	}
}

func TestBuildCacheSectionHidden(t *testing.T) {
	cases := []struct {
		name  string
		usage mobyclient.BuildCacheDiskUsage
	}{
		{"no build cache reported", mobyclient.BuildCacheDiskUsage{}},
		{"all in use", mobyclient.BuildCacheDiskUsage{TotalCount: 3, ActiveCount: 3, TotalSize: 1 << 20}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := newFake()
			fake.Usage.BuildCache = c.usage
			m := loaded(t, fake)
			if strings.Contains(m.View(), "Build cache") {
				t.Errorf("view should not show the build cache, got:\n%s", m.View())
			}
		})
	}
}

func TestBuildCacheUnavailable(t *testing.T) {
	fake := withBuildCache(newFake(), false)
	fake.DiskUsageErr = errors.New("permission denied")
	m := loaded(t, fake)

	if !strings.Contains(m.View(), "🧱 Build cache (unavailable: permission denied)") {
		t.Errorf("view should note the build cache is unavailable, got:\n%s", m.View())
	}
	if listed(m, docker.BuildCacheID) {
		t.Errorf("the build cache should not be selectable without disk usage, rows: %v", m.items)
	}
}

func TestDeleteBuildCache(t *testing.T) {
	cases := []struct {
		name     string
		frontend bool
		wantAll  bool
	}{
		{"plain prune", false, false},
		{"unused frontend record needs All", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := withBuildCache(newFake(), c.frontend)
			m := loaded(t, fake, docker.BuildCacheID)

			m = send(t, m, runes("d"))
			if want := "Delete build cache (3.0 GB)? y/N"; !strings.Contains(m.View(), want) {
				t.Errorf("view should contain %q, got:\n%s", want, m.View())
			}
			m = send(t, m, runes("y"))

			if len(fake.PruneOpts) != 1 || fake.PruneOpts[0].All != c.wantAll {
				t.Errorf("BuildCachePrune options = %+v, want one call with All=%v", fake.PruneOpts, c.wantAll)
			}
			view := m.View()
			if want := "✅ Deleted 1 · Build cache freed 3.0 GB"; !strings.Contains(view, want) {
				t.Errorf("view should contain %q, got:\n%s", want, view)
			}
			// The refresh finds nothing left to reclaim.
			if strings.Contains(view, "🧱 Build cache") || len(m.selected) != 0 {
				t.Errorf("the pruned cache should be gone, selected=%v view:\n%s", selectedIDs(m), view)
			}
		})
	}
}

func TestBuildCachePruneFailure(t *testing.T) {
	fake := withBuildCache(newFake(), false)
	fake.PruneErr = errors.New("builder is busy")
	m := loaded(t, fake, docker.BuildCacheID)

	m = send(t, m, runes("d"))
	m = send(t, m, runes("y"))

	view := m.View()
	for _, want := range []string{"❌ Deleted 0 · Failed 1", "build cache: builder is busy"} {
		if !strings.Contains(view, want) {
			t.Errorf("view should contain %q, got:\n%s", want, view)
		}
	}
	if !slices.Equal(selectedIDs(m), []string{docker.BuildCacheID}) {
		t.Errorf("the build cache should stay selected for a retry, selected = %v", selectedIDs(m))
	}
}

// TestDeleteEverything selects one item of every type and checks the dialog
// and the removal order.
func TestDeleteEverything(t *testing.T) {
	fake := withBuildCache(withNetworks(withVolumes(newFake())), false)
	m := loaded(t, fake, netSpare, docker.BuildCacheID, volCache, imgA, imgB, ctrStopped)

	m = send(t, m, runes("d"))
	// 1.5 GB of images, 2 MB container, 1 GB volume, 3 GB build cache.
	if want := "Delete 2 images, 1 container, 1 volume, 1 network, build cache (5.5 GB)? y/N"; !strings.Contains(m.View(), want) {
		t.Errorf("view should contain %q, got:\n%s", want, m.View())
	}
	m = send(t, m, runes("y"))

	want := []string{
		"container:" + ctrStopped,
		"image:" + imgA,
		"image:" + imgB,
		"volume:" + volCache,
		"network:" + netSpare,
		"buildcache",
	}
	if !slices.Equal(fake.Removed, want) {
		t.Errorf("Removed = %v, want %v", fake.Removed, want)
	}
	if v := m.View(); !strings.Contains(v, "✅ Deleted 6 · Build cache freed 3.0 GB") {
		t.Errorf("view should report every deletion, got:\n%s", v)
	}
}

func TestDialogMixedTypes(t *testing.T) {
	fake := withBuildCache(withVolumes(newFake()), false)
	m := loaded(t, fake, imgA, imgB, volData, docker.BuildCacheID)

	m = send(t, m, runes("d"))

	// 1.5 GB of images, 48 MB volume and 3 GB of build cache.
	if want := "Delete 2 images, 1 volume, build cache (4.5 GB)? y/N"; !strings.Contains(m.View(), want) {
		t.Errorf("view should contain %q, got:\n%s", want, m.View())
	}
}
