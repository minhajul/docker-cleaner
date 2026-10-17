package prune

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/filter"
)

// Exit codes of the prune command.
const (
	// ExitOK means everything planned was deleted, or there was nothing to
	// delete, or the user declined.
	ExitOK = 0
	// ExitFailure means a removal failed, or Docker or the config file
	// could not be read.
	ExitFailure = 1
	// ExitUsage means the command line was wrong, or the command refused
	// to run as given.
	ExitUsage = 2
)

const usageText = `docker-cleaner prune - delete the Docker resources that match filters, without the UI.

Usage:
  docker-cleaner prune [flags]

For scripts, cron jobs and CI. It never deletes protected resources (the
built-in system rules and the config file's protect-list; see
docker-cleaner --help) or Docker's predefined networks.

Filters (at least one is needed, unless --type is just build-cache):
      --dangling          Untagged images
      --stopped           Stopped containers (exited, created or dead)
      --unused            Images, volumes and networks no container uses,
                          running or stopped
      --older-than <age>  Images, containers and volumes created more than
                          age ago, e.g. 30d, 2w, 12h
  Each filter narrows only the types it names; filters combine with AND.

Flags:
      --type <types>      Comma-separated types to prune: images, containers,
                          volumes, networks, build-cache. Default: every type
                          a filter narrows, except volumes, which hold data
                          and must be named. Every type named, except
                          build-cache, needs a filter that narrows it.
                          build-cache prunes all unused build cache, filters
                          aside, and is never included by default.
      --dry-run           List what would be deleted, and what is skipped and
                          why, then exit without deleting anything
  -y, --yes               Delete without asking. Without it (or --dry-run),
                          prune asks on a terminal and refuses otherwise.
      --force             Also remove running containers, and images that
                          containers use or that have several tags; without
                          it they are skipped or fail
      --json              Print a single JSON object instead of text
      --config <path>     Read the config file at path instead of the default
  -h, --help              Show this help and exit

Exit status: 0 on success or when there is nothing to delete, 1 if a removal
failed or Docker could not be reached, 2 on a usage error or refusal.

Examples:
  docker-cleaner prune --dangling --dry-run
  docker-cleaner prune --dangling --older-than 7d --yes
  docker-cleaner prune --stopped --unused --older-than 30d --yes --json
  docker-cleaner prune --type build-cache --yes
`

// Options are the settings taken from the prune command line.
type Options struct {
	// Filters select the resources to delete within each type.
	Filters filter.Options
	// Types are the resource types to prune, in docker.RemovalOrder and
	// without duplicates: those --type names, or by default every type
	// Filters narrows.
	Types []docker.CleanupItemType
	// DryRun lists the plan without deleting anything.
	DryRun bool
	// Yes deletes without asking.
	Yes bool
	// Force removes running containers and images in use or with several
	// tags (see docker.CleanupItem.Force).
	Force bool
	// JSON prints a single JSON object instead of text.
	JSON bool
	// ConfigPath is the config file named by --config; empty for the
	// default location.
	ConfigPath string
}

// typeInfo names a resource type.
type typeInfo struct {
	typ docker.CleanupItemType
	// plural is the name --type takes, e.g. "images"; singular, what JSON
	// calls one resource of the type, e.g. "image". --type takes either.
	plural, singular string
	// title heads the type's section in the text output.
	title string
}

// typeInfos name every resource type, in docker.RemovalOrder.
var typeInfos = []typeInfo{
	{docker.CleanupItemTypeContainer, "containers", "container", "Containers"},
	{docker.CleanupItemTypeImage, "images", "image", "Images"},
	{docker.CleanupItemTypeVolume, "volumes", "volume", "Volumes"},
	{docker.CleanupItemTypeNetwork, "networks", "network", "Networks"},
	{docker.CleanupItemTypeBuildCache, "build-cache", "build-cache", "Build cache"},
}

// infoOf returns the names of type t.
func infoOf(t docker.CleanupItemType) typeInfo {
	if i := slices.IndexFunc(typeInfos, func(ti typeInfo) bool { return ti.typ == t }); i >= 0 {
		return typeInfos[i]
	}
	name := fmt.Sprintf("type %d", t)
	return typeInfo{t, name, name, name}
}

// parseTypes parses a --type value: comma-separated type names, plural or
// singular, with optional spaces.
func parseTypes(s string) ([]docker.CleanupItemType, error) {
	var types []docker.CleanupItemType
	for name := range strings.SplitSeq(s, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		i := slices.IndexFunc(typeInfos, func(ti typeInfo) bool {
			return name == ti.plural || name == ti.singular
		})
		if i < 0 {
			return nil, fmt.Errorf("unknown type %q; want images, containers, volumes, networks or build-cache", name)
		}
		types = append(types, typeInfos[i].typ)
	}
	return types, nil
}

// filterFlags are the filter flags, each with the Options it sets, so that
// what a flag narrows is always asked of filter.Options.Narrows.
var filterFlags = []struct {
	flag string
	opts filter.Options
}{
	{"--dangling", filter.Options{Dangling: true}},
	{"--stopped", filter.Options{Stopped: true}},
	{"--unused", filter.Options{Unused: true}},
	{"--older-than", filter.Options{OlderThan: filter.Day}},
}

// only returns the part of f that narrows resources of type t: the filters
// that apply to t.
func only(f filter.Options, t docker.CleanupItemType) filter.Options {
	var out filter.Options
	if f.Dangling && (filter.Options{Dangling: true}).Narrows(t) {
		out.Dangling = true
	}
	if f.Stopped && (filter.Options{Stopped: true}).Narrows(t) {
		out.Stopped = true
	}
	if f.Unused && (filter.Options{Unused: true}).Narrows(t) {
		out.Unused = true
	}
	if f.OlderThan > 0 && (filter.Options{OlderThan: f.OlderThan}).Narrows(t) {
		out.OlderThan = f.OlderThan
	}
	return out
}

// narrowingFlags lists the filter flags that narrow type t, e.g.
// "--unused or --older-than".
func narrowingFlags(t docker.CleanupItemType) string {
	var flags []string
	for _, ff := range filterFlags {
		if ff.opts.Narrows(t) {
			flags = append(flags, ff.flag)
		}
	}
	if len(flags) <= 1 {
		return strings.Join(flags, "")
	}
	return strings.Join(flags[:len(flags)-1], ", ") + " or " + flags[len(flags)-1]
}

// errNoFilter is the refusal to prune without any filter.
var errNoFilter = errors.New(`refusing to prune without a filter, which would delete everything.
Pass --dangling, --stopped, --unused or --older-than, or --type build-cache to prune only the build cache`)

// resolveTypes returns the types to prune: explicit, the types --type named
// (nil if it was not given), or else every type f narrows except volumes,
// which hold data and, as with docker system prune --volumes, are pruned
// only when named. It refuses to
// prune a type, other than the build cache, that no filter narrows: every
// resource of it would be deleted.
func resolveTypes(f filter.Options, explicit []docker.CleanupItemType) ([]docker.CleanupItemType, error) {
	var types []docker.CleanupItemType
	for _, t := range docker.RemovalOrder() {
		switch {
		case explicit == nil && f.Narrows(t) && t != docker.CleanupItemTypeVolume:
			types = append(types, t)
		case !slices.Contains(explicit, t):
		case t == docker.CleanupItemTypeBuildCache || f.Narrows(t):
			types = append(types, t)
		case !f.Active():
			return nil, errNoFilter
		default:
			name := infoOf(t).plural
			return nil, fmt.Errorf("refusing to prune all %s: no filter given narrows them.\nAdd %s, or leave %s out of --type", name, narrowingFlags(t), name)
		}
	}
	if len(types) == 0 {
		return nil, errNoFilter
	}
	return types, nil
}

// Parse handles the prune command line, args being the arguments after
// "prune". It returns the options to run with, or reports that the program
// should exit instead, and with which exit code. Besides malformed flags, it
// refuses (with ExitUsage) a command line that would prune a type no filter
// narrows (see resolveTypes).
func Parse(args []string, stdout, stderr io.Writer) (opts Options, code int, exit bool) {
	fs := flag.NewFlagSet("docker-cleaner prune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), usageText) }

	var showHelp bool
	var explicit []docker.CleanupItemType
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	fs.BoolVar(&opts.Filters.Dangling, "dangling", false, "")
	fs.BoolVar(&opts.Filters.Stopped, "stopped", false, "")
	fs.BoolVar(&opts.Filters.Unused, "unused", false, "")
	fs.Func("older-than", "", func(s string) error {
		d, err := filter.ParseAge(s)
		opts.Filters.OlderThan = d
		return err
	})
	fs.Func("type", "", func(s string) error {
		types, err := parseTypes(s)
		explicit = append(explicit, types...)
		return err
	})
	fs.BoolVar(&opts.DryRun, "dry-run", false, "")
	fs.BoolVar(&opts.Yes, "y", false, "")
	fs.BoolVar(&opts.Yes, "yes", false, "")
	fs.BoolVar(&opts.Force, "force", false, "")
	fs.BoolVar(&opts.JSON, "json", false, "")
	fs.StringVar(&opts.ConfigPath, "config", "", "")

	if err := fs.Parse(args); err != nil {
		return Options{}, ExitUsage, true
	}

	switch {
	case showHelp:
		_, _ = fmt.Fprint(stdout, usageText)
		return Options{}, ExitOK, true
	case fs.NArg() > 0:
		_, _ = fmt.Fprintf(stderr, "unexpected argument: %s\n", fs.Arg(0))
		fs.Usage()
		return Options{}, ExitUsage, true
	}

	types, err := resolveTypes(opts.Filters, explicit)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "docker-cleaner prune: %v\nRun 'docker-cleaner prune --help' for the flags.\n", err)
		return Options{}, ExitUsage, true
	}
	opts.Types = types
	return opts, ExitOK, false
}
