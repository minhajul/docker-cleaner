package docker

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbletea"
	client "github.com/fsouza/go-dockerclient"
)

// NetworksFetchedMsg carries the networks FetchNetworks listed.
type NetworksFetchedMsg struct{ Networks []client.Network }

// FetchNetworks returns a tea.Cmd that lists all networks. The daemon leaves
// the attached containers out of the list; the model counts them from the
// containers' own network settings.
func FetchNetworks(cli API) tea.Cmd {
	return func() tea.Msg {
		networks, err := cli.ListNetworks()
		if err != nil {
			return ErrMsg{err}
		}
		return NetworksFetchedMsg{networks}
	}
}

// systemNetworkNames are networks that are not the user's to delete: the
// daemon's predefined ones, the swarm's, and those Kubernetes-in-Docker tools
// create for their clusters.
var systemNetworkNames = []string{
	"bridge", "host", "none", // predefined; the daemon refuses to remove them
	"ingress", "docker_gwbridge", // swarm
	"kind", "minikube",
}

// predefinedNetworkNames are the networks every daemon creates and refuses
// to remove.
var predefinedNetworkNames = []string{"bridge", "host", "none"}

// IsPredefinedNetwork reports whether n is one of the daemon's predefined
// networks. These exist everywhere and can never be removed, so they are
// left out entirely rather than shown as protected.
func IsPredefinedNetwork(n client.Network) bool {
	return slices.Contains(predefinedNetworkNames, n.Name)
}

// IsSystemNetwork reports whether a network is predefined or belongs to a
// system orchestration layer (swarm, kind, minikube) and should not be
// deleted.
func IsSystemNetwork(n client.Network) bool {
	return slices.Contains(systemNetworkNames, n.Name) || hasSystemLabel(n.Labels)
}

// FilterUserNetworks returns networks that are not system networks.
func FilterUserNetworks(networks []client.Network) []client.Network {
	out := make([]client.Network, 0, len(networks))
	for _, n := range networks {
		if !IsSystemNetwork(n) {
			out = append(out, n)
		}
	}
	return out
}

// networkFailureReason explains why the daemon refused to remove a network.
// It answers 403 Forbidden both for a network with containers attached and
// for a predefined one; the API error's "API error (403): " prefix is
// dropped.
func networkFailureReason(apiErr *client.Error) string {
	switch {
	case strings.Contains(apiErr.Message, "active endpoints"):
		return "network has containers attached; remove or disconnect them first"
	case apiErr.Message != "":
		return apiErr.Message
	default:
		return apiErr.Error()
	}
}
