package docker_test

import (
	"errors"
	"strings"
	"testing"

	"docker-cleaner/docker"
	"docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
)

var _ docker.API = (*dockertest.Fake)(nil)

func TestPerformCleanupRemovesItems(t *testing.T) {
	fake := &dockertest.Fake{
		Images:     []client.APIImages{{ID: "sha256:aaaaaaaaaaaaaaaa"}},
		Containers: []client.APIContainers{{ID: "cccccccccccccccc"}},
	}
	msg := docker.PerformCleanup(fake, []docker.CleanupItem{
		{ID: "cccccccccccccccc", Type: docker.CleanupItemTypeContainer},
		{ID: "sha256:aaaaaaaaaaaaaaaa", Type: docker.CleanupItemTypeImage},
	})()

	done, ok := msg.(docker.CleanupDoneMsg)
	if !ok {
		t.Fatalf("expected CleanupDoneMsg, got %T: %v", msg, msg)
	}
	if done.Message != "Successfully deleted 1 image and 1 container" {
		t.Errorf("unexpected message %q", done.Message)
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

	errMsg, ok := msg.(docker.ErrMsg)
	if !ok {
		t.Fatalf("expected ErrMsg, got %T: %v", msg, msg)
	}
	if !strings.Contains(errMsg.Error(), "image is in use") {
		t.Errorf("expected error to mention the cause, got %q", errMsg.Error())
	}
}
