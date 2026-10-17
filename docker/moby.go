package docker

import (
	"context"
	"path/filepath"

	client "github.com/fsouza/go-dockerclient"
	mobyclient "github.com/moby/moby/client"
)

// Client is the Docker client docker-cleaner uses. go-dockerclient, which it
// embeds, does everything except reporting disk usage and pruning the build
// cache; those go through the official moby client, which is connected to
// the same daemon (see NewClient).
type Client struct {
	*client.Client
	moby *mobyclient.Client
}

// DiskUsage reports the daemon's disk usage (as `docker system df` does)
// through the moby client.
func (c *Client) DiskUsage(ctx context.Context, opts mobyclient.DiskUsageOptions) (mobyclient.DiskUsageResult, error) {
	return c.moby.DiskUsage(ctx, opts)
}

// BuildCachePrune removes unused build cache (as `docker builder prune`
// does) through the moby client.
func (c *Client) BuildCachePrune(ctx context.Context, opts mobyclient.BuildCachePruneOptions) (mobyclient.BuildCachePruneResult, error) {
	return c.moby.BuildCachePrune(ctx, opts)
}

// newMobyClient connects the moby client to the daemon at host, the address
// the go-dockerclient client uses, with tlsFiles' material if it is not nil.
// A non-empty apiVersion pins the API version; otherwise the client
// negotiates it with the daemon on its first request, which the moby client
// does by default.
func newMobyClient(host string, tlsFiles *TLSFiles, apiVersion string) (*mobyclient.Client, error) {
	opts := []mobyclient.Opt{mobyclient.WithHost(host)}
	if tlsFiles != nil {
		opts = append(opts, mobyclient.WithTLSClientConfig(tlsFiles.CA, tlsFiles.Cert, tlsFiles.Key))
	}
	if apiVersion != "" {
		opts = append(opts, mobyclient.WithAPIVersion(apiVersion))
	}
	return mobyclient.New(opts...)
}

// envTLS returns the TLS material go-dockerclient's NewClientFromEnv uses for
// the DOCKER_HOST and default sources: none unless DOCKER_TLS_VERIFY is set,
// and then ca.pem, cert.pem and key.pem from DOCKER_CERT_PATH, or from
// .docker in homeDir if that is unset.
func envTLS(getenv func(string) string, homeDir string) *TLSFiles {
	if getenv("DOCKER_TLS_VERIFY") == "" {
		return nil
	}
	dir := getenv("DOCKER_CERT_PATH")
	if dir == "" {
		dir = filepath.Join(homeDir, ".docker")
	}
	return &TLSFiles{
		CA:   filepath.Join(dir, "ca.pem"),
		Cert: filepath.Join(dir, "cert.pem"),
		Key:  filepath.Join(dir, "key.pem"),
	}
}
