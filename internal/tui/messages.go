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
	deltaMsg     llm.Delta
	assistantMsg llm.Message
	usageMsg     llm.Usage
	todosMsg     []tools.Todo
	printDoneMsg struct{}

	toolStartMsg struct {
		call    llm.ToolCall
		summary string
	}
	toolResultMsg struct {
		call   llm.ToolCall
		result string
		isErr  bool
	}
	turnDoneMsg struct {
		msgs []llm.Message
		err  error
	}
	approvalMsg struct {
		req   agent.ApprovalRequest
		reply chan agent.Decision
		ctx   context.Context
	}
	tasksChangedMsg struct{}
	mcpChangedMsg   struct{}
	taskFinishedMsg agent.Task
	modelsLoadedMsg struct {
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
}

func (o uiObserver) Delta(d llm.Delta)              { o.send(deltaMsg(d)) }
func (o uiObserver) AssistantMessage(m llm.Message) { o.send(assistantMsg(m)) }
func (o uiObserver) Usage(u llm.Usage)              { o.send(usageMsg(u)) }

func (o uiObserver) ToolStart(call llm.ToolCall, t tools.Tool) {
	o.send(toolStartMsg{call: call, summary: t.Summary(json.RawMessage(call.Arguments))})
}

func (o uiObserver) ToolResult(call llm.ToolCall, result string, isErr bool) {
	o.send(toolResultMsg{call: call, result: result, isErr: isErr})
}
