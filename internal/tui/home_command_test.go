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

func TestCtrlNIsGoneFromTheSessionList(t *testing.T) {
	app := newTestApp(t)
	before := app.sessionRun
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if app.view != viewHome || app.sessionRun != before || len(app.runs) != 1 {
		t.Fatal("ctrl+n must no longer start a session from the list")
	}
	typeText(app, "?")
	if strings.Contains(ansi.Strip(app.View()), "ctrl+n") {
		t.Fatal("the help must not list ctrl+n")
	}
}

func TestPlaceholderShowsOnlyWithoutSessions(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	view := ansi.Strip(app.View())
	if !strings.Contains(view, "● (new session)") || !strings.Contains(view, "current") {
		t.Fatalf("with no sessions, the placeholder shows as current:\n%s", view)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	opened := saveTestSession(t, app.rt.Cwd, "real work", "q")
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if view := ansi.Strip(app.View()); strings.Contains(view, "(new session)") || strings.Contains(view, "current") {
		t.Fatalf("with sessions, the unused placeholder hides:\n%s", view)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if view := ansi.Strip(app.View()); !strings.Contains(view, "● real work") || !strings.Contains(view, "current") {
		t.Fatalf("an opened session shows as current after coming back:\n%s", view)
	}
	if app.session.ID != opened.ID {
		t.Fatal("enter must have opened the first session")
	}
}
