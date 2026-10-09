package tui

import (
	"context"
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type (
	printDoneMsg struct{}

	deltaMsg struct {
		run   *sessionRun
		delta llm.Delta
	}
	assistantMsg struct {
		run     *sessionRun
		message llm.Message
	}
	usageMsg struct {
		run   *sessionRun
		usage llm.Usage
	}
	todosMsg struct {
		run   *sessionRun
		todos []tools.Todo
	}
	toolStartMsg struct {
		run     *sessionRun
		call    llm.ToolCall
		summary string
	}
	toolResultMsg struct {
		run    *sessionRun
		call   llm.ToolCall
		result string
		isErr  bool
	}
	turnDoneMsg struct {
		run  *sessionRun
		msgs []llm.Message
		err  error
	}
	approvalMsg struct {
		req   agent.ApprovalRequest
		reply chan agent.Decision
		ctx   context.Context
	}
	tasksChangedMsg  struct{}
	mcpChangedMsg    struct{}
	contextWindowMsg struct{}
	taskFinishedMsg  agent.Task
	modelsLoadedMsg  struct {
		provider string
		models   []llm.Model
		err      error
	}
	keyCheckedMsg struct {
		provider string
		key      string
		models   []llm.Model
		err      error
	}
	titleMsg struct {
		sessionID string
		title     string
		err       error
	}
	compactDoneMsg struct {
		run     *sessionRun
		summary string
		err     error
	}
	editorDoneMsg   struct{ err error }
	agentStartedMsg struct {
		name string
		err  error
	}
)

type uiObserver struct {
	send func(tea.Msg)
	run  *sessionRun
}

func (o uiObserver) Delta(d llm.Delta) { o.send(deltaMsg{run: o.run, delta: d}) }

func (o uiObserver) AssistantMessage(m llm.Message) { o.send(assistantMsg{run: o.run, message: m}) }

func (o uiObserver) Usage(u llm.Usage) { o.send(usageMsg{run: o.run, usage: u}) }

func (o uiObserver) ToolStart(call llm.ToolCall, t tools.Tool) {
	o.send(toolStartMsg{run: o.run, call: call, summary: t.Summary(json.RawMessage(call.Arguments))})
}

func (o uiObserver) ToolResult(call llm.ToolCall, result string, isErr bool) {
	o.send(toolResultMsg{run: o.run, call: call, result: result, isErr: isErr})
}
