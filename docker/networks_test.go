package docker_test

import (
	"testing"

	"github.com/minhajul/docker-cleaner/docker"

	client "github.com/fsouza/go-dockerclient"
)

func TestIsSystemNetwork(t *testing.T) {
	cases := []struct {
		name    string
		network client.Network
		want    bool
	}{
		{"predefined bridge", client.Network{Name: "bridge", Driver: "bridge"}, true},
		{"predefined host", client.Network{Name: "host", Driver: "host"}, true},
		{"predefined none", client.Network{Name: "none", Driver: "null"}, true},
		{"swarm ingress", client.Network{Name: "ingress", Driver: "overlay"}, true},
		{"swarm gateway bridge", client.Network{Name: "docker_gwbridge", Driver: "bridge"}, true},
		{"kind", client.Network{Name: "kind", Driver: "bridge"}, true},
		{"minikube", client.Network{Name: "minikube", Driver: "bridge"}, true},
		{
			name: "minikube profile",
			network: client.Network{Name: "dev", Labels: map[string]string{
				"created_by.minikube.sigs.k8s.io": "true",
				"name.minikube.sigs.k8s.io":       "dev",
			}},
			want: true,
		},
		{"compose project", client.Network{Name: "app_default", Labels: map[string]string{"com.docker.compose.network": "default"}}, false},
		{"user network", client.Network{Name: "my-net", Driver: "bridge"}, false},
		// Only exact names are predefined.
		{"name containing bridge", client.Network{Name: "bridge-2"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := docker.IsSystemNetwork(c.network); got != c.want {
				t.Errorf("IsSystemNetwork(%q) = %v, want %v", c.network.Name, got, c.want)
			}
		})
	}
}

func TestFilterUserNetworks(t *testing.T) {
	networks := []client.Network{
		{Name: "bridge"}, {Name: "app_default"}, {Name: "host"}, {Name: "my-net"}, {Name: "none"},
	}
	got := docker.FilterUserNetworks(networks)
	if len(got) != 2 || got[0].Name != "app_default" || got[1].Name != "my-net" {
		t.Errorf("FilterUserNetworks() = %v, want app_default and my-net", got)
	}
}
