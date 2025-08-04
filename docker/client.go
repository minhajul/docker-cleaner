package docker

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	client "github.com/fsouza/go-dockerclient"
)

type ImagesFetchedMsg struct{ Images []client.APIImages }
type ContainersFetchedMsg struct{ Containers []client.APIContainers }
type CleanupMsg struct{ ItemsToClean []string }
type CleanupDoneMsg struct{}
type ErrMsg struct{ Err error }

func (e ErrMsg) Error() string {
	return e.Err.Error()
}

// FetchDockerImages fetches all Docker images from the Docker daemon
func FetchDockerImages() tea.Msg {
	dockerClient, err := client.NewClientFromEnv()
	if err != nil {
		return ErrMsg{err}
	}

	images, err := dockerClient.ListImages(client.ListImagesOptions{All: false})
	if err != nil {
		return ErrMsg{err}
	}
	return ImagesFetchedMsg{images}
}

// FetchDockerContainers fetches all Docker containers from the Docker daemon
func FetchDockerContainers() tea.Msg {
	dockerClient, err := client.NewClientFromEnv()
	if err != nil {
		return ErrMsg{err}
	}

	containers, err := dockerClient.ListContainers(client.ListContainersOptions{All: true})
	if err != nil {
		return ErrMsg{err}
	}
	return ContainersFetchedMsg{containers}
}

// PerformCleanup removes the selected Docker images and containers
func PerformCleanup(cli *client.Client, items []string) tea.Cmd {
	return func() tea.Msg {
		for _, itemID := range items {
			if strings.HasPrefix(itemID, "sha256:") {
				// It's an image ID
				err := cli.RemoveImage(itemID)
				if err != nil {
					return ErrMsg{fmt.Errorf("failed to remove image %s: %w", itemID, err)}
				}
			} else {
				// It's a container ID
				err := cli.RemoveContainer(client.RemoveContainerOptions{ID: itemID, Force: true})
				if err != nil {
					return ErrMsg{fmt.Errorf("failed to remove container %s: %w", itemID, err)}
				}
			}
		}
		return CleanupDoneMsg{}
	}
}
