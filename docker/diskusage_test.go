package docker_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
	"github.com/moby/moby/api/types/volume"
	mobyclient "github.com/moby/moby/client"
)

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
