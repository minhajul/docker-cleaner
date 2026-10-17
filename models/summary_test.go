package models

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"
	mobyclient "github.com/moby/moby/client"
)

// withTotals gives fake volumes, a build cache (3 GB, all of it reclaimable)
// and the per-type totals of a daemon holding them.
func withTotals(fake *dockertest.Fake) *dockertest.Fake {
	fake = withVolumes(withBuildCache(fake, false))
	fake.Usage.Images = mobyclient.ImagesDiskUsage{TotalCount: 12, ActiveCount: 4, TotalSize: 4 << 30, Reclaimable: 1 << 30}
	fake.Usage.Containers = mobyclient.ContainersDiskUsage{TotalCount: 2, ActiveCount: 2, TotalSize: 120 << 20}
	fake.Usage.Volumes.TotalCount, fake.Usage.Volumes.ActiveCount = 3, 1
	fake.Usage.Volumes.TotalSize, fake.Usage.Volumes.Reclaimable = 2<<30, 500<<20
	fake.Usage.BuildCache.TotalSize = 3 << 30
	return fake
}

func TestSummaryLines(t *testing.T) {
	cases := []struct {
		name  string
		width int
		want  []string
	}{
		{"width unknown", 0, []string{
			"Images 4.0 GB (1.0 GB reclaimable) · Containers 120.0 MB · Volumes 2.0 GB (500.0 MB) · Build cache 3.0 GB (3.0 GB)",
		}},
		{"wide", 160, []string{
			"Images 4.0 GB (1.0 GB reclaimable) · Containers 120.0 MB · Volumes 2.0 GB (500.0 MB) · Build cache 3.0 GB (3.0 GB)",
		}},
		{"80 columns", 80, []string{
			"Images 4.0 GB (1.0 GB reclaimable) · Containers 120.0 MB",
			"Volumes 2.0 GB (500.0 MB) · Build cache 3.0 GB (3.0 GB)",
		}},
		// Out of lines: the rest is cut off between segments.
		{"narrow", 40, []string{
			"Images 4.0 GB (1.0 GB reclaimable)",
			"Containers 120.0 MB · Volumes 2.0 GB…",
		}},
		// A segment too wide on its own loses its reclaimable figure.
		{"very narrow", 20, []string{
			"Images 4.0 GB…",
			"Containers 120.0 MB…",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := loaded(t, withTotals(newFake()))
			m = send(t, m, tea.WindowSizeMsg{Width: c.width, Height: 40})

			got := m.summaryLines()
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("summaryLines() =\n%q\nwant\n%q", got, c.want)
			}
			if !strings.Contains(m.View(), strings.Join(c.want, "\n")) {
				t.Errorf("the summary should follow the title, got:\n%s", m.View())
			}
			for _, line := range got {
				if c.width > 0 && lipgloss.Width(line) > c.width {
					t.Errorf("line %q is wider than %d columns", line, c.width)
				}
			}
		})
	}
}

func TestSummaryStates(t *testing.T) {
	cases := []struct {
		name  string
		fetch bool // whether the disk usage fetch has answered
		usage mobyclient.DiskUsageResult
		err   error
		want  []string
	}{
		{name: "not fetched yet"},
		{
			name:  "empty daemon",
			fetch: true,
			want:  []string{"Images 0 B · Containers 0 B · Volumes 0 B · Build cache 0 B"},
		},
		{
			name:  "nothing reclaimable",
			fetch: true,
			usage: mobyclient.DiskUsageResult{
				Images:     mobyclient.ImagesDiskUsage{TotalCount: 3, ActiveCount: 3, TotalSize: 129494866},
				Containers: mobyclient.ContainersDiskUsage{TotalCount: 4, ActiveCount: 4, TotalSize: 397312},
			},
			want: []string{"Images 123.5 MB · Containers 388.0 KB · Volumes 0 B · Build cache 0 B"},
		},
		{
			// The label goes with the first reclaimable figure.
			name:  "only the build cache reclaimable",
			fetch: true,
			usage: mobyclient.DiskUsageResult{
				Images:     mobyclient.ImagesDiskUsage{TotalCount: 3, ActiveCount: 3, TotalSize: 129494866},
				BuildCache: mobyclient.BuildCacheDiskUsage{TotalCount: 1, TotalSize: 40314693, Reclaimable: 40314693},
			},
			want: []string{"Images 123.5 MB · Containers 0 B · Volumes 0 B · Build cache 38.4 MB (38.4 MB reclaimable)"},
		},
		{
			name:  "failed",
			fetch: true,
			usage: mobyclient.DiskUsageResult{Images: mobyclient.ImagesDiskUsage{TotalSize: 1 << 30}},
			err:   errors.New("permission denied"),
			want:  []string{"Disk usage: sizes unavailable"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &dockertest.Fake{Usage: c.usage, DiskUsageErr: c.err}
			m := newModel(fake)
			m = send(t, m, tea.WindowSizeMsg{Width: 160, Height: 40})
			if c.fetch {
				m = send(t, m, docker.FetchDiskUsage(fake)())
			}

			if got := m.summaryLines(); !reflect.DeepEqual(got, c.want) {
				t.Errorf("summaryLines() = %q, want %q", got, c.want)
			}
			if c.want == nil && strings.Contains(m.View(), " · ") {
				t.Errorf("view should have no summary, got:\n%s", m.View())
			}
		})
	}
}

func TestSummaryFollowsRefreshAndCleanup(t *testing.T) {
	fake := withTotals(newFake())
	m := loaded(t, fake)
	m = send(t, m, tea.WindowSizeMsg{Width: 160, Height: 40})

	// Space freed outside docker-cleaner shows after a refresh.
	fake.Usage.Images.TotalSize, fake.Usage.Images.Reclaimable = 3<<30, 0
	m = send(t, m, runes("r"))
	want := "Images 3.0 GB · Containers 120.0 MB · Volumes 2.0 GB (500.0 MB reclaimable) · Build cache 3.0 GB (3.0 GB)"
	if got := strings.Join(m.summaryLines(), "\n"); got != want {
		t.Errorf("after refresh, summary = %q, want %q", got, want)
	}

	// The fake's prune empties the build cache; the cleanup's refresh
	// must pick that up.
	m = toggle(t, m, docker.BuildCacheID)
	m = send(t, m, runes("d"))
	m = send(t, m, runes("y"))
	want = "Images 3.0 GB · Containers 120.0 MB · Volumes 2.0 GB (500.0 MB reclaimable) · Build cache 0 B"
	if got := strings.Join(m.summaryLines(), "\n"); got != want {
		t.Errorf("after cleanup, summary = %q, want %q", got, want)
	}
}

func TestSelectedCounterMatchesDialog(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		want string
	}{
		{"one image", []string{imgA}, "Selected: 1 item · 1.0 GB"},
		// The container's writable layer, not its virtual size.
		{"container and volume", []string{ctrStopped, volData}, "Selected: 2 items · 50.0 MB"},
		// A volume of unknown size counts for nothing.
		{"volume of unknown size", []string{volAnon}, "Selected: 1 item · 0 B"},
		{"build cache", []string{docker.BuildCacheID}, "Selected: 1 item · 3.0 GB"},
		{"every type", []string{imgA, imgB, ctrRunning, ctrStopped, volData, volCache, docker.BuildCacheID}, "Selected: 7 items · 5.6 GB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := loaded(t, withTotals(newFake()), c.ids...)

			if got := m.selectedCounter(); got != c.want {
				t.Errorf("selectedCounter() = %q, want %q", got, c.want)
			}
			if !strings.Contains(m.View(), c.want) {
				t.Errorf("view should show %q, got:\n%s", c.want, m.View())
			}

			// The dialog totals the same selection to the same size,
			// and the counter makes way for it.
			m = send(t, m, runes("d"))
			if m.confirm == nil {
				t.Fatal("d should open the confirmation dialog")
			}
			dialog := fmt.Sprintf("Selected: %s · %s", plural(len(m.confirm.items), "item"), docker.HumanSize(m.confirm.size))
			if dialog != c.want {
				t.Errorf("the dialog totals %q, the counter %q", dialog, c.want)
			}
			if strings.Contains(m.View(), "Selected:") {
				t.Errorf("the counter should be hidden while the dialog is open, got:\n%s", m.View())
			}
		})
	}
}

func TestSelectedCounterHiddenWithoutSelection(t *testing.T) {
	m := loaded(t, withTotals(newFake()))
	if got := m.selectedCounter(); got != "" {
		t.Errorf("selectedCounter() = %q, want empty", got)
	}

	// Selecting and deselecting leaves nothing to count either.
	m = toggle(t, m, imgA)
	m = toggle(t, m, imgA)
	if got := m.selectedCounter(); got != "" {
		t.Errorf("after deselecting, selectedCounter() = %q, want empty", got)
	}
	if strings.Contains(m.View(), "Selected:") {
		t.Errorf("view should have no counter, got:\n%s", m.View())
	}
}

func TestSelectionSummaryMatchesDialog(t *testing.T) {
	m := loaded(t, withTotals(newFake()), imgB, ctrRunning, volCache, docker.BuildCacheID)
	summary := m.selectionSummary()
	m = send(t, m, runes("d"))
	if !reflect.DeepEqual(*m.confirm, summary) {
		t.Errorf("dialog = %+v, want the selection summary %+v", *m.confirm, summary)
	}
}
