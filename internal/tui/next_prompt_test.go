package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/llm"
)

type predictProvider struct{ echoProvider }

func (predictProvider) Chat(ctx context.Context, req llm.Request, onDelta func(llm.Delta)) (*llm.Response, error) {
	if req.System == agent.SuggestSystemPrompt {
		return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: "\"Go with option 2\"\n"}}, nil
	}
	return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: "Option 1 or option 2? I recommend option 2."}}, nil
}

func TestNextPromptShowsAsGhostTextAndTabAcceptsIt(t *testing.T) {
	predictEnabled = true
	t.Cleanup(func() { predictEnabled = false })
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", predictProvider{})
	app.send = func(msg tea.Msg) { app.Update(msg) }

	typeText(app, "fix the bug")
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drain(app, cmd)
	if app.nextPrompt != "Go with option 2" || !strings.Contains(app.View(), "Go with option 2  · tab to accept") {
		t.Fatalf("the prediction shows in the empty input: %q\n%s", app.nextPrompt, app.View())
	}

	ghost := func() bool { return strings.Contains(app.View(), "tab to accept") }

	press(app, tea.KeyTab)
	if got := app.input.Value(); got != "Go with option 2" || ghost() {
		t.Fatalf("tab fills the input with the prediction, and the ghost text hides: %q", got)
	}
	press(app, tea.KeyEsc)
	if app.input.Value() != "" || !ghost() {
		t.Fatal("the ghost text comes back when the input is empty again")
	}

	typeText(app, "x")
	if ghost() {
		t.Fatal("typed text hides the ghost text")
	}
	press(app, tea.KeyBackspace)
	if !ghost() {
		t.Fatal("deleting back to an empty input shows the ghost text again")
	}
	press(app, tea.KeyRight)
	if app.input.Value() != "Go with option 2" {
		t.Fatal("→ also accepts the prediction")
	}
	app.editInput(app.input.Reset)

	stale := app.promptSeq
	app.dropNextPrompt()
	app.Update(nextPromptMsg{run: app.sessionRun, seq: stale, text: "old"})
	if app.nextPrompt != "" {
		t.Fatal("a prediction for an earlier reply is ignored")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/suggestions off")})
	press(app, tea.KeyEnter)
	if app.rt.SuggestsPrompts() {
		t.Fatal("/suggestions off turns the feature off")
	}
	typeText(app, "again")
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drain(app, cmd)
	if app.nextPrompt != "" {
		t.Fatalf("no prediction while the feature is off: %q", app.nextPrompt)
	}
}
