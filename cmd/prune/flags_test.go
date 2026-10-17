package prune

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/filter"
)

const (
	typeImage      = docker.CleanupItemTypeImage
	typeContainer  = docker.CleanupItemTypeContainer
	typeVolume     = docker.CleanupItemTypeVolume
	typeNetwork    = docker.CleanupItemTypeNetwork
	typeBuildCache = docker.CleanupItemTypeBuildCache
)

func TestParseTypes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []docker.CleanupItemType
	}{
		// By default, every type a filter narrows, in removal order.
		{"dangling", []string{"--dangling"}, []docker.CleanupItemType{typeImage}},
		{"stopped", []string{"--stopped"}, []docker.CleanupItemType{typeContainer}},
		{"unused", []string{"--unused"}, []docker.CleanupItemType{typeImage, typeVolume, typeNetwork}},
		{"older than", []string{"--older-than", "30d"}, []docker.CleanupItemType{typeContainer, typeImage, typeVolume}},
		{"dangling and stopped", []string{"--dangling", "--stopped"}, []docker.CleanupItemType{typeContainer, typeImage}},
		{"every filter", []string{"--dangling", "--stopped", "--unused", "--older-than=7d"},
			[]docker.CleanupItemType{typeContainer, typeImage, typeVolume, typeNetwork}},

		// --type picks among them, in removal order whatever the order
		// given, and may add the build cache.
		{"type", []string{"--unused", "--type", "volumes"}, []docker.CleanupItemType{typeVolume}},
		{"types", []string{"--unused", "--type=networks,images"}, []docker.CleanupItemType{typeImage, typeNetwork}},
		{"repeated", []string{"--unused", "--type", "networks", "--type", "images,networks"}, []docker.CleanupItemType{typeImage, typeNetwork}},
		{"singular, spaces and case", []string{"--older-than", "1w", "--type", " Container , image"}, []docker.CleanupItemType{typeContainer, typeImage}},
		{"build cache alone", []string{"--type", "build-cache"}, []docker.CleanupItemType{typeBuildCache}},
		{"build cache and a filter", []string{"--dangling", "--type", "images,build-cache"}, []docker.CleanupItemType{typeImage, typeBuildCache}},
		{"build cache with a filter that narrows nothing else", []string{"--dangling", "--type", "build-cache"}, []docker.CleanupItemType{typeBuildCache}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts, code, exit := Parse(c.args, &stdout, &stderr)
			if exit || code != ExitOK {
				t.Fatalf("Parse(%q) = (%d, %v), want (0, false); stderr %q", c.args, code, exit, stderr.String())
			}
			if !slices.Equal(opts.Types, c.want) {
				t.Errorf("Types = %v, want %v", opts.Types, c.want)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("unexpected output: stdout %q, stderr %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestParseOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts, code, exit := Parse([]string{
		"--dangling", "--stopped", "--unused", "--older-than", "2w",
		"--dry-run", "-y", "--force", "--json", "--config", "/tmp/dc.yaml",
	}, &stdout, &stderr)
	if exit || code != ExitOK {
		t.Fatalf("Parse = (%d, %v), want (0, false); stderr %q", code, exit, stderr.String())
	}
	wantFilters := filter.Options{Dangling: true, Stopped: true, Unused: true, OlderThan: 14 * filter.Day}
	if opts.Filters != wantFilters {
		t.Errorf("Filters = %+v, want %+v", opts.Filters, wantFilters)
	}
	if !opts.DryRun || !opts.Yes || !opts.Force || !opts.JSON || opts.ConfigPath != "/tmp/dc.yaml" {
		t.Errorf("opts = %+v, want dry run, yes, force, JSON and the config path", opts)
	}

	opts, _, _ = Parse([]string{"--dangling", "--yes"}, &stdout, &stderr)
	if !opts.Yes || opts.DryRun || opts.Force || opts.JSON || opts.ConfigPath != "" {
		t.Errorf("opts = %+v, want only Yes", opts)
	}
}

func TestParseOlderThan(t *testing.T) {
	cases := map[string]time.Duration{
		"30d":   30 * filter.Day,
		"2w":    14 * filter.Day,
		"12h":   12 * time.Hour,
		"1h30m": 90 * time.Minute,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts, _, exit := Parse([]string{"--older-than", in}, &stdout, &stderr)
			if exit || opts.Filters.OlderThan != want {
				t.Errorf("--older-than %s: OlderThan = %v (exit %v), want %v; stderr %q", in, opts.Filters.OlderThan, exit, want, stderr.String())
			}
		})
	}
}

func TestParseRefuses(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"nothing", nil, "refusing to prune without a filter"},
		{"only flags", []string{"--yes", "--force"}, "refusing to prune without a filter"},
		{"type without filter", []string{"--type", "images"}, "refusing to prune without a filter"},
		{"type and build cache without filter", []string{"--type", "images,build-cache"}, "refusing to prune without a filter"},
		{"type no filter narrows", []string{"--dangling", "--type", "volumes"}, "refusing to prune all volumes: no filter given narrows them.\nAdd --unused or --older-than"},
		{"one of the types not narrowed", []string{"--stopped", "--type", "containers,images"}, "refusing to prune all images"},
		{"networks with older-than", []string{"--older-than", "30d", "--type", "networks"}, "Add --unused, or leave networks out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			_, code, exit := Parse(c.args, &stdout, &stderr)
			if !exit || code != ExitUsage {
				t.Fatalf("Parse(%q) = (%d, %v), want (2, true)", c.args, code, exit)
			}
			if !strings.Contains(stderr.String(), c.wantErr) {
				t.Errorf("stderr = %q, want %q", stderr.String(), c.wantErr)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"unknown flag", []string{"--all"}, "flag provided but not defined: -all"},
		{"argument", []string{"--dangling", "images"}, "unexpected argument: images"},
		{"age without unit", []string{"--older-than", "30"}, `invalid age "30"`},
		{"zero age", []string{"--older-than", "0d"}, `invalid age "0d": must be more than zero`},
		{"negative age", []string{"--older-than", "-1d"}, `invalid age "-1d"`},
		{"fractional days", []string{"--older-than", "1.5d"}, `invalid age "1.5d"`},
		{"age missing", []string{"--older-than"}, "flag needs an argument: -older-than"},
		{"unknown type", []string{"--dangling", "--type", "images,pods"}, `unknown type "pods"`},
		{"empty type", []string{"--dangling", "--type", ""}, `unknown type ""`},
		{"all is not a type", []string{"--dangling", "--type", "all"}, `unknown type "all"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			_, code, exit := Parse(c.args, &stdout, &stderr)
			if !exit || code != ExitUsage {
				t.Fatalf("Parse(%q) = (%d, %v), want (2, true)", c.args, code, exit)
			}
			if !strings.Contains(stderr.String(), c.wantErr) || !strings.Contains(stderr.String(), "Usage:") {
				t.Errorf("stderr = %q, want %q and the usage", stderr.String(), c.wantErr)
			}
		})
	}
}

func TestParseHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			// Help wins over a refusal: no filter is given.
			_, code, exit := Parse([]string{arg}, &stdout, &stderr)
			if !exit || code != ExitOK {
				t.Fatalf("Parse(%q) = (%d, %v), want (0, true)", arg, code, exit)
			}
			for _, want := range []string{"docker-cleaner prune [flags]", "--dangling", "--stopped", "--unused", "--older-than",
				"--type", "build-cache", "--dry-run", "--yes", "--force", "--json", "--config", "Exit status"} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("help lacks %q:\n%s", want, stdout.String())
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}
