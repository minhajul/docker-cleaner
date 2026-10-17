// Package filter narrows Docker resources down to cleanup candidates and
// orders them. It holds the filters and sort orders the TUI offers, so that
// a non-interactive mode taking the same filters as flags selects exactly
// the same resources. It works on the docker package's resource types and
// knows nothing of the UI.
//
// Each filter narrows only the resource types it applies to and leaves the
// others untouched (see Options.Narrows); active filters combine with AND.
package filter

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	client "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
)

// Day is the unit OlderThan is shown in, e.g. "older than 30d".
const Day = 24 * time.Hour

// Options are the filters to apply. The zero value applies none.
type Options struct {
	// Dangling keeps only untagged images (see IsDangling). It narrows
	// images only.
	Dangling bool
	// Stopped keeps only stopped containers (see IsStopped). It narrows
	// containers only.
	Stopped bool
	// Unused keeps only images no container uses, volumes no container
	// mounts and networks no container is attached to, counting running
	// and stopped containers alike (see Usage).
	Unused bool
	// OlderThan, when positive, keeps only images, containers and volumes
	// created more than OlderThan ago (see IsOlderThan); one whose
	// creation time is unknown is left out. Networks are untouched: the
	// daemon's network list, as go-dockerclient decodes it, has no
	// creation time.
	OlderThan time.Duration
}

// Active reports whether any filter is on.
func (o Options) Active() bool {
	return o != Options{}
}

// Narrows reports whether an active filter applies to resources of type t,
// i.e. whether Options may leave some of them out. The build cache is never
// narrowed.
func (o Options) Narrows(t docker.CleanupItemType) bool {
	switch t {
	case docker.CleanupItemTypeImage:
		return o.Dangling || o.Unused || o.OlderThan > 0
	case docker.CleanupItemTypeContainer:
		return o.Stopped || o.OlderThan > 0
	case docker.CleanupItemTypeVolume:
		return o.Unused || o.OlderThan > 0
	case docker.CleanupItemTypeNetwork:
		return o.Unused
	}
	return false
}

// Labels names the active filters, e.g. ["dangling", "older than 30d"], in
// the order the fields are declared.
func (o Options) Labels() []string {
	var labels []string
	if o.Dangling {
		labels = append(labels, "dangling")
	}
	if o.Stopped {
		labels = append(labels, "stopped")
	}
	if o.Unused {
		labels = append(labels, "unused")
	}
	if o.OlderThan > 0 {
		labels = append(labels, "older than "+FormatAge(o.OlderThan))
	}
	return labels
}

// FormatAge formats an OlderThan duration in whole days, e.g. "30d", else in
// whole hours, e.g. "12h", else in Go duration syntax, e.g. "1h30m0s". Each
// form is one ParseAge reads back.
func FormatAge(d time.Duration) string {
	switch {
	case d%Day == 0:
		return fmt.Sprintf("%dd", d/Day)
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return d.String()
}

// ParseAge parses an age for OlderThan: a whole number of days ("30d") or
// weeks ("2w"), or a Go duration ("12h", "90m", "1h30m"). The age must be
// positive.
func ParseAge(s string) (time.Duration, error) {
	d, err := parseAge(s)
	if err != nil {
		return 0, fmt.Errorf("invalid age %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid age %q: must be more than zero", s)
	}
	return d, nil
}

// parseAge is ParseAge without the checks on the result.
func parseAge(s string) (time.Duration, error) {
	unit := map[string]time.Duration{"d": Day, "w": 7 * Day}[s[max(0, len(s)-1):]]
	if unit == 0 {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, errors.New("want a number of days (30d), weeks (2w) or hours (12h)")
		}
		return d, nil
	}
	n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil {
		return 0, errors.New("want a whole number before d or w, e.g. 30d")
	}
	if n > int64(math.MaxInt64/unit) {
		return 0, errors.New("too long")
	}
	return time.Duration(n) * unit, nil
}

// Images returns the images that pass the filters, in their original order.
// u tells which images containers use; now is the time ages are measured
// from.
func (o Options) Images(images []client.APIImages, u Usage, now time.Time) []client.APIImages {
	return keep(images, func(img client.APIImages) bool {
		return (!o.Dangling || IsDangling(img)) &&
			(!o.Unused || !u.ImageUsed(img)) &&
			o.oldEnough(ImageCreated(img), now)
	})
}

// Containers returns the containers that pass the filters, in their original
// order.
func (o Options) Containers(containers []client.APIContainers, now time.Time) []client.APIContainers {
	return keep(containers, func(c client.APIContainers) bool {
		return (!o.Stopped || IsStopped(c)) && o.oldEnough(ContainerCreated(c), now)
	})
}

// Volumes returns the volumes that pass the filters, in their original order.
func (o Options) Volumes(volumes []client.Volume, u Usage, now time.Time) []client.Volume {
	return keep(volumes, func(v client.Volume) bool {
		return (!o.Unused || u.VolumeRefs(v.Name) == 0) && o.oldEnough(v.CreatedAt, now)
	})
}

// Networks returns the networks that pass the filters, in their original
// order. Only Unused applies to them.
func (o Options) Networks(networks []client.Network, u Usage) []client.Network {
	return keep(networks, func(n client.Network) bool {
		return !o.Unused || u.NetworkContainers(n) == 0
	})
}

// oldEnough reports whether a resource created at created passes OlderThan.
func (o Options) oldEnough(created, now time.Time) bool {
	return o.OlderThan <= 0 || IsOlderThan(created, o.OlderThan, now)
}

// keep returns a new slice of the items for which ok is true.
func keep[T any](items []T, ok func(T) bool) []T {
	out := make([]T, 0, len(items))
	for _, it := range items {
		if ok(it) {
			out = append(out, it)
		}
	}
	return slices.Clip(out)
}

// IsDangling reports whether img is untagged: it has no tags, or only the
// "<none>:<none>" placeholder the daemon reports for one.
func IsDangling(img client.APIImages) bool {
	for _, tag := range img.RepoTags {
		if tag != "<none>:<none>" {
			return false
		}
	}
	return true
}

// IsStopped reports whether c is stopped: exited, created but never
// started, or dead. Running, paused, restarting and removing containers are
// not.
func IsStopped(c client.APIContainers) bool {
	switch c.State {
	case "exited", "created", "dead":
		return true
	}
	return false
}

// IsOlderThan reports whether something created at created is more than d
// old at now. It is false when created is unknown (zero).
func IsOlderThan(created time.Time, d time.Duration, now time.Time) bool {
	return !created.IsZero() && now.Sub(created) > d
}

// ImageCreated returns when img was created, or the zero time if the daemon
// did not say.
func ImageCreated(img client.APIImages) time.Time {
	return unixTime(img.Created)
}

// ContainerCreated returns when c was created, or the zero time if the
// daemon did not say.
func ContainerCreated(c client.APIContainers) time.Time {
	return unixTime(c.Created)
}

// unixTime converts Unix seconds to a time, treating 0 (or less) as unknown.
func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
