package docker_test

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
	mobyclient "github.com/moby/moby/client"
)

const (
	rmImg = "sha256:1111111111111111"
	rmCtr = "cccccccccccccccc"
	rmVol = "pgdata"
	rmNet = "nnnnnnnnnnnnnnnn"
)

// everything returns a fake holding one resource of each type, with 3 GB of
// reclaimable build cache.
func everything() *dockertest.Fake {
	return &dockertest.Fake{
		Images:     []client.APIImages{{ID: rmImg}},
		Containers: []client.APIContainers{{ID: rmCtr}},
		Volumes:    []client.Volume{{Name: rmVol}},
		Networks:   []client.Network{{ID: rmNet, Name: "app"}},
		Usage: mobyclient.DiskUsageResult{
			BuildCache: mobyclient.BuildCacheDiskUsage{TotalCount: 4, Reclaimable: 3 << 30},
		},
	}
}

func TestPerformCleanupRemovalOrder(t *testing.T) {
	var (
		img   = docker.CleanupItem{ID: rmImg, Type: docker.CleanupItemTypeImage}
		ctr   = docker.CleanupItem{ID: rmCtr, Type: docker.CleanupItemTypeContainer}
		vol   = docker.CleanupItem{ID: rmVol, Type: docker.CleanupItemTypeVolume}
		net   = docker.CleanupItem{ID: rmNet, Type: docker.CleanupItemTypeNetwork}
		cache = docker.CleanupItem{ID: docker.BuildCacheID, Type: docker.CleanupItemTypeBuildCache}
	)
	want := []string{"container:" + rmCtr, "image:" + rmImg, "volume:" + rmVol, "network:" + rmNet, "buildcache"}
	cases := []struct {
		name  string
		items []docker.CleanupItem
	}{
		{"already in order", []docker.CleanupItem{ctr, img, vol, net, cache}},
		{"reversed", []docker.CleanupItem{cache, net, vol, img, ctr}},
		{"shuffled", []docker.CleanupItem{vol, cache, img, net, ctr}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := everything()

			msg := docker.PerformCleanup(fake, c.items)()

			result, ok := msg.(docker.CleanupResultMsg)
			if !ok {
				t.Fatalf("expected CleanupResultMsg, got %T: %v", msg, msg)
			}
			if !slices.Equal(fake.Removed, want) {
				t.Errorf("Removed = %v, want %v", fake.Removed, want)
			}
			if len(result.Failed) != 0 {
				t.Errorf("Failed = %+v, want none", result.Failed)
			}
		})
	}
}

func TestPerformCleanupVolumeAndNetworkErrors(t *testing.T) {
	cases := []struct {
		name       string
		item       docker.CleanupItem
		err        error
		wantReason string
	}{
		{
			name:       "volume in use",
			item:       docker.CleanupItem{ID: rmVol, Type: docker.CleanupItemTypeVolume},
			err:        client.ErrVolumeInUse,
			wantReason: "volume is in use by a container; remove the container first",
		},
		{
			name:       "volume gone",
			item:       docker.CleanupItem{ID: rmVol, Type: docker.CleanupItemTypeVolume},
			err:        client.ErrNoSuchVolume,
			wantReason: "no such volume",
		},
		{
			name: "network with containers attached",
			item: docker.CleanupItem{ID: rmNet, Type: docker.CleanupItemTypeNetwork},
			err: &client.Error{
				Status:  http.StatusForbidden,
				Message: `error while removing network: network app id nnnnnnnnnnnn has active endpoints (name:"web" id:"cccccccccccc")`,
			},
			wantReason: "network has containers attached; remove or disconnect them first",
		},
		{
			name:       "predefined network",
			item:       docker.CleanupItem{ID: rmNet, Type: docker.CleanupItemTypeNetwork},
			err:        &client.Error{Status: http.StatusForbidden, Message: "bridge is a pre-defined network and cannot be removed"},
			wantReason: "bridge is a pre-defined network and cannot be removed",
		},
		{
			name:       "network error without message",
			item:       docker.CleanupItem{ID: rmNet, Type: docker.CleanupItemTypeNetwork},
			err:        &client.Error{Status: http.StatusInternalServerError},
			wantReason: "API error (500): ",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := everything()
			fake.RemoveErrs = map[string]error{c.item.ID: c.err}

			// Force must not make a volume or network forceable.
			item := c.item
			item.Force = true
			result := docker.PerformCleanup(fake, []docker.CleanupItem{c.item, item})().(docker.CleanupResultMsg)

			want := []docker.CleanupFailure{
				{Item: c.item, Reason: c.wantReason},
				{Item: item, Reason: c.wantReason},
			}
			if !slices.Equal(result.Failed, want) {
				t.Errorf("Failed = %+v, want %+v", result.Failed, want)
			}
			if retry := result.ForceRetry(); len(retry) != 0 {
				t.Errorf("ForceRetry() = %v, want none", retry)
			}
		})
	}
}

func TestPerformCleanupPrunesBuildCache(t *testing.T) {
	cases := []struct {
		name    string
		force   bool
		wantAll bool
	}{
		{"plain prune", false, false},
		{"internal and frontend records need All", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := everything()
			cache := docker.CleanupItem{ID: docker.BuildCacheID, Type: docker.CleanupItemTypeBuildCache, Force: c.force}
			img := docker.CleanupItem{ID: rmImg, Type: docker.CleanupItemTypeImage}

			result := docker.PerformCleanup(fake, []docker.CleanupItem{cache, img})().(docker.CleanupResultMsg)

			if want := []mobyclient.BuildCachePruneOptions{{All: c.wantAll}}; !slices.EqualFunc(fake.PruneOpts, want, func(a, b mobyclient.BuildCachePruneOptions) bool {
				return a.All == b.All
			}) {
				t.Errorf("BuildCachePrune options = %+v, want All=%v", fake.PruneOpts, c.wantAll)
			}
			if result.BuildCacheReclaimed != 3<<30 {
				t.Errorf("BuildCacheReclaimed = %d, want %d", result.BuildCacheReclaimed, int64(3<<30))
			}
			if got, want := result.Summary(), "Deleted 2 · Build cache freed 3.0 GB"; got != want {
				t.Errorf("Summary() = %q, want %q", got, want)
			}
		})
	}
}

func TestPerformCleanupBuildCacheError(t *testing.T) {
	fake := everything()
	fake.PruneErr = errors.New("builder is busy")
	cache := docker.CleanupItem{ID: docker.BuildCacheID, Type: docker.CleanupItemTypeBuildCache}

	result := docker.PerformCleanup(fake, []docker.CleanupItem{cache})().(docker.CleanupResultMsg)

	want := []docker.CleanupFailure{{Item: cache, Reason: "builder is busy"}}
	if !slices.Equal(result.Failed, want) {
		t.Errorf("Failed = %+v, want %+v", result.Failed, want)
	}
	// Nothing was pruned, so the summary does not mention freed space.
	if got, want := result.Summary(), "Deleted 0 · Failed 1"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

func TestRemovalOrder(t *testing.T) {
	want := []docker.CleanupItemType{
		docker.CleanupItemTypeContainer,
		docker.CleanupItemTypeImage,
		docker.CleanupItemTypeVolume,
		docker.CleanupItemTypeNetwork,
		docker.CleanupItemTypeBuildCache,
	}
	if got := docker.RemovalOrder(); !slices.Equal(got, want) {
		t.Errorf("RemovalOrder() = %v, want %v", got, want)
	}
}
