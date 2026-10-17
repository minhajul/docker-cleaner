package prune

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	client "github.com/fsouza/go-dockerclient"
	"github.com/minhajul/docker-cleaner/docker"
	"github.com/minhajul/docker-cleaner/docker/dockertest"
	"github.com/minhajul/docker-cleaner/filter"
	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/volume"
	mobyclient "github.com/moby/moby/client"
)

// now is the fixed time the tests measure ages from.
var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// daysAgo is the Unix time n days before now.
func daysAgo(n int) int64 {
	return now.Add(-time.Duration(n) * filter.Day).Unix()
}

// id64 returns a 64-character ID made of s repeated.
func id64(s string) string {
	return strings.Repeat(s, 64/len(s))
}

const mb = 1 << 20

// The resources of world. Ages are in days.
var (
	imgDangling    = "sha256:" + id64("1") // untagged, 40, unused
	imgDanglingNew = "sha256:" + id64("2") // untagged, 2, unused
	imgPostgres    = "sha256:" + id64("3") // postgres:16, 60, run by my-db (protected)
	imgAlpine      = "sha256:" + id64("4") // alpine:3, 90, unused
	imgNginx       = "sha256:" + id64("5") // nginx:latest, 50, run by web (running)
	imgPause       = "sha256:" + id64("6") // registry.k8s.io/pause, 100, unused (system)
	imgProd        = "sha256:" + id64("7") // prod-api:1, 70, unused (protect-list)
	imgBusybox     = "sha256:" + id64("8") // busybox:1, 45, run by old-job and new-job

	ctrDB  = id64("a") // my-db, running, 60, mounts pgdata (protect-list)
	ctrWeb = id64("b") // web, running, 50, attached to app-net
	ctrJob = id64("c") // old-job, exited, 40, mounts jobdata
	ctrNew = id64("d") // new-job, exited, 1

	volAnon = id64("e") // anonymous, 3, unused
	// Volumes cache (45, unused), pgdata (60, my-db), jobdata (40, old-job)
	// and prod-data (80, unused, protect-list) go by name.

	netBridge = id64("nb") // bridge, predefined
	netOld    = id64("no") // old-net, unused
	netApp    = id64("na") // app-net, web attached
	netProd   = id64("np") // prod-net, unused (protect-list)
	netKind   = id64("nk") // kind, unused (system)
)

// protectList is the config file's protect-list world is pruned with.
var protectList = []string{"my-db", "prod-*"}

// world returns a fake daemon holding a bit of everything: dangling, unused,
// used and protected images; running, stopped, old and new containers;
// volumes in use and not; networks; and 5 GB of build cache that takes a
// prune with All to free.
func world() *dockertest.Fake {
	at := func(days int) time.Time { return time.Unix(daysAgo(days), 0) }
	return &dockertest.Fake{
		Images: []client.APIImages{
			{ID: imgDangling, RepoTags: []string{"<none>:<none>"}, Created: daysAgo(40), Size: 100 * mb},
			{ID: imgDanglingNew, Created: daysAgo(2), Size: 50 * mb},
			{ID: imgPostgres, RepoTags: []string{"postgres:16"}, Created: daysAgo(60), Size: 400 * mb},
			{ID: imgAlpine, RepoTags: []string{"alpine:3"}, Created: daysAgo(90), Size: 8 * mb},
			{ID: imgNginx, RepoTags: []string{"nginx:latest"}, Created: daysAgo(50), Size: 190 * mb},
			{ID: imgPause, RepoTags: []string{"registry.k8s.io/pause:3.9"}, Created: daysAgo(100), Size: 1 * mb},
			{ID: imgProd, RepoTags: []string{"prod-api:1"}, Created: daysAgo(70), Size: 300 * mb},
			{ID: imgBusybox, RepoTags: []string{"busybox:1"}, Created: daysAgo(45), Size: 4 * mb},
		},
		Containers: []client.APIContainers{
			{ID: ctrDB, Names: []string{"/my-db"}, Image: "postgres:16", State: "running", Created: daysAgo(60), SizeRw: 1 * mb,
				Mounts: []client.APIMount{{Type: "volume", Name: "pgdata"}}},
			{ID: ctrWeb, Names: []string{"/web"}, Image: "nginx", State: "running", Created: daysAgo(50), SizeRw: 2 * mb,
				Networks: client.NetworkList{Networks: map[string]client.ContainerNetwork{"app-net": {NetworkID: netApp}}}},
			{ID: ctrJob, Names: []string{"/old-job"}, Image: "busybox:1", State: "exited", Created: daysAgo(40), SizeRw: 3 * mb,
				Mounts: []client.APIMount{{Type: "volume", Name: "jobdata"}}},
			{ID: ctrNew, Names: []string{"/new-job"}, Image: "busybox:1", State: "exited", Created: daysAgo(1), SizeRw: 5 * mb},
		},
		Volumes: []client.Volume{
			{Name: "cache", Driver: "local", CreatedAt: at(45)},
			{Name: "pgdata", Driver: "local", CreatedAt: at(60)},
			{Name: "jobdata", Driver: "local", CreatedAt: at(40)},
			{Name: volAnon, Driver: "local", CreatedAt: at(3)},
			{Name: "prod-data", Driver: "local", CreatedAt: at(80)},
		},
		Networks: []client.Network{
			{ID: netBridge, Name: "bridge", Driver: "bridge"},
			{ID: netOld, Name: "old-net", Driver: "bridge"},
			{ID: netApp, Name: "app-net", Driver: "bridge"},
			{ID: netProd, Name: "prod-net", Driver: "bridge"},
			{ID: netKind, Name: "kind", Driver: "bridge"},
		},
		Usage: mobyclient.DiskUsageResult{
			Volumes: mobyclient.VolumesDiskUsage{Items: []volume.Volume{
				{Name: "cache", UsageData: &volume.UsageData{Size: 10 * mb, RefCount: 0}},
				{Name: "pgdata", UsageData: &volume.UsageData{Size: 200 * mb, RefCount: 1}},
				{Name: "jobdata", UsageData: &volume.UsageData{Size: 20 * mb, RefCount: 1}},
				{Name: volAnon, UsageData: &volume.UsageData{Size: -1, RefCount: 0}},
				{Name: "prod-data", UsageData: &volume.UsageData{Size: 30 * mb, RefCount: 0}},
			}},
			BuildCache: mobyclient.BuildCacheDiskUsage{
				TotalCount:  3,
				Reclaimable: 5 << 30,
				Items: []build.CacheRecord{
					{Type: "regular", Size: 4 << 30},
					{Type: "frontend", Size: 1 << 30},
				},
			},
		},
	}
}

// result is what a run printed and returned.
type result struct {
	code           int
	stdout, stderr string
}

// run parses args, as Parse does, and runs the command against fake with
// world's protect-list and env. env.Stdin defaults to empty input and
// env.Now to now; env.Interactive is false unless set.
func run(t *testing.T, fake *dockertest.Fake, env Env, args ...string) result {
	t.Helper()
	var parseOut, parseErr bytes.Buffer
	opts, code, exit := Parse(args, &parseOut, &parseErr)
	if exit {
		t.Fatalf("Parse(%q) = (%d, true); stderr %q", args, code, parseErr.String())
	}
	protector, err := docker.NewProtector(protectList)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	env.Stdout, env.Stderr = &stdout, &stderr
	if env.Stdin == nil {
		env.Stdin = strings.NewReader("")
	}
	if env.Now == nil {
		env.Now = func() time.Time { return now }
	}
	code = Run(context.Background(), fake, protector, opts, env)
	return result{code, stdout.String(), stderr.String()}
}

// noEnv is the environment of a script or cron job: no terminal.
var noEnv = Env{}

// checkRemoved fails the test unless fake removed exactly want, in order.
func checkRemoved(t *testing.T, fake *dockertest.Fake, want ...string) {
	t.Helper()
	if !slices.Equal(fake.Removed, want) {
		t.Errorf("removed\n  %v\nwant\n  %v", fake.Removed, want)
	}
}

func TestRunRefusesWithoutTerminal(t *testing.T) {
	fake := world()
	r := run(t, fake, noEnv, "--dangling")
	if r.code != ExitUsage {
		t.Errorf("exit code = %d, want %d", r.code, ExitUsage)
	}
	for _, want := range []string{"stdin is not a terminal", "--yes", "--dry-run"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr = %q, want %q", r.stderr, want)
		}
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	checkRemoved(t, fake)
	if fake.ListContainersOpts.All {
		t.Error("refused, but listed the containers first")
	}
}

func TestRunDryRunDeletesNothing(t *testing.T) {
	for _, args := range [][]string{
		{"--older-than", "30d", "--dry-run"},
		{"--dangling", "--stopped", "--unused", "--dry-run", "--force"},
		{"--older-than", "30d", "--dry-run", "--yes"}, // --dry-run wins
		{"--unused", "--type", "images,volumes,networks,build-cache", "--dry-run", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			fake := world()
			r := run(t, fake, noEnv, args...)
			if r.code != ExitOK {
				t.Errorf("exit code = %d, want 0; stderr %q", r.code, r.stderr)
			}
			checkRemoved(t, fake)
			if len(fake.PruneOpts) > 0 || len(fake.RemoveContainerOpts) > 0 || len(fake.RemoveImageOpts) > 0 {
				t.Error("a dry run called the daemon to remove something")
			}
		})
	}
}

func TestRunDryRunText(t *testing.T) {
	fake := world()
	r := run(t, fake, noEnv, "--older-than", "30d", "--type", "containers,images,volumes", "--dry-run")
	if r.code != ExitOK || r.stderr != "" {
		t.Fatalf("exit code %d, stderr %q", r.code, r.stderr)
	}

	// The plan, by type in removal order, with each resource's short ID,
	// name, size, age and the filters it matched.
	sections := []string{"Containers (1)", "Images (2)", "Volumes (2)", "Skipped (9)"}
	last := -1
	for _, s := range sections {
		i := strings.Index(r.stdout, s)
		if i < last {
			t.Errorf("section %q missing or out of order:\n%s", s, r.stdout)
		}
		last = i
	}
	for _, want := range []string{
		"cccccccccccc  old-job  3.0 MB  5 weeks ago  older than 30d",
		"111111111111  <none>    100.0 MB  5 weeks ago   older than 30d",
		"444444444444  alpine:3",
		"cache",
		"jobdata",
		// Skipped, with the reason.
		"container  aaaaaaaaaaaa  my-db                      protect-list",
		"container  bbbbbbbbbbbb  web                        container is running; pass --force to remove it",
		"image      333333333333  postgres:16                used by protected container",
		"image      555555555555  nginx:latest               used by container web; pass --force to remove it",
		"image      666666666666  registry.k8s.io/pause:3.9  system",
		"image      777777777777  prod-api:1                 protect-list",
		"image      888888888888  busybox:1                  used by container new-job; pass --force to remove it",
		"volume     -             pgdata                     in use by container my-db",
		// The total.
		"Would delete 5 items (141.0 MB). Dry run: nothing was deleted.",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "prod-data") {
		t.Errorf("output lacks the protected volume prod-data:\n%s", r.stdout)
	}
}

func TestRunDeletesMatched(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"older than", []string{"--older-than", "30d", "--type", "containers,images,volumes"}, []string{
			// Containers first; then the images, which old-job no
			// longer holds; then the volumes, jobdata among them.
			"container:" + ctrJob,
			"image:" + imgDangling, "image:" + imgAlpine,
			"volume:cache", "volume:jobdata",
		}},
		{"dangling", []string{"--dangling"}, []string{"image:" + imgDangling, "image:" + imgDanglingNew}},
		{"dangling and older than", []string{"--dangling", "--older-than", "7d", "--type", "images"}, []string{"image:" + imgDangling}},
		{"stopped", []string{"--stopped"}, []string{"container:" + ctrJob, "container:" + ctrNew}},
		{"stopped and older than", []string{"--stopped", "--older-than", "30d", "--type", "containers"}, []string{"container:" + ctrJob}},
		{"unused", []string{"--unused", "--type", "images,volumes,networks"}, []string{
			"image:" + imgDangling, "image:" + imgDanglingNew, "image:" + imgAlpine,
			"volume:cache", "volume:" + volAnon,
			"network:" + netOld,
		}},
		{"unused images", []string{"--unused", "--type", "images"}, []string{
			"image:" + imgDangling, "image:" + imgDanglingNew, "image:" + imgAlpine,
		}},
		{"unused volumes and networks", []string{"--unused", "--type", "networks,volumes"}, []string{
			"volume:cache", "volume:" + volAnon, "network:" + netOld,
		}},
		{"unused and older than", []string{"--unused", "--older-than", "30d", "--type", "containers,images,volumes,networks"}, []string{
			// Every filter that applies must pass: --unused does not
			// apply to containers, and networks have no age, so only
			// --unused narrows them.
			"container:" + ctrJob,
			"image:" + imgDangling, "image:" + imgAlpine,
			"volume:cache",
			"network:" + netOld,
		}},
		{"build cache", []string{"--type", "build-cache"}, []string{"buildcache"}},
		{"build cache and dangling", []string{"--dangling", "--type", "build-cache,images"}, []string{
			"image:" + imgDangling, "image:" + imgDanglingNew, "buildcache",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := world()
			r := run(t, fake, noEnv, append(c.args, "--yes")...)
			if r.code != ExitOK {
				t.Errorf("exit code = %d, want 0; stderr %q", r.code, r.stderr)
			}
			checkRemoved(t, fake, c.want...)
			if want := "Deleted " + strconv.Itoa(len(c.want)); !strings.Contains(r.stdout, want) {
				t.Errorf("output lacks %q:\n%s", want, r.stdout)
			}
		})
	}
}

func TestRunNeverTouchesProtectedOrPredefined(t *testing.T) {
	fake := world()
	// Everything any filter can reach, forced.
	r := run(t, fake, noEnv, "--older-than", "1h", "--unused", "--type", "images,volumes,networks", "--yes", "--force")
	if r.code != ExitOK {
		t.Fatalf("exit code = %d; stderr %q", r.code, r.stderr)
	}
	r2 := run(t, fake, noEnv, "--older-than", "1h", "--type", "containers,images,volumes", "--yes", "--force")
	if r2.code != ExitOK {
		t.Fatalf("exit code = %d; stderr %q", r2.code, r2.stderr)
	}
	for _, kept := range []string{
		"container:" + ctrDB, "image:" + imgPostgres, "image:" + imgPause, "image:" + imgProd,
		"volume:prod-data", "volume:pgdata",
		"network:" + netBridge, "network:" + netProd, "network:" + netKind, "network:" + netApp,
	} {
		if slices.Contains(fake.Removed, kept) {
			t.Errorf("removed %s; removed %v", kept, fake.Removed)
		}
	}
	if strings.Contains(r.stdout+r2.stdout, "bridge") {
		t.Errorf("output mentions the predefined bridge network:\n%s%s", r.stdout, r2.stdout)
	}
	if !strings.Contains(r.stdout, "kind") || !strings.Contains(r.stdout, "system") {
		t.Errorf("output does not list the kind network as skipped (system):\n%s", r.stdout)
	}
}

func TestRunForce(t *testing.T) {
	fake := world()
	r := run(t, fake, noEnv, "--older-than", "30d", "--type", "containers,images,volumes", "--yes", "--force")
	if r.code != ExitOK {
		t.Fatalf("exit code = %d; stderr %q", r.code, r.stderr)
	}
	// The running web container now goes, and with it the hold on nginx;
	// busybox, which new-job uses, is force-removed too. my-db and its
	// postgres image stay protected.
	checkRemoved(t, fake,
		"container:"+ctrWeb, "container:"+ctrJob,
		"image:"+imgDangling, "image:"+imgAlpine, "image:"+imgNginx, "image:"+imgBusybox,
		"volume:cache", "volume:jobdata",
	)
	for _, opts := range fake.RemoveContainerOpts {
		if !opts.Force {
			t.Errorf("container %s removed without Force", opts.ID)
		}
	}
	if len(fake.RemoveImageOpts) != 4 {
		t.Errorf("RemoveImageExtended called %d times, want 4 (every image forced)", len(fake.RemoveImageOpts))
	}
	for _, opts := range fake.RemoveImageOpts {
		if !opts.Force {
			t.Error("image removed without Force")
		}
	}
}

func TestRunWithoutForceDoesNotForce(t *testing.T) {
	fake := world()
	run(t, fake, noEnv, "--older-than", "30d", "--yes")
	for _, opts := range fake.RemoveContainerOpts {
		if opts.Force {
			t.Errorf("container %s removed with Force", opts.ID)
		}
	}
	if len(fake.RemoveImageOpts) != 0 {
		t.Errorf("images removed with RemoveImageExtended %v, want plain RemoveImage", fake.RemoveImageOpts)
	}
}

func TestRunBuildCachePrunesWithAllWhenNeeded(t *testing.T) {
	for _, needsAll := range []bool{true, false} {
		fake := world()
		if !needsAll {
			fake.Usage.BuildCache.Items = fake.Usage.BuildCache.Items[:1]
		}
		r := run(t, fake, noEnv, "--type", "build-cache", "--yes")
		if r.code != ExitOK {
			t.Fatalf("exit code = %d; stderr %q", r.code, r.stderr)
		}
		if len(fake.PruneOpts) != 1 || fake.PruneOpts[0].All != needsAll {
			t.Errorf("needs all %v: prune options %+v, want one prune with All %v", needsAll, fake.PruneOpts, needsAll)
		}
		if !strings.Contains(r.stdout, "Build cache freed 5.0 GB") {
			t.Errorf("output lacks the space freed:\n%s", r.stdout)
		}
	}
}

func TestRunNothingToDelete(t *testing.T) {
	fake := world()
	fake.Usage.BuildCache = mobyclient.BuildCacheDiskUsage{}
	for _, args := range [][]string{
		{"--type", "build-cache", "--yes"},
		{"--type", "build-cache"}, // nothing to ask about
	} {
		r := run(t, fake, Env{Interactive: true}, args...)
		if r.code != ExitOK || !strings.Contains(r.stdout, "Nothing to delete.") {
			t.Errorf("%q: exit code %d, output %q; want 0 and nothing to delete", args, r.code, r.stdout)
		}
	}
	checkRemoved(t, fake)
}

func TestRunPartialFailure(t *testing.T) {
	fake := world()
	fake.RemoveErrs = map[string]error{
		imgAlpine: &client.Error{Status: http.StatusConflict,
			Message: "conflict: unable to delete 444444444444 (must be forced) - image is referenced in multiple repositories"},
		"cache": errors.New("disk on fire"),
	}
	r := run(t, fake, noEnv, "--older-than", "30d", "--type", "containers,images,volumes", "--yes")
	if r.code != ExitFailure {
		t.Errorf("exit code = %d, want %d", r.code, ExitFailure)
	}
	checkRemoved(t, fake, "container:"+ctrJob, "image:"+imgDangling, "volume:jobdata")
	for _, want := range []string{
		"deleted  container  cccccccccccc  old-job",
		"failed   image      444444444444  alpine:3  image is referenced in multiple repositories",
		"failed   volume     -             cache     disk on fire",
		"Deleted 3 · Failed 2",
		"1 image failed because it is in use or has several tags; pass --force to remove it.",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}
}

func TestRunConfirm(t *testing.T) {
	cases := []struct {
		answer  string
		deletes bool
	}{
		{"y\n", true},
		{"YES\n", true},
		{"yes", true}, // no newline before the end of input
		{"n\n", false},
		{"\n", false},
		{"", false}, // end of input
		{"sure\n", false},
	}
	for _, c := range cases {
		t.Run(c.answer, func(t *testing.T) {
			fake := world()
			r := run(t, fake, Env{Stdin: strings.NewReader(c.answer), Interactive: true}, "--dangling")
			if r.code != ExitOK {
				t.Errorf("exit code = %d, want 0; stderr %q", r.code, r.stderr)
			}
			// The plan, then the question.
			if !strings.Contains(r.stdout, "Images (2)") || !strings.Contains(r.stdout, "Delete 2 items (150.0 MB)? [y/N] ") {
				t.Errorf("output lacks the plan or the question:\n%s", r.stdout)
			}
			if c.deletes {
				checkRemoved(t, fake, "image:"+imgDangling, "image:"+imgDanglingNew)
			} else {
				checkRemoved(t, fake)
				if !strings.Contains(r.stdout, "Nothing was deleted.") {
					t.Errorf("output lacks %q:\n%s", "Nothing was deleted.", r.stdout)
				}
			}
		})
	}
}

func TestRunYesDoesNotAsk(t *testing.T) {
	fake := world()
	r := run(t, fake, Env{Stdin: strings.NewReader("n\n"), Interactive: true}, "--dangling", "--yes")
	if strings.Contains(r.stdout, "[y/N]") {
		t.Errorf("asked despite --yes:\n%s", r.stdout)
	}
	checkRemoved(t, fake, "image:"+imgDangling, "image:"+imgDanglingNew)
}

func TestRunErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name    string
		setup   func(*dockertest.Fake)
		args    []string
		wantErr string
		code    int
	}{
		{"list fails", func(f *dockertest.Fake) { f.ListErr = boom }, []string{"--dangling", "--yes"}, "listing containers: boom", ExitFailure},
		{"disk usage fails for the build cache", func(f *dockertest.Fake) { f.DiskUsageErr = boom },
			[]string{"--type", "build-cache", "--yes"}, "reading disk usage: boom", ExitFailure},
		// Volumes do without their sizes, as in the TUI.
		{"disk usage fails for volumes", func(f *dockertest.Fake) { f.DiskUsageErr = boom },
			[]string{"--unused", "--type", "volumes", "--dry-run"}, "warning: volume sizes unknown", ExitOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := world()
			c.setup(fake)
			r := run(t, fake, noEnv, c.args...)
			if r.code != c.code || !strings.Contains(r.stderr, c.wantErr) {
				t.Errorf("exit code %d, stderr %q; want %d and %q", r.code, r.stderr, c.code, c.wantErr)
			}
			checkRemoved(t, fake)
		})
	}
}

func TestRunJSONDryRun(t *testing.T) {
	fake := world()
	r := run(t, fake, noEnv, "--older-than", "30d", "--type", "containers,images,volumes", "--dry-run", "--json")
	if r.code != ExitOK || r.stderr != "" {
		t.Fatalf("exit code %d, stderr %q", r.code, r.stderr)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.stdout), &raw); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, r.stdout)
	}
	for _, field := range []string{"dry_run", "filters", "types", "planned", "deleted", "failed", "skipped", "totals"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("JSON lacks %q", field)
		}
	}
	// Empty lists are [], not null.
	if string(raw["deleted"]) != "[]" || string(raw["failed"]) != "[]" {
		t.Errorf("deleted = %s, failed = %s; want []", raw["deleted"], raw["failed"])
	}
	if !strings.Contains(r.stdout, `"name": "<none>"`) {
		t.Errorf("JSON escapes the untagged image's name:\n%s", r.stdout)
	}

	var rep Report
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || !slices.Equal(rep.Filters, []string{"older than 30d"}) ||
		!slices.Equal(rep.Types, []string{"containers", "images", "volumes"}) {
		t.Errorf("report = %+v", rep)
	}
	if got, want := ids(rep.Planned), []string{ctrJob, imgDangling, imgAlpine, "cache", "jobdata"}; !slices.Equal(got, want) {
		t.Errorf("planned = %v, want %v", got, want)
	}
	first := rep.Planned[0]
	if first.Name != "old-job" || first.Type != "container" || first.Size == nil || *first.Size != 3*mb ||
		!first.Created.Equal(time.Unix(daysAgo(40), 0)) || first.Reason != "older than 30d" {
		t.Errorf("planned[0] = %+v", first)
	}
	skipped := map[string]string{}
	for _, e := range rep.Skipped {
		skipped[e.ID] = e.Reason
	}
	for id, reason := range map[string]string{
		ctrDB:       "protect-list",
		imgPostgres: "used by protected container",
		imgPause:    "system",
		"prod-data": "protect-list",
		"pgdata":    "in use by container my-db",
	} {
		if skipped[id] != reason {
			t.Errorf("skipped[%s] = %q, want %q", id, skipped[id], reason)
		}
	}
	want := ReportTotals{Planned: 5, PlannedSize: (3 + 100 + 8 + 10 + 20) * mb, Skipped: len(rep.Skipped)}
	if rep.Totals != want {
		t.Errorf("totals = %+v, want %+v", rep.Totals, want)
	}
}

func TestRunJSON(t *testing.T) {
	fake := world()
	fake.RemoveErrs = map[string]error{imgAlpine: &client.Error{Status: http.StatusConflict,
		Message: "conflict: unable to delete 444444444444 (must be forced) - image is referenced in multiple repositories"}}
	r := run(t, fake, noEnv, "--older-than", "30d", "--type", "images,volumes,build-cache", "--yes", "--json")
	if r.code != ExitFailure {
		t.Errorf("exit code = %d, want %d", r.code, ExitFailure)
	}
	var rep Report
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, r.stdout)
	}
	if rep.DryRun {
		t.Error("dry_run = true")
	}
	if got, want := ids(rep.Deleted), []string{imgDangling, "cache", docker.BuildCacheID}; !slices.Equal(got, want) {
		t.Errorf("deleted = %v, want %v", got, want)
	}
	if len(rep.Failed) != 1 || rep.Failed[0].ID != imgAlpine || !rep.Failed[0].Forceable ||
		rep.Failed[0].Reason != "image is referenced in multiple repositories" {
		t.Errorf("failed = %+v", rep.Failed)
	}
	bc := rep.Deleted[2]
	if bc.Type != "build-cache" || bc.Size == nil || *bc.Size != 5<<30 || !bc.Created.IsZero() {
		t.Errorf("deleted build cache = %+v", bc)
	}
	want := ReportTotals{
		Planned:             4,
		PlannedSize:         (100+8+10)*mb + 5<<30,
		Deleted:             3,
		DeletedSize:         (100+10)*mb + 5<<30,
		Failed:              1,
		Skipped:             len(rep.Skipped),
		BuildCacheReclaimed: 5 << 30,
	}
	if rep.Totals != want {
		t.Errorf("totals = %+v, want %+v", rep.Totals, want)
	}
}

func TestRunJSONConfirmsOnStderr(t *testing.T) {
	fake := world()
	r := run(t, fake, Env{Stdin: strings.NewReader("y\n"), Interactive: true}, "--dangling", "--json")
	if !strings.Contains(r.stderr, "[y/N]") || !strings.Contains(r.stderr, "Images (2)") {
		t.Errorf("stderr lacks the plan and question: %q", r.stderr)
	}
	var rep Report
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil || len(rep.Deleted) != 2 {
		t.Errorf("stdout = %q (%v), want a report of 2 deleted", r.stdout, err)
	}
}

func TestRunUnknownVolumeSize(t *testing.T) {
	fake := world()
	r := run(t, fake, noEnv, "--unused", "--type", "volumes", "--dry-run", "--json")
	var rep Report
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatal(err)
	}
	for _, e := range rep.Planned {
		if e.ID == volAnon && e.Size != nil {
			t.Errorf("anonymous volume size = %d, want null", *e.Size)
		}
	}
	text := run(t, world(), noEnv, "--unused", "--type", "volumes", "--dry-run")
	if !strings.Contains(text.stdout, "eeeeeeeeeeee  unknown") {
		t.Errorf("output lacks the short anonymous volume with an unknown size:\n%s", text.stdout)
	}
}

// ids returns the IDs of entries, in order.
func ids(entries []ReportEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}
