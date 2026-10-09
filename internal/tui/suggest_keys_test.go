package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestArrowKeysMoveThroughSlashCommands(t *testing.T) {
	app := newTestApp(t)
	typeText(app, "/")
	all := app.suggestions()
	selected := func() string {
		c, _ := app.selectedSuggestion()
		return c.name
	}
	if selected() != all[0].name {
		t.Fatalf("the first command starts selected: %q", selected())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if selected() != all[1].name {
		t.Fatalf("down selects the next command: %q", selected())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyUp})
	app.Update(tea.KeyMsg{Type: tea.KeyUp})
	last := all[len(all)-1].name
	if selected() != last {
		t.Fatalf("up from the first command wraps to the last: %q", selected())
	}
	if !strings.Contains(ansi.Strip(app.View()), "/"+last) {
		t.Fatalf("the list scrolls to show the selected command:\n%s", ansi.Strip(app.View()))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if app.input.Value() != "/"+last+" " {
		t.Fatalf("tab completes the selected command: %q", app.input.Value())
	}

	app.editInput(app.input.Reset)
	typeText(app, "/mo")
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	typeText(app, "d")
	if selected() != "model" {
		t.Fatalf("a change to the text selects the first match again: %q", selected())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.input.Value() != "/mode " || len(app.inputHistory) != 0 {
		t.Fatalf("enter completes the selected command and does not run it: %q, history %v", app.input.Value(), app.inputHistory)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(app.inputHistory) != 1 || app.inputHistory[0] != "/mode" {
		t.Fatalf("a second enter runs the completed command: %v", app.inputHistory)
	}

	typeText(app, "/help")
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if app.input.Value() != "/help " {
		t.Fatalf("tab completes a command that is typed in full: %q", app.input.Value())
	}
	app.editInput(app.input.Reset)
	typeText(app, "/help")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.input.Value() != "" || app.inputHistory[len(app.inputHistory)-1] != "/help" {
		t.Fatalf("enter runs a command that is typed in full: %q", app.input.Value())
	}
}

func TestArrowKeysStillRecallSlashCommandsFromHistory(t *testing.T) {
	app := newTestApp(t)
	app.inputHistory = []string{"/model", "/help"}
	app.historyPos = len(app.inputHistory)
	app.Update(tea.KeyMsg{Type: tea.KeyUp})
	app.Update(tea.KeyMsg{Type: tea.KeyUp})
	if app.input.Value() != "/model" {
		t.Fatalf("up keeps moving through the history: %q", app.input.Value())
	}
}

func TestArrowKeysMoveThroughSlashCommandsInSessionList(t *testing.T) {
	app := newTestApp(t)
	saveTestSession(t, app.rt.Cwd, "first work", "a")
	saveTestSession(t, app.rt.Cwd, "second work", "b")
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "/")
	all := app.suggestions()
	cursor := app.home.cursor
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c, _ := app.selectedSuggestion(); c.name != all[1].name || app.home.cursor != cursor {
		t.Fatalf("down moves the command selection, not the session cursor: %q, cursor %d", c.name, app.home.cursor)
	}
	app.editInput(app.input.Reset)
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if app.home.cursor == cursor {
		t.Fatal("with no command list, down moves the session cursor")
	}
}
