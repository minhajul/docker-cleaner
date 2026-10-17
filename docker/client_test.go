package docker

import (
	"testing"

	client "github.com/fsouza/go-dockerclient"
)

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.in); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsSystemContainer(t *testing.T) {
	cases := []struct {
		name string
		c    client.APIContainers
		want bool
	}{
		{"plain user container", client.APIContainers{Names: []string{"/web"}}, false},
		{"k8s pod container", client.APIContainers{Names: []string{"k8s_POD_web_..."}}, true},
		{"k8s by label", client.APIContainers{Labels: map[string]string{"io.kubernetes.container.name": "POD"}}, true},
		{"empty names, no labels", client.APIContainers{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSystemContainer(c.c); got != c.want {
				t.Errorf("IsSystemContainer(%v) = %v, want %v", c.c, got, c.want)
			}
		})
	}
}

func TestIsSystemImage(t *testing.T) {
	cases := []struct {
		name string
		img  client.APIImages
		want bool
	}{
		{"alpine user image", client.APIImages{RepoTags: []string{"alpine:latest"}}, false},
		{"kicbase", client.APIImages{RepoTags: []string{"gcr.io/k8s-minikube/kicbase:v0.0.50"}}, true},
		{"rancher pause", client.APIImages{RepoTags: []string{"rancher/mirrored-pause:3.6"}}, true},
		{"coredns", client.APIImages{RepoTags: []string{"rancher/mirrored-coredns-coredns:1.10.1"}}, true},
		{"none tagged", client.APIImages{RepoTags: []string{"<none>:<none>"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSystemImage(c.img); got != c.want {
				t.Errorf("IsSystemImage(%v) = %v, want %v", c.img, got, c.want)
			}
		})
	}
}

func TestFilterUserImages(t *testing.T) {
	in := []client.APIImages{
		{RepoTags: []string{"alpine:latest"}},
		{RepoTags: []string{"gcr.io/k8s-minikube/kicbase:v0.0.50"}},
		{RepoTags: []string{"myapp:1.0"}},
		{RepoTags: []string{"rancher/mirrored-pause:3.6"}},
	}
	got := FilterUserImages(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 user images, got %d", len(got))
	}
}

func TestFilterUserContainers(t *testing.T) {
	in := []client.APIContainers{
		{Names: []string{"/web"}},
		{Names: []string{"k8s_POD_x_default"}},
		{Labels: map[string]string{"io.kubernetes.container.name": "POD"}},
		{Names: []string{"/db"}},
	}
	got := FilterUserContainers(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 user containers, got %d", len(got))
	}
}
