package models

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	dockerClient "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker/dockertest"
)

const (
	netApp   = "aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000"
	netSpare = "bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111"
	netBr    = "cccc2222cccc2222cccc2222cccc2222cccc2222cccc2222cccc2222cccc2222"
)

// withNetworks adds the predefined bridge network and two user networks to
// fake: app_default, which both containers are attached to, and spare.
func withNetworks(fake *dockertest.Fake) *dockertest.Fake {
	fake.Networks = []dockerClient.Network{
		{ID: netBr, Name: "bridge", Driver: "bridge"},
		{ID: netApp, Name: "app_default", Driver: "bridge"},
		{ID: netSpare, Name: "spare", Driver: "overlay"},
	}
	for i := range fake.Containers {
		fake.Containers[i].Networks.Networks = map[string]dockerClient.ContainerNetwork{
			"app_default": {NetworkID: netApp},
		}
	}
	return fake
}

func TestNetworksSection(t *testing.T) {
	m := loaded(t, withNetworks(newFake()))

	want := map[string]string{
		netApp:   "app_default (ID: aaaa0000aaaa, Driver: bridge, Containers: 2)",
		netSpare: "spare (ID: bbbb1111bbbb, Driver: overlay, Containers: 0)",
	}
	for id, display := range want {
		if got := rowDisplay(m, id); got != display {
			t.Errorf("row %s = %q, want %q", id, got, display)
		}
	}
	if listed(m, netBr) {
		t.Errorf("the predefined bridge network should be hidden, rows: %v", m.items)
	}
	if !strings.Contains(m.View(), "🔗 Networks") {
		t.Errorf("view should show the Networks header, got:\n%s", m.View())
	}
}

func TestNetworksSectionHiddenWithOnlyPredefined(t *testing.T) {
	fake := newFake()
	fake.Networks = []dockerClient.Network{
		{ID: netBr, Name: "bridge"},
		{ID: netApp, Name: "host"},
		{ID: netSpare, Name: "none"},
	}
	m := loaded(t, fake)
	if strings.Contains(m.View(), "Networks") {
		t.Errorf("view should not show an empty Networks section, got:\n%s", m.View())
	}
}

func TestDeleteNetworks(t *testing.T) {
	fake := withNetworks(newFake())
	fake.RemoveErrs = map[string]error{
		netApp: &dockerClient.Error{
			Status:  http.StatusForbidden,
			Message: "error while removing network: network app_default id aaaa0000aaaa has active endpoints",
		},
	}
	m := loaded(t, fake, netApp, netSpare, ctrStopped)

	m = send(t, m, runes("d"))
	if want := "Delete 1 container, 2 networks (2.0 MB)? y/N"; !strings.Contains(m.View(), want) {
		t.Errorf("view should contain %q, got:\n%s", want, m.View())
	}
	m = send(t, m, runes("y"))

	// The container goes first, so it no longer holds on to a network.
	if want := []string{"container:" + ctrStopped, "network:" + netSpare}; !slices.Equal(fake.Removed, want) {
		t.Errorf("Removed = %v, want %v", fake.Removed, want)
	}
	view := m.View()
	for _, want := range []string{
		"❌ Deleted 2 · Failed 1",
		"aaaa0000aaaa app_default: network has containers attached; remove or disconnect them first",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view should contain %q, got:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Press f") {
		t.Errorf("a network should not offer a forced retry, got:\n%s", view)
	}
}
