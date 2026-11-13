package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestOverlaysFromTheSessionListStayOnTheFullScreen(t *testing.T) {
	for _, c := range []struct {
		command string
		view    viewKind
	}{
		{"/model", viewModels},
		{"/agents", viewAgents},
		{"/mcp", viewMCP},
	} {
		app := newTestApp(t)
		app.Update(tea.KeyMsg{Type: tea.KeyLeft})
		if !app.alt {
			t.Fatal("the session list uses the alternate screen")
		}
		app.printQueue = nil
		typeText(app, c.command)
		app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if app.view != c.view || !app.alt {
			t.Fatalf("%s must open on the alternate screen: view=%v alt=%v", c.command, app.view, app.alt)
		}
		if strings.Contains(ansi.Strip(strings.Join(app.printQueue, "\n")), c.command) {
			t.Fatalf("%s must not leave its input line in the chat", c.command)
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if app.view != viewHome || !app.alt || app.home == nil {
			t.Fatalf("esc in %s must go back to the session list: view=%v alt=%v", c.command, app.view, app.alt)
		}
	}
}

func TestModelPickerFromTheChatStaysInline(t *testing.T) {
	app := newTestApp(t)
	typeText(app, "/model")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.view != viewModels || app.alt {
		t.Fatalf("/model in the chat stays inline: view=%v alt=%v", app.view, app.alt)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewChat || app.alt {
		t.Fatalf("esc goes back to the chat: view=%v alt=%v", app.view, app.alt)
	}
}
