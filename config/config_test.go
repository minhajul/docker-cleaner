package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeConfig writes content to dir/docker-cleaner/config.yaml and returns
// the file's path.
func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "docker-cleaner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		name string
		xdg  string
		want string
	}{
		{"XDG_CONFIG_HOME set", "/xdg", "/xdg/docker-cleaner/config.yaml"},
		{"XDG_CONFIG_HOME unset", "", filepath.Join(home, ".config", "docker-cleaner", "config.yaml")},
		{"XDG_CONFIG_HOME relative", "rel", filepath.Join(home, ".config", "docker-cleaner", "config.yaml")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", c.xdg)
			got, err := DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("DefaultPath() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLoadDefaultLocations(t *testing.T) {
	const content = "protect:\n  - \"postgres:*\"\n"
	cases := []struct {
		name  string
		setup func(t *testing.T) // sets HOME and XDG_CONFIG_HOME, writes the file
		want  []string
	}{
		{"missing file", func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", "")
		}, nil},
		{"under XDG_CONFIG_HOME", func(t *testing.T) {
			xdg := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", xdg)
			writeConfig(t, xdg, content)
		}, []string{"postgres:*"}},
		{"under ~/.config", func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			writeConfig(t, filepath.Join(home, ".config"), content)
		}, []string{"postgres:*"}},
		{"XDG_CONFIG_HOME wins over ~/.config", func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			writeConfig(t, filepath.Join(home, ".config"), content)
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.setup(t)
			cfg, err := Load("")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !slices.Equal(cfg.Protect, c.want) {
				t.Errorf("Protect = %q, want %q", cfg.Protect, c.want)
			}
		})
	}
}

func TestLoadExplicitPath(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `
# Resources matching any pattern are hidden and can't be deleted.
protect:
  - "postgres:*"        # image repo:tag glob
  - "my-db"             # container name
  - "prod-*"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"postgres:*", "my-db", "prod-*"}; !slices.Equal(cfg.Protect, want) {
		t.Errorf("Protect = %q, want %q", cfg.Protect, want)
	}
}

func TestLoadExplicitPathMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Load(%q) = %v, want an error naming the file", path, err)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	for name, content := range map[string]string{
		"empty":         "",
		"comments only": "# nothing here yet\n",
		"no patterns":   "protect: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, t.TempDir(), content))
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Protect) != 0 {
				t.Errorf("Protect = %q, want none", cfg.Protect)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string // in the error, besides the path
	}{
		{"malformed YAML", "protect: [\"a\"\n", "yaml:"},
		{"unknown key", "protcet:\n  - a\n", "field protcet not found"},
		{"not a list", "protect: postgres\n", "cannot unmarshal"},
		{"bad pattern", "protect:\n  - ok\n  - \"db-[0-9\"\n", `protect[1]: invalid pattern "db-[0-9"`},
		{"empty pattern", "protect:\n  - \"\"\n", "protect[0]: empty pattern"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeConfig(t, t.TempDir(), c.content)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			for _, want := range []string{path, c.want} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}
