package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSlashCommandsFromTheSessionList(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.printing = true

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "/sta")
	lines := strings.Split(ansi.Strip(app.View()), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "/status") {
		t.Fatalf("suggestions must replace the hint line: %q", last)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if app.input.Value() != "/status " {
		t.Fatalf("tab must complete the command: %q", app.input.Value())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	out := ansi.Strip(strings.Join(app.printQueue, "\n"))
	if app.view != viewChat || !strings.Contains(out, "> /status") || !strings.Contains(out, "Provider:") {
		t.Fatalf("/status must close the list and print in the chat: view=%v\n%s", app.view, out)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "/model")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.view != viewModels {
		t.Fatalf("/model must open the model picker, view=%v", app.view)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "/resume")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.view != viewHome || app.input.Value() != "" {
		t.Fatal("plain /resume stays in the list")
	}

	app.printQueue = nil
	typeText(app, "/nonsense")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.view != viewChat || !strings.Contains(ansi.Strip(strings.Join(app.printQueue, "\n")), "Unknown command /nonsense") {
		t.Fatal("an unknown command must close the list and say so")
	}

	app.rt.RegisterProvider("fake", echoProvider{})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "not a command")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.view != viewHome || app.session.Title != "not a command" {
		t.Fatalf("plain text still starts a new session: view=%v title=%q", app.view, app.session.Title)
	}
}
