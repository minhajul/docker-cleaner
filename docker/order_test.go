package docker_test

import (
	"slices"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
)

func TestPerformCleanupRemovesContainersBeforeImages(t *testing.T) {
	const (
		img1 = "sha256:1111111111111111"
		img2 = "sha256:2222222222222222"
		ctr1 = "cccccccccccc1111"
		ctr2 = "cccccccccccc2222"
	)
	cases := []struct {
		name  string
		items []docker.CleanupItem
		want  []string
	}{
		{
			name: "image then container",
			items: []docker.CleanupItem{
				{ID: img1, Type: docker.CleanupItemTypeImage},
				{ID: ctr1, Type: docker.CleanupItemTypeContainer},
			},
			want: []string{"container:" + ctr1, "image:" + img1},
		},
		{
			name: "interleaved keeps order within each type",
			items: []docker.CleanupItem{
				{ID: img2, Type: docker.CleanupItemTypeImage},
				{ID: ctr2, Type: docker.CleanupItemTypeContainer},
				{ID: img1, Type: docker.CleanupItemTypeImage},
				{ID: ctr1, Type: docker.CleanupItemTypeContainer},
			},
			want: []string{"container:" + ctr2, "container:" + ctr1, "image:" + img2, "image:" + img1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &dockertest.Fake{
				Images:     []client.APIImages{{ID: img1}, {ID: img2}},
				Containers: []client.APIContainers{{ID: ctr1}, {ID: ctr2}},
			}
			input := slices.Clone(c.items)

			msg := docker.PerformCleanup(fake, c.items)()

			if _, ok := msg.(docker.CleanupResultMsg); !ok {
				t.Fatalf("expected CleanupResultMsg, got %T: %v", msg, msg)
			}
			if !slices.Equal(fake.Removed, c.want) {
				t.Errorf("Removed = %v, want %v", fake.Removed, c.want)
			}
			if !slices.Equal(c.items, input) {
				t.Errorf("PerformCleanup reordered the caller's slice: %v", c.items)
			}
		})
	}
}
