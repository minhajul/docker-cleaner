package docker

import (
	"context"

	"github.com/charmbracelet/bubbletea"
	mobyclient "github.com/moby/moby/client"
)

// VolumeUsage is a volume's disk usage as the daemon reports it.
type VolumeUsage struct {
	// Size is the space the volume uses, in bytes, or -1 if its driver
	// does not report it.
	Size int64
	// RefCount is the number of containers using the volume, or -1 if
	// unknown.
	RefCount int64
}

// DiskUsage is the part of the daemon's disk usage report (`docker system
// df -v`) that docker-cleaner shows.
type DiskUsage struct {
	// Volumes maps a volume's name to its usage. A volume missing from it
	// has no usage data.
	Volumes map[string]VolumeUsage
}

// DiskUsageFetchedMsg carries the daemon's disk usage, or the error fetching
// it. Unlike the other fetches, a failure is not an ErrMsg: the lists still
// work without the sizes it adds.
type DiskUsageFetchedMsg struct {
	Usage DiskUsage
	Err   error
}

// FetchDiskUsage returns a tea.Cmd that fetches the daemon's disk usage. It is
// the slowest fetch, since the daemon measures every volume, so it runs on its
// own and the lists it adds to show without it first.
func FetchDiskUsage(cli API) tea.Cmd {
	return func() tea.Msg {
		// Verbose: from API 1.52 the daemon lists the individual volumes
		// only when asked to.
		res, err := cli.DiskUsage(context.Background(), mobyclient.DiskUsageOptions{
			Volumes: true,
			Verbose: true,
		})
		if err != nil {
			return DiskUsageFetchedMsg{Err: err}
		}
		return DiskUsageFetchedMsg{Usage: newDiskUsage(res)}
	}
}

// newDiskUsage extracts what docker-cleaner shows from the daemon's report.
func newDiskUsage(res mobyclient.DiskUsageResult) DiskUsage {
	u := DiskUsage{Volumes: make(map[string]VolumeUsage, len(res.Volumes.Items))}
	for _, v := range res.Volumes.Items {
		if v.UsageData != nil {
			u.Volumes[v.Name] = VolumeUsage{Size: v.UsageData.Size, RefCount: v.UsageData.RefCount}
		}
	}
	return u
}
