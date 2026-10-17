package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/llm"
)

func holdLock(t *testing.T, id string) {
	t.Helper()
	path := filepath.Join(config.SessionsDir(), id+".lock")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getppid())), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
}

func printed(app *App) string {
	return ansi.Strip(strings.Join(app.printQueue, "\n"))
}

func TestSyncShowsATurnFromAnotherProcess(t *testing.T) {
	app := newTestApp(t)
	s := saveTestSession(t, app.rt.Cwd, "shared work", "first question")
	app.loadSession(s)

	time.Sleep(10 * time.Millisecond)
	other, err := agent.LoadSession(config.SessionsDir(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	other.Messages = append(other.Messages,
		llm.Message{Role: llm.RoleUser, Content: "question from the dashboard"},
		llm.Message{Role: llm.RoleAssistant, Content: "answer for the dashboard"})
	if err := other.Save(config.SessionsDir()); err != nil {
		t.Fatal(err)
	}

	app.syncSessions()
	if len(app.history) != 4 {
		t.Fatalf("the open session takes the new turn: %d messages", len(app.history))
	}
	out := printed(app)
	for _, want := range []string{"Another mga process updated this session", "question from the dashboard", "answer for the dashboard"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the screen shows %q:\n%s", want, out)
		}
	}
	before := len(app.printQueue)
	app.syncSessions()
	if len(app.printQueue) != before {
		t.Fatal("an unchanged file is not shown again")
	}
}

func TestTurnWaitsWhileAnotherProcessWorks(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", echoProvider{})
	s := saveTestSession(t, app.rt.Cwd, "busy elsewhere", "first question")
	app.loadSession(s)
	holdLock(t, s.ID)

	typeText(app, "second question")
	press(app, tea.KeyEnter)
	if len(app.history) != 2 || app.busy {
		t.Fatalf("no turn starts while another process works: history=%d busy=%v", len(app.history), app.busy)
	}
	if !strings.Contains(printed(app), "is working in another mga process") || app.input.Value() != "second question" {
		t.Fatalf("the error shows and the text stays in the input: %q\n%s", app.input.Value(), printed(app))
	}

	app.editInput(app.input.Reset)
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app.syncSessions()
	if !strings.Contains(ansi.Strip(app.View()), "working in another") {
		t.Fatalf("the session list marks the busy session:\n%s", ansi.Strip(app.View()))
	}
}

func TestTurnReleasesItsLock(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", echoProvider{})
	typeText(app, "hello")
	press(app, tea.KeyEnter)
	if len(app.history) != 2 {
		t.Fatalf("the turn runs: %d", len(app.history))
	}
	if _, err := os.Stat(filepath.Join(config.SessionsDir(), app.session.ID+".lock")); !os.IsNotExist(err) {
		t.Fatal("the lock is gone after the turn")
	}
}
