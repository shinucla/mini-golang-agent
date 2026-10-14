package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/llm"
	"github.com/shinucla/mini-golang-agent/internal/tools"
)

func TestClearEmptiesTheSessionAndKeepsIt(t *testing.T) {
	app := newTestApp(t)
	s := saveTestSession(t, app.rt.Cwd, "login bug", "fix the login bug", "now run the tests")
	if err := s.SetPin(config.SessionsDir(), true, 1); err != nil {
		t.Fatal(err)
	}
	app.loadSession(s)
	app.usage = llm.Usage{InputTokens: 900, OutputTokens: 100}
	app.contextTokens = 1000
	app.todos = []tools.Todo{{Content: "old task", Status: "pending"}}
	app.queue = []string{"queued message"}

	runSlash(app, "/clear")
	if app.session.ID != s.ID {
		t.Fatalf("/clear keeps the session: %s, want %s", app.session.ID, s.ID)
	}
	if len(app.history) != 0 || len(app.queue) != 0 || len(app.todos) != 0 || app.usage != (llm.Usage{}) || app.contextTokens != 0 {
		t.Fatalf("/clear empties the conversation: history=%d queue=%v todos=%v usage=%+v ctx=%d", len(app.history), app.queue, app.todos, app.usage, app.contextTokens)
	}
	if !app.clearScreen || !strings.Contains(ansi.Strip(strings.Join(app.printQueue, "\n")), "Cleared the conversation") {
		t.Fatal("/clear clears the screen and says so")
	}
	saved, err := agent.LoadSession(config.SessionsDir(), s.ID)
	if err != nil || len(saved.Messages) != 0 || saved.Title != "login bug" || !saved.Pinned {
		t.Fatalf("the cleared session is saved with its name and pin: %+v err=%v", saved, err)
	}
	if strings.Contains(ansi.Strip(app.View()), "fix the login bug") {
		t.Fatal("the old conversation is gone from the screen")
	}
	if app.needsTitle("a new topic") {
		t.Fatal("the next message after /clear does not rename the session")
	}
}

func TestClearWaitsForTheTurnAndNewStillStartsASession(t *testing.T) {
	app := newTestApp(t)
	s := saveTestSession(t, app.rt.Cwd, "busy work", "do the work")
	app.loadSession(s)
	app.busy = true
	runSlash(app, "/clear")
	if len(app.history) == 0 || !strings.Contains(ansi.Strip(strings.Join(app.printQueue, "\n")), "Press esc to stop the turn") {
		t.Fatal("/clear does nothing while a turn runs")
	}
	app.busy = false

	runSlash(app, "/new")
	if app.session.ID == s.ID || len(app.history) != 0 {
		t.Fatal("/new starts a new session")
	}
}
