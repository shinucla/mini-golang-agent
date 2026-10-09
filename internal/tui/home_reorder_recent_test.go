package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
)

func recentOrder(app *App) string {
	var names []string
	for _, s := range app.home.ordered(app) {
		if !s.Pinned {
			names = append(names, sessionName(s))
		}
	}
	return strings.Join(names, ",")
}

func TestReorderRecentSessions(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	oldest := saveTestSession(t, app.rt.Cwd, "oldest", "1")
	middle := saveTestSession(t, app.rt.Cwd, "middle", "2")
	saveTestSession(t, app.rt.Cwd, "newest", "3")

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := recentOrder(app); got != "newest,middle,oldest" {
		t.Fatalf("start order = %s", got)
	}
	selectSession(t, app, oldest.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	if got := recentOrder(app); got != "newest,oldest,middle" {
		t.Fatalf("after shift+up = %s", got)
	}
	if app.home.items(app)[app.home.cursor].session.ID != oldest.ID {
		t.Fatal("the cursor must follow the moved session")
	}
	if s, _ := agent.LoadSession(config.SessionsDir(), middle.ID); s.Order != 3 {
		t.Fatalf("saved order of middle = %d, want 3", s.Order)
	}

	middle, _ = agent.LoadSession(config.SessionsDir(), middle.ID)
	middle.Messages = append(middle.Messages, llm.Message{Role: llm.RoleUser, Content: "used again"})
	if err := middle.Save(config.SessionsDir()); err != nil {
		t.Fatal(err)
	}
	saveTestSession(t, app.rt.Cwd, "brand new", "4")
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := recentOrder(app); got != "brand new,newest,oldest,middle" {
		t.Fatalf("a used session keeps its place and a new one goes on top: %s", got)
	}

	selectSession(t, app, oldest.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if strings.Contains(recentOrder(app), "oldest") {
		t.Fatal("pinned sessions leave the Recent group")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := recentOrder(app); got != "brand new,newest,oldest,middle" {
		t.Fatalf("unpin must restore the old place: %s", got)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	typeText(app, "e")
	selectSession(t, app, middle.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := recentOrder(app); got != "brand new,newest,middle,oldest" {
		t.Fatalf("a move during a search swaps with the visible neighbor: %s", got)
	}
}
