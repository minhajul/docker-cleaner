package docker_test

import (
	"testing"

	"docker-cleaner/docker"
	"docker-cleaner/docker/dockertest"
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
