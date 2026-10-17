package models

import (
	"fmt"
	"strings"

	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
)

// Section headers of the resources listed after images and containers. Each
// section is left out while it has nothing to show.
const (
	volumesHeader  = "💾 Volumes"
	networksHeader = "🔗 Networks"
)

// volumeRows returns the Volumes section: its header and one row per user
// volume, or nothing if there are none. Sizes and use counts come from the
// daemon's disk usage; until it arrives, or if it failed, a volume's size is
// unknown and its use is counted from the listed containers' mounts.
func (m *Model) volumeRows() []SelectableItem {
	volumes := docker.FilterUserVolumes(m.volumes)
	if len(volumes) == 0 {
		return nil
	}

	header := volumesHeader
	if m.diskUsageErr != nil {
		header += " (sizes unavailable)"
	}
	rows := []SelectableItem{{Type: ItemTypeHeader, Display: header}}

	for _, v := range volumes {
		usage, known := m.diskUsage.Volumes[v.Name]
		size := "unknown"
		var bytes int64
		if known && usage.Size >= 0 {
			bytes = usage.Size
			size = docker.HumanSize(bytes)
		}
		refs := int64(m.volumeMounts(v.Name))
		if known && usage.RefCount >= 0 {
			refs = usage.RefCount
		}
		use := "Unused"
		if refs > 0 {
			use = "Used by: " + plural(int(refs), "container")
		}

		name := volumeName(v.Name)
		rows = append(rows, SelectableItem{
			ID:      v.Name,
			Type:    ItemTypeVolume,
			Display: fmt.Sprintf("%s (Driver: %s, Size: %s, %s)", name, v.Driver, size, use),
			Size:    bytes,
			Name:    name,
		})
	}
	return rows
}

// volumeMounts counts the listed containers, running or not, that mount the
// volume called name.
func (m *Model) volumeMounts(name string) int {
	n := 0
	for _, c := range m.containers {
		for _, mount := range c.Mounts {
			if mount.Type == "volume" && mount.Name == name {
				n++
				break
			}
		}
	}
	return n
}

// networkRows returns the Networks section: its header and one row per
// user-created network, or nothing if there are none. Networks free no
// measurable space, so their Size is 0.
func (m *Model) networkRows() []SelectableItem {
	networks := docker.FilterUserNetworks(m.networks)
	if len(networks) == 0 {
		return nil
	}

	rows := []SelectableItem{{Type: ItemTypeHeader, Display: networksHeader}}
	for _, n := range networks {
		rows = append(rows, SelectableItem{
			ID:   n.ID,
			Type: ItemTypeNetwork,
			Display: fmt.Sprintf("%s (ID: %s, Driver: %s, Containers: %d)",
				n.Name, docker.ShortID(n.ID), n.Driver, m.networkContainers(n)),
			Name: n.Name,
		})
	}
	return rows
}

// networkContainers counts the containers attached to n. The daemon's
// network list leaves them out, so they are taken from the listed
// containers' network settings, along with any the network does report.
func (m *Model) networkContainers(n dockerClient.Network) int {
	attached := make(map[string]struct{}, len(n.Containers))
	for id := range n.Containers {
		attached[id] = struct{}{}
	}
	for _, c := range m.containers {
		for name, settings := range c.Networks.Networks {
			if settings.NetworkID == n.ID || (settings.NetworkID == "" && name == n.Name) {
				attached[c.ID] = struct{}{}
			}
		}
	}
	return len(attached)
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
// longer listed), and the name alone for volumes, whose name is their ID.
func failureLabel(item docker.CleanupItem, name string) string {
	if item.Type == docker.CleanupItemTypeVolume {
		return volumeName(item.ID)
	}
	label := docker.ShortID(item.ID)
	if name != "" {
		label += " " + name
	}
	return label
}
