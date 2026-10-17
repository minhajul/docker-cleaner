package prune

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/minhajul/docker-cleaner/docker"
)

// planText describes p for a person: a table per type of what is deleted,
// then what is skipped and why. A dry run ends with the total; otherwise the
// confirmation that follows states it.
func planText(p Plan, now time.Time, dryRun bool) string {
	var b strings.Builder
	for _, t := range docker.RemovalOrder() {
		rows := [][]string{{"ID", "NAME", "SIZE", "CREATED", "MATCHED"}}
		for _, e := range p.Delete {
			if e.Type == t {
				rows = append(rows, []string{displayID(e), displayName(e), sizeText(e), ageText(e, now), e.Reason})
			}
		}
		if len(rows) > 1 {
			fmt.Fprintf(&b, "%s (%d)\n", infoOf(t).title, len(rows)-1)
			b.WriteString(table(rows))
			b.WriteString("\n")
		}
	}
	b.WriteString(skippedText(p))

	switch {
	case len(p.Delete) == 0:
		b.WriteString("Nothing to delete.\n")
	case dryRun:
		fmt.Fprintf(&b, "Would delete %s (%s). Dry run: nothing was deleted.\n",
			plural(len(p.Delete), "item"), docker.HumanSize(p.Size()))
	}
	return b.String()
}

// skippedText lists the resources p skips and why, or is empty if none.
func skippedText(p Plan) string {
	if len(p.Skip) == 0 {
		return ""
	}
	rows := [][]string{{"TYPE", "ID", "NAME", "REASON"}}
	for _, e := range p.Skip {
		rows = append(rows, []string{infoOf(e.Type).singular, displayID(e), displayName(e), e.Reason})
	}
	return fmt.Sprintf("Skipped (%d)\n", len(p.Skip)) + table(rows) + "\n"
}

// resultText reports a cleanup of p's resources: a line per resource saying
// whether it was deleted or why it failed, what was skipped, and
// res.Summary(). Unless force was set, it says how many failures a forced
// removal may fix.
func resultText(p Plan, res docker.CleanupResultMsg, force bool) string {
	failures := make(map[key]docker.CleanupFailure, len(res.Failed))
	for _, f := range res.Failed {
		failures[keyOf(f.Item)] = f
	}

	var rows [][]string
	for _, e := range p.Delete {
		if f, ok := failures[keyOf(e.item())]; ok {
			rows = append(rows, []string{"failed", infoOf(e.Type).singular, displayID(e), displayName(e), f.Reason})
		} else {
			rows = append(rows, []string{"deleted", infoOf(e.Type).singular, displayID(e), displayName(e), ""})
		}
	}

	var b strings.Builder
	b.WriteString(table(rows))
	b.WriteString("\n")
	b.WriteString(skippedText(p))
	b.WriteString(res.Summary() + "\n")
	if n := len(res.ForceRetry()); n > 0 && !force {
		why := "they are in use or have several tags; pass --force to remove them"
		if n == 1 {
			why = "it is in use or has several tags; pass --force to remove it"
		}
		fmt.Fprintf(&b, "%s failed because %s.\n", plural(n, "image"), why)
	}
	return b.String()
}

// table aligns rows into columns, each row indented by two spaces.
func table(rows [][]string) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 8, 2, ' ', 0)
	for _, row := range rows {
		_, _ = io.WriteString(tw, "  "+strings.TrimRight(strings.Join(row, "\t"), "\t")+"\n")
	}
	_ = tw.Flush()
	return b.String()
}

// displayID is e's ID as the table shows it: a short ID, or "-" for a
// volume (whose name is its ID) and the build cache.
func displayID(e Entry) string {
	switch e.Type {
	case docker.CleanupItemTypeVolume, docker.CleanupItemTypeBuildCache:
		return "-"
	}
	return shortID(e.ID)
}

// displayName is e's name as the table shows it, with an anonymous volume's
// name shortened.
func displayName(e Entry) string {
	if e.Type == docker.CleanupItemTypeVolume {
		return docker.ShortVolumeName(e.Name)
	}
	return e.Name
}

// sizeText is e's size as the table shows it: "-" for a network, which
// frees no measurable space.
func sizeText(e Entry) string {
	switch {
	case e.Type == docker.CleanupItemTypeNetwork:
		return "-"
	case e.Size < 0:
		return "unknown"
	}
	return docker.HumanSize(e.Size)
}

// ageText is how long before now e was created, or "-" if unknown.
func ageText(e Entry, now time.Time) string {
	if e.Created.IsZero() {
		return "-"
	}
	return docker.HumanAge(e.Created, now)
}

// plural formats a count with its noun, adding an "s" unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// key identifies a resource across a plan and a cleanup result.
type key struct {
	typ docker.CleanupItemType
	id  string
}

// keyOf returns item's key.
func keyOf(item docker.CleanupItem) key {
	return key{item.Type, item.ID}
}

// Report is the JSON object --json prints, for a dry run and a real run
// alike.
type Report struct {
	// DryRun is set for --dry-run, when nothing is deleted.
	DryRun bool `json:"dry_run"`
	// Filters names the active filters, e.g. ["dangling", "older than 7d"].
	Filters []string `json:"filters"`
	// Types are the types pruned, as --type names them.
	Types []string `json:"types"`
	// Planned lists the resources to delete; Deleted and Failed, those
	// that were and were not (with Reason saying why); both are empty for
	// a dry run. Skipped lists the resources kept, with Reason saying why.
	Planned []ReportEntry `json:"planned"`
	Deleted []ReportEntry `json:"deleted"`
	Failed  []ReportEntry `json:"failed"`
	Skipped []ReportEntry `json:"skipped"`
	Totals  ReportTotals  `json:"totals"`
}

// ReportEntry is one resource in a Report.
type ReportEntry struct {
	// ID is an image, container or network ID, a volume's name, or
	// "<build cache>".
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type is "image", "container", "volume", "network" or "build-cache".
	Type string `json:"type"`
	// Size is in bytes; null if unknown. For deleted build cache it is the
	// space the prune freed.
	Size *int64 `json:"size"`
	// Created is left out if unknown.
	Created time.Time `json:"created,omitzero"`
	// Reason says why the resource matched, was skipped or failed.
	Reason string `json:"reason"`
	// Forceable marks a failure that --force may fix.
	Forceable bool `json:"forceable,omitempty"`
}

// ReportTotals sums up a Report. Sizes are in bytes.
type ReportTotals struct {
	Planned             int   `json:"planned"`
	PlannedSize         int64 `json:"planned_size"`
	Deleted             int   `json:"deleted"`
	DeletedSize         int64 `json:"deleted_size"`
	Failed              int   `json:"failed"`
	Skipped             int   `json:"skipped"`
	BuildCacheReclaimed int64 `json:"build_cache_reclaimed"`
}

// newReport builds the Report of p, pruned with opts; res is the cleanup's
// result, or nil if nothing was deleted (a dry run, or nothing to delete).
func newReport(opts Options, p Plan, res *docker.CleanupResultMsg) Report {
	r := Report{
		DryRun:  opts.DryRun,
		Filters: append([]string{}, opts.Filters.Labels()...),
		Types:   []string{},
		Planned: reportEntries(p.Delete),
		Deleted: []ReportEntry{},
		Failed:  []ReportEntry{},
		Skipped: reportEntries(p.Skip),
	}
	for _, t := range opts.Types {
		r.Types = append(r.Types, infoOf(t).plural)
	}
	r.Totals.Planned = len(p.Delete)
	r.Totals.PlannedSize = p.Size()
	r.Totals.Skipped = len(p.Skip)
	if res == nil {
		return r
	}

	failures := make(map[key]docker.CleanupFailure, len(res.Failed))
	for _, f := range res.Failed {
		failures[keyOf(f.Item)] = f
	}
	for _, e := range p.Delete {
		if f, ok := failures[keyOf(e.item())]; ok {
			re := reportEntry(e)
			re.Reason, re.Forceable = f.Reason, f.Forceable
			r.Failed = append(r.Failed, re)
			continue
		}
		if e.Type == docker.CleanupItemTypeBuildCache {
			e.Size = res.BuildCacheReclaimed
		}
		r.Deleted = append(r.Deleted, reportEntry(e))
		r.Totals.DeletedSize += max(e.Size, 0)
	}
	r.Totals.Deleted = len(r.Deleted)
	r.Totals.Failed = len(r.Failed)
	r.Totals.BuildCacheReclaimed = res.BuildCacheReclaimed
	return r
}

// reportEntries converts entries for a Report; it is never nil, so an empty
// list is [] in the JSON.
func reportEntries(entries []Entry) []ReportEntry {
	out := make([]ReportEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, reportEntry(e))
	}
	return out
}

// reportEntry converts e for a Report.
func reportEntry(e Entry) ReportEntry {
	re := ReportEntry{ID: e.ID, Name: e.Name, Type: infoOf(e.Type).singular, Reason: e.Reason}
	if e.Size >= 0 {
		size := e.Size
		re.Size = &size
	}
	if !e.Created.IsZero() {
		re.Created = e.Created.UTC()
	}
	return re
}

// writeJSON writes r to w as indented JSON.
func writeJSON(w io.Writer, r Report) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
}
