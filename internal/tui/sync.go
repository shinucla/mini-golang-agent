package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/llm"
)

const syncInterval = time.Second

var syncEnabled = true

type syncTickMsg struct{}

func syncTick() tea.Cmd {
	if !syncEnabled {
		return nil
	}
	return tea.Tick(syncInterval, func(time.Time) tea.Msg { return syncTickMsg{} })
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func (a *App) markSeen(r *sessionRun) {
	r.seenMod = modTime(filepath.Join(config.SessionsDir(), r.session.ID+".json"))
}

func (a *App) syncSessions() {
	dir := config.SessionsDir()
	for _, r := range a.runs {
		if r.busy {
			continue
		}
		mod := modTime(filepath.Join(dir, r.session.ID+".json"))
		if mod.IsZero() || mod.Equal(r.seenMod) {
			continue
		}
		r.seenMod = mod
		disk, err := agent.LoadSession(dir, r.session.ID)
		if err != nil {
			continue
		}
		if r.session.Updated.Before(disk.Updated) {
			a.adopt(r, disk)
			continue
		}
		r.session.Title, r.session.TitleSource = disk.Title, disk.TitleSource
		r.session.Pinned, r.session.PinOrder, r.session.Order = disk.Pinned, disk.PinOrder, disk.Order
	}
	if a.home == nil {
		return
	}
	if mod := modTime(dir); !mod.Equal(a.home.dirMod) {
		a.home.dirMod = mod
		a.home.reload(a)
	}
	a.home.elsewhere = lockedElsewhere(dir)
}

func lockedElsewhere(dir string) map[string]bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".lock"); ok && agent.LockedBy(dir, id) != 0 {
			out[id] = true
		}
	}
	return out
}

func (a *App) adopt(r *sessionRun, disk *agent.Session) {
	old := r.history
	*r.session = *disk
	r.history = slices.Clone(disk.Messages)
	r.usage, r.contextTokens = disk.Usage, disk.ContextTokens
	if disk.Provider != "" && disk.Model != "" {
		r.provider, r.model = disk.Provider, disk.Model
	}
	if r != a.sessionRun {
		return
	}
	a.rt.SetCurrent(r.provider, r.model)
	note := styleDim.Render("  ⎿  Another mga process updated this session")
	sameStart := len(old) <= len(r.history) && slices.EqualFunc(old, r.history[:len(old)], func(x, y llm.Message) bool {
		return x.Role == y.Role && x.Content == y.Content
	})
	if sameStart {
		a.emit(note)
		a.replay(r.history[len(old):])
		return
	}
	a.resetScreen()
	a.emit(note)
	a.replay(r.history)
}

func (a *App) lockRun(r *sessionRun) error {
	dir := config.SessionsDir()
	if disk, ok := r.session.Reload(dir); ok {
		a.adopt(r, disk)
		a.markSeen(r)
	}
	release, err := agent.LockSession(dir, r.session.ID)
	if err != nil {
		return err
	}
	r.unlock = release
	return nil
}

func (r *sessionRun) release() {
	if r.unlock != nil {
		r.unlock()
		r.unlock = nil
	}
}
