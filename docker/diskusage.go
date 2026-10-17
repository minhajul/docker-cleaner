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

// BuildCacheUsage summarises the builder's cache.
type BuildCacheUsage struct {
	// Entries is the number of cache records, in use or not.
	Entries int64
	// Reclaimable is the space the unused records take, in bytes, as
	// `docker system df` reports it.
	Reclaimable int64
	// NeedsAll reports whether freeing all of Reclaimable takes a prune
	// with All set. A plain prune (`docker builder prune`) keeps unused
	// internal and frontend records (e.g. the Dockerfile frontend image),
	// which the daemon still counts as reclaimable.
	NeedsAll bool
}

// TypeUsage is one row of `docker system df`: the totals for one type of
// resource.
type TypeUsage struct {
	// Total is the number of resources of the type.
	Total int64
	// Active is the number of them in use (see DiskUsageTotals).
	Active int64
	// Size is the disk space they take, in bytes.
	Size int64
	// Reclaimable is the part of Size that removing the unused ones would
	// free, in bytes.
	Reclaimable int64
}

// DiskUsageTotals holds the per-type totals `docker system df` prints. They
// are the daemon's own figures, taken as reported, as the docker CLI (v29)
// does. Daemons older than API 1.52 report only the individual resources;
// for them the moby client derives the totals with these definitions:
//
//   - Images: Size is the size of all image layers on disk, shared layers
//     counted once. Active images are used by at least one container.
//     Reclaimable is the sum of Size − SharedSize over unused images: their
//     layers no other image shares.
//   - Containers: Size is the sum of the containers' writable layers
//     (SizeRw). Active containers are running, paused or restarting.
//     Reclaimable is Size minus the active containers' writable layers.
//   - Volumes: Size is the sum of the sizes the volume drivers report.
//     Active volumes are mounted by at least one container, running or not.
//     Reclaimable is Size minus the active volumes' sizes.
//   - Build cache: Size is the sum of the records' sizes, shared records
//     excluded. Active records are in use. Reclaimable is Size minus the
//     unshared records in use.
type DiskUsageTotals struct {
	Images     TypeUsage
	Containers TypeUsage
	Volumes    TypeUsage
	BuildCache TypeUsage
}

// DiskUsage is the part of the daemon's disk usage report (`docker system
// df -v`) that docker-cleaner shows.
type DiskUsage struct {
	// Totals sums up each type of resource, as `docker system df` does.
	Totals DiskUsageTotals
	// Volumes maps a volume's name to its usage. A volume missing from it
	// has no usage data.
	Volumes map[string]VolumeUsage
	// BuildCache is zero if the daemon reports no build cache.
	BuildCache BuildCacheUsage
}

// DiskUsageFetchedMsg carries the daemon's disk usage, or the error fetching
// it. Unlike the other fetches, a failure is not an ErrMsg: the lists still
// work without the sizes it adds.
type DiskUsageFetchedMsg struct {
	Usage DiskUsage
	Err   error
}

// FetchDiskUsage returns a tea.Cmd that fetches the daemon's disk usage (see
// GetDiskUsage). It is the slowest fetch, since the daemon measures every
// volume, so it runs on its own and the lists it adds to show without it
// first.
func FetchDiskUsage(cli API) tea.Cmd {
	return func() tea.Msg {
		u, err := GetDiskUsage(context.Background(), cli)
		if err != nil {
			return DiskUsageFetchedMsg{Err: err}
		}
		return DiskUsageFetchedMsg{Usage: u}
	}
}

// GetDiskUsage asks the daemon for its disk usage, as `docker system df -v`
// reports it.
func GetDiskUsage(ctx context.Context, cli API) (DiskUsage, error) {
	// Every type, for the totals. Verbose: from API 1.52 the daemon lists
	// the individual volumes and build cache records only when asked to.
	res, err := cli.DiskUsage(ctx, mobyclient.DiskUsageOptions{
		Images:     true,
		Containers: true,
		Volumes:    true,
		BuildCache: true,
		Verbose:    true,
	})
	if err != nil {
		return DiskUsage{}, err
	}
	return newDiskUsage(res), nil
}

// newDiskUsage extracts what docker-cleaner shows from the daemon's report.
func newDiskUsage(res mobyclient.DiskUsageResult) DiskUsage {
	u := DiskUsage{
		Totals: DiskUsageTotals{
			Images:     TypeUsage{res.Images.TotalCount, res.Images.ActiveCount, res.Images.TotalSize, res.Images.Reclaimable},
			Containers: TypeUsage{res.Containers.TotalCount, res.Containers.ActiveCount, res.Containers.TotalSize, res.Containers.Reclaimable},
			Volumes:    TypeUsage{res.Volumes.TotalCount, res.Volumes.ActiveCount, res.Volumes.TotalSize, res.Volumes.Reclaimable},
			BuildCache: TypeUsage{res.BuildCache.TotalCount, res.BuildCache.ActiveCount, res.BuildCache.TotalSize, res.BuildCache.Reclaimable},
		},
		Volumes: make(map[string]VolumeUsage, len(res.Volumes.Items)),
	}
	for _, v := range res.Volumes.Items {
		if v.UsageData != nil {
			u.Volumes[v.Name] = VolumeUsage{Size: v.UsageData.Size, RefCount: v.UsageData.RefCount}
		}
	}

	bc := res.BuildCache
	u.BuildCache = BuildCacheUsage{Entries: bc.TotalCount, Reclaimable: bc.Reclaimable}
	for _, r := range bc.Items {
		// The records BuildKit's prune skips unless All is set.
		keptByPlainPrune := r.Type == buildCacheTypeInternal || r.Type == buildCacheTypeFrontend
		if keptByPlainPrune && !r.InUse && !r.Shared && r.Size > 0 {
			u.BuildCache.NeedsAll = true
			break
		}
	}
	return u
}

// Build cache record types (build.CacheRecord.Type) that a prune without
// All keeps.
const (
	buildCacheTypeInternal = "internal"
	buildCacheTypeFrontend = "frontend"
)
