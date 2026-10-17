package docker_test

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"

	client "github.com/fsouza/go-dockerclient"
)

const (
	resImg1 = "sha256:1111111111111111"
	resImg2 = "sha256:2222222222222222"
	resCtr  = "cccccccccccccccc"
)

func TestPerformCleanupPartialFailure(t *testing.T) {
	fake := &dockertest.Fake{
		Images:     []client.APIImages{{ID: resImg1}, {ID: resImg2}},
		Containers: []client.APIContainers{{ID: resCtr}},
		RemoveErrs: map[string]error{resImg1: errors.New("boom")},
	}
	img1 := docker.CleanupItem{ID: resImg1, Type: docker.CleanupItemTypeImage}
	img2 := docker.CleanupItem{ID: resImg2, Type: docker.CleanupItemTypeImage}
	ctr := docker.CleanupItem{ID: resCtr, Type: docker.CleanupItemTypeContainer}

	msg := docker.PerformCleanup(fake, []docker.CleanupItem{img1, img2, ctr})()

	result, ok := msg.(docker.CleanupResultMsg)
	if !ok {
		t.Fatalf("expected CleanupResultMsg, got %T: %v", msg, msg)
	}
	if want := []docker.CleanupItem{ctr, img2}; !slices.Equal(result.Deleted, want) {
		t.Errorf("Deleted = %v, want %v", result.Deleted, want)
	}
	want := []docker.CleanupFailure{{Item: img1, Reason: "boom"}}
	if !slices.Equal(result.Failed, want) {
		t.Errorf("Failed = %+v, want %+v", result.Failed, want)
	}
}

func TestPerformCleanupImageErrors(t *testing.T) {
	conflict := func(msg string) error {
		return &client.Error{Status: http.StatusConflict, Message: msg}
	}
	cases := []struct {
		name          string
		force         bool
		err           error
		wantReason    string
		wantForceable bool
	}{
		{
			name:          "several tags",
			err:           conflict("conflict: unable to delete 111111111111 (must be forced) - image is referenced in multiple repositories"),
			wantReason:    "image is referenced in multiple repositories",
			wantForceable: true,
		},
		{
			name:          "used by stopped container",
			err:           conflict("conflict: unable to delete 111111111111 (must be forced) - image is being used by stopped container cccccccccccc"),
			wantReason:    "image is being used by stopped container cccccccccccc",
			wantForceable: true,
		},
		{
			// Force cannot remove an image a running container uses.
			name:       "used by running container",
			err:        conflict("conflict: unable to delete 111111111111 (cannot be forced) - image is being used by running container cccccccccccc"),
			wantReason: "image is being used by running container cccccccccccc",
		},
		{
			name:          "conflict without message",
			err:           conflict(""),
			wantReason:    "image is in use or has several tags",
			wantForceable: true,
		},
		{
			name:       "conflict despite force",
			force:      true,
			err:        conflict("conflict: unable to delete 111111111111 (must be forced) - image is referenced in multiple repositories"),
			wantReason: "image is referenced in multiple repositories",
		},
		{
			name:       "other API error",
			err:        &client.Error{Status: http.StatusInternalServerError, Message: "boom"},
			wantReason: "API error (500): boom",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &dockertest.Fake{
				Images:     []client.APIImages{{ID: resImg1}},
				RemoveErrs: map[string]error{resImg1: c.err},
			}
			msg := docker.PerformCleanup(fake, []docker.CleanupItem{
				{ID: resImg1, Type: docker.CleanupItemTypeImage, Force: c.force},
			})()

			result := msg.(docker.CleanupResultMsg)
			if len(result.Failed) != 1 {
				t.Fatalf("Failed = %v, want one failure", result.Failed)
			}
			f := result.Failed[0]
			if f.Reason != c.wantReason {
				t.Errorf("Reason = %q, want %q", f.Reason, c.wantReason)
			}
			if f.Forceable != c.wantForceable {
				t.Errorf("Forceable = %v, want %v", f.Forceable, c.wantForceable)
			}
		})
	}
}

func TestPerformCleanupForcesImagesOnlyWhenAsked(t *testing.T) {
	cases := []struct {
		force    bool
		wantOpts []client.RemoveImageOptions
	}{
		{false, nil},
		{true, []client.RemoveImageOptions{{Force: true}}},
	}
	for _, c := range cases {
		fake := &dockertest.Fake{Images: []client.APIImages{{ID: resImg1}}}
		docker.PerformCleanup(fake, []docker.CleanupItem{
			{ID: resImg1, Type: docker.CleanupItemTypeImage, Force: c.force},
		})()

		if !slices.Equal(fake.RemoveImageOpts, c.wantOpts) {
			t.Errorf("Force=%v: RemoveImageExtended options = %v, want %v", c.force, fake.RemoveImageOpts, c.wantOpts)
		}
		if want := []string{"image:" + resImg1}; !slices.Equal(fake.Removed, want) {
			t.Errorf("Force=%v: Removed = %v, want %v", c.force, fake.Removed, want)
		}
	}
}

func TestCleanupResultForceRetry(t *testing.T) {
	img1 := docker.CleanupItem{ID: resImg1, Type: docker.CleanupItemTypeImage}
	img2 := docker.CleanupItem{ID: resImg2, Type: docker.CleanupItemTypeImage}
	ctr := docker.CleanupItem{ID: resCtr, Type: docker.CleanupItemTypeContainer}
	result := docker.CleanupResultMsg{
		Deleted: []docker.CleanupItem{ctr},
		Failed: []docker.CleanupFailure{
			{Item: img1, Reason: "boom"},
			{Item: img2, Reason: "image is referenced in multiple repositories", Forceable: true},
		},
	}

	want := []docker.CleanupItem{{ID: resImg2, Type: docker.CleanupItemTypeImage, Force: true}}
	if got := result.ForceRetry(); !slices.Equal(got, want) {
		t.Errorf("ForceRetry() = %v, want %v", got, want)
	}
}

func TestCleanupResultSummary(t *testing.T) {
	item := docker.CleanupItem{ID: resImg1, Type: docker.CleanupItemTypeImage}
	failure := docker.CleanupFailure{Item: item, Reason: "boom"}
	cases := []struct {
		name    string
		deleted int
		failed  int
		want    string
	}{
		{"all deleted", 4, 0, "Deleted 4"},
		{"partial failure", 4, 1, "Deleted 4 · Failed 1"},
		{"all failed", 0, 2, "Deleted 0 · Failed 2"},
		{"nothing", 0, 0, "Deleted 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := docker.CleanupResultMsg{
				Deleted: slices.Repeat([]docker.CleanupItem{item}, c.deleted),
				Failed:  slices.Repeat([]docker.CleanupFailure{failure}, c.failed),
			}
			if got := r.Summary(); got != c.want {
				t.Errorf("Summary() = %q, want %q", got, c.want)
			}
		})
	}
}
