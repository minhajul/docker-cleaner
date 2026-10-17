package docker_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
)

var _ docker.API = (*dockertest.Fake)(nil)

func TestPerformCleanupRemovesItems(t *testing.T) {
	fake := &dockertest.Fake{
		Images:     []client.APIImages{{ID: "sha256:aaaaaaaaaaaaaaaa"}},
		Containers: []client.APIContainers{{ID: "cccccccccccccccc"}},
	}
	items := []docker.CleanupItem{
		{ID: "cccccccccccccccc", Type: docker.CleanupItemTypeContainer},
		{ID: "sha256:aaaaaaaaaaaaaaaa", Type: docker.CleanupItemTypeImage},
	}
	msg := docker.PerformCleanup(fake, items)()

	result, ok := msg.(docker.CleanupResultMsg)
	if !ok {
		t.Fatalf("expected CleanupResultMsg, got %T: %v", msg, msg)
	}
	if !slices.Equal(result.Deleted, items) || len(result.Failed) != 0 {
		t.Errorf("result = %+v, want all of %v deleted", result, items)
	}
	if len(fake.Removed) != 2 {
		t.Errorf("expected 2 removals, got %v", fake.Removed)
	}
}

func TestPerformCleanupReportsErrors(t *testing.T) {
	fake := &dockertest.Fake{
		Images:     []client.APIImages{{ID: "sha256:aaaaaaaaaaaaaaaa"}},
		RemoveErrs: map[string]error{"sha256:aaaaaaaaaaaaaaaa": errors.New("image is in use")},
	}
	msg := docker.PerformCleanup(fake, []docker.CleanupItem{
		{ID: "sha256:aaaaaaaaaaaaaaaa", Type: docker.CleanupItemTypeImage},
	})()

	result, ok := msg.(docker.CleanupResultMsg)
	if !ok {
		t.Fatalf("expected CleanupResultMsg, got %T: %v", msg, msg)
	}
	if len(result.Deleted) != 0 || len(result.Failed) != 1 {
		t.Fatalf("result = %+v, want exactly one failure", result)
	}
	if got := result.Failed[0].Reason; got != "image is in use" {
		t.Errorf("Reason = %q, want the cause %q", got, "image is in use")
	}
}
