package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
)

type gatedProvider struct {
	gate chan struct{}
}

func (gatedProvider) Name() string                                    { return "fake" }
func (gatedProvider) ListModels(context.Context) ([]llm.Model, error) { return nil, nil }

func (p gatedProvider) Chat(ctx context.Context, req llm.Request, _ func(llm.Delta)) (*llm.Response, error) {
	reply := func(text string) (*llm.Response, error) {
		return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: text}}, nil
	}
	if strings.Contains(req.System, "name chat sessions") {
		return reply("Some title")
	}
	last, toolResult := "", ""
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleUser:
			last, toolResult = m.Content, ""
		case llm.RoleTool:
			toolResult = m.Content
		}
	}
	switch {
	case strings.Contains(last, "needs bash") && toolResult == "":
		return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "b1", Name: "Bash", Arguments: `{"command":"echo approved-run"}`},
		}}}, nil
	case strings.Contains(last, "needs bash"):
		return reply("bash said: " + toolResult)
	case strings.Contains(last, "slow"):
		select {
		case <-p.gate:
			return reply("slow done")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return reply("fast: " + last)
}

type harness struct {
	t    *testing.T
	app  *App
	msgs chan tea.Msg
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, app: newTestApp(t), msgs: make(chan tea.Msg, 1000)}
	h.app.send = func(m tea.Msg) { h.msgs <- m }
	for _, r := range h.app.runs {
		h.app.wire(r)
	}
	h.app.rt.Approve = h.app.approver
	return h
}

func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		switch m := cmd().(type) {
		case nil, spinner.TickMsg:
		case tea.BatchMsg:
			for _, c := range m {
				h.run(c)
			}
		default:
			if name := fmt.Sprintf("%T", m); strings.HasPrefix(name, "cursor.") || strings.HasPrefix(name, "tea.") {
				return
			}
			h.msgs <- m
		}
	}()
}

func (h *harness) update(m tea.Msg) {
	_, cmd := h.app.Update(m)
	h.run(cmd)
}

func (h *harness) key(k tea.KeyType) { h.update(tea.KeyMsg{Type: k}) }

func (h *harness) send(text string) {
	for _, r := range text {
		h.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	h.key(tea.KeyEnter)
}

func (h *harness) until(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case m := <-h.msgs:
			h.update(m)
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (h *harness) view() string { return ansi.Strip(h.app.View()) }

func TestSessionsKeepWorkingInTheBackground(t *testing.T) {
	h := newHarness(t)
	gate := make(chan struct{})
	h.app.rt.RegisterProvider("fake", gatedProvider{gate: gate})
	h.app.provider, h.app.model = "fake", "m"
	a := h.app.sessionRun

	h.send("a slow task")
	if !a.busy {
		t.Fatal("session A must be working")
	}
	h.key(tea.KeyLeft)
	if h.app.view != viewHome || !h.app.alt {
		t.Fatal("the session list must open on the full screen")
	}
	view := h.view()
	if !strings.Contains(view, "Working") || !strings.Contains(view, "working") {
		t.Fatalf("A must show under Working:\n%s", view)
	}

	h.send("/new")
	b := h.app.sessionRun
	if b == a || !a.busy {
		t.Fatal("a new session must not stop A")
	}
	if line := ansi.Strip(h.app.statusLine()); !strings.Contains(line, "1 other session(s) working") {
		t.Fatalf("the status bar must count the working session: %q", line)
	}
	h.send("quick question")
	h.until("B's answer", func() bool { return !b.busy && 2 <= len(b.history) })
	if b.history[1].Content != "fast: quick question" || !a.busy {
		t.Fatalf("B = %+v, A busy = %v", b.history, a.busy)
	}

	h.send("this needs bash")
	h.key(tea.KeyLeft)
	h.send("/new")
	c := h.app.sessionRun
	h.until("B's approval request", func() bool { return b.waiting() })
	if strings.Contains(h.view(), "Do you want to proceed?") {
		t.Fatal("B's approval must not show in session C")
	}
	if !strings.Contains(h.app.notice, "needs your input") {
		t.Fatalf("notice = %q", h.app.notice)
	}
	if line := ansi.Strip(h.app.statusLine()); !strings.Contains(line, "1 session(s) need your input") {
		t.Fatalf("the status bar must count the waiting session: %q", line)
	}
	h.key(tea.KeyLeft)
	if !strings.Contains(h.view(), "needs input") {
		t.Fatalf("B must show as needing input:\n%s", h.view())
	}

	h.app.loadSession(b.session)
	h.app.closeOverlay()
	if h.app.sessionRun != b || !strings.Contains(h.view(), "Do you want to proceed?") {
		t.Fatalf("switching to B must show its approval:\n%s", h.view())
	}
	h.key(tea.KeyEnter)
	h.until("B's bash turn", func() bool { return !b.busy })
	if got := b.history[len(b.history)-1].Content; got != "bash said: approved-run" {
		t.Fatalf("B's last answer = %q", got)
	}

	close(gate)
	h.until("A's slow turn", func() bool { return !a.busy })
	saved, err := agent.LoadSession(config.SessionsDir(), a.session.ID)
	if err != nil || saved.Messages[len(saved.Messages)-1].Content != "slow done" {
		t.Fatalf("A must finish and save in the background: %v", err)
	}
	if c.busy || len(h.app.runs) != 3 {
		t.Fatalf("runs = %d", len(h.app.runs))
	}

	h.app.printing = true
	h.app.printQueue = nil
	h.app.loadSession(a.session)
	if out := ansi.Strip(strings.Join(h.app.printQueue, "\n")); !strings.Contains(out, "slow done") || !strings.Contains(out, "a slow task") {
		t.Fatalf("A's replay:\n%s", out)
	}
}
