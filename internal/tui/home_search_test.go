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

func TestSessionSearchDeleteAndKeys(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	saveTestSession(t, app.rt.Cwd, "Fix login bug", "a")
	target := saveTestSession(t, app.rt.Cwd, "Refactor the agent loop", "b")
	saveTestSession(t, app.rt.Cwd, "Agent docs", "c")

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !strings.Contains(ansi.Strip(app.View()), "ctrl+f search · ctrl+r rename · ctrl+x stop/delete · esc back · ? help") {
		t.Fatalf("hint line:\n%s", ansi.Strip(app.View()))
	}
	typeText(app, "q")
	if app.view != viewHome || app.input.Value() != "q" {
		t.Fatal("q must type into the input box, not close the list")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewHome || app.input.Value() != "" {
		t.Fatal("esc with text in the input box must clear it and stay in the list")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	typeText(app, "AGENT")
	names := listNames(app)
	if strings.Join(names, ",") != "Agent docs,Refactor the agent loop" || !strings.Contains(ansi.Strip(app.View()), "2 of 4 sessions") {
		t.Fatalf("search results = %v\n%s", names, ansi.Strip(app.View()))
	}
	typeText(app, "?jkl")
	if app.home.showKeys || app.home.search.Value() != "AGENT?jkl" {
		t.Fatal("while searching, letters and ? go into the search field")
	}
	for range "?jkl" {
		app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}

	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if !app.home.searching || app.home.items(app)[app.home.cursor].session.ID != target.ID {
		t.Fatal("ctrl keys work during search and the cursor follows the session")
	}
	if s, _ := agent.LoadSession(config.SessionsDir(), target.ID); !s.Pinned {
		t.Fatal("ctrl+t during search must pin")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewHome || app.home.searching || len(listNames(app)) != 4 {
		t.Fatal("the first esc clears the search only")
	}
	if app.home.items(app)[app.home.cursor].session.ID != target.ID {
		t.Fatal("the cursor stays on the selected session after the search closes")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if !strings.Contains(ansi.Strip(app.View()), `Press ctrl+x again to delete "Refactor the agent loop"`) {
		t.Fatalf("first ctrl+x must ask:\n%s", ansi.Strip(app.View()))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyUp})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if _, err := agent.LoadSession(config.SessionsDir(), target.ID); err != nil {
		t.Fatal("another key between the two presses must cancel the delete")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if _, err := agent.LoadSession(config.SessionsDir(), target.ID); err == nil {
		t.Fatal("two ctrl+x presses in a row must delete the session")
	}

	app.rt.RegisterProvider("fake", echoProvider{})
	if _, err := app.rt.Spawn(t.Context(), tools.SpawnRequest{AgentType: "Explore", Prompt: "x", Description: "finished agent"}); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if len(app.rt.Tasks.Snapshot()) != 0 {
		t.Fatal("ctrl+x on a finished agent must remove it from the list")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewChat {
		t.Fatal("esc without a search leaves the list")
	}
}

func listNames(app *App) []string {
	var names []string
	for _, item := range app.home.items(app) {
		if item.isSession() {
			names = append(names, sessionName(item.session))
		}
	}
	return names
}
