package docker

import (
	"fmt"

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
func PerformCleanup(cli *client.Client, items []CleanupItem) tea.Cmd {
	return func() tea.Msg {
		var errors []string
		var successCount int
		var imageCount, containerCount int

		for _, item := range items {
			switch item.Type {
			case CleanupItemTypeImage:
				err := cli.RemoveImage(item.ID)
				if err != nil {
					errors = append(errors, fmt.Sprintf("failed to remove image %s: %v", item.ID[:12], err))
				} else {
					successCount++
					imageCount++
				}
			case CleanupItemTypeContainer:
				err := cli.RemoveContainer(client.RemoveContainerOptions{
					ID:    item.ID,
					Force: true,
				})
				if err != nil {
					errors = append(errors, fmt.Sprintf("failed to remove container %s: %v", item.ID[:12], err))
				} else {
					successCount++
					containerCount++
				}
			}
		}

		if len(errors) > 0 {
			return ErrMsg{fmt.Errorf("cleanup errors: %v", errors)}
		}

		// Create success message
		var message string
		if successCount > 0 {
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
				message = fmt.Sprintf("Successfully deleted %s", parts[0])
			} else {
				message = fmt.Sprintf("Successfully deleted %s and %s", parts[0], parts[1])
			}
		}

		return CleanupDoneMsg{Message: message}
	}
}
