package filter

import (
	client "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
)

// Usage tells which images, volumes and networks containers use. Build it
// from every container the daemon lists, running or not, including any the
// caller hides (such as protected ones): a resource a hidden container uses
// is still in use.
type Usage struct {
	containers []client.APIContainers
	volumes    map[string]docker.VolumeUsage
}

// NewUsage returns the Usage of containers. volumes is the daemon's disk
// usage per volume (docker.DiskUsage.Volumes); it may be nil or incomplete,
// in which case a volume's use is counted from the containers' mounts.
func NewUsage(containers []client.APIContainers, volumes map[string]docker.VolumeUsage) Usage {
	return Usage{containers: containers, volumes: volumes}
}

// ImageUsed reports whether a container, running or not, was created from
// img (see docker.ImageMatchesRef).
func (u Usage) ImageUsed(img client.APIImages) bool {
	for _, c := range u.containers {
		if docker.ImageMatchesRef(img, c.Image) {
			return true
		}
	}
	return false
}

// VolumeRefs counts the containers using the volume called name: the
// daemon's reference count when its disk usage reports one, and otherwise
// the containers, running or not, that mount the volume.
func (u Usage) VolumeRefs(name string) int64 {
	if usage, ok := u.volumes[name]; ok && usage.RefCount >= 0 {
		return usage.RefCount
	}
	var n int64
	for _, c := range u.containers {
		for _, mount := range c.Mounts {
			if mount.Type == "volume" && mount.Name == name {
				n++
				break
			}
		}
	}
	return n
}

// NetworkContainers counts the containers attached to n. The daemon's network
// list leaves them out, so they are taken from the containers' own network
// settings, along with any the network does report.
func (u Usage) NetworkContainers(n client.Network) int {
	attached := make(map[string]struct{}, len(n.Containers))
	for id := range n.Containers {
		attached[id] = struct{}{}
	}
	for _, c := range u.containers {
		for name, settings := range c.Networks.Networks {
			if settings.NetworkID == n.ID || (settings.NetworkID == "" && name == n.Name) {
				attached[c.ID] = struct{}{}
			}
		}
	}
	return len(attached)
}
