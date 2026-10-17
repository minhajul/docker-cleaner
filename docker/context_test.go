package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes data to path, creating its directories.
func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeConfig writes a config.json to configDir naming current as the
// current context.
func writeConfig(t *testing.T, configDir, current string) {
	t.Helper()
	writeFile(t, filepath.Join(configDir, "config.json"), `{"auths":{},"currentContext":"`+current+`"}`)
}

// writeContext stores a context called name whose Docker endpoint is host,
// the way `docker context create` does.
func writeContext(t *testing.T, configDir, name, host string) {
	t.Helper()
	writeFile(t, metaPath(configDir, name),
		`{"Name":"`+name+`","Metadata":{"Description":"test"},"Endpoints":{"docker":{"Host":"`+host+`","SkipTLSVerify":false}}}`)
}

func metaPath(configDir, name string) string {
	return filepath.Join(configDir, "contexts", "meta", contextDirName(name), "meta.json")
}

func tlsDir(configDir, name string) string {
	return filepath.Join(configDir, "contexts", "tls", contextDirName(name), "docker")
}

// fakeEnv returns a getenv that looks variables up in vars.
func fakeEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

const (
	colimaHost   = "unix:///Users/x/.colima/default/docker.sock"
	orbstackHost = "unix:///Users/x/.orbstack/run/docker.sock"
)

func TestResolveEndpoint(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		// setup prepares the home directory; dockerDir is home/.docker.
		setup       func(t *testing.T, home, dockerDir string)
		wantHost    string
		wantSource  EndpointSource
		wantContext string
		wantTLS     bool
		wantErr     []string // substrings the error must contain
	}{
		{
			name: "DOCKER_HOST wins over everything",
			env:  map[string]string{"DOCKER_HOST": "tcp://10.0.0.1:2375", "DOCKER_CONTEXT": "colima"},
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "orbstack")
				writeContext(t, d, "colima", colimaHost)
				writeContext(t, d, "orbstack", orbstackHost)
			},
			wantHost:   "tcp://10.0.0.1:2375",
			wantSource: SourceDockerHost,
		},
		{
			name: "DOCKER_CONTEXT wins over currentContext",
			env:  map[string]string{"DOCKER_CONTEXT": "colima"},
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "orbstack")
				writeContext(t, d, "colima", colimaHost)
				writeContext(t, d, "orbstack", orbstackHost)
			},
			wantHost:    colimaHost,
			wantSource:  SourceContextEnv,
			wantContext: "colima",
		},
		{
			name: "currentContext used when no env",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "orbstack")
				writeContext(t, d, "orbstack", orbstackHost)
			},
			wantHost:    orbstackHost,
			wantSource:  SourceContextConfig,
			wantContext: "orbstack",
		},
		{
			name:       "currentContext default",
			setup:      func(t *testing.T, _, d string) { writeConfig(t, d, "default") },
			wantSource: SourceDefault,
		},
		{
			name: "DOCKER_CONTEXT default overrides currentContext",
			env:  map[string]string{"DOCKER_CONTEXT": "default"},
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "colima")
				writeContext(t, d, "colima", colimaHost)
			},
			wantSource: SourceDefault,
		},
		{
			name:       "no config.json",
			setup:      func(*testing.T, string, string) {},
			wantSource: SourceDefault,
		},
		{
			name: "config.json without currentContext",
			setup: func(t *testing.T, _, d string) {
				writeFile(t, filepath.Join(d, "config.json"), `{"auths":{}}`)
			},
			wantSource: SourceDefault,
		},
		{
			name: "DOCKER_CONFIG respected",
			env:  map[string]string{"DOCKER_CONFIG": "custom"}, // made absolute by the test loop
			setup: func(t *testing.T, home, d string) {
				writeConfig(t, d, "orbstack")
				writeContext(t, d, "orbstack", orbstackHost)
				custom := filepath.Join(home, "custom")
				writeConfig(t, custom, "colima")
				writeContext(t, custom, "colima", colimaHost)
			},
			wantHost:    colimaHost,
			wantSource:  SourceContextConfig,
			wantContext: "colima",
		},
		{
			name:    "missing context",
			env:     map[string]string{"DOCKER_CONTEXT": "colima"},
			setup:   func(*testing.T, string, string) {},
			wantErr: []string{`docker context "colima" not found (looked in `, contextDirName("colima")},
		},
		{
			name: "malformed meta.json",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "colima")
				writeFile(t, metaPath(d, "colima"), `{"Endpoints":`)
			},
			wantErr: []string{`parsing docker context "colima"`},
		},
		{
			name:    "malformed config.json",
			setup:   func(t *testing.T, _, d string) { writeFile(t, filepath.Join(d, "config.json"), `not json`) },
			wantErr: []string{"parsing Docker config"},
		},
		{
			name: "context without a docker endpoint",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "colima")
				writeFile(t, metaPath(d, "colima"), `{"Name":"colima","Endpoints":{}}`)
			},
			wantErr: []string{`docker context "colima" has no Docker endpoint`},
		},
		{
			name: "SkipTLSVerify rejected",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "remote")
				writeFile(t, metaPath(d, "remote"),
					`{"Name":"remote","Endpoints":{"docker":{"Host":"tcp://10.0.0.1:2376","SkipTLSVerify":true}}}`)
			},
			wantErr: []string{`docker context "remote" skips TLS verification`},
		},
		{
			name: "SSH endpoint rejected",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "remote")
				writeContext(t, d, "remote", "ssh://user@example.com")
			},
			wantErr: []string{`docker context "remote" uses an SSH endpoint`},
		},
		{
			name: "TLS material used",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "remote")
				writeContext(t, d, "remote", "tcp://10.0.0.1:2376")
				for _, f := range []string{"ca.pem", "cert.pem", "key.pem"} {
					writeFile(t, filepath.Join(tlsDir(d, "remote"), f), "pem")
				}
			},
			wantHost:    "tcp://10.0.0.1:2376",
			wantSource:  SourceContextConfig,
			wantContext: "remote",
			wantTLS:     true,
		},
		{
			name: "TLS material without CA rejected",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "remote")
				writeContext(t, d, "remote", "tcp://10.0.0.1:2376")
				writeFile(t, filepath.Join(tlsDir(d, "remote"), "cert.pem"), "pem")
				writeFile(t, filepath.Join(tlsDir(d, "remote"), "key.pem"), "pem")
			},
			wantErr: []string{`docker context "remote" has TLS material without a CA certificate`},
		},
		{
			name: "TLS cert without key rejected",
			setup: func(t *testing.T, _, d string) {
				writeConfig(t, d, "remote")
				writeContext(t, d, "remote", "tcp://10.0.0.1:2376")
				writeFile(t, filepath.Join(tlsDir(d, "remote"), "ca.pem"), "pem")
				writeFile(t, filepath.Join(tlsDir(d, "remote"), "cert.pem"), "pem")
			},
			wantErr: []string{`docker context "remote" has incomplete TLS material`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			dockerDir := filepath.Join(home, ".docker")
			c.setup(t, home, dockerDir)

			env := map[string]string{}
			for k, v := range c.env {
				env[k] = v
			}
			if v, ok := env["DOCKER_CONFIG"]; ok {
				env["DOCKER_CONFIG"] = filepath.Join(home, v)
			}
			getenv := fakeEnv(env)
			configDir := ConfigDir(getenv, home)

			got, err := ResolveEndpoint(getenv, configDir)
			if len(c.wantErr) > 0 {
				if err == nil {
					t.Fatalf("ResolveEndpoint() = %+v, want error containing %q", got, c.wantErr)
				}
				for _, want := range c.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveEndpoint() error: %v", err)
			}
			if got.Host != c.wantHost || got.Source != c.wantSource || got.Context != c.wantContext {
				t.Errorf("ResolveEndpoint() = {Host:%q Source:%d Context:%q}, want {Host:%q Source:%d Context:%q}",
					got.Host, got.Source, got.Context, c.wantHost, c.wantSource, c.wantContext)
			}
			if !c.wantTLS {
				if got.TLS != nil {
					t.Errorf("TLS = %+v, want nil", got.TLS)
				}
				return
			}
			dir := tlsDir(configDir, c.wantContext)
			want := TLSFiles{
				CA:   filepath.Join(dir, "ca.pem"),
				Cert: filepath.Join(dir, "cert.pem"),
				Key:  filepath.Join(dir, "key.pem"),
			}
			if got.TLS == nil || *got.TLS != want {
				t.Errorf("TLS = %+v, want %+v", got.TLS, want)
			}
		})
	}
}

// TestContextDirName pins the hashing to the docker CLI's: this is the
// directory `docker context inspect orbstack` reports.
func TestContextDirName(t *testing.T) {
	const want = "2d89b732b01a00a2d1675ed3cee9fd0f965daadf90603c989dd3afd4569c6896"
	if got := contextDirName("orbstack"); got != want {
		t.Errorf("contextDirName(%q) = %q, want %q", "orbstack", got, want)
	}
}

func TestConfigDir(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		home string
		want string
	}{
		{"home", nil, "/home/x", filepath.Join("/home/x", ".docker")},
		{"DOCKER_CONFIG wins", map[string]string{"DOCKER_CONFIG": "/etc/docker-cfg/"}, "/home/x", filepath.Clean("/etc/docker-cfg")},
		{"no home", nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ConfigDir(fakeEnv(c.env), c.home); got != c.want {
				t.Errorf("ConfigDir() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveEndpointNoConfigDir(t *testing.T) {
	_, err := ResolveEndpoint(fakeEnv(map[string]string{"DOCKER_CONTEXT": "colima"}), "")
	if err == nil || !strings.Contains(err.Error(), `docker context "colima" not found`) {
		t.Errorf("ResolveEndpoint() error = %v, want context not found", err)
	}
	ep, err := ResolveEndpoint(fakeEnv(nil), "")
	if err != nil || ep.Source != SourceDefault {
		t.Errorf("ResolveEndpoint() = %+v, %v; want default source", ep, err)
	}
}

func TestEndpointString(t *testing.T) {
	cases := []struct {
		ep   Endpoint
		want string
	}{
		{Endpoint{Host: "tcp://10.0.0.1:2375", Source: SourceDockerHost}, "tcp://10.0.0.1:2375 (DOCKER_HOST)"},
		{Endpoint{Host: colimaHost, Source: SourceContextEnv, Context: "colima"}, colimaHost + ` (context "colima" from DOCKER_CONTEXT)`},
		{Endpoint{Host: colimaHost, Source: SourceContextConfig, Context: "colima"}, colimaHost + ` (context "colima")`},
		{Endpoint{Host: "unix:///var/run/docker.sock", Source: SourceDefault}, "unix:///var/run/docker.sock (default)"},
		{Endpoint{Source: SourceDefault}, "the default socket"},
	}
	for _, c := range cases {
		if got := c.ep.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}
