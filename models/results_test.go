package models

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"docker-cleaner/docker"
	dockerClient "github.com/fsouza/go-dockerclient"
)

// listed reports whether a row with id is in the list.
func listed(m Model, id string) bool {
	return slices.ContainsFunc(m.items, func(it SelectableItem) bool { return it.ID == id })
}

func TestCleanupReportsEachItem(t *testing.T) {
	cases := []struct {
		name         string
		errs         map[string]error
		wantSummary  string
		wantFailures []string
		wantSelected []string // failed items stay selected
	}{
		{
			name:        "all deleted",
			wantSummary: "✅ Deleted 3",
		},
		{
			name:         "partial failure",
			errs:         map[string]error{imgB: errors.New("boom")},
			wantSummary:  "❌ Deleted 2 · Failed 1",
			wantFailures: []string{"bbbbbbbbbbbb busybox:latest: boom"},
			wantSelected: []string{imgB},
		},
		{
			name: "running container",
			errs: map[string]error{
				ctrRunning: &dockerClient.Error{Status: http.StatusConflict},
			},
			wantSummary:  "❌ Deleted 2 · Failed 1",
			wantFailures: []string{"rrrrrrrrrrrr web: container is running; stop it first"},
			wantSelected: []string{ctrRunning},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := newFake()
			fake.RemoveErrs = c.errs
			m := loaded(t, fake, imgA, imgB, ctrRunning)

			m = send(t, m, runes("d"))
			m = send(t, m, runes("y"))
			view := m.View()

			for _, want := range append([]string{c.wantSummary}, c.wantFailures...) {
				if !strings.Contains(view, want) {
					t.Errorf("view should contain %q, got:\n%s", want, view)
				}
			}
			if got := selectedIDs(m); !slices.Equal(got, c.wantSelected) {
				t.Errorf("selected = %v, want %v", got, c.wantSelected)
			}
			// The lists were refreshed: deleted items are gone, failed
			// ones are still there.
			for _, id := range []string{imgA, imgB, ctrRunning} {
				_, failed := c.errs[id]
				if got := listed(m, id); got != failed {
					t.Errorf("%s listed = %v, want %v", id, got, failed)
				}
			}
			// Nothing here is worth forcing, so f does nothing.
			if strings.Contains(view, "Press f") {
				t.Errorf("view should not offer a forced retry, got:\n%s", view)
			}
			removed := len(fake.Removed)
			send(t, m, runes("f"))
			if len(fake.Removed) != removed {
				t.Errorf("f removed %v with nothing to retry", fake.Removed[removed:])
			}
		})
	}
}

func TestForceRetryRemovesOnlyFailedImages(t *testing.T) {
	fake := newFake()
	fake.RemoveErrs = map[string]error{
		imgB: &dockerClient.Error{
			Status:  http.StatusConflict,
			Message: "conflict: unable to delete bbbbbbbbbbbb (must be forced) - image is referenced in multiple repositories",
		},
		ctrRunning: &dockerClient.Error{Status: http.StatusConflict},
	}
	m := loaded(t, fake, imgA, imgB, ctrRunning)
	m = press(m, "?") // full help, to check the retry key appears in it
	if strings.Contains(m.View(), "force-remove failed images") {
		t.Fatalf("help should not offer a retry before any cleanup:\n%s", m.View())
	}

	m = send(t, m, runes("d"))
	m = send(t, m, runes("y"))
	view := m.View()
	for _, want := range []string{
		"❌ Deleted 1 · Failed 2",
		"rrrrrrrrrrrr web: container is running; stop it first",
		"bbbbbbbbbbbb busybox:latest: image is referenced in multiple repositories",
		"Press f to force-remove 1 image that is in use or has several tags",
		"force-remove failed images",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view should contain %q, got:\n%s", want, view)
		}
	}

	// The daemon would now accept a forced removal.
	delete(fake.RemoveErrs, imgB)
	m = send(t, m, runes("f"))

	if want := []dockerClient.RemoveImageOptions{{Force: true}}; !slices.Equal(fake.RemoveImageOpts, want) {
		t.Errorf("RemoveImageExtended options = %v, want %v", fake.RemoveImageOpts, want)
	}
	if want := []string{"image:" + imgA, "image:" + imgB}; !slices.Equal(fake.Removed, want) {
		t.Errorf("Removed = %v, want %v", fake.Removed, want)
	}
	if len(fake.RemoveContainerOpts) != 1 {
		t.Errorf("the running container should not be retried, RemoveContainer calls: %v", fake.RemoveContainerOpts)
	}

	view = m.View()
	if !strings.Contains(view, "✅ Deleted 1") || strings.Contains(view, "Press f") {
		t.Errorf("retry should report its own result without a further hint, got:\n%s", view)
	}
	if got, want := selectedIDs(m), []string{ctrRunning}; !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}

	// The retry key is spent.
	removed := len(fake.Removed)
	send(t, m, runes("f"))
	if len(fake.Removed) != removed {
		t.Errorf("a second f removed %v", fake.Removed[removed:])
	}
}

func TestNewReportNamesFailures(t *testing.T) {
	m := newModel(nil)
	m = updated(m, images(imgA))
	// imgA is listed (unnamed images show "<none>"); imgC is not, so its
	// line has only the short ID.
	r := m.newReport(docker.CleanupResultMsg{
		Failed: []docker.CleanupFailure{
			{Item: docker.CleanupItem{ID: imgA, Type: docker.CleanupItemTypeImage}, Reason: "boom"},
			{Item: docker.CleanupItem{ID: imgC, Type: docker.CleanupItemTypeImage}, Reason: "no such image"},
		},
	})

	want := []string{"aaaaaaaaaaaa <none>: boom", "cccccccccccc: no such image"}
	if !slices.Equal(r.failures, want) {
		t.Errorf("failures = %q, want %q", r.failures, want)
	}
	if r.summary != "Deleted 0 · Failed 2" {
		t.Errorf("summary = %q", r.summary)
	}
}
