package models

import (
	"fmt"
	"slices"

	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/styles"
)

// listedImages returns the images to list, recording every protected one in
// m.protected. Protected images are left out unless m.showProtected is set.
// Besides the protector's own rules, an image is protected while a protected
// container runs on it.
func (m *Model) listedImages() []dockerClient.APIImages {
	var protectedRefs []string
	for _, c := range m.containers {
		if m.protector.ContainerReason(c) != docker.NotProtected {
			protectedRefs = append(protectedRefs, c.Image)
		}
	}

	out := make([]dockerClient.APIImages, 0, len(m.images))
	for _, img := range m.images {
		r := m.protector.ImageReason(img)
		if r == docker.NotProtected && slices.ContainsFunc(protectedRefs, func(ref string) bool {
			return docker.ImageMatchesRef(img, ref)
		}) {
			r = docker.ReasonUsedByProtected
		}
		if r != docker.NotProtected {
			m.protected[img.ID] = r
			if !m.showProtected {
				continue
			}
		}
		out = append(out, img)
	}
	return out
}

// listedContainers is listedImages for containers.
func (m *Model) listedContainers() []dockerClient.APIContainers {
	out := make([]dockerClient.APIContainers, 0, len(m.containers))
	for _, c := range m.containers {
		if r := m.protector.ContainerReason(c); r != docker.NotProtected {
			m.protected[c.ID] = r
			if !m.showProtected {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// listedVolumes is listedImages for volumes.
func (m *Model) listedVolumes() []dockerClient.Volume {
	out := make([]dockerClient.Volume, 0, len(m.volumes))
	for _, v := range m.volumes {
		if r := m.protector.VolumeReason(v); r != docker.NotProtected {
			m.protected[v.Name] = r
			if !m.showProtected {
				continue
			}
		}
		out = append(out, v)
	}
	return out
}

// listedNetworks is listedImages for networks. The daemon's predefined
// networks are never listed or counted as protected.
func (m *Model) listedNetworks() []dockerClient.Network {
	out := make([]dockerClient.Network, 0, len(m.networks))
	for _, n := range m.networks {
		if docker.IsPredefinedNetwork(n) {
			continue
		}
		if r := m.protector.NetworkReason(n); r != docker.NotProtected {
			m.protected[n.ID] = r
			if !m.showProtected {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// isProtected reports whether item is a protected resource, which can be
// listed (read-only) but never selected or deleted.
func (m *Model) isProtected(item SelectableItem) bool {
	return item.Type != ItemTypeHeader && m.protected[item.ID] != docker.NotProtected
}

// toggleShowProtected reveals or hides the protected rows. The cursor stays
// on the row it was on; if that row is hidden, it stays at the same index.
func (m *Model) toggleShowProtected() {
	key, ok := m.rowKey(m.cursor)
	m.showProtected = !m.showProtected
	m.updateItems()
	if !ok {
		return
	}
	for i := range m.items {
		if k, _ := m.rowKey(i); k == key {
			m.cursor = i
			return
		}
	}
}

// rowKey identifies row i across rebuilds of m.items: by ID for an item and
// by its text for a section header. ok is false if i is out of range.
func (m *Model) rowKey(i int) (key string, ok bool) {
	if i < 0 || i >= len(m.items) {
		return "", false
	}
	if item := m.items[i]; item.Type != ItemTypeHeader {
		return "id:" + item.ID, true
	}
	return "header:" + m.items[i].Display, true
}

// protectedHint tells the user how many resources are protected and how to
// reveal or hide them, e.g. "🔒 3 protected items hidden · press p to show".
// It is empty when nothing is protected.
func (m Model) protectedHint() string {
	n := len(m.protected)
	if n == 0 {
		return ""
	}
	items := "items"
	if n == 1 {
		items = "item"
	}
	if m.showProtected {
		return fmt.Sprintf("🔒 %d protected %s shown read-only · press p to hide", n, items)
	}
	return fmt.Sprintf("🔒 %d protected %s hidden · press p to show", n, items)
}

// protectedRowView renders the checkbox and text of protected row item: a
// lock in place of the checkbox, then the text dimmed and why it is
// protected, e.g. "🔒 postgres:16 (ID: …) · protect-list".
func (m Model) protectedRowView(item SelectableItem) string {
	return "🔒" + styles.Protected.Render(fmt.Sprintf("%s · %s", item.Display, m.protected[item.ID]))
}
