package prune

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	client "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/filter"
)

// Entry is one resource in a plan: one to delete or one skipped.
type Entry struct {
	Type docker.CleanupItemType
	// ID is the resource's ID: an image, container or network ID, a
	// volume's name, or docker.BuildCacheID.
	ID string
	// Name is the resource's readable name: an image's first tag (or
	// "<none>"), a container's name, a volume's or network's name, or
	// "build cache".
	Name string
	// Size is the space deleting the resource frees, in bytes, as the TUI
	// counts it, or -1 if unknown.
	Size int64
	// Created is when the resource was created; zero if unknown.
	Created time.Time
	// Reason says why the resource matched (the filters that selected it)
	// or, for a skipped one, why it is kept.
	Reason string
	// Force is the docker.CleanupItem's Force.
	Force bool
}

// item returns the cleanup item that deletes e.
func (e Entry) item() docker.CleanupItem {
	return docker.CleanupItem{ID: e.ID, Type: e.Type, Force: e.Force}
}

// Plan is what a prune deletes and what it leaves alone.
type Plan struct {
	// Delete lists the resources to delete, by type in
	// docker.RemovalOrder and otherwise in the daemon's order.
	Delete []Entry
	// Skip lists the resources the filters match but the prune keeps:
	// protected ones, and those Docker would refuse to remove without
	// --force.
	Skip []Entry
	// Warnings note what the plan had to do without, e.g. volume sizes.
	Warnings []string
}

// Size totals the known sizes of the resources to delete.
func (p Plan) Size() int64 {
	var n int64
	for _, e := range p.Delete {
		n += max(e.Size, 0)
	}
	return n
}

// buildPlan lists the resources of the types opts names and decides which to
// delete. A resource is a candidate when every filter that applies to its
// type passes, as in the TUI. A candidate is skipped when it is protected:
// by the system rules, by protector's patterns, or, for an image, because a
// protected container runs on it (see docker.Protector.ProtectedImages).
// Without opts.Force it is also skipped when Docker would refuse to remove
// it: a running, paused or restarting container, or an image or volume a
// container that stays uses. Docker's predefined networks are left out
// altogether, as the TUI does.
func buildPlan(ctx context.Context, cli docker.API, protector docker.Protector, opts Options, now time.Time) (Plan, error) {
	var p Plan
	want := func(t docker.CleanupItemType) bool { return slices.Contains(opts.Types, t) }

	// Every container, whatever the types: they decide what is in use and
	// which images are protected.
	containers, err := cli.ListContainers(client.ListContainersOptions{
		All:     true,
		Size:    want(docker.CleanupItemTypeContainer),
		Context: ctx,
	})
	if err != nil {
		return Plan{}, fmt.Errorf("listing containers: %w", err)
	}

	var du docker.DiskUsage
	if want(docker.CleanupItemTypeVolume) || want(docker.CleanupItemTypeBuildCache) {
		du, err = docker.GetDiskUsage(ctx, cli)
		switch {
		case err != nil && want(docker.CleanupItemTypeBuildCache):
			return Plan{}, fmt.Errorf("reading disk usage: %w", err)
		case err != nil:
			p.Warnings = append(p.Warnings, fmt.Sprintf("volume sizes unknown: reading disk usage: %v", err))
		}
	}
	usage := filter.NewUsage(containers, du.Volumes)

	// Containers come first, so that an image or volume only containers
	// being deleted use is not skipped as in use.
	deleting := make(map[string]bool)
	if want(docker.CleanupItemTypeContainer) {
		reason := matchReason(opts.Filters, docker.CleanupItemTypeContainer)
		for _, c := range opts.Filters.Containers(containers, now) {
			e := Entry{
				Type:    docker.CleanupItemTypeContainer,
				ID:      c.ID,
				Name:    containerName(c),
				Size:    c.SizeRw,
				Created: filter.ContainerCreated(c),
				Reason:  reason,
				Force:   opts.Force,
			}
			if r := protector.ContainerReason(c); r != docker.NotProtected {
				p.skip(e, string(r))
			} else if active(c) && !opts.Force {
				p.skip(e, fmt.Sprintf("container is %s; pass --force to remove it", c.State))
			} else {
				p.Delete = append(p.Delete, e)
				deleting[c.ID] = true
			}
		}
	}
	staying := slices.DeleteFunc(slices.Clone(containers), func(c client.APIContainers) bool {
		return deleting[c.ID]
	})

	if want(docker.CleanupItemTypeImage) {
		images, err := cli.ListImages(client.ListImagesOptions{Context: ctx})
		if err != nil {
			return Plan{}, fmt.Errorf("listing images: %w", err)
		}
		protected := protector.ProtectedImages(images, containers)
		reason := matchReason(opts.Filters, docker.CleanupItemTypeImage)
		for _, img := range opts.Filters.Images(images, usage, now) {
			e := Entry{
				Type:    docker.CleanupItemTypeImage,
				ID:      img.ID,
				Name:    imageName(img),
				Size:    img.Size,
				Created: filter.ImageCreated(img),
				Reason:  reason,
				Force:   opts.Force,
			}
			user := slices.IndexFunc(staying, func(c client.APIContainers) bool {
				return docker.ImageMatchesRef(img, c.Image)
			})
			if r, ok := protected[img.ID]; ok {
				p.skip(e, string(r))
			} else if user >= 0 && !opts.Force {
				p.skip(e, fmt.Sprintf("used by container %s; pass --force to remove it", containerName(staying[user])))
			} else {
				p.Delete = append(p.Delete, e)
			}
		}
	}

	if want(docker.CleanupItemTypeVolume) {
		volumes, err := cli.ListVolumes(client.ListVolumesOptions{Context: ctx})
		if err != nil {
			return Plan{}, fmt.Errorf("listing volumes: %w", err)
		}
		reason := matchReason(opts.Filters, docker.CleanupItemTypeVolume)
		for _, v := range opts.Filters.Volumes(volumes, usage, now) {
			e := Entry{
				Type:    docker.CleanupItemTypeVolume,
				ID:      v.Name,
				Name:    v.Name,
				Size:    -1,
				Created: v.CreatedAt,
				Reason:  reason,
			}
			if u, ok := du.Volumes[v.Name]; ok && u.Size >= 0 {
				e.Size = u.Size
			}
			user := slices.IndexFunc(staying, func(c client.APIContainers) bool { return mounts(c, v.Name) })
			if r := protector.VolumeReason(v); r != docker.NotProtected {
				p.skip(e, string(r))
			} else if user >= 0 {
				// Force does not help: the daemon never removes a
				// volume in use.
				p.skip(e, "in use by container "+containerName(staying[user]))
			} else {
				p.Delete = append(p.Delete, e)
			}
		}
	}

	if want(docker.CleanupItemTypeNetwork) {
		networks, err := cli.ListNetworks()
		if err != nil {
			return Plan{}, fmt.Errorf("listing networks: %w", err)
		}
		networks = slices.DeleteFunc(networks, docker.IsPredefinedNetwork)
		reason := matchReason(opts.Filters, docker.CleanupItemTypeNetwork)
		for _, n := range opts.Filters.Networks(networks, usage) {
			e := Entry{Type: docker.CleanupItemTypeNetwork, ID: n.ID, Name: n.Name, Reason: reason}
			if r := protector.NetworkReason(n); r != docker.NotProtected {
				p.skip(e, string(r))
			} else {
				p.Delete = append(p.Delete, e)
			}
		}
	}

	if bc := du.BuildCache; want(docker.CleanupItemTypeBuildCache) && bc.Reclaimable > 0 {
		p.Delete = append(p.Delete, Entry{
			Type:   docker.CleanupItemTypeBuildCache,
			ID:     docker.BuildCacheID,
			Name:   "build cache",
			Size:   bc.Reclaimable,
			Reason: "unused build cache",
			// Prune with All only when a plain prune would leave part
			// of the reclaimable space, as the TUI does.
			Force: bc.NeedsAll,
		})
	}
	return p, nil
}

// skip adds e to the skipped resources, kept for reason.
func (p *Plan) skip(e Entry, reason string) {
	e.Reason = reason
	p.Skip = append(p.Skip, e)
}

// matchReason says why the filters f select a resource of type t, by naming
// the filters that apply to it, e.g. "dangling, older than 30d".
func matchReason(f filter.Options, t docker.CleanupItemType) string {
	return strings.Join(only(f, t).Labels(), ", ")
}

// active reports whether c is running, paused or restarting: a container
// Docker removes only with force.
func active(c client.APIContainers) bool {
	switch c.State {
	case "running", "paused", "restarting":
		return true
	}
	return false
}

// mounts reports whether c mounts the volume called name.
func mounts(c client.APIContainers, name string) bool {
	return slices.ContainsFunc(c.Mounts, func(m client.APIMount) bool {
		return m.Type == "volume" && m.Name == name
	})
}

// imageName returns img's first tag, or "<none>" if it is untagged.
func imageName(img client.APIImages) string {
	for _, tag := range img.RepoTags {
		if tag != "<none>:<none>" {
			return tag
		}
	}
	return "<none>"
}

// containerName returns c's first name without Docker's leading "/", or its
// short ID if it has none.
func containerName(c client.APIContainers) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return shortID(c.ID)
}

// shortID is docker.ShortID, except that an ID too short to cut is returned
// whole rather than panicking: the output of a cleanup should not fail on
// one odd ID.
func shortID(id string) string {
	if len(strings.TrimPrefix(id, "sha256:")) < 12 {
		return id
	}
	return docker.ShortID(id)
}
