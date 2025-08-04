package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/bubbletea"
	docker "github.com/fsouza/go-dockerclient"
)

type model struct {
	choices      []string         // items on the to-do list
	cursor       int              // which to-do list item our cursor is pointing at
	selected     map[int]struct{} // which to-do list items are selected
	dockerClient *docker.Client
	images       []docker.APIImages
	containers   []docker.APIContainers
	errorMsg     string
	cleaning     bool
}

func initialModel() model {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		log.Fatalf("Error creating Docker client: %v", err)
	}

	return model{
		selected:     make(map[int]struct{}),
		dockerClient: client,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchDockerImages, fetchDockerContainers)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		case "enter", " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}
		case "d": // Delete selected items
			if len(m.selected) > 0 {
				m.cleaning = true
				return m, func() tea.Msg {
					return cleanupMsg{m.getSelectionForCleanup()}
				}
			}
		}

	case imagesFetchedMsg:
		m.images = msg.images
		m.updateChoices()
		return m, nil

	case containersFetchedMsg:
		m.containers = msg.containers
		m.updateChoices()
		return m, nil

	case cleanupMsg:
		return m, performCleanup(m.dockerClient, msg.itemsToClean)

	case cleanupDoneMsg:
		m.cleaning = false
		m.selected = make(map[int]struct{})
		return m, tea.Batch(fetchDockerImages, fetchDockerContainers)

	case errMsg:
		m.errorMsg = msg.Error()
		m.cleaning = false
		return m, nil
	}

	return m, nil
}

func (m *model) getSelectionForCleanup() []string {
	var items []string
	for i := range m.selected {
		item := m.choices[i]
		if strings.Contains(item, "(ID:") {
			parts := strings.Split(item, "(ID: ")
			id := strings.TrimSuffix(parts[1], ")")
			items = append(items, id)
		}
	}
	return items
}

func (m *model) updateChoices() {
	m.choices = []string{}
	m.choices = append(m.choices, "--- Docker Images ---")
	for _, img := range m.images {
		name := "<none>"
		if len(img.RepoTags) > 0 {
			name = img.RepoTags[0]
		}
		m.choices = append(m.choices, fmt.Sprintf("  %s (ID: %s)", name, img.ID[:12]))
	}

	m.choices = append(m.choices, "\n--- Docker Containers ---")
	for _, container := range m.containers {
		name := container.Names[0]
		m.choices = append(m.choices, fmt.Sprintf("  %s (ID: %s, Image: %s)", name, container.ID[:12], container.Image))
	}
}

func (m model) View() string {
	s := "Docker Cleaner\n\n"

	for i, choice := range m.choices {
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
	}

	s += "\nPress q to quit. Press space to select/deselect. Press d to delete selected.\n"
	if m.cleaning {
		s += "Cleaning up...\n"
	}
	if m.errorMsg != "" {
		s += fmt.Sprintf("Error: %s\n", m.errorMsg)
	}

	return s
}

type imagesFetchedMsg struct{ images []docker.APIImages }
type containersFetchedMsg struct{ containers []docker.APIContainers }
type cleanupMsg struct{ itemsToClean []string }
type cleanupDoneMsg struct{}
type errMsg struct{ error }

func fetchDockerImages() tea.Msg {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return errMsg{err}
	}

	images, err := client.ListImages(docker.ListImagesOptions{All: false})
	if err != nil {
		return errMsg{err}
	}
	return imagesFetchedMsg{images}
}

func fetchDockerContainers() tea.Msg {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return errMsg{err}
	}

	containers, err := client.ListContainers(docker.ListContainersOptions{All: true})
	if err != nil {
		return errMsg{err}
	}
	return containersFetchedMsg{containers}
}

func performCleanup(client *docker.Client, items []string) tea.Cmd {
	return func() tea.Msg {
		for _, itemID := range items {
			if strings.HasPrefix(itemID, "sha256:") {
				// It's an image ID
				err := client.RemoveImage(itemID)
				if err != nil {
					return errMsg{fmt.Errorf("failed to remove image %s: %w", itemID, err)}
				}
			} else {
				// It's a container ID
				err := client.RemoveContainer(docker.RemoveContainerOptions{ID: itemID, Force: true})
				if err != nil {
					return errMsg{fmt.Errorf("failed to remove container %s: %w", itemID, err)}
				}
			}
		}
		return cleanupDoneMsg{}
	}
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Alas, there's been an error: %v", err)
	}
}
