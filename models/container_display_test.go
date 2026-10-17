package models

import (
	"testing"

	dockerClient "github.com/fsouza/go-dockerclient"
)

func TestUpdateItemsContainerDisplay(t *testing.T) {
	cases := []struct {
		name      string
		container dockerClient.APIContainers
		want      string
	}{
		{
			name: "writable layer and virtual size",
			container: dockerClient.APIContainers{
				ID: "container1234abcd", Names: []string{"/web"}, Image: "nginx",
				SizeRw: 12 * 1024 * 1024, SizeRootFs: 1288490188,
			},
			want: "web (ID: container123, Image: nginx, Size: 12.0 MB (virtual 1.2 GB))",
		},
		{
			name: "no sizes reported",
			container: dockerClient.APIContainers{
				ID: "abcdef0123456789", Names: []string{"/db"}, Image: "postgres",
			},
			want: "db (ID: abcdef012345, Image: postgres, Size: 0 B (virtual 0 B))",
		},
		{
			name: "unnamed container",
			container: dockerClient.APIContainers{
				ID: "0123456789abcdef", Image: "alpine", SizeRw: 512, SizeRootFs: 2048,
			},
			want: "<none> (ID: 0123456789ab, Image: alpine, Size: 512 B (virtual 2.0 KB))",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Model{containers: []dockerClient.APIContainers{c.container}}
			m.updateItems()

			var got []string
			for _, it := range m.items {
				if it.Type == ItemTypeContainer {
					got = append(got, it.Display)
				}
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 container item, got %v", got)
			}
			if got[0] != c.want {
				t.Errorf("display = %q, want %q", got[0], c.want)
			}
		})
	}
}
