package filter

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	client "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
)

// now is the fixed time the tests measure ages from.
var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// daysAgo is the Unix time n days before now.
func daysAgo(n int) int64 {
	return now.Add(-time.Duration(n) * Day).Unix()
}

func TestIsDangling(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want bool
	}{
		{"no tags", nil, true},
		{"placeholder tag", []string{"<none>:<none>"}, true},
		{"tagged", []string{"alpine:latest"}, false},
		{"tagged and placeholder", []string{"<none>:<none>", "alpine:3"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsDangling(client.APIImages{RepoTags: c.tags}); got != c.want {
				t.Errorf("IsDangling(%v) = %v, want %v", c.tags, got, c.want)
			}
		})
	}
}

func TestIsStopped(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{
		{"exited", true},
		{"created", true},
		{"dead", true},
		{"running", false},
		{"paused", false},
		{"restarting", false},
		{"removing", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			if got := IsStopped(client.APIContainers{State: c.state}); got != c.want {
				t.Errorf("IsStopped(%q) = %v, want %v", c.state, got, c.want)
			}
		})
	}
}

func TestIsOlderThan(t *testing.T) {
	cases := []struct {
		name    string
		created time.Time
		d       time.Duration
		want    bool
	}{
		{"older", now.Add(-31 * Day), 30 * Day, true},
		{"exactly the age", now.Add(-30 * Day), 30 * Day, false},
		{"newer", now.Add(-29 * Day), 30 * Day, false},
		{"unknown creation time", time.Time{}, 30 * Day, false},
		{"in the future", now.Add(time.Hour), 7 * Day, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsOlderThan(c.created, c.d, now); got != c.want {
				t.Errorf("IsOlderThan(%v, %v) = %v, want %v", c.created, c.d, got, c.want)
			}
		})
	}
}

func TestCreatedTreatsZeroAsUnknown(t *testing.T) {
	if got := ImageCreated(client.APIImages{}); !got.IsZero() {
		t.Errorf("ImageCreated without Created = %v, want zero", got)
	}
	if got := ContainerCreated(client.APIContainers{}); !got.IsZero() {
		t.Errorf("ContainerCreated without Created = %v, want zero", got)
	}
	if got, want := ImageCreated(client.APIImages{Created: daysAgo(3)}), now.Add(-3*Day); !got.Equal(want) {
		t.Errorf("ImageCreated = %v, want %v", got, want)
	}
}

// fixture is a daemon's worth of resources: images used and unused, tagged
// and dangling, old and new; containers in every state; volumes and networks
// in use or not.
var (
	images = []client.APIImages{
		{ID: "sha256:app", RepoTags: []string{"app:latest"}, Created: daysAgo(2)},
		{ID: "sha256:old", RepoTags: []string{"old:1"}, Created: daysAgo(100)},
		{ID: "sha256:dangling", RepoTags: []string{"<none>:<none>"}, Created: daysAgo(40)},
		{ID: "sha256:untagged", Created: daysAgo(1)}, // used by a container, by ID
		{ID: "sha256:noage", RepoTags: []string{"noage:1"}},
	}
	containers = []client.APIContainers{
		{ID: "web", Image: "app", State: "running", Created: daysAgo(1),
			Mounts:   []client.APIMount{{Type: "volume", Name: "data"}},
			Networks: client.NetworkList{Networks: map[string]client.ContainerNetwork{"app_net": {NetworkID: "net-app"}}}},
		{ID: "job", Image: "sha256:untagged", State: "exited", Created: daysAgo(45),
			// Attached by name, without a network ID.
			Networks: client.NetworkList{Networks: map[string]client.ContainerNetwork{"named": {}}}},
		{ID: "new", Image: "app:latest", State: "created", Created: daysAgo(10)},
		{ID: "zombie", Image: "app", State: "dead", Created: daysAgo(200)},
		{ID: "nap", Image: "app", State: "paused"},
	}
	volumes = []client.Volume{
		{Name: "data", CreatedAt: now.Add(-50 * Day)},   // mounted by web
		{Name: "cache", CreatedAt: now.Add(-5 * Day)},   // unused
		{Name: "logs", CreatedAt: now.Add(-60 * Day)},   // unused, old
		{Name: "counted", CreatedAt: now.Add(-1 * Day)}, // used per disk usage
		{Name: "noage"}, // unused, no creation time
	}
	volumeUsage = map[string]docker.VolumeUsage{
		"counted": {Size: 10, RefCount: 2},
		"logs":    {Size: 10, RefCount: 0},
		// No RefCount: falls back to the mounts.
		"data": {Size: -1, RefCount: -1},
	}
	networks = []client.Network{
		{ID: "net-app", Name: "app_net"},
		{ID: "net-spare", Name: "spare"},
		// Reports its own attached container.
		{ID: "net-own", Name: "own", Containers: map[string]client.Endpoint{"other": {}}},
		// job is attached to it by name.
		{ID: "net-named", Name: "named"},
	}
)

func TestImages(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"no filters", Options{}, []string{"sha256:app", "sha256:old", "sha256:dangling", "sha256:untagged", "sha256:noage"}},
		{"dangling", Options{Dangling: true}, []string{"sha256:dangling", "sha256:untagged"}},
		{"unused", Options{Unused: true}, []string{"sha256:old", "sha256:dangling", "sha256:noage"}},
		{"older than 30d", Options{OlderThan: 30 * Day}, []string{"sha256:old", "sha256:dangling"}},
		{"older than 90d", Options{OlderThan: 90 * Day}, []string{"sha256:old"}},
		{"dangling and unused", Options{Dangling: true, Unused: true}, []string{"sha256:dangling"}},
		{"unused and older than 7d", Options{Unused: true, OlderThan: 7 * Day}, []string{"sha256:old", "sha256:dangling"}},
		{"stopped does not narrow images", Options{Stopped: true}, []string{"sha256:app", "sha256:old", "sha256:dangling", "sha256:untagged", "sha256:noage"}},
	}
	u := NewUsage(containers, nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, img := range c.opts.Images(images, u, now) {
				got = append(got, img.ID)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("Images = %v, want %v", got, c.want)
			}
		})
	}
}

func TestContainers(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"no filters", Options{}, []string{"web", "job", "new", "zombie", "nap"}},
		{"stopped", Options{Stopped: true}, []string{"job", "new", "zombie"}},
		{"older than 30d", Options{OlderThan: 30 * Day}, []string{"job", "zombie"}},
		{"stopped and older than 7d", Options{Stopped: true, OlderThan: 7 * Day}, []string{"job", "new", "zombie"}},
		{"stopped and older than 90d", Options{Stopped: true, OlderThan: 90 * Day}, []string{"zombie"}},
		{"dangling and unused do not narrow containers", Options{Dangling: true, Unused: true}, []string{"web", "job", "new", "zombie", "nap"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, ctr := range c.opts.Containers(containers, now) {
				got = append(got, ctr.ID)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("Containers = %v, want %v", got, c.want)
			}
		})
	}
}

func TestVolumes(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"no filters", Options{}, []string{"data", "cache", "logs", "counted", "noage"}},
		{"unused", Options{Unused: true}, []string{"cache", "logs", "noage"}},
		{"older than 30d", Options{OlderThan: 30 * Day}, []string{"data", "logs"}},
		{"unused and older than 30d", Options{Unused: true, OlderThan: 30 * Day}, []string{"logs"}},
		{"dangling and stopped do not narrow volumes", Options{Dangling: true, Stopped: true}, []string{"data", "cache", "logs", "counted", "noage"}},
	}
	u := NewUsage(containers, volumeUsage)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, v := range c.opts.Volumes(volumes, u, now) {
				got = append(got, v.Name)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("Volumes = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNetworks(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"no filters", Options{}, []string{"app_net", "spare", "own", "named"}},
		{"unused", Options{Unused: true}, []string{"spare"}},
		{"older than does not narrow networks", Options{OlderThan: 7 * Day}, []string{"app_net", "spare", "own", "named"}},
	}
	u := NewUsage(containers, nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, n := range c.opts.Networks(networks, u) {
				got = append(got, n.Name)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("Networks = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUsageCounts(t *testing.T) {
	u := NewUsage(containers, volumeUsage)
	volumeCases := []struct {
		name string
		want int64
	}{
		{"data", 1},    // RefCount unknown: counted from the mounts
		{"counted", 2}, // RefCount from the disk usage, no mounts
		{"logs", 0},
		{"missing", 0},
	}
	for _, c := range volumeCases {
		if got := u.VolumeRefs(c.name); got != c.want {
			t.Errorf("VolumeRefs(%q) = %d, want %d", c.name, got, c.want)
		}
	}
	networkCases := []struct {
		name string
		want int
	}{
		{"app_net", 1},
		{"spare", 0},
		{"own", 1},
		{"named", 1},
	}
	for i, c := range networkCases {
		if got := u.NetworkContainers(networks[i]); got != c.want {
			t.Errorf("NetworkContainers(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestOptionsDescribeThemselves(t *testing.T) {
	cases := []struct {
		name    string
		opts    Options
		active  bool
		labels  []string
		narrows []docker.CleanupItemType
	}{
		{"none", Options{}, false, nil, nil},
		{"dangling", Options{Dangling: true}, true, []string{"dangling"},
			[]docker.CleanupItemType{docker.CleanupItemTypeImage}},
		{"stopped", Options{Stopped: true}, true, []string{"stopped"},
			[]docker.CleanupItemType{docker.CleanupItemTypeContainer}},
		{"unused", Options{Unused: true}, true, []string{"unused"},
			[]docker.CleanupItemType{docker.CleanupItemTypeImage, docker.CleanupItemTypeVolume, docker.CleanupItemTypeNetwork}},
		{"older than", Options{OlderThan: 30 * Day}, true, []string{"older than 30d"},
			[]docker.CleanupItemType{docker.CleanupItemTypeImage, docker.CleanupItemTypeContainer, docker.CleanupItemTypeVolume}},
		{"all", Options{Dangling: true, Stopped: true, Unused: true, OlderThan: 7 * Day}, true,
			[]string{"dangling", "stopped", "unused", "older than 7d"},
			[]docker.CleanupItemType{docker.CleanupItemTypeImage, docker.CleanupItemTypeContainer, docker.CleanupItemTypeVolume, docker.CleanupItemTypeNetwork}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.opts.Active(); got != c.active {
				t.Errorf("Active = %v, want %v", got, c.active)
			}
			if got := c.opts.Labels(); !slices.Equal(got, c.labels) {
				t.Errorf("Labels = %q, want %q", got, c.labels)
			}
			var narrows []docker.CleanupItemType
			for _, typ := range docker.RemovalOrder() {
				if c.opts.Narrows(typ) {
					narrows = append(narrows, typ)
				}
			}
			slices.Sort(narrows)
			if !slices.Equal(narrows, c.narrows) {
				t.Errorf("Narrows = %v, want %v", narrows, c.narrows)
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{7 * Day, "7d"},
		{90 * Day, "90d"},
		{36 * time.Hour, "36h"},
		{90 * time.Minute, "1h30m0s"},
	}
	for _, c := range cases {
		if got := FormatAge(c.d); got != c.want {
			t.Errorf("FormatAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestParseAge(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"30d", 30 * Day},
		{"1d", Day},
		{"2w", 14 * Day},
		{"12h", 12 * time.Hour},
		{"90m", 90 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"036d", 36 * Day},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseAge(c.in)
			if err != nil || got != c.want {
				t.Errorf("ParseAge(%q) = %v, %v; want %v", c.in, got, err, c.want)
			}
		})
	}
}

func TestParseAgeInvalid(t *testing.T) {
	for _, in := range []string{
		"", "30", "d", "w", "30x", "1.5d", "thirty days", "d30", "3 d",
		"0d", "0h", "-1d", "-2h", "0",
		"99999999999999999999d", "200000w",
	} {
		t.Run(in, func(t *testing.T) {
			if d, err := ParseAge(in); err == nil {
				t.Errorf("ParseAge(%q) = %v, want an error", in, d)
			} else if !strings.Contains(err.Error(), fmt.Sprintf("%q", in)) {
				t.Errorf("ParseAge(%q) error %q does not name the input", in, err)
			}
		})
	}
}

func TestParseAgeRoundTrip(t *testing.T) {
	for _, d := range []time.Duration{Day, 7 * Day, 30 * Day, 36 * time.Hour, 90 * time.Minute} {
		got, err := ParseAge(FormatAge(d))
		if err != nil || got != d {
			t.Errorf("ParseAge(FormatAge(%v)) = %v, %v", d, got, err)
		}
	}
}
