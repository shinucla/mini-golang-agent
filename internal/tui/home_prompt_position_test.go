package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/shinucla/mini-golang-agent/internal/tools"
)

func lineIndex(view, text string) int {
	for i, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(line, text) {
			return i
		}
	}
	return -1
}

func TestSearchLineUsesTheRenameLineBelowTheAgents(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	saveTestSession(t, app.rt.Cwd, "Fix login bug", "a")
	saveTestSession(t, app.rt.Cwd, "Agent docs", "b")
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	rename := lineIndex(app.View(), "enter save · esc cancel") - 1
	if rename <= lineIndex(app.View(), "No sub-agents in this run.") {
		t.Fatalf("the rename field comes after the Agents section:\n%s", ansi.Strip(app.View()))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	search := lineIndex(app.View(), "of 2 sessions")
	if search <= lineIndex(app.View(), "Fix login bug") || search <= lineIndex(app.View(), "Agent docs") {
		t.Fatalf("the search line comes after the session rows:\n%s", ansi.Strip(app.View()))
	}
	if lineIndex(app.View(), "esc cancel") != search+1 {
		t.Fatalf("the hint esc cancel comes under the search line, as under the rename field:\n%s", ansi.Strip(app.View()))
	}
	if search != rename || lineIndex(app.View(), "No sub-agents in this run.") <= 0 {
		t.Fatalf("the search line (%d) uses the rename line (%d) and the Agents section stays:\n%s", search, rename, ansi.Strip(app.View()))
	}

	typeText(app, "zzz")
	view := app.View()
	if lineIndex(view, "No session name matches.") <= 0 || lineIndex(view, "0 of 2 sessions") <= lineIndex(view, "No sub-agents in this run.") {
		t.Fatalf("with no match the note is in the list and the search line stays at the bottom:\n%s", ansi.Strip(view))
	}
}

func TestSearchKeepsTheAgentRows(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	saveTestSession(t, app.rt.Cwd, "Fix login bug", "a")
	app.rt.RegisterProvider("fake", echoProvider{})
	if _, err := app.rt.Spawn(t.Context(), tools.SpawnRequest{AgentType: "Explore", Prompt: "x", Description: "finished agent"}); err != nil {
		t.Fatal(err)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	typeText(app, "zzz")
	view := app.View()
	agents, row, search := lineIndex(view, "sub-agents of this run"), lineIndex(view, "finished agent"), lineIndex(view, "0 of 1 sessions")
	if agents <= 0 || row <= agents || search <= row || 0 <= lineIndex(view, "No sub-agents in this run.") {
		t.Fatalf("the search keeps the Agents section with its rows, and the search line comes after it:\n%s", ansi.Strip(view))
	}
}
