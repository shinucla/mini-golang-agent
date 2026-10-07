package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const maxToolResult = 60000

var ErrDenied = errors.New("the user denied a tool call")

type Observer interface {
	Delta(d llm.Delta)
	AssistantMessage(m llm.Message)
	ToolStart(call llm.ToolCall, t tools.Tool)
	ToolResult(call llm.ToolCall, result string, isError bool)
	Usage(u llm.Usage)
}

type Agent struct {
	Name       string
	Provider   llm.Provider
	Model      string
	System     string
	Tools      []tools.Tool
	Env        *tools.Env
	Perms      *Permissions
	Approve    Approver
	Review     Reviewer
	AskPolicy  AskPolicy
	StopOnDeny bool
	MaxSteps   int

	userRequest string
}

func (a *Agent) Run(ctx context.Context, history []llm.Message, obs Observer) ([]llm.Message, error) {
	msgs := slices.Clone(history)
	a.userRequest = lastUserText(msgs)
	specs := make([]llm.ToolSpec, len(a.Tools))
	for i, t := range a.Tools {
		specs[i] = llm.ToolSpec{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()}
	}
	for step := 0; a.MaxSteps == 0 || step < a.MaxSteps; step++ {
		resp, err := a.Provider.Chat(ctx, llm.Request{Model: a.Model, System: a.System, Messages: msgs, Tools: specs}, obs.Delta)
		if err != nil {
			return msgs, err
		}
		obs.Usage(resp.Usage)
		msgs = append(msgs, resp.Message)
		obs.AssistantMessage(resp.Message)
		if len(resp.Message.ToolCalls) == 0 {
			return msgs, nil
		}
		results, err := a.runTools(ctx, resp.Message.ToolCalls, obs)
		msgs = append(msgs, results...)
		if err != nil {
			return msgs, err
		}
	}
	return msgs, fmt.Errorf("stopped after %d steps", a.MaxSteps)
}

func (a *Agent) findTool(name string) tools.Tool {
	for _, t := range a.Tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

func (a *Agent) runTools(ctx context.Context, calls []llm.ToolCall, obs Observer) ([]llm.Message, error) {
	results := make([]llm.Message, len(calls))
	if a.allReadOnly(calls) && 1 < len(calls) {
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], _ = a.runTool(ctx, call, obs)
			}()
		}
		wg.Wait()
		return results, ctx.Err()
	}
	var stop error
	for i, call := range calls {
		if stop == nil {
			stop = ctx.Err()
		}
		if stop != nil {
			results[i] = toolMessage(call, "Not run: "+stop.Error(), true)
			obs.ToolResult(call, results[i].Content, true)
			continue
		}
		results[i], stop = a.runTool(ctx, call, obs)
	}
	return results, stop
}

func (a *Agent) allReadOnly(calls []llm.ToolCall) bool {
	for _, c := range calls {
		t := a.findTool(c.Name)
		if t == nil || !t.ReadOnly() {
			return false
		}
	}
	return true
}

func (a *Agent) runTool(ctx context.Context, call llm.ToolCall, obs Observer) (llm.Message, error) {
	finish := func(content string, isErr bool) llm.Message {
		content = tools.Truncate(content, maxToolResult)
		obs.ToolResult(call, content, isErr)
		return toolMessage(call, content, isErr)
	}
	t := a.findTool(call.Name)
	if t == nil {
		return finish(fmt.Sprintf("Unknown tool %q", call.Name), true), nil
	}
	obs.ToolStart(call, t)
	input := json.RawMessage(call.Arguments)
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	if !json.Valid(input) {
		return finish("The tool arguments are not valid JSON: "+call.Arguments, true), nil
	}

	verdict, reason := a.Perms.check(t, input)
	if verdict == verdictReview {
		verdict, reason = a.review(ctx, t, input)
	}
	switch verdict {
	case verdictBlock:
		return finish(reason, true), nil
	case verdictAsk:
		decision := Deny
		if a.Approve != nil {
			decision = a.Approve(ctx, ApprovalRequest{
				Agent: a.Name, Tool: t.Name(), Summary: t.Summary(input), Input: input,
				AlwaysLabel: AlwaysLabel(t.Name(), input), Reason: reason,
			})
		}
		if ctx.Err() != nil {
			return finish("Interrupted by the user", true), ctx.Err()
		}
		switch decision {
		case Deny:
			msg := finish("The user denied this tool call. Do not retry it the same way.", true)
			if a.StopOnDeny {
				return msg, ErrDenied
			}
			return msg, nil
		case AllowAlways:
			a.Perms.remember(t, input)
		}
	}

	out, err := t.Run(ctx, a.Env, input)
	if verdict == verdictAllow && reason != "" {
		out = "(" + reason + ")\n" + out
	}
	if err != nil {
		if out != "" {
			out += "\n"
		}
		return finish(out+"Error: "+err.Error(), true), nil
	}
	return finish(out, false), nil
}

func toolMessage(call llm.ToolCall, content string, isErr bool) llm.Message {
	return llm.Message{Role: llm.RoleTool, Content: content, ToolCallID: call.ID, ToolName: call.Name, IsError: isErr}
}

func LastAssistantText(msgs []llm.Message) string {
	for i := len(msgs) - 1; 0 <= i; i-- {
		if msgs[i].Role == llm.RoleAssistant && msgs[i].Content != "" {
			return msgs[i].Content
		}
	}
	return ""
}
