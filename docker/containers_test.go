package docker_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
)

func TestFetchDockerContainersRequestsSizes(t *testing.T) {
	fake := &dockertest.Fake{}
	msg := docker.FetchDockerContainers(fake)()

	if _, ok := msg.(docker.ContainersFetchedMsg); !ok {
		t.Fatalf("expected ContainersFetchedMsg, got %T: %v", msg, msg)
	}
	if !fake.ListContainersOpts.All {
		t.Errorf("expected All=true so stopped containers are listed")
	}
	if !fake.ListContainersOpts.Size {
		t.Errorf("expected Size=true so the daemon reports container sizes")
	}
}

func TestPerformCleanupDoesNotForceRemoveContainers(t *testing.T) {
	fake := &dockertest.Fake{
		Containers: []client.APIContainers{{ID: "cccccccccccccccc"}},
	}
	docker.PerformCleanup(fake, []docker.CleanupItem{
		{ID: "cccccccccccccccc", Type: docker.CleanupItemTypeContainer},
	})()

	if len(fake.RemoveContainerOpts) != 1 {
		t.Fatalf("expected 1 RemoveContainer call, got %d", len(fake.RemoveContainerOpts))
	}
	if fake.RemoveContainerOpts[0].Force {
		t.Errorf("RemoveContainer called with Force=true; running containers must not be killed silently")
	}
}

func TestPerformCleanupContainerErrors(t *testing.T) {
	const id = "cccccccccccccccc"
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "running container",
			err:  &client.Error{Status: http.StatusConflict, Message: "cannot remove a running container"},
			want: "container is running; stop it first",
		},
		{
			name: "wrapped running container",
			err:  fmt.Errorf("remove: %w", &client.Error{Status: http.StatusConflict}),
			want: "container is running; stop it first",
		},
		{
			name: "paused container",
			err:  &client.Error{Status: http.StatusConflict, Message: "cannot remove a paused container: unpause and then stop the container before attempting removal or force remove"},
			want: "container is paused; unpause and stop it first",
		},
		{
			name: "restarting container",
			err:  &client.Error{Status: http.StatusConflict, Message: "cannot remove a restarting container: stop the container before attempting removal or force remove"},
			want: "container is restarting; stop it first",
		},
		{
			name: "other API error",
			err:  &client.Error{Status: http.StatusInternalServerError, Message: "boom"},
			want: "API error (500): boom",
		},
		{
			name: "plain error",
			err:  errors.New("connection refused"),
			want: "connection refused",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &dockertest.Fake{
				Containers: []client.APIContainers{{ID: id}},
				RemoveErrs: map[string]error{id: c.err},
			}
			msg := docker.PerformCleanup(fake, []docker.CleanupItem{
				{ID: id, Type: docker.CleanupItemTypeContainer},
			})()

			result, ok := msg.(docker.CleanupResultMsg)
			if !ok {
				t.Fatalf("expected CleanupResultMsg, got %T: %v", msg, msg)
			}
			if len(result.Failed) != 1 {
				t.Fatalf("Failed = %v, want one failure", result.Failed)
			}
			if got := result.Failed[0].Reason; got != c.want {
				t.Errorf("Reason = %q, want %q", got, c.want)
			}
			if result.Failed[0].Forceable {
				t.Errorf("container failures must not be offered a forced retry")
			}
		})
	}
}
