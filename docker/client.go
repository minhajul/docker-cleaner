package docker

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	// Force removes the item even if Docker would refuse otherwise: a
	// running container is killed first, and an image is removed even if
	// a container uses it or it has several tags.
	Force bool
}

// CleanupFailure is an item PerformCleanup could not remove.
type CleanupFailure struct {
	Item CleanupItem
	// Reason says why, readably (e.g. "container is running; stop it
	// first"). It does not repeat the item's ID.
	Reason string
	// Forceable reports whether retrying with Force may succeed. It is set
	// for images Docker refused with 409 Conflict because a container uses
	// them or they have several tags.
	Forceable bool
}

type ImagesFetchedMsg struct{ Images []client.APIImages }
type ContainersFetchedMsg struct{ Containers []client.APIContainers }
type CleanupMsg struct{ ItemsToClean []CleanupItem }
type ErrMsg struct{ Err error }

// CleanupResultMsg reports the outcome of PerformCleanup for every item it
// was given: each one is either in Deleted or in Failed.
type CleanupResultMsg struct {
	Deleted []CleanupItem
	Failed  []CleanupFailure
}

// API is the subset of the Docker client this app uses. Depending on it
// instead of *client.Client lets tests substitute a fake for a real daemon.
type API interface {
	Ping() error
	ListImages(opts client.ListImagesOptions) ([]client.APIImages, error)
	ListContainers(opts client.ListContainersOptions) ([]client.APIContainers, error)
	RemoveImage(name string) error
	RemoveImageExtended(name string, opts client.RemoveImageOptions) error
	RemoveContainer(opts client.RemoveContainerOptions) error
}

var _ API = (*client.Client)(nil)

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
func FetchDockerImages(cli API) tea.Cmd {
	return func() tea.Msg {
		images, err := cli.ListImages(client.ListImagesOptions{All: false})
		if err != nil {
			return ErrMsg{err}
		}
		return ImagesFetchedMsg{images}
	}
}

// FetchDockerContainers returns a tea.Cmd that fetches all Docker containers
// using the provided client. Sizes are requested explicitly; without them the
// daemon leaves SizeRw and SizeRootFs at zero.
func FetchDockerContainers(cli API) tea.Cmd {
	return func() tea.Msg {
		containers, err := cli.ListContainers(client.ListContainersOptions{All: true, Size: true})
		if err != nil {
			return ErrMsg{err}
		}
		return ContainersFetchedMsg{containers}
	}
}

// PerformCleanup removes the selected Docker images and containers and
// reports, item by item, what was deleted and what failed. All containers are
// removed before any image, since Docker refuses to remove an image that a
// container still uses; the order within each type is kept.
func PerformCleanup(cli API, items []CleanupItem) tea.Cmd {
	items = containersFirst(items)
	return func() tea.Msg {
		var result CleanupResultMsg
		for _, item := range items {
			var err error
			switch item.Type {
			case CleanupItemTypeImage:
				if item.Force {
					err = cli.RemoveImageExtended(item.ID, client.RemoveImageOptions{Force: true})
				} else {
					err = cli.RemoveImage(item.ID)
				}
			case CleanupItemTypeContainer:
				err = cli.RemoveContainer(client.RemoveContainerOptions{
					ID:    item.ID,
					Force: item.Force,
				})
			default:
				err = fmt.Errorf("unknown item type %d", item.Type)
			}

			if err != nil {
				result.Failed = append(result.Failed, removeFailure(item, err))
			} else {
				result.Deleted = append(result.Deleted, item)
			}
		}
		return result
	}
}

// removeFailure describes why item could not be removed. Docker answers 409
// Conflict both for a running container removed without Force and for an
// image that a container uses or that has several tags; those get readable
// reasons instead of the raw API error.
func removeFailure(item CleanupItem, err error) CleanupFailure {
	f := CleanupFailure{Item: item, Reason: err.Error()}

	var apiErr *client.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		return f
	}
	switch item.Type {
	case CleanupItemTypeContainer:
		f.Reason = "container is running; stop it first"
	case CleanupItemTypeImage:
		f.Reason = imageConflictReason(apiErr.Message)
		// The daemon says "cannot be forced" when a running container
		// uses the image, so a forced retry would fail the same way.
		f.Forceable = !item.Force && !strings.Contains(apiErr.Message, "cannot be forced")
	}
	return f
}

// imageConflictReason extracts the explanation from the daemon's 409 message
// for an image: "conflict: unable to delete 0123456789ab (must be forced) -
// image is referenced in multiple repositories" becomes "image is referenced
// in multiple repositories".
func imageConflictReason(msg string) string {
	if _, reason, ok := strings.Cut(msg, " - "); ok && reason != "" {
		return reason
	}
	if msg != "" {
		return msg
	}
	return "image is in use or has several tags"
}

// ForceRetry returns the failed items that a forced retry may remove, with
// Force set.
func (r CleanupResultMsg) ForceRetry() []CleanupItem {
	var items []CleanupItem
	for _, f := range r.Failed {
		if f.Forceable {
			item := f.Item
			item.Force = true
			items = append(items, item)
		}
	}
	return items
}

// Summary is a one-line account of the result, e.g. "Deleted 4 · Failed 1".
// The failure count is left out when nothing failed.
func (r CleanupResultMsg) Summary() string {
	s := fmt.Sprintf("Deleted %d", len(r.Deleted))
	if len(r.Failed) > 0 {
		s += fmt.Sprintf(" · Failed %d", len(r.Failed))
	}
	return s
}

// containersFirst returns a copy of items with every container ahead of every
// image, preserving the relative order within each type.
func containersFirst(items []CleanupItem) []CleanupItem {
	sorted := slices.Clone(items)
	slices.SortStableFunc(sorted, func(a, b CleanupItem) int {
		isImage := func(it CleanupItem) int {
			if it.Type == CleanupItemTypeImage {
				return 1
			}
			return 0
		}
		return isImage(a) - isImage(b)
	})
	return sorted
}

// shortIDLength is the standard Docker short ID length (the first N hex chars
// of a SHA256). Used by ShortID.
const shortIDLength = 12

// ShortID returns the canonical short form of a Docker ID (first
// shortIDLength hex characters), matching the IMAGE ID / CONTAINER ID
// columns of the docker CLI. A leading "sha256:" digest prefix, as found on
// image IDs, is stripped first. Panics if the remaining ID is shorter than
// shortIDLength, since a corrupt short ID indicates bad data from the
// Docker daemon and should surface loudly rather than be silently passed
// through.
func ShortID(id string) string {
	hex := strings.TrimPrefix(id, "sha256:")
	if len(hex) < shortIDLength {
		panic(fmt.Sprintf("docker: ShortID called with %d-char id %q (need >= %d)", len(hex), id, shortIDLength))
	}
	return hex[:shortIDLength]
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
