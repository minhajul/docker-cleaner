package docker_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/volume"
	mobyclient "github.com/moby/moby/client"
)

func TestFetchDiskUsageBuildCache(t *testing.T) {
	record := func(typ string, size int64, inUse, shared bool) build.CacheRecord {
		return build.CacheRecord{Type: typ, Size: size, InUse: inUse, Shared: shared}
	}
	cases := []struct {
		name    string
		records []build.CacheRecord
		want    bool // NeedsAll
	}{
		{"no records", nil, false},
		{"plain prune frees everything", []build.CacheRecord{record("regular", 10, false, false), record("source.local", 5, false, false)}, false},
		{"unused frontend", []build.CacheRecord{record("regular", 10, false, false), record("frontend", 40, false, false)}, true},
		{"unused internal", []build.CacheRecord{record("internal", 1, false, false)}, true},
		// Not reclaimable anyway, so no reason for All.
		{"frontend in use", []build.CacheRecord{record("frontend", 40, true, false)}, false},
		{"shared frontend", []build.CacheRecord{record("frontend", 40, false, true)}, false},
		{"empty frontend", []build.CacheRecord{record("frontend", 0, false, false)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &dockertest.Fake{Usage: mobyclient.DiskUsageResult{
				BuildCache: mobyclient.BuildCacheDiskUsage{
					TotalCount:  int64(len(c.records)) + 1,
					Reclaimable: 50,
					Items:       c.records,
				},
			}}

			got := docker.FetchDiskUsage(fake)().(docker.DiskUsageFetchedMsg).Usage.BuildCache

			want := docker.BuildCacheUsage{Entries: int64(len(c.records)) + 1, Reclaimable: 50, NeedsAll: c.want}
			if got != want {
				t.Errorf("BuildCache = %+v, want %+v", got, want)
			}
			if !fake.DiskUsageOpts.BuildCache {
				t.Errorf("DiskUsage options = %+v, want BuildCache", fake.DiskUsageOpts)
			}
		})
	}
}

func TestFetchDiskUsage(t *testing.T) {
	fake := &dockertest.Fake{
		Usage: mobyclient.DiskUsageResult{
			Volumes: mobyclient.VolumesDiskUsage{Items: []volume.Volume{
				{Name: "pgdata", UsageData: &volume.UsageData{Size: 48 << 20, RefCount: 1}},
				{Name: "nfs", UsageData: &volume.UsageData{Size: -1, RefCount: 0}},
				{Name: "nodata"},
			}},
		},
	}

	msg := docker.FetchDiskUsage(fake)()

	got, ok := msg.(docker.DiskUsageFetchedMsg)
	if !ok {
		t.Fatalf("expected DiskUsageFetchedMsg, got %T: %v", msg, msg)
	}
	want := map[string]docker.VolumeUsage{
		"pgdata": {Size: 48 << 20, RefCount: 1},
		"nfs":    {Size: -1, RefCount: 0},
	}
	if got.Err != nil || !reflect.DeepEqual(got.Usage.Volumes, want) {
		t.Errorf("got %+v, want volumes %+v", got, want)
	}
	// Without Verbose, API 1.52+ daemons leave out the volume list.
	if !fake.DiskUsageOpts.Volumes || !fake.DiskUsageOpts.Verbose {
		t.Errorf("DiskUsage options = %+v, want Volumes and Verbose", fake.DiskUsageOpts)
	}
}

func TestFetchDiskUsageError(t *testing.T) {
	boom := errors.New("df not supported")
	msg := docker.FetchDiskUsage(&dockertest.Fake{DiskUsageErr: boom})()

	// Not an ErrMsg: the rest of the UI keeps working without sizes.
	got, ok := msg.(docker.DiskUsageFetchedMsg)
	if !ok {
		t.Fatalf("expected DiskUsageFetchedMsg, got %T: %v", msg, msg)
	}
	if !errors.Is(got.Err, boom) {
		t.Errorf("Err = %v, want %v", got.Err, boom)
	}
}

func TestFilterUserVolumes(t *testing.T) {
	volumes := []client.Volume{
		{Name: "pgdata"},
		{Name: "labelled", Labels: map[string]string{"com.docker.compose.project": "app"}},
		{Name: "minikube", Labels: map[string]string{
			"created_by.minikube.sigs.k8s.io": "true",
			"name.minikube.sigs.k8s.io":       "minikube",
		}},
		{Name: "kind-data", Labels: map[string]string{"io.x-k8s.kind.cluster": "kind"}},
	}

	var names []string
	for _, v := range docker.FilterUserVolumes(volumes) {
		names = append(names, v.Name)
	}
	if want := []string{"pgdata", "labelled"}; !reflect.DeepEqual(names, want) {
		t.Errorf("FilterUserVolumes() = %v, want %v", names, want)
	}
}
