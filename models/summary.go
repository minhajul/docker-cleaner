package models

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/minhajul/docker-cleaner/docker"
)

// summaryMaxLines caps the disk usage summary, so the list keeps its room.
const summaryMaxLines = 2

// summaryLines returns the disk usage summary shown under the title: one
// segment per type of resource, as `docker system df` has one row per type,
// e.g. "Images 4.2 GB (1.1 GB reclaimable) · Containers 120.0 MB · Volumes
// 2.0 GB (500.0 MB) · Build cache 1.0 GB (1.0 GB)". A segment gives the
// type's size and its reclaimable space unless there is none; the first
// reclaimable figure is labelled. The segments wrap to the terminal width in
// at most summaryMaxLines lines. There is no summary until the disk usage
// arrives, and only "sizes unavailable" if it could not be fetched.
func (m Model) summaryLines() []string {
	if !m.diskUsageFetched {
		return nil
	}
	if m.diskUsageErr != nil {
		return wrapSegments([]string{"Disk usage: sizes unavailable"}, "", m.width, summaryMaxLines)
	}

	t := m.diskUsage.Totals
	labelled := false
	var segs []string
	for _, row := range []struct {
		name  string
		usage docker.TypeUsage
	}{
		{"Images", t.Images},
		{"Containers", t.Containers},
		{"Volumes", t.Volumes},
		{"Build cache", t.BuildCache},
	} {
		seg := row.name + " " + docker.HumanSize(row.usage.Size)
		if row.usage.Reclaimable > 0 {
			reclaimable := docker.HumanSize(row.usage.Reclaimable)
			if !labelled {
				reclaimable += " reclaimable"
				labelled = true
			}
			seg += " (" + reclaimable + ")"
		}
		segs = append(segs, seg)
	}
	return wrapSegments(segs, " · ", m.width, summaryMaxLines)
}

// selectedCounter is the footer's live count of the selection with the space
// deleting it frees, e.g. "Selected: 3 items · 2.3 GB", or "" when nothing is
// selected. It comes from selectionSummary, so it always matches the
// confirmation dialog.
func (m Model) selectedCounter() string {
	if len(m.selected) == 0 {
		return ""
	}
	p := m.selectionSummary()
	if len(p.items) == 0 {
		return ""
	}
	return fmt.Sprintf("Selected: %s · %s", plural(len(p.items), "item"), docker.HumanSize(p.size))
}

// wrapSegments joins segs with sep into lines at most width columns wide,
// breaking only between segments. Past maxLines lines the rest goes on the
// last line, which is then cut short with "…", as is a lone segment wider
// than width. A width of 0 or less (the terminal size is not known yet) puts
// everything on one line.
func wrapSegments(segs []string, sep string, width, maxLines int) []string {
	if len(segs) == 0 {
		return nil
	}
	if width <= 0 {
		return []string{strings.Join(segs, sep)}
	}

	var lines []string
	line := segs[0]
	for i := 1; i < len(segs); i++ {
		if lipgloss.Width(line+sep+segs[i]) <= width {
			line += sep + segs[i]
			continue
		}
		if len(lines) == maxLines-1 {
			// No line left: the rest is cut off below.
			line += sep + strings.Join(segs[i:], sep)
			break
		}
		lines = append(lines, line)
		line = segs[i]
	}
	lines = append(lines, line)

	for i := range lines {
		lines[i] = truncate(lines[i], width)
	}
	return lines
}

// truncate shortens s to at most width columns, ending it with "…" if
// anything was cut. It cuts before a " · " or " (" where it can, so that no
// size is split from its unit, and anywhere otherwise.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return strings.Repeat("…", max(width, 0))
	}

	clean := -1 // the last cut before " · " or " (" that fits
	cut := 0    // the last cut that fits
	for i := range s {
		if lipgloss.Width(s[:i]) > width-1 {
			break
		}
		cut = i
		if strings.HasPrefix(s[i:], " · ") || strings.HasPrefix(s[i:], " (") {
			clean = i
		}
	}
	if clean >= 0 {
		cut = clean
	}
	return strings.TrimRight(s[:cut], " ") + "…"
}
