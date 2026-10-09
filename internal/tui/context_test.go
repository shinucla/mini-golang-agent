package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
)

type sizedProvider struct {
	lists *atomic.Int32
}

func (sizedProvider) Name() string { return "fake" }

func (p sizedProvider) ListModels(context.Context) ([]llm.Model, error) {
	p.lists.Add(1)
	return []llm.Model{{ID: "my-model", ContextWindow: 200000}, {ID: "other-model", ContextWindow: 100000}}, nil
}

func (sizedProvider) Chat(context.Context, llm.Request, func(llm.Delta)) (*llm.Response, error) {
	return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}, nil
}

func TestContextSizeLearnedAtStartAndOnSessionOpen(t *testing.T) {
	app := newTestApp(t)
	lists := &atomic.Int32{}
	app.rt.RegisterProvider("fake", sizedProvider{lists: lists})
	app.rt.SetCurrent("fake", "my-model")
	app.contextTokens = 50000
	if strings.Contains(app.statusLine(), "ctx remaining") {
		t.Fatal("the size is unknown before the model list arrives")
	}
	drain(app, app.Init())
	if !strings.Contains(app.statusLine(), "75% ctx remaining") || lists.Load() != 1 {
		t.Fatalf("after start: %q, lists=%d", app.statusLine(), lists.Load())
	}

	other := saveTestSession(t, app.rt.Cwd, "other work", "q")
	other.Provider, other.Model, other.ContextTokens = "fake", "unlisted-model", 10000
	if err := other.Save(config.SessionsDir()); err != nil {
		t.Fatal(err)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	selectSession(t, app, other.ID)
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRight})
	drain(app, cmd)
	if lists.Load() != 2 {
		t.Fatalf("opening a session with an unknown size must look it up once, lists=%d", lists.Load())
	}

	app.rt.SetCurrent("fake", "other-model")
	if app.learnContextWindow() != nil {
		t.Fatal("a known size must not trigger another lookup")
	}
	app.rt.SetCurrent("fake", "gpt-5")
	if app.learnContextWindow() != nil {
		t.Fatal("a model in the built-in table must not trigger a lookup")
	}
}
