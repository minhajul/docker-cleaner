// Command demo runs the docker-cleaner TUI on canned data instead of a
// Docker daemon. It exists to record the README's demo (see demo.tape and
// `make demo`) without touching anyone's real images, containers or volumes.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	client "github.com/fsouza/go-dockerclient"
	"github.com/moby/moby/api/types/volume"
	mobyclient "github.com/moby/moby/client"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"
	"github.com/minhajul/docker-cleaner/models"
)

const (
	kb = int64(1) << 10
	mb = int64(1) << 20
	gb = int64(1) << 30
)

func main() {
	protector, err := docker.NewProtector([]string{"pgdata"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m := models.NewWithClient(sample(time.Now()), protector)
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// id returns a 64-hex-character ID made of c repeated, so short IDs read
// distinctly in the recording.
func id(c string) string {
	return strings.Repeat(c, 64)
}

// sample returns a fake daemon holding a typical developer machine's
// leftovers: a couple of projects' images and containers, dangling images,
// stopped containers, volumes, networks and build cache.
func sample(now time.Time) *dockertest.Fake {
	ago := func(d time.Duration) int64 { return now.Add(-d).Unix() }
	day := 24 * time.Hour

	return &dockertest.Fake{
		Images: []client.APIImages{
			{ID: "sha256:" + id("a"), RepoTags: []string{"postgres:16"}, Size: 432 * mb, Created: ago(40 * day)},
			{ID: "sha256:" + id("b"), RepoTags: []string{"myapp:1.4.2"}, Size: 286 * mb, Created: ago(2 * day)},
			{ID: "sha256:" + id("c"), RepoTags: []string{"myapp:1.4.1"}, Size: 284 * mb, Created: ago(9 * day)},
			{ID: "sha256:" + id("d"), RepoTags: []string{"myapp:1.3.0"}, Size: 279 * mb, Created: ago(52 * day)},
			{ID: "sha256:" + id("e"), RepoTags: []string{"<none>:<none>"}, Size: 281 * mb, Created: ago(16 * day)},
			{ID: "sha256:" + id("f"), RepoTags: []string{"<none>:<none>"}, Size: 1210 * mb, Created: ago(95 * day)},
			{ID: "sha256:" + id("1"), RepoTags: []string{"node:20-alpine"}, Size: 135 * mb, Created: ago(30 * day)},
			{ID: "sha256:" + id("2"), RepoTags: []string{"python:3.12"}, Size: 1020 * mb, Created: ago(120 * day)},
			{ID: "sha256:" + id("3"), RepoTags: []string{"redis:7"}, Size: 117 * mb, Created: ago(60 * day)},
		},
		Containers: []client.APIContainers{
			{ID: id("4"), Names: []string{"/db"}, Image: "postgres:16", State: "running", SizeRw: 64 * kb, SizeRootFs: 432 * mb, Created: ago(40 * day),
				Mounts: []client.APIMount{{Type: "volume", Name: "pgdata"}}},
			{ID: id("5"), Names: []string{"/api"}, Image: "myapp:1.4.2", State: "running", SizeRw: 2 * mb, SizeRootFs: 288 * mb, Created: ago(2 * day)},
			{ID: id("6"), Names: []string{"/worker-old"}, Image: "myapp:1.4.1", State: "exited", SizeRw: 18 * mb, SizeRootFs: 302 * mb, Created: ago(9 * day)},
			{ID: id("7"), Names: []string{"/test-run"}, Image: "node:20-alpine", State: "exited", SizeRw: 96 * mb, SizeRootFs: 231 * mb, Created: ago(21 * day)},
			{ID: id("8"), Names: []string{"/migrate"}, Image: "myapp:1.3.0", State: "exited", SizeRw: 4 * mb, SizeRootFs: 283 * mb, Created: ago(52 * day)},
		},
		Volumes: []client.Volume{
			{Name: "pgdata", Driver: "local", CreatedAt: now.Add(-40 * day)},
			{Name: "redis-data", Driver: "local", CreatedAt: now.Add(-60 * day)},
			{Name: id("9"), Driver: "local", CreatedAt: now.Add(-21 * day)},
		},
		Networks: []client.Network{
			{ID: id("0"), Name: "bridge", Driver: "bridge"},
			{ID: id("ab"), Name: "myapp_default", Driver: "bridge",
				Containers: map[string]client.Endpoint{id("4"): {}, id("5"): {}}},
			{ID: id("cd"), Name: "oldproject_default", Driver: "bridge"},
		},
		Usage: mobyclient.DiskUsageResult{
			Images:     mobyclient.ImagesDiskUsage{TotalCount: 9, ActiveCount: 2, TotalSize: 3730 * mb, Reclaimable: 3110 * mb},
			Containers: mobyclient.ContainersDiskUsage{TotalCount: 5, ActiveCount: 2, TotalSize: 120 * mb, Reclaimable: 118 * mb},
			Volumes: mobyclient.VolumesDiskUsage{TotalCount: 3, ActiveCount: 1, TotalSize: 1600 * mb, Reclaimable: 1100 * mb,
				Items: []volume.Volume{
					{Name: "pgdata", UsageData: &volume.UsageData{Size: 500 * mb, RefCount: 1}},
					{Name: "redis-data", UsageData: &volume.UsageData{Size: 1024 * mb, RefCount: 0}},
					{Name: id("9"), UsageData: &volume.UsageData{Size: 76 * mb, RefCount: 0}},
				}},
			BuildCache: mobyclient.BuildCacheDiskUsage{TotalCount: 214, TotalSize: 4 * gb, Reclaimable: 4 * gb},
		},
	}
}
