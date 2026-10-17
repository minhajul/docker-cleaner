package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/filter"
)

// Section headers of the resources listed after images and containers. Each
// section is left out while it has nothing to show.
const (
	volumesHeader    = "💾 Volumes"
	networksHeader   = "🔗 Networks"
	buildCacheHeader = "🧱 Build cache"
)

// volumeRows returns the Volumes section: its header and one row per listed
// volume (see listedVolumes) that passes the filters and the search, or
// nothing if there are none. Sizes and use counts come from the daemon's disk
// usage; until it arrives, or if it failed, a volume's size is unknown and
// its use is counted from the containers' mounts (see usage).
func (m *Model) volumeRows() []SelectableItem {
	u := m.usage()
	now := m.clock()
	volumes := m.filters.Volumes(m.listedVolumes(), u, now)

	var rows []SelectableItem
	for _, v := range volumes {
		usage, known := m.diskUsage.Volumes[v.Name]
		size := "unknown"
		var bytes int64
		if known && usage.Size >= 0 {
			bytes = usage.Size
			size = docker.HumanSize(bytes)
		}
		refs := u.VolumeRefs(v.Name)
		use := "Unused"
		if refs > 0 {
			use = "Used by: " + plural(int(refs), "container")
		}

		name := volumeName(v.Name)
		rows = append(rows, SelectableItem{
			ID:      v.Name,
			Type:    ItemTypeVolume,
			Display: fmt.Sprintf("%s (Driver: %s, Size: %s, %s%s)", name, v.Driver, size, use, ageSuffix(v.CreatedAt, now)),
			Size:    bytes,
			Name:    name,
			Created: v.CreatedAt,
		})
	}

	header := volumesHeader
	if m.diskUsageErr != nil {
		header += " (sizes unavailable)"
	}
	return m.section(header, rows)
}

// networkRows returns the Networks section: its header and one row per
// listed network (see listedNetworks) that passes the filters and the
// search, or nothing if there are none. Networks free no measurable space,
// so their Size is 0.
func (m *Model) networkRows() []SelectableItem {
	u := m.usage()
	networks := m.filters.Networks(m.listedNetworks(), u)

	var rows []SelectableItem
	for _, n := range networks {
		rows = append(rows, SelectableItem{
			ID:   n.ID,
			Type: ItemTypeNetwork,
			Display: fmt.Sprintf("%s (ID: %s, Driver: %s, Containers: %d)",
				n.Name, docker.ShortID(n.ID), n.Driver, u.NetworkContainers(n)),
			Name: n.Name,
		})
	}
	return m.section(networksHeader, rows)
}

// usage tells which resources the fetched containers, running or not and
// listed or not, use. Volume use counts come from the daemon's disk usage
// when it reports them, and otherwise from the containers' mounts.
func (m *Model) usage() filter.Usage {
	return filter.NewUsage(m.containers, m.diskUsage.Volumes)
}

// buildCacheRows returns the Build cache section: its header and a single
// row standing for the whole cache, e.g. "Build cache: 312 entries, 4.1 GB
// reclaimable". There is nothing to show while the disk usage has not
// arrived, the cache has nothing reclaimable or the search leaves the row
// out; if the disk usage could not be fetched, the header alone says so.
// The filters leave the build cache alone.
func (m *Model) buildCacheRows() []SelectableItem {
	if m.diskUsageErr != nil {
		return []SelectableItem{{
			Type:    ItemTypeHeader,
			Display: fmt.Sprintf("%s (unavailable: %v)", buildCacheHeader, m.diskUsageErr),
		}}
	}
	bc := m.diskUsage.BuildCache
	if bc.Reclaimable <= 0 {
		return nil
	}
	entries := fmt.Sprintf("%d entries", bc.Entries)
	if bc.Entries == 1 {
		entries = "1 entry"
	}
	return m.section(buildCacheHeader, []SelectableItem{{
		ID:      docker.BuildCacheID,
		Type:    ItemTypeBuildCache,
		Display: fmt.Sprintf("Build cache: %s, %s reclaimable", entries, docker.HumanSize(bc.Reclaimable)),
		Size:    bc.Reclaimable,
		Name:    "build cache",
	}})
}

// section returns a section that is left out while it has nothing to show:
// its header followed by the rows that pass the search, in the sort order
// (see shown), or nothing if none do.
func (m *Model) section(header string, rows []SelectableItem) []SelectableItem {
	rows = m.shown(rows)
	if len(rows) == 0 {
		return nil
	}
	return append([]SelectableItem{{Type: ItemTypeHeader, Display: header}}, rows...)
}

// ageSuffix is the age appended to a row's details, e.g. ", Created: 3 weeks
// ago", or "" if created is unknown.
func ageSuffix(created, now time.Time) string {
	if created.IsZero() {
		return ""
	}
	return ", Created: " + docker.HumanAge(created, now)
}

// volumeName shortens an anonymous volume's name, a random 64-character hex
// string, to a short ID; other names are kept whole.
func volumeName(name string) string {
	if len(name) == 64 && strings.Trim(name, "0123456789abcdef") == "" {
		return docker.ShortID(name)
	}
	return name
}

// failureLabel names a failed item in the cleanup report: "<short id> <name>"
// for images, containers and networks (or just the short ID if the item is no
// longer listed), the name alone for volumes, whose name is their ID, and
// "build cache" for the build cache.
func failureLabel(item docker.CleanupItem, name string) string {
	switch item.Type {
	case docker.CleanupItemTypeVolume:
		return volumeName(item.ID)
	case docker.CleanupItemTypeBuildCache:
		return "build cache"
	}
	label := docker.ShortID(item.ID)
	if name != "" {
		label += " " + name
	}
	return label
}
