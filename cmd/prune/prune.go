// Package prune implements `docker-cleaner prune`, which deletes the Docker
// resources that filters match without the terminal UI, for scripts, cron
// jobs and CI. It selects resources with the filter package, as the TUI's
// filters do, and protects exactly what the TUI protects.
//
// Since it deletes unattended, it refuses to run without a filter (except to
// prune the build cache alone), refuses to prune a type no filter narrows,
// and, unless --yes or --dry-run is given, asks before deleting, or refuses
// when there is no terminal to ask on.
package prune

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/minhajul/docker-cleaner/config"
	"github.com/minhajul/docker-cleaner/docker"
)

// Env is what Run needs from the process around it.
type Env struct {
	// Stdin is where the answer to the confirmation is read from.
	Stdin io.Reader
	// Stdout receives the plan and the results, or with --json the JSON
	// object alone; Stderr receives errors, warnings and, with --json,
	// the confirmation.
	Stdout, Stderr io.Writer
	// Interactive reports whether Stdin is a terminal a person can answer
	// the confirmation on.
	Interactive bool
	// Now is the clock ages are measured from; nil means time.Now.
	Now func() time.Time
}

// Main runs the prune command with args, the arguments after "prune", and
// returns the exit code. It loads the config file, connects to Docker as the
// TUI does (see docker.NewClient) and hands over to Run.
func Main(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	opts, code, exit := Parse(args, stdout, stderr)
	if exit {
		return code
	}
	env := Env{
		Stdin:       stdin,
		Stdout:      stdout,
		Stderr:      stderr,
		Interactive: term.IsTerminal(stdin.Fd()),
		Now:         time.Now,
	}
	// Refuse before connecting, so the refusal is not hidden behind a
	// connection error. Run checks again for its other callers.
	if err := checkUnattended(opts, env.Interactive); err != nil {
		errorf(stderr, "%v", err)
		return ExitUsage
	}

	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		errorf(stderr, "%v", err)
		return ExitFailure
	}
	protector, err := docker.NewProtector(cfg.Protect)
	if err != nil {
		errorf(stderr, "%v", err)
		return ExitFailure
	}

	cli, endpoint, err := docker.NewClient()
	if err != nil {
		errorf(stderr, "unable to connect to Docker: %v", err)
		return ExitFailure
	}
	if err := cli.Ping(); err != nil {
		errorf(stderr, "cannot reach Docker at %s. Is it running? (%v)", endpoint, err)
		return ExitFailure
	}
	return Run(context.Background(), cli, protector, opts, env)
}

// Run prunes what opts selects through cli and returns the exit code: it
// lists the resources, plans what to delete (see buildPlan), and then prints
// the plan (--dry-run), or asks for confirmation unless opts.Yes is set and
// deletes the planned resources, printing the result. protector says which
// resources are protected.
func Run(ctx context.Context, cli docker.API, protector docker.Protector, opts Options, env Env) int {
	if err := checkUnattended(opts, env.Interactive); err != nil {
		errorf(env.Stderr, "%v", err)
		return ExitUsage
	}
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	start := now()

	plan, err := buildPlan(ctx, cli, protector, opts, start)
	if err != nil {
		errorf(env.Stderr, "%v", err)
		return ExitFailure
	}
	for _, w := range plan.Warnings {
		errorf(env.Stderr, "warning: %s", w)
	}

	// Text for a person goes to stdout, unless stdout carries the JSON.
	human := env.Stdout
	if opts.JSON {
		human = env.Stderr
	}

	if opts.DryRun || len(plan.Delete) == 0 {
		if opts.JSON {
			writeJSON(env.Stdout, newReport(opts, plan, nil))
		} else {
			write(env.Stdout, planText(plan, start, opts.DryRun))
		}
		return ExitOK
	}

	if !opts.Yes {
		write(human, planText(plan, start, false))
		if !confirm(env.Stdin, human, plan) {
			write(human, "Nothing was deleted.\n")
			return ExitOK
		}
	}

	items := make([]docker.CleanupItem, len(plan.Delete))
	for i, e := range plan.Delete {
		items[i] = e.item()
	}
	res, ok := docker.PerformCleanup(cli, items)().(docker.CleanupResultMsg)
	if !ok {
		errorf(env.Stderr, "cleanup returned no result")
		return ExitFailure
	}

	if opts.JSON {
		writeJSON(env.Stdout, newReport(opts, plan, &res))
	} else {
		write(env.Stdout, resultText(plan, res, opts.Force))
	}
	if len(res.Failed) > 0 {
		return ExitFailure
	}
	return ExitOK
}

// checkUnattended refuses to delete when nobody asked for it to happen
// without confirmation (--yes), it is not a dry run, and there is no
// terminal to ask on.
func checkUnattended(opts Options, interactive bool) error {
	if opts.Yes || opts.DryRun || interactive {
		return nil
	}
	return errors.New("refusing to delete without confirmation: stdin is not a terminal.\nPass --yes to delete, or --dry-run to only list what would be deleted")
}

// confirm asks on out whether to delete what p plans, and reads the answer
// from in. Only "y" or "yes", in any case, is a yes; so is no answer at all
// (end of input) a no.
func confirm(in io.Reader, out io.Writer, p Plan) bool {
	write(out, fmt.Sprintf("Delete %s (%s)? [y/N] ", plural(len(p.Delete), "item"), docker.HumanSize(p.Size())))
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		write(out, "\n")
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// write writes s to w. Output that cannot be written (say, to a closed
// pipe) changes neither what was deleted nor the exit code, so errors are
// ignored.
func write(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
}

// errorf writes a message to stderr, prefixed with the command's name.
func errorf(stderr io.Writer, format string, a ...any) {
	write(stderr, "docker-cleaner prune: "+fmt.Sprintf(format, a...)+"\n")
}
