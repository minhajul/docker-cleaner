package docker

import (
	"strings"

	client "github.com/fsouza/go-dockerclient"
)

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
