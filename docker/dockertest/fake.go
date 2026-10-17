// Package dockertest provides an in-memory fake of docker.API for tests.
package dockertest

import (
	"context"
	"fmt"
	"slices"

	client "github.com/fsouza/go-dockerclient"
	"github.com/moby/moby/api/types/volume"
	mobyclient "github.com/moby/moby/client"
)

// Fake is an in-memory stand-in for the Docker daemon. Set the exported
// fields to shape what it returns; inspect Removed and the recorded options
// to assert what the code under test did.
type Fake struct {
	Images     []client.APIImages
	Containers []client.APIContainers
	Volumes    []client.Volume
	Networks   []client.Network
	// Usage is what DiskUsage returns. Removing a volume drops it from
	// Usage.Volumes.Items, and pruning the build cache empties
	// Usage.BuildCache.
	Usage mobyclient.DiskUsageResult

	// PingErr is returned by Ping.
	PingErr error
	// ListErr is returned by ListImages, ListContainers, ListVolumes and
	// ListNetworks.
	ListErr error
	// DiskUsageErr is returned by DiskUsage.
	DiskUsageErr error
	// PruneErr is returned by BuildCachePrune.
	PruneErr error
	// RemoveErrs maps an image or container ID, a volume name or a network
	// ID to the error its removal should return.
	RemoveErrs map[string]error

	// Removed records successful removals in call order, as "image:<id>",
	// "container:<id>", "volume:<name>", "network:<id>" or "buildcache".
	Removed []string
	// DiskUsageOpts is the options passed to the last DiskUsage call.
	DiskUsageOpts mobyclient.DiskUsageOptions
	// PruneOpts records the options of every BuildCachePrune call.
	PruneOpts []mobyclient.BuildCachePruneOptions
	// ListContainersOpts is the options passed to the last ListContainers call.
	ListContainersOpts client.ListContainersOptions
	// RemoveContainerOpts records the options of every RemoveContainer call.
	RemoveContainerOpts []client.RemoveContainerOptions
	// RemoveImageOpts records the options of every RemoveImageExtended call.
	RemoveImageOpts []client.RemoveImageOptions
}

func (f *Fake) Ping() error {
	return f.PingErr
}

func (f *Fake) ListImages(_ client.ListImagesOptions) ([]client.APIImages, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.Images, nil
}

func (f *Fake) ListContainers(opts client.ListContainersOptions) ([]client.APIContainers, error) {
	f.ListContainersOpts = opts
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.Containers, nil
}

func (f *Fake) RemoveImage(name string) error {
	if err := f.RemoveErrs[name]; err != nil {
		return err
	}
	for i, img := range f.Images {
		if img.ID == name {
			f.Images = append(f.Images[:i], f.Images[i+1:]...)
			f.Removed = append(f.Removed, "image:"+name)
			return nil
		}
	}
	return fmt.Errorf("no such image: %s", name)
}

// RemoveImageExtended behaves like RemoveImage, recording opts first. It
// ignores opts.Force: an entry in RemoveErrs fails the call either way.
func (f *Fake) RemoveImageExtended(name string, opts client.RemoveImageOptions) error {
	f.RemoveImageOpts = append(f.RemoveImageOpts, opts)
	return f.RemoveImage(name)
}

func (f *Fake) RemoveContainer(opts client.RemoveContainerOptions) error {
	f.RemoveContainerOpts = append(f.RemoveContainerOpts, opts)
	if err := f.RemoveErrs[opts.ID]; err != nil {
		return err
	}
	for i, c := range f.Containers {
		if c.ID == opts.ID {
			f.Containers = append(f.Containers[:i], f.Containers[i+1:]...)
			f.Removed = append(f.Removed, "container:"+opts.ID)
			return nil
		}
	}
	return fmt.Errorf("no such container: %s", opts.ID)
}

func (f *Fake) ListVolumes(_ client.ListVolumesOptions) ([]client.Volume, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.Volumes, nil
}

// RemoveVolumeWithOptions removes the volume opts.Name. It ignores
// opts.Force: an entry in RemoveErrs fails the call either way.
func (f *Fake) RemoveVolumeWithOptions(opts client.RemoveVolumeOptions) error {
	if err := f.RemoveErrs[opts.Name]; err != nil {
		return err
	}
	i := slices.IndexFunc(f.Volumes, func(v client.Volume) bool { return v.Name == opts.Name })
	if i < 0 {
		return client.ErrNoSuchVolume
	}
	f.Volumes = slices.Delete(f.Volumes, i, i+1)
	f.Usage.Volumes.Items = slices.DeleteFunc(f.Usage.Volumes.Items, func(v volume.Volume) bool { return v.Name == opts.Name })
	f.Removed = append(f.Removed, "volume:"+opts.Name)
	return nil
}

func (f *Fake) ListNetworks() ([]client.Network, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.Networks, nil
}

func (f *Fake) RemoveNetwork(id string) error {
	if err := f.RemoveErrs[id]; err != nil {
		return err
	}
	i := slices.IndexFunc(f.Networks, func(n client.Network) bool { return n.ID == id })
	if i < 0 {
		return &client.NoSuchNetwork{ID: id}
	}
	f.Networks = slices.Delete(f.Networks, i, i+1)
	f.Removed = append(f.Removed, "network:"+id)
	return nil
}

func (f *Fake) DiskUsage(_ context.Context, opts mobyclient.DiskUsageOptions) (mobyclient.DiskUsageResult, error) {
	f.DiskUsageOpts = opts
	if f.DiskUsageErr != nil {
		return mobyclient.DiskUsageResult{}, f.DiskUsageErr
	}
	return f.Usage, nil
}

// BuildCachePrune reports Usage.BuildCache.Reclaimable as reclaimed, whatever
// opts says, and then empties Usage.BuildCache.
func (f *Fake) BuildCachePrune(_ context.Context, opts mobyclient.BuildCachePruneOptions) (mobyclient.BuildCachePruneResult, error) {
	f.PruneOpts = append(f.PruneOpts, opts)
	if f.PruneErr != nil {
		return mobyclient.BuildCachePruneResult{}, f.PruneErr
	}
	var result mobyclient.BuildCachePruneResult
	if r := f.Usage.BuildCache.Reclaimable; r > 0 {
		result.Report.SpaceReclaimed = uint64(r)
	}
	f.Usage.BuildCache = mobyclient.BuildCacheDiskUsage{}
	f.Removed = append(f.Removed, "buildcache")
	return result, nil
}
