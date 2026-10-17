// Package dockertest provides an in-memory fake of docker.API for tests.
package dockertest

import (
	"fmt"

	client "github.com/fsouza/go-dockerclient"
)

// Fake is an in-memory stand-in for the Docker daemon. Set the exported
// fields to shape what it returns; inspect Removed and the recorded options
// to assert what the code under test did.
type Fake struct {
	Images     []client.APIImages
	Containers []client.APIContainers

	// PingErr is returned by Ping.
	PingErr error
	// ListErr is returned by ListImages and ListContainers.
	ListErr error
	// RemoveErrs maps an image or container ID to the error its removal
	// should return.
	RemoveErrs map[string]error

	// Removed records successful removals in call order, as "image:<id>"
	// or "container:<id>".
	Removed []string
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
