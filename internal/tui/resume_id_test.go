package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func runSlash(app *App, text string) {
	typeText(app, text)
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drain(app, cmd)
}

func TestResumeByID(t *testing.T) {
	app := newTestApp(t)
	other := saveTestSession(t, filepath.Join(app.rt.Cwd, "elsewhere"), "other folder work", "earlier question")

	runSlash(app, "/resume "+other.ID)
	if app.view != viewChat || app.session.ID != other.ID || len(app.history) != 2 {
		t.Fatalf("view=%v session=%s history=%d", app.view, app.session.ID, len(app.history))
	}

	for _, bad := range []string{"20991231-000000-abcdef", "../../etc/passwd"} {
		runSlash(app, "/resume "+bad)
		view := ansi.Strip(app.View())
		if app.view != viewHome || !strings.Contains(view, `Session "`+bad+`" does not exist`) {
			t.Fatalf("unknown id %q: view=%v\n%s", bad, app.view, view)
		}
		if app.session.ID != other.ID {
			t.Fatal("an unknown id must not change the current session")
		}
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
		if strings.Contains(ansi.Strip(app.View()), "does not exist") {
			t.Fatal("the error must clear on the next key")
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	}

	runSlash(app, "/resume "+other.ID)
	if app.view != viewChat || !strings.Contains(ansi.Strip(strings.Join(app.printQueue, "\n")), "Already in session") {
		t.Fatal("resuming the current session must say so and stay in the chat")
	}

	runSlash(app, "/resume")
	if app.view != viewHome || strings.Contains(ansi.Strip(app.View()), "does not exist") {
		t.Fatal("plain /resume must open the list without an error")
	}
}
