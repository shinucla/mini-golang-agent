package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

func listOrder(app *App) []string {
	var titles []string
	for _, s := range app.home.sessions {
		titles = append(titles, sessionName(s))
	}
	return titles
}

func TestPinSessions(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	oldest := saveTestSession(t, app.rt.Cwd, "oldest work", "a")
	saveTestSession(t, app.rt.Cwd, "middle work", "b")
	saveTestSession(t, app.rt.Cwd, "newest work", "c")
	updated := oldest.Updated

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if strings.Contains(ansi.Strip(app.View()), "Pinned") {
		t.Fatal("no Pinned group before any pin")
	}
	selectSession(t, app, oldest.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	if got := strings.Join(listOrder(app)[:2], ","); got != "oldest work,(new session)" {
		t.Fatalf("order after pin = %v", listOrder(app))
	}
	if app.home.items(app)[app.home.cursor].session.ID != oldest.ID {
		t.Fatal("the cursor must follow the pinned session")
	}
	view := ansi.Strip(app.View())
	pinnedAt, recentAt, oldestAt := strings.Index(view, "Pinned"), strings.Index(view, "Recent"), strings.Index(view, "oldest work")
	if pinnedAt < 0 || recentAt < 0 || !(pinnedAt < oldestAt && oldestAt < recentAt) {
		t.Fatalf("groups wrong:\n%s", view)
	}
	saved, _ := agent.LoadSession(config.SessionsDir(), oldest.ID)
	if !saved.Pinned || !saved.Updated.Equal(updated) {
		t.Fatalf("pin not saved, or the update time changed: %+v", saved)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if listOrder(app)[0] != "oldest work" {
		t.Fatalf("the pin must survive reopening: %v", listOrder(app))
	}

	selectSession(t, app, oldest.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := listOrder(app); got[len(got)-1] != "oldest work" || strings.Contains(ansi.Strip(app.View()), "Pinned") {
		t.Fatalf("unpin must restore the order and drop the groups: %v", got)
	}

	app.rt.RegisterProvider("fake", echoProvider{})
	if _, err := app.rt.Spawn(t.Context(), tools.SpawnRequest{AgentType: "Explore", Prompt: "x", Description: "an agent"}); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(ansi.Strip(app.View()), "Only sessions can be pinned") {
		t.Fatal("pinning an agent must explain why it does nothing")
	}

	typeText(app, "?")
	if !strings.Contains(ansi.Strip(app.View()), "pin or unpin the session") {
		t.Fatal("the help panel must list p")
	}
}
