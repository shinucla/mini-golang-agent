package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionListKeyPanel(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !strings.Contains(ansi.Strip(app.View()), "esc back · ? help") {
		t.Fatalf("hint line:\n%s", ansi.Strip(app.View()))
	}

	typeText(app, "?")
	view := ansi.Strip(app.View())
	for _, want := range []string{"Session list keys", "rename the session", "start a new session", "press twice on a session to delete it", "stop a running agent", "search sessions by name"} {
		if !strings.Contains(view, want) {
			t.Fatalf("key panel misses %q:\n%s", want, view)
		}
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewHome || strings.Contains(ansi.Strip(app.View()), "Session list keys") {
		t.Fatal("esc must close only the key panel")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewChat {
		t.Fatal("a second esc must leave the session list")
	}

	app.session.Title = "named"
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	typeText(app, "?")
	if app.home.showKeys || !strings.HasSuffix(app.home.nameInput.Value(), "?") {
		t.Fatal("while renaming, ? is part of the name")
	}
}
