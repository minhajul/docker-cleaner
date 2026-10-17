package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	client "github.com/fsouza/go-dockerclient"
)

// EndpointSource says how an Endpoint was chosen.
type EndpointSource int

const (
	// SourceDefault means nothing named an endpoint (or the context is
	// "default"), so the platform's default socket is used.
	SourceDefault EndpointSource = iota
	// SourceDockerHost means the DOCKER_HOST environment variable is set.
	SourceDockerHost
	// SourceContextEnv means the DOCKER_CONTEXT environment variable named
	// the context.
	SourceContextEnv
	// SourceContextConfig means currentContext in the Docker config file
	// named the context.
	SourceContextConfig
)

// Endpoint is the Docker daemon address docker-cleaner connects to, and how
// it was chosen.
type Endpoint struct {
	// Host is the daemon address, e.g. "unix:///var/run/docker.sock". It
	// is empty from ResolveEndpoint for SourceDefault; NewClient fills it
	// in with the address the client uses.
	Host   string
	Source EndpointSource
	// Context is the Docker context's name for SourceContextEnv and
	// SourceContextConfig; empty otherwise.
	Context string
	// TLS holds the context's TLS material; nil for a plain connection
	// and for the DOCKER_HOST and default sources, whose TLS settings
	// come from DOCKER_TLS_VERIFY and DOCKER_CERT_PATH.
	TLS *TLSFiles
}

// TLSFiles are the paths of a Docker context's TLS material. CA is always
// set; Cert and Key are both set for client authentication or both empty.
type TLSFiles struct {
	CA, Cert, Key string
}

// String describes the endpoint for messages, e.g.
// `unix:///Users/x/.colima/default/docker.sock (context "colima")`.
func (e Endpoint) String() string {
	host := e.Host
	if host == "" {
		if e.Source == SourceDefault {
			return "the default socket"
		}
		host = "<no host>"
	}
	switch e.Source {
	case SourceDockerHost:
		return host + " (DOCKER_HOST)"
	case SourceContextEnv:
		return fmt.Sprintf("%s (context %q from DOCKER_CONTEXT)", host, e.Context)
	case SourceContextConfig:
		return fmt.Sprintf("%s (context %q)", host, e.Context)
	default:
		return host + " (default)"
	}
}

// defaultContext is the docker CLI's name for "no context": DOCKER_HOST or
// the default socket.
const defaultContext = "default"

// ConfigDir returns the Docker CLI's configuration directory: DOCKER_CONFIG
// if set, else ".docker" in homeDir. It returns "" if neither is known.
func ConfigDir(getenv func(string) string, homeDir string) string {
	if dir := getenv("DOCKER_CONFIG"); dir != "" {
		return filepath.Clean(dir)
	}
	if homeDir == "" {
		return ""
	}
	return filepath.Join(homeDir, ".docker")
}

// ResolveEndpoint picks the Docker endpoint the way the docker CLI does:
//
//  1. DOCKER_HOST, if set (SourceDockerHost);
//  2. else the context named by DOCKER_CONTEXT (SourceContextEnv), or by
//     currentContext in <configDir>/config.json (SourceContextConfig);
//  3. no context, or the context "default", means the default socket
//     (SourceDefault).
//
// A named context is read from
// <configDir>/contexts/meta/<sha256 of name>/meta.json, and its TLS material,
// if any, from <configDir>/contexts/tls/<sha256 of name>/docker/. getenv
// looks up environment variables (os.Getenv outside tests); configDir is
// usually ConfigDir's result. A missing config.json is not an error; a
// context that does not exist, or a file that is not valid JSON, is.
func ResolveEndpoint(getenv func(string) string, configDir string) (Endpoint, error) {
	if host := getenv("DOCKER_HOST"); host != "" {
		return Endpoint{Host: host, Source: SourceDockerHost}, nil
	}

	name, source := getenv("DOCKER_CONTEXT"), SourceContextEnv
	if name == "" {
		var err error
		if name, err = currentContext(configDir); err != nil {
			return Endpoint{}, err
		}
		source = SourceContextConfig
	}
	if name == "" || name == defaultContext {
		return Endpoint{Source: SourceDefault}, nil
	}
	if configDir == "" {
		return Endpoint{}, fmt.Errorf("docker context %q not found: no Docker config directory (set DOCKER_CONFIG or HOME)", name)
	}

	host, err := contextHost(configDir, name)
	if err != nil {
		return Endpoint{}, err
	}
	tlsFiles, err := contextTLS(configDir, name)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{Host: host, Source: source, Context: name, TLS: tlsFiles}, nil
}

// currentContext returns currentContext from <configDir>/config.json, or ""
// if the file or the field is missing.
func currentContext(configDir string) (string, error) {
	if configDir == "" {
		return "", nil
	}
	path := filepath.Join(configDir, "config.json")
	data, err := os.ReadFile(path) // #nosec G304 -- the Docker config file is meant to be read
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading Docker config: %w", err)
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parsing Docker config %s: %w", path, err)
	}
	return cfg.CurrentContext, nil
}

// contextDirName is the directory a context's files are stored under: the
// hex SHA-256 of its name, as the docker CLI does. Hashing also keeps a
// hostile name from escaping the contexts directory.
func contextDirName(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// contextHost returns the Docker endpoint of the context name, read from its
// meta.json.
func contextHost(configDir, name string) (string, error) {
	path := filepath.Join(configDir, "contexts", "meta", contextDirName(name), "meta.json")
	data, err := os.ReadFile(path) // #nosec G304 -- path is built from the config dir and a hash
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("docker context %q not found (looked in %s)", name, path)
	}
	if err != nil {
		return "", fmt.Errorf("reading docker context %q: %w", name, err)
	}

	var meta struct {
		Endpoints map[string]struct {
			Host          string
			SkipTLSVerify bool
		}
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("parsing docker context %q (%s): %w", name, path, err)
	}
	ep, ok := meta.Endpoints["docker"]
	if !ok || ep.Host == "" {
		return "", fmt.Errorf("docker context %q has no Docker endpoint (%s)", name, path)
	}
	if ep.SkipTLSVerify {
		// The docker CLI then connects without verifying the daemon's
		// certificate. Refuse rather than quietly do the same, or quietly
		// verify and fail in a confusing way.
		return "", fmt.Errorf("docker context %q skips TLS verification, which docker-cleaner does not support; set DOCKER_HOST instead", name)
	}
	if strings.HasPrefix(ep.Host, "ssh://") {
		return "", fmt.Errorf("docker context %q uses an SSH endpoint (%s), which docker-cleaner does not support", name, ep.Host)
	}
	return ep.Host, nil
}

// contextTLS returns the TLS material stored for the context name, or nil if
// it has none. Material without a CA certificate, or with only one of the
// client certificate and key, is an error: the Docker client library would
// skip verifying the daemon without a CA, and the docker CLI would not.
func contextTLS(configDir, name string) (*TLSFiles, error) {
	dir := filepath.Join(configDir, "contexts", "tls", contextDirName(name), "docker")
	exists := func(file string) (string, bool, error) {
		path := filepath.Join(dir, file)
		_, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("reading TLS material of docker context %q: %w", name, err)
		}
		return path, true, nil
	}

	ca, hasCA, err := exists("ca.pem")
	if err != nil {
		return nil, err
	}
	cert, hasCert, err := exists("cert.pem")
	if err != nil {
		return nil, err
	}
	key, hasKey, err := exists("key.pem")
	if err != nil {
		return nil, err
	}

	switch {
	case !hasCA && !hasCert && !hasKey:
		return nil, nil
	case !hasCA:
		return nil, fmt.Errorf("docker context %q has TLS material without a CA certificate (%s), which docker-cleaner does not support", name, dir)
	case hasCert != hasKey:
		return nil, fmt.Errorf("docker context %q has incomplete TLS material: need both cert.pem and key.pem (%s)", name, dir)
	}
	return &TLSFiles{CA: ca, Cert: cert, Key: key}, nil
}

// NewClient connects to the Docker endpoint ResolveEndpoint picks from the
// process environment and the user's Docker config directory, and returns
// the client and that endpoint, with Host filled in. It does not contact the
// daemon; call Ping to check that it is reachable.
//
// For SourceDockerHost and SourceDefault the client comes from
// client.NewClientFromEnv, which also honours DOCKER_TLS_VERIFY,
// DOCKER_CERT_PATH and DOCKER_API_VERSION. A context's client honours
// DOCKER_API_VERSION too.
func NewClient() (*client.Client, Endpoint, error) {
	home, _ := os.UserHomeDir() // "" is handled: only DOCKER_CONFIG is then used
	ep, err := ResolveEndpoint(os.Getenv, ConfigDir(os.Getenv, home))
	if err != nil {
		return nil, ep, err
	}

	if ep.Source == SourceDockerHost || ep.Source == SourceDefault {
		cli, err := client.NewClientFromEnv()
		if err != nil {
			return nil, ep, fmt.Errorf("connecting to %s: %w", ep, err)
		}
		ep.Host = cli.Endpoint()
		return cli, ep, nil
	}

	apiVersion := os.Getenv("DOCKER_API_VERSION")
	var cli *client.Client
	if ep.TLS != nil {
		cli, err = client.NewVersionedTLSClient(ep.Host, ep.TLS.Cert, ep.TLS.Key, ep.TLS.CA, apiVersion)
	} else {
		cli, err = client.NewVersionedClient(ep.Host, apiVersion)
	}
	if err != nil {
		return nil, ep, fmt.Errorf("connecting to %s: %w", ep, err)
	}
	// As in client.NewClientFromEnv: without a pinned version, use the
	// daemon's.
	cli.SkipServerVersionCheck = apiVersion == ""
	return cli, ep, nil
}
