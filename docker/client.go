package docker

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	client "github.com/fsouza/go-dockerclient"
)

type CleanupItemType int

const (
	CleanupItemTypeImage CleanupItemType = iota + 1 // Start from 1 to match ItemType from models
	CleanupItemTypeContainer
)

type CleanupItem struct {
	ID   string
	Type CleanupItemType
}

type ImagesFetchedMsg struct{ Images []client.APIImages }
type ContainersFetchedMsg struct{ Containers []client.APIContainers }
type CleanupMsg struct{ ItemsToClean []CleanupItem }
type CleanupDoneMsg struct{ Message string }
type ErrMsg struct{ Err error }

func (e ErrMsg) Error() string {
	return e.Err.Error()
}

// HumanSize formats a byte count as a human-readable string (e.g. "1.2 GB").
func HumanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FetchDockerImages returns a tea.Cmd that fetches all Docker images using the
// provided client. Sharing the client avoids the overhead of creating a new
// connection on every refresh.
func FetchDockerImages(cli *client.Client) tea.Cmd {
	return func() tea.Msg {
		images, err := cli.ListImages(client.ListImagesOptions{All: false})
		if err != nil {
			return ErrMsg{err}
		}
		return ImagesFetchedMsg{images}
	}
}

// FetchDockerContainers returns a tea.Cmd that fetches all Docker containers
// using the provided client.
func FetchDockerContainers(cli *client.Client) tea.Cmd {
	return func() tea.Msg {
		containers, err := cli.ListContainers(client.ListContainersOptions{All: true})
		if err != nil {
			return ErrMsg{err}
		}
		return ContainersFetchedMsg{containers}
	}
}

// PerformCleanup removes the selected Docker images and containers.
func PerformCleanup(cli *client.Client, items []CleanupItem) tea.Cmd {
	return func() tea.Msg {
		var errors []string
		var imageCount, containerCount int

		for _, item := range items {
			switch item.Type {
			case CleanupItemTypeImage:
				err := cli.RemoveImage(item.ID)
				if err != nil {
					errors = append(errors, fmt.Sprintf("failed to remove image %s: %v", ShortID(item.ID), err))
				} else {
					imageCount++
				}
			case CleanupItemTypeContainer:
				err := cli.RemoveContainer(client.RemoveContainerOptions{
					ID:    item.ID,
					Force: true,
				})
				if err != nil {
					errors = append(errors, fmt.Sprintf("failed to remove container %s: %v", ShortID(item.ID), err))
				} else {
					containerCount++
				}
			}
		}

		if len(errors) > 0 {
			return ErrMsg{fmt.Errorf("cleanup errors: %v", errors)}
		}

		return CleanupDoneMsg{Message: formatCleanupMessage(imageCount, containerCount)}
	}
}

// formatCleanupMessage builds the human-readable success message shown after
// a successful cleanup.
func formatCleanupMessage(imageCount, containerCount int) string {
	if imageCount == 0 && containerCount == 0 {
		return ""
	}

	var parts []string
	if imageCount > 0 {
		if imageCount == 1 {
			parts = append(parts, "1 image")
		} else {
			parts = append(parts, fmt.Sprintf("%d images", imageCount))
		}
	}
	if containerCount > 0 {
		if containerCount == 1 {
			parts = append(parts, "1 container")
		} else {
			parts = append(parts, fmt.Sprintf("%d containers", containerCount))
		}
	}

	if len(parts) == 1 {
		return fmt.Sprintf("Successfully deleted %s", parts[0])
	}
	return fmt.Sprintf("Successfully deleted %s and %s", parts[0], parts[1])
}

// shortIDLength is the standard Docker short ID length (the first N hex chars
// of a SHA256). Used by ShortID.
const shortIDLength = 12

// ShortID returns the canonical short form of a Docker ID (first
// shortIDLength hex characters). Panics if the ID is shorter than
// shortIDLength, since a corrupt short ID indicates bad data from the
// Docker daemon and should surface loudly rather than be silently passed
// through.
func ShortID(id string) string {
	if len(id) < shortIDLength {
		panic(fmt.Sprintf("docker: ShortID called with %d-char id %q (need >= %d)", len(id), id, shortIDLength))
	}
	return id[:shortIDLength]
}

// System container and image prefixes. Anything matching is hidden from the
// TUI by default because deleting it would break the host system (minikube,
// k3s, Docker Desktop's bundled k8s, etc.).
var (
	systemContainerPrefixes = []string{"k8s_"}
	systemImageRepos        = []string{
		"gcr.io/k8s-minikube/",
		"rancher/",
		"k8s.gcr.io/",
		"registry.k8s.io/",
		"docker.io/rancher/",
		"kicbase",
		"coredns",
		"pause",
		"local-path-provisioner",
	}
)

// IsSystemContainer reports whether a container belongs to a system
// orchestration layer (k8s, minikube, k3s, kind) and should not be deleted.
func IsSystemContainer(c client.APIContainers) bool {
	for _, name := range c.Names {
		for _, prefix := range systemContainerPrefixes {
			if strings.HasPrefix(strings.TrimPrefix(name, "/"), prefix) {
				return true
			}
		}
	}
	// k8s sets io.kubernetes.container.name on every pod and its sub-containers.
	if _, ok := c.Labels["io.kubernetes.container.name"]; ok {
		return true
	}
	return false
}

// IsSystemImage reports whether an image is part of the host system and
// should not be deleted.
func IsSystemImage(img client.APIImages) bool {
	for _, tag := range img.RepoTags {
		repo := strings.SplitN(tag, ":", 2)[0]
		for _, sys := range systemImageRepos {
			if strings.Contains(repo, sys) {
				return true
			}
		}
	}
	return false
}

// FilterUserImages returns images that are not system images.
func FilterUserImages(images []client.APIImages) []client.APIImages {
	out := make([]client.APIImages, 0, len(images))
	for _, img := range images {
		if !IsSystemImage(img) {
			out = append(out, img)
		}
	}
	return out
}

// FilterUserContainers returns containers that are not system containers.
func FilterUserContainers(containers []client.APIContainers) []client.APIContainers {
	out := make([]client.APIContainers, 0, len(containers))
	for _, c := range containers {
		if !IsSystemContainer(c) {
			out = append(out, c)
		}
	}
	return out
}
