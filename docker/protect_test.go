package docker

import (
	"testing"

	client "github.com/fsouza/go-dockerclient"
)

func TestValidatePattern(t *testing.T) {
	cases := []struct {
		pattern string
		ok      bool
	}{
		{"postgres:*", true},
		{"my-db", true},
		{"/my-db", true},
		{"ghcr.io/acme/*", true},
		{"[a-z]?-db", true},
		{`literal\*`, true},
		{"", false},
		{"/", false},
		{"[", false},
		{"db-[0-9", false},
		{`trailing\`, false},
	}
	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			err := ValidatePattern(c.pattern)
			if (err == nil) != c.ok {
				t.Errorf("ValidatePattern(%q) = %v, want ok %v", c.pattern, err, c.ok)
			}
		})
	}
}

func TestNewProtectorRejectsBadPattern(t *testing.T) {
	if _, err := NewProtector([]string{"ok-*", "bad["}); err == nil {
		t.Fatal("NewProtector accepted an unclosed [")
	}
}

func TestProtectorImageReason(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		tags     []string
		want     Reason
	}{
		{"no patterns", nil, []string{"postgres:16"}, NotProtected},
		{"repo:tag glob", []string{"postgres:*"}, []string{"postgres:16"}, ReasonProtectList},
		{"repo only", []string{"postgres"}, []string{"postgres:16"}, ReasonProtectList},
		{"exact tag", []string{"postgres:16"}, []string{"postgres:16"}, ReasonProtectList},
		{"other tag", []string{"postgres:16"}, []string{"postgres:15"}, NotProtected},
		{"prefix glob", []string{"prod-*"}, []string{"prod-api:2.1"}, ReasonProtectList},
		{"whole name only", []string{"postgres"}, []string{"postgres-exporter:1"}, NotProtected},
		{"any tag matches", []string{"keep:*"}, []string{"scratch:1", "keep:1"}, ReasonProtectList},
		{"star crosses slash", []string{"ghcr.io/acme/*"}, []string{"ghcr.io/acme/tools/api:1.0"}, ReasonProtectList},
		{"leading star crosses slash", []string{"*/postgres"}, []string{"docker.io/library/postgres:16"}, ReasonProtectList},
		{"registry port repo", []string{"localhost:5000/app"}, []string{"localhost:5000/app:1.0"}, ReasonProtectList},
		{"registry port not a tag", []string{"localhost"}, []string{"localhost:5000/app:1.0"}, NotProtected},
		{"question mark", []string{"redis:?"}, []string{"redis:7"}, ReasonProtectList},
		{"question mark is one character", []string{"postgres:1?"}, []string{"postgres:16.4"}, NotProtected},
		{"other repo", []string{"ghcr.io/acme/*"}, []string{"ghcr.io/other/api:1.0"}, NotProtected},
		{"character class", []string{"db[0-9]"}, []string{"db1:latest"}, ReasonProtectList},
		{"case-sensitive", []string{"Postgres"}, []string{"postgres:16"}, NotProtected},
		{"untagged never matches", []string{"*"}, []string{"<none>:<none>"}, NotProtected},
		{"system rules still apply", nil, []string{"gcr.io/k8s-minikube/kicbase:v0.0.50"}, ReasonSystem},
		{"system wins over pattern", []string{"rancher/*"}, []string{"rancher/mirrored-pause:3.6"}, ReasonSystem},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewProtector(c.patterns)
			if err != nil {
				t.Fatal(err)
			}
			img := client.APIImages{RepoTags: c.tags}
			if got := p.ImageReason(img); got != c.want {
				t.Errorf("ImageReason(%v) with %q = %q, want %q", c.tags, c.patterns, got, c.want)
			}
			if got := p.ProtectsImage(img); got != (c.want != NotProtected) {
				t.Errorf("ProtectsImage(%v) = %v, want %v", c.tags, got, !got)
			}
		})
	}
}

func TestProtectorContainerReason(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		c        client.APIContainers
		want     Reason
	}{
		{"no patterns", nil, client.APIContainers{Names: []string{"/my-db"}}, NotProtected},
		{"name with slash", []string{"my-db"}, client.APIContainers{Names: []string{"/my-db"}}, ReasonProtectList},
		{"name without slash", []string{"my-db"}, client.APIContainers{Names: []string{"my-db"}}, ReasonProtectList},
		{"pattern with slash", []string{"/my-db"}, client.APIContainers{Names: []string{"/my-db"}}, ReasonProtectList},
		{"glob", []string{"prod-*"}, client.APIContainers{Names: []string{"/prod-web-1"}}, ReasonProtectList},
		{"no match", []string{"prod-*"}, client.APIContainers{Names: []string{"/staging-web"}}, NotProtected},
		{"second name", []string{"alias"}, client.APIContainers{Names: []string{"/web", "/other/alias"}}, NotProtected},
		{"second name glob", []string{"*/alias"}, client.APIContainers{Names: []string{"/web", "/other/alias"}}, ReasonProtectList},
		{"image is not a name", []string{"postgres"}, client.APIContainers{Names: []string{"/db"}, Image: "postgres"}, NotProtected},
		{"system by name", nil, client.APIContainers{Names: []string{"/k8s_POD_web"}}, ReasonSystem},
		{"system by label", []string{"web"}, client.APIContainers{Names: []string{"/web"}, Labels: map[string]string{"io.kubernetes.container.name": "web"}}, ReasonSystem},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewProtector(c.patterns)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.ContainerReason(c.c); got != c.want {
				t.Errorf("ContainerReason(%v) with %q = %q, want %q", c.c.Names, c.patterns, got, c.want)
			}
			if got := p.ProtectsContainer(c.c); got != (c.want != NotProtected) {
				t.Errorf("ProtectsContainer(%v) = %v, want %v", c.c.Names, got, !got)
			}
		})
	}
}

func TestZeroProtectorAppliesSystemRules(t *testing.T) {
	var p Protector
	if !p.ProtectsImage(client.APIImages{RepoTags: []string{"registry.k8s.io/pause:3.9"}}) {
		t.Error("zero Protector let a system image through")
	}
	if p.ProtectsImage(client.APIImages{RepoTags: []string{"alpine:latest"}}) {
		t.Error("zero Protector protected a user image")
	}
}

func TestImageRepo(t *testing.T) {
	cases := map[string]string{
		"postgres:16":            "postgres",
		"postgres":               "postgres",
		"ghcr.io/acme/api:1.0":   "ghcr.io/acme/api",
		"localhost:5000/app:1.0": "localhost:5000/app",
		"localhost:5000/app":     "localhost:5000/app",
	}
	for in, want := range cases {
		if got := imageRepo(in); got != want {
			t.Errorf("imageRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
