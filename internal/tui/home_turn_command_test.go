package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shinucla/mini-golang-agent/internal/llm"
)

func TestSkillsAndCommandsFromTheSessionListStartANewSession(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", echoProvider{})
	withPluginParts(app)
	app.printing = true
	highlighted := app.sessionRun
	highlighted.history = []llm.Message{{Role: llm.RoleUser, Content: "old work"}, {Role: llm.RoleAssistant, Content: "done"}}

	for _, c := range []struct{ typed, prompt string }{
		{"/acme:greet Bob", "Say hello to Bob."},
		{"/init", initPrompt},
	} {
		app.Update(tea.KeyMsg{Type: tea.KeyLeft})
		app.home.cursor = 0
		typeText(app, c.typed)
		app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if app.sessionRun == highlighted || len(highlighted.history) != 2 {
			t.Fatalf("%s must leave the highlighted session alone", c.typed)
		}
		if got := lastUserMessage(app); got != c.prompt {
			t.Fatalf("%s must start the new session with its prompt: %q", c.typed, got)
		}
		if app.view != viewHome || app.session.Title != c.typed {
			t.Fatalf("%s must keep the list open and name the session after the command: view=%v title=%q", c.typed, app.view, app.session.Title)
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	}
}

func TestEveryBuiltinCommandIsKnownToTheSessionList(t *testing.T) {
	for _, c := range commands {
		if c.name != "init" && !builtinCommands[c.name] {
			t.Fatalf("/%s is missing from builtinCommands, so the session list treats it as a skill", c.name)
		}
	}
}
