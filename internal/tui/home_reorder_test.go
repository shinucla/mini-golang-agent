package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
)

func pinnedOrder(app *App) string {
	var names []string
	for _, s := range app.home.pinned(app) {
		names = append(names, sessionName(s))
	}
	return strings.Join(names, ",")
}

func TestReorderPinnedSessions(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a := saveTestSession(t, app.rt.Cwd, "alpha", "1")
	b := saveTestSession(t, app.rt.Cwd, "beta", "2")
	c := saveTestSession(t, app.rt.Cwd, "gamma", "3")

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	for _, s := range []*agent.Session{c, a, b} {
		selectSession(t, app, s.ID)
		app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	}
	if got := pinnedOrder(app); got != "gamma,alpha,beta" {
		t.Fatalf("pin order = %s; new pins go to the bottom", got)
	}

	selectSession(t, app, b.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	if got := pinnedOrder(app); got != "gamma,beta,alpha" {
		t.Fatalf("after shift+up = %s", got)
	}
	if app.home.items(app)[app.home.cursor].session.ID != b.ID {
		t.Fatal("the cursor must follow the moved session")
	}
	for id, want := range map[string]int{c.ID: 1, b.ID: 2, a.ID: 3} {
		if s, _ := agent.LoadSession(config.SessionsDir(), id); s.PinOrder != want {
			t.Fatalf("saved pin order of %s = %d, want %d", sessionName(s), s.PinOrder, want)
		}
	}

	selectSession(t, app, c.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	selectSession(t, app, a.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyShiftDown})
	if got := pinnedOrder(app); got != "gamma,beta,alpha" {
		t.Fatalf("moves past the edges must do nothing: %s", got)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := pinnedOrder(app); got != "gamma,beta,alpha" {
		t.Fatalf("the order must survive reopening: %s", got)
	}

	selectSession(t, app, c.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := pinnedOrder(app); got != "beta,alpha,gamma" {
		t.Fatalf("a re-pinned session goes to the bottom: %s", got)
	}

	typeText(app, "?")
	if !strings.Contains(ansi.Strip(app.View()), "move the session up or down within its group") {
		t.Fatal("the help panel must list shift+arrows")
	}
}
