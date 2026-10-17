package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/minhajul/docker-cleaner/models"
	"github.com/minhajul/docker-cleaner/styles"

	"github.com/charmbracelet/bubbletea"
)

// Build information, set at build time with
// -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

const usageText = `docker-cleaner - remove Docker images, containers, volumes, networks and build cache from a terminal UI.

Usage:
  docker-cleaner [flags]

Flags:
      --config <path>  Read the config file at path instead of the default
  -h, --help           Show this help and exit
  -v, --version        Print the version and exit

Config:
  An optional YAML file lists resources to protect from deletion. It is read
  from $XDG_CONFIG_HOME/docker-cleaner/config.yaml, or
  ~/.config/docker-cleaner/config.yaml if XDG_CONFIG_HOME is unset:

    protect:
      - "postgres:*"   # image tags, image repositories
      - "my-db"        # and container names; * matches anything

Keys:
  ↑/k, ↓/j      Move the cursor
  space/enter   Select an item or a whole section
  a / n         Select all / none
  r             Refresh the lists
  d             Delete selected (asks for confirmation)
  p             Show or hide protected items
  /             Search by name (enter keeps it, esc clears it)
  D / S / U     Show only dangling images / stopped containers / unused items
  O             Show only items older than 7, 30, 90 days, then all again
  esc           Clear filters and search
  ?             Show all keys
  q, ctrl+c     Quit
`

func main() {
	opts, code, exit := parseFlags(os.Args[1:], os.Stdout, os.Stderr)
	if exit {
		os.Exit(code)
	}

	m, err := models.InitialModel(opts.configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, styles.ErrorMessage.Render(fmt.Sprintf("Could not start: %v", err)))
		os.Exit(1)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		msg := styles.ErrorMessage.Render(fmt.Sprintf("There's been an error: %v", err))
		log.Fatalf("%s", msg)
	}
}

// options are the settings taken from the command line.
type options struct {
	// configPath is the config file named by --config; empty for the
	// default location.
	configPath string
}

// parseFlags handles the command-line flags. It returns the options to start
// the TUI with, or reports that the program should exit instead, and with
// which exit code.
func parseFlags(args []string, stdout, stderr io.Writer) (opts options, code int, exit bool) {
	fs := flag.NewFlagSet("docker-cleaner", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), usageText) }

	var showHelp, showVersion bool
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")
	fs.BoolVar(&showVersion, "v", false, "")
	fs.BoolVar(&showVersion, "version", false, "")
	fs.StringVar(&opts.configPath, "config", "", "")

	if err := fs.Parse(args); err != nil {
		return options{}, 2, true
	}

	switch {
	case showHelp:
		_, _ = fmt.Fprint(stdout, usageText)
		return options{}, 0, true
	case showVersion:
		_, _ = fmt.Fprintln(stdout, versionString())
		return options{}, 0, true
	case fs.NArg() > 0:
		_, _ = fmt.Fprintf(stderr, "unexpected argument: %s\n", fs.Arg(0))
		fs.Usage()
		return options{}, 2, true
	}
	return opts, 0, false
}

// versionString describes the build, leaving out whatever is unknown, e.g.
// "docker-cleaner v0.1.0 (commit abc1234, built 2026-10-17)".
func versionString() string {
	v := version
	if isRelease(v) {
		v = "v" + v
	}

	var details []string
	if commit != "" {
		details = append(details, "commit "+commit)
	}
	if date != "" {
		d := date
		if t, err := time.Parse(time.RFC3339, d); err == nil {
			d = t.UTC().Format("2006-01-02")
		}
		details = append(details, "built "+d)
	}

	s := "docker-cleaner " + v
	if len(details) > 0 {
		s += " (" + strings.Join(details, ", ") + ")"
	}
	return s
}

// isRelease reports whether v looks like a version number without its "v"
// prefix (as GoReleaser passes it), e.g. "0.1.0" but not "dev" or a commit hash.
func isRelease(v string) bool {
	major, _, ok := strings.Cut(v, ".")
	if !ok || major == "" {
		return false
	}
	for _, r := range major {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
