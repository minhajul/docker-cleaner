package docker

import (
	"strings"

	"github.com/charmbracelet/bubbletea"
	client "github.com/fsouza/go-dockerclient"
)

// VolumesFetchedMsg carries the volumes FetchVolumes listed.
type VolumesFetchedMsg struct{ Volumes []client.Volume }

// FetchVolumes returns a tea.Cmd that lists all volumes. The list has no
// sizes; those come from FetchDiskUsage.
func FetchVolumes(cli API) tea.Cmd {
	return func() tea.Msg {
		volumes, err := cli.ListVolumes(client.ListVolumesOptions{})
		if err != nil {
			return ErrMsg{err}
		}
		return VolumesFetchedMsg{volumes}
	}
}

// IsSystemVolume reports whether a volume belongs to a system orchestration
// layer and should not be deleted, e.g. the volume holding a minikube
// cluster's node.
func IsSystemVolume(v client.Volume) bool {
	return hasSystemLabel(v.Labels)
}

// FilterUserVolumes returns volumes that are not system volumes.
func FilterUserVolumes(volumes []client.Volume) []client.Volume {
	out := make([]client.Volume, 0, len(volumes))
	for _, v := range volumes {
		if !IsSystemVolume(v) {
			out = append(out, v)
		}
	}
	return out
}

// hasSystemLabel reports whether labels mark a resource as created by
// Kubernetes tooling: minikube labels its volumes and networks
// "*.minikube.sigs.k8s.io", kind uses "io.x-k8s.kind.*", and the kubelet
// "io.kubernetes.*".
func hasSystemLabel(labels map[string]string) bool {
	for key := range labels {
		if strings.Contains(key, "k8s.io") || strings.HasPrefix(key, "io.x-k8s.") || strings.HasPrefix(key, "io.kubernetes.") {
			return true
		}
	}
	return false
}
