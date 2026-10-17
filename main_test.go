package main

import (
	"bytes"
	"strings"
	"testing"
)

// setBuildInfo overrides the ldflags variables for the duration of a test.
func setBuildInfo(t *testing.T, v, c, d string) {
	t.Helper()
	oldV, oldC, oldD := version, commit, date
	version, commit, date = v, c, d
	t.Cleanup(func() { version, commit, date = oldV, oldC, oldD })
}

func TestParseFlagsVersion(t *testing.T) {
	setBuildInfo(t, "0.1.0", "abc1234", "2026-10-17T09:30:00Z")
	for _, arg := range []string{"--version", "-version", "-v", "--v"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, exit := parseFlags([]string{arg}, &stdout, &stderr)
			if !exit || code != 0 {
				t.Fatalf("parseFlags(%q) = (%d, %v), want (0, true)", arg, code, exit)
			}
			want := "docker-cleaner v0.1.0 (commit abc1234, built 2026-10-17)\n"
			if got := stdout.String(); got != want {
				t.Errorf("stdout = %q, want %q", got, want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	tests := []struct {
		name, version, commit, date, want string
	}{
		{"defaults", "dev", "", "", "docker-cleaner dev"},
		{"release", "1.2.3", "", "", "docker-cleaner v1.2.3"},
		{"tag", "v1.2.3", "abc1234", "", "docker-cleaner v1.2.3 (commit abc1234)"},
		{"describe", "v1.2.3-4-gabc1234-dirty", "abc1234", "", "docker-cleaner v1.2.3-4-gabc1234-dirty (commit abc1234)"},
		{"hash only", "1234abc", "1234abc", "", "docker-cleaner 1234abc (commit 1234abc)"},
		{"date only", "dev", "", "2026-10-17T23:59:59Z", "docker-cleaner dev (built 2026-10-17)"},
		{"odd date", "dev", "", "yesterday", "docker-cleaner dev (built yesterday)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBuildInfo(t, tt.version, tt.commit, tt.date)
			if got := versionString(); got != tt.want {
				t.Errorf("versionString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseFlagsHelp(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "-help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, exit := parseFlags([]string{arg}, &stdout, &stderr)
			if !exit || code != 0 {
				t.Fatalf("parseFlags(%q) = (%d, %v), want (0, true)", arg, code, exit)
			}
			for _, want := range []string{"Usage:", "--version", "--help", "Keys:", "Quit"} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("help output lacks %q:\n%s", want, stdout.String())
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestParseFlagsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"unknown flag", []string{"--nope"}, "flag provided but not defined: -nope"},
		{"argument", []string{"images"}, "unexpected argument: images"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, exit := parseFlags(tt.args, &stdout, &stderr)
			if !exit || code != 2 {
				t.Fatalf("parseFlags(%q) = (%d, %v), want (2, true)", tt.args, code, exit)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantErr) || !strings.Contains(stderr.String(), "Usage:") {
				t.Errorf("stderr = %q, want %q and the usage", stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestParseFlagsNone(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code, exit := parseFlags(nil, &stdout, &stderr); exit || code != 0 {
		t.Fatalf("parseFlags(nil) = (%d, %v), want (0, false)", code, exit)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("unexpected output: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}
