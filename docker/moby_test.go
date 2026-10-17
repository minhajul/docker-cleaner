package docker

import (
	"path/filepath"
	"testing"
)

func TestNewMobyClientUsesEndpoint(t *testing.T) {
	cases := []struct {
		name        string
		host        string
		apiVersion  string
		wantVersion string // "" means negotiated on first request
	}{
		{name: "unix socket", host: "unix:///Users/x/.orbstack/run/docker.sock"},
		{name: "tcp", host: "tcp://192.168.1.10:2375"},
		{name: "pinned version", host: "unix:///var/run/docker.sock", apiVersion: "1.44", wantVersion: "1.44"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cli, err := newMobyClient(c.host, nil, c.apiVersion)
			if err != nil {
				t.Fatalf("newMobyClient: %v", err)
			}
			if got := cli.DaemonHost(); got != c.host {
				t.Errorf("DaemonHost() = %q, want %q", got, c.host)
			}
			if c.wantVersion != "" && cli.ClientVersion() != c.wantVersion {
				t.Errorf("ClientVersion() = %q, want %q", cli.ClientVersion(), c.wantVersion)
			}
		})
	}
}

func TestNewMobyClientLoadsTLS(t *testing.T) {
	// The files do not exist, so loading them must fail: that shows the
	// TLS material is used rather than ignored.
	dir := t.TempDir()
	tlsFiles := &TLSFiles{
		CA:   filepath.Join(dir, "ca.pem"),
		Cert: filepath.Join(dir, "cert.pem"),
		Key:  filepath.Join(dir, "key.pem"),
	}
	if _, err := newMobyClient("tcp://192.168.1.10:2376", tlsFiles, ""); err == nil {
		t.Error("newMobyClient should fail to load missing TLS files")
	}
}

func TestNewMobyClientRejectsBadVersion(t *testing.T) {
	if _, err := newMobyClient("unix:///var/run/docker.sock", nil, "latest"); err == nil {
		t.Error("newMobyClient should reject a malformed DOCKER_API_VERSION")
	}
}

func TestEnvTLS(t *testing.T) {
	home := filepath.FromSlash("/home/u")
	cases := []struct {
		name string
		env  map[string]string
		want *TLSFiles
	}{
		{name: "no TLS", env: nil, want: nil},
		{
			// As in go-dockerclient, a cert path alone does not enable TLS.
			name: "cert path without verify",
			env:  map[string]string{"DOCKER_CERT_PATH": "/certs"},
			want: nil,
		},
		{
			name: "verify with cert path",
			env:  map[string]string{"DOCKER_TLS_VERIFY": "1", "DOCKER_CERT_PATH": "/certs"},
			want: &TLSFiles{
				CA:   filepath.Join("/certs", "ca.pem"),
				Cert: filepath.Join("/certs", "cert.pem"),
				Key:  filepath.Join("/certs", "key.pem"),
			},
		},
		{
			name: "verify defaults to ~/.docker",
			env:  map[string]string{"DOCKER_TLS_VERIFY": "1"},
			want: &TLSFiles{
				CA:   filepath.Join(home, ".docker", "ca.pem"),
				Cert: filepath.Join(home, ".docker", "cert.pem"),
				Key:  filepath.Join(home, ".docker", "key.pem"),
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := envTLS(func(k string) string { return c.env[k] }, home)
			switch {
			case got == nil && c.want == nil:
			case got == nil || c.want == nil || *got != *c.want:
				t.Errorf("envTLS() = %+v, want %+v", got, c.want)
			}
		})
	}
}
