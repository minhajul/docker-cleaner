package models

import (
	"strings"
	"testing"

	"docker-cleaner/docker/dockertest"
	tea "github.com/charmbracelet/bubbletea"
	dockerClient "github.com/fsouza/go-dockerclient"
)

const (
	ctrRunning = "rrrrrrrrrrrrrrrr"
	ctrStopped = "ssssssssssssssss"
)

var (
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
	keyCtrlC = tea.KeyMsg{Type: tea.KeyCtrlC}
)

// runes is the KeyMsg for typing s.
func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// send feeds msg to the model and then settles the command it returns.
func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	return settle(t, next.(Model), cmd)
}

// settle runs cmd and every command the resulting messages lead to, feeding
// each message back into the model, as the Bubble Tea runtime would. It
// returns the model once no command is left.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 100 {
			t.Fatal("commands did not settle after 100 steps")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		next, nextCmd := m.Update(msg)
		m = next.(Model)
		queue = append(queue, nextCmd)
	}
	return m
}

// newFake returns a daemon with two images and two containers, one of them
// running.
func newFake() *dockertest.Fake {
	return &dockertest.Fake{
		Images: []dockerClient.APIImages{
			{ID: imgA, RepoTags: []string{"alpine:latest"}, Size: 1 << 30},
			{ID: imgB, RepoTags: []string{"busybox:latest"}, Size: 512 << 20},
		},
		Containers: []dockerClient.APIContainers{
			{ID: ctrRunning, Names: []string{"/web"}, State: "running", SizeRw: 10 << 20, SizeRootFs: 2 << 30},
			{ID: ctrStopped, Names: []string{"/db"}, State: "exited", SizeRw: 2 << 20, SizeRootFs: 2 << 30},
		},
	}
}

// loaded returns a model backed by fake with its lists fetched and ids
// selected.
func loaded(t *testing.T, fake *dockertest.Fake, ids ...string) Model {
	t.Helper()
	m := newModel(fake)
	m = settle(t, m, m.Init())
	for _, id := range ids {
		m = toggle(t, m, id)
	}
	return m
}

func TestDeleteWaitsForConfirmation(t *testing.T) {
	cases := []struct {
		name        string
		answer      []tea.KeyMsg // keys pressed after d
		wantRemoved int
		wantOpen    bool // dialog still open afterwards
	}{
		{"d alone", nil, 0, true},
		{"y deletes", []tea.KeyMsg{runes("y")}, 2, false},
		{"Y deletes", []tea.KeyMsg{runes("Y")}, 2, false},
		{"n cancels", []tea.KeyMsg{runes("n")}, 0, false},
		{"esc cancels", []tea.KeyMsg{keyEsc}, 0, false},
		{"other key cancels", []tea.KeyMsg{runes("x")}, 0, false},
		// f is only offered when a selected container is running.
		{"f without running containers cancels", []tea.KeyMsg{runes("f")}, 0, false},
		{"y after cancel does nothing", []tea.KeyMsg{runes("n"), runes("y")}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := newFake()
			m := loaded(t, fake, imgA, ctrStopped)

			m = send(t, m, runes("d"))
			if m.confirm == nil {
				t.Fatal("d should open the confirmation dialog")
			}
			for _, k := range c.answer {
				m = send(t, m, k)
			}

			if len(fake.Removed) != c.wantRemoved {
				t.Errorf("Removed = %v, want %d removals", fake.Removed, c.wantRemoved)
			}
			if open := m.confirm != nil; open != c.wantOpen {
				t.Errorf("dialog open = %v, want %v", open, c.wantOpen)
			}
		})
	}
}

func TestConfirmForceRemovesContainers(t *testing.T) {
	cases := []struct {
		answer    string
		wantForce bool
	}{
		{"y", false},
		{"f", true},
	}
	for _, c := range cases {
		t.Run(c.answer, func(t *testing.T) {
			fake := newFake()
			m := loaded(t, fake, imgA, ctrRunning)

			m = send(t, m, runes("d"))
			send(t, m, runes(c.answer))

			if len(fake.RemoveContainerOpts) != 1 {
				t.Fatalf("expected 1 RemoveContainer call, got %v", fake.RemoveContainerOpts)
			}
			if got := fake.RemoveContainerOpts[0].Force; got != c.wantForce {
				t.Errorf("RemoveContainer Force = %v, want %v", got, c.wantForce)
			}
		})
	}
}

func TestConfirmIgnoresOtherKeys(t *testing.T) {
	fake := newFake()
	m := loaded(t, fake, imgA)
	m = send(t, m, runes("d"))
	cursor := m.cursor

	// Each of these would move, select or toggle help outside the dialog.
	// Every key closes the dialog, so reopen it before the next one.
	for _, k := range []tea.KeyMsg{runes("j"), runes("k"), runes(" "), runes("?"), runes("q")} {
		next, cmd := m.Update(k)
		m = next.(Model)
		if cmd != nil {
			t.Errorf("%q returned a command while confirming", k)
		}
		if m.cursor != cursor || len(m.selected) != 1 || m.help.ShowAll {
			t.Errorf("%q changed the model while confirming: cursor=%d selected=%v showAll=%v",
				k, m.cursor, selectedIDs(m), m.help.ShowAll)
		}
		m = send(t, m, runes("d"))
	}
	if len(fake.Removed) != 0 {
		t.Errorf("Removed = %v, want nothing", fake.Removed)
	}
}

func TestConfirmCtrlCQuits(t *testing.T) {
	m := loaded(t, newFake(), imgA)
	m = send(t, m, runes("d"))

	_, cmd := m.Update(keyCtrlC)
	if cmd == nil {
		t.Fatal("ctrl+c should quit while confirming")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c returned %T, want tea.QuitMsg", cmd())
	}
}

func TestConfirmDialogText(t *testing.T) {
	cases := []struct {
		name        string
		ids         []string
		wantPrompt  string
		wantWarning string // "" means no warning and no f key
	}{
		{
			name:       "one image",
			ids:        []string{imgB},
			wantPrompt: "Delete 1 image (512.0 MB)? y/N",
		},
		{
			// Containers count their writable layer, not the virtual size.
			name:       "images and stopped container",
			ids:        []string{imgA, imgB, ctrStopped},
			wantPrompt: "Delete 2 images, 1 container (1.5 GB)? y/N",
		},
		{
			name:        "running container",
			ids:         []string{ctrRunning},
			wantPrompt:  "Delete 1 container (10.0 MB)? y/N",
			wantWarning: "⚠ 1 container is running and won't be removed with y; press f to force-remove it",
		},
		{
			name:        "several containers, one running",
			ids:         []string{ctrRunning, ctrStopped},
			wantPrompt:  "Delete 2 containers (12.0 MB)? y/N",
			wantWarning: "⚠ 1 container is running and won't be removed with y; press f to force-remove it",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := loaded(t, newFake(), c.ids...)
			m = send(t, m, runes("d"))
			view := m.View()

			if !strings.Contains(view, c.wantPrompt) {
				t.Errorf("view should contain %q, got:\n%s", c.wantPrompt, view)
			}
			if c.wantWarning != "" && !strings.Contains(view, c.wantWarning) {
				t.Errorf("view should contain %q, got:\n%s", c.wantWarning, view)
			}
			if c.wantWarning == "" && strings.Contains(view, "⚠") {
				t.Errorf("view should not warn, got:\n%s", view)
			}
			// The footer lists the dialog's keys, not the list's.
			if strings.Contains(view, "delete selected") {
				t.Errorf("footer should show the dialog keys, got:\n%s", view)
			}
			if got, want := strings.Contains(view, "force delete"), c.wantWarning != ""; got != want {
				t.Errorf("footer offers f = %v, want %v:\n%s", got, want, view)
			}
		})
	}
}

func TestConfirmWarningCountsRunning(t *testing.T) {
	p := &pendingDelete{containers: 3, running: 2}
	want := "⚠ 2 containers are running and won't be removed with y; press f to force-remove them"
	if got := p.warning(); got != want {
		t.Errorf("warning() = %q, want %q", got, want)
	}
}
