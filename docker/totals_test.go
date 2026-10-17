package docker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minhajul/docker-cleaner/docker/dockertest"
	mobyclient "github.com/moby/moby/client"
)

func TestFetchDiskUsageTotals(t *testing.T) {
	cases := []struct {
		name  string
		usage mobyclient.DiskUsageResult
		err   error
		want  DiskUsageTotals
	}{
		{name: "empty daemon"},
		{
			name: "every type",
			usage: mobyclient.DiskUsageResult{
				Images:     mobyclient.ImagesDiskUsage{TotalCount: 12, ActiveCount: 4, TotalSize: 4 << 30, Reclaimable: 1 << 30},
				Containers: mobyclient.ContainersDiskUsage{TotalCount: 5, ActiveCount: 2, TotalSize: 120 << 20, Reclaimable: 20 << 20},
				Volumes:    mobyclient.VolumesDiskUsage{TotalCount: 3, ActiveCount: 1, TotalSize: 2 << 30, Reclaimable: 500 << 20},
				BuildCache: mobyclient.BuildCacheDiskUsage{TotalCount: 40, ActiveCount: 0, TotalSize: 1 << 30, Reclaimable: 1 << 30},
			},
			want: DiskUsageTotals{
				Images:     TypeUsage{Total: 12, Active: 4, Size: 4 << 30, Reclaimable: 1 << 30},
				Containers: TypeUsage{Total: 5, Active: 2, Size: 120 << 20, Reclaimable: 20 << 20},
				Volumes:    TypeUsage{Total: 3, Active: 1, Size: 2 << 30, Reclaimable: 500 << 20},
				BuildCache: TypeUsage{Total: 40, Active: 0, Size: 1 << 30, Reclaimable: 1 << 30},
			},
		},
		{
			name: "everything in use",
			usage: mobyclient.DiskUsageResult{
				Images:     mobyclient.ImagesDiskUsage{TotalCount: 2, ActiveCount: 2, TotalSize: 300},
				Containers: mobyclient.ContainersDiskUsage{TotalCount: 2, ActiveCount: 2, TotalSize: 40},
			},
			want: DiskUsageTotals{
				Images:     TypeUsage{Total: 2, Active: 2, Size: 300},
				Containers: TypeUsage{Total: 2, Active: 2, Size: 40},
			},
		},
		{
			name: "failed",
			// Totals the fake would otherwise report must not leak through.
			usage: mobyclient.DiskUsageResult{Images: mobyclient.ImagesDiskUsage{TotalCount: 1, TotalSize: 10}},
			err:   errors.New("df not supported"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &dockertest.Fake{Usage: c.usage, DiskUsageErr: c.err}

			msg := FetchDiskUsage(fake)().(DiskUsageFetchedMsg)

			if !errors.Is(msg.Err, c.err) {
				t.Errorf("Err = %v, want %v", msg.Err, c.err)
			}
			if msg.Usage.Totals != c.want {
				t.Errorf("Totals = %+v, want %+v", msg.Usage.Totals, c.want)
			}
			if o := fake.DiskUsageOpts; !o.Images || !o.Containers || !o.Volumes || !o.BuildCache {
				t.Errorf("DiskUsage options = %+v, want every type", o)
			}
		})
	}
}

// TestDiskUsageTotalsMatchSystemDF runs the real moby client against a fake
// daemon to check the totals follow the definitions DiskUsageTotals documents
// (those of `docker system df`), whether the daemon reports them itself (API
// 1.52 and later) or only lists the resources for the client to sum up.
func TestDiskUsageTotalsMatchSystemDF(t *testing.T) {
	// Image b is in use; a's unshared 500 bytes are reclaimable, while c's
	// shared size is unknown, so it counts for nothing. Container c1 is
	// running. Volume v1 is mounted; v3's driver reports no size. Build
	// cache record r2 is in use, and the shared r3 is left out.
	const legacy = `{
		"LayersSize": 1000,
		"Images": [
			{"Id": "a", "Size": 600, "SharedSize": 100, "Containers": 0},
			{"Id": "b", "Size": 400, "SharedSize": 100, "Containers": 2},
			{"Id": "c", "Size": 50, "SharedSize": -1, "Containers": 0}
		],
		"Containers": [
			{"Id": "c1", "State": "running", "SizeRw": 10},
			{"Id": "c2", "State": "exited", "SizeRw": 5}
		],
		"Volumes": [
			{"Name": "v1", "UsageData": {"Size": 100, "RefCount": 1}},
			{"Name": "v2", "UsageData": {"Size": 30, "RefCount": 0}},
			{"Name": "v3", "UsageData": {"Size": -1, "RefCount": 0}}
		],
		"BuildCache": [
			{"ID": "r1", "Size": 70, "InUse": false, "Shared": false},
			{"ID": "r2", "Size": 20, "InUse": true, "Shared": false},
			{"ID": "r3", "Size": 40, "InUse": false, "Shared": true}
		]
	}`
	// The same daemon, reporting the totals itself.
	const current = `{
		"ImageUsage": {"TotalCount": 3, "ActiveCount": 1, "TotalSize": 1000, "Reclaimable": 500},
		"ContainerUsage": {"TotalCount": 2, "ActiveCount": 1, "TotalSize": 15, "Reclaimable": 5},
		"VolumeUsage": {"TotalCount": 3, "ActiveCount": 1, "TotalSize": 130, "Reclaimable": 30},
		"BuildCacheUsage": {"TotalCount": 3, "ActiveCount": 1, "TotalSize": 90, "Reclaimable": 70}
	}`
	want := DiskUsageTotals{
		Images:     TypeUsage{Total: 3, Active: 1, Size: 1000, Reclaimable: 500},
		Containers: TypeUsage{Total: 2, Active: 1, Size: 15, Reclaimable: 5},
		Volumes:    TypeUsage{Total: 3, Active: 1, Size: 130, Reclaimable: 30},
		BuildCache: TypeUsage{Total: 3, Active: 1, Size: 90, Reclaimable: 70},
	}

	cases := []struct {
		name       string
		apiVersion string
		body       string
	}{
		{"API 1.51, totals derived by the client", "1.51", legacy},
		{"API 1.52, totals from the daemon", "1.52", current},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/system/df") {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			mc, err := newMobyClient("tcp://"+srv.Listener.Addr().String(), nil, c.apiVersion)
			if err != nil {
				t.Fatalf("newMobyClient: %v", err)
			}
			msg := FetchDiskUsage(&Client{moby: mc})().(DiskUsageFetchedMsg)

			if msg.Err != nil {
				t.Fatalf("FetchDiskUsage: %v", msg.Err)
			}
			if msg.Usage.Totals != want {
				t.Errorf("Totals = %+v, want %+v", msg.Usage.Totals, want)
			}
		})
	}
}
