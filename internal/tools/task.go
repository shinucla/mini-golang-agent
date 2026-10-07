package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type AgentInfo struct {
	Name        string
	Description string
	Tools       []string
}

type Task struct {
	Agents func() []AgentInfo
}

func (Task) Name() string   { return "Task" }
func (Task) ReadOnly() bool { return true }

func (t Task) Description() string {
	var b strings.Builder
	b.WriteString("Start a sub-agent that works on its own on a complex, multi-step task and returns one final report. " +
		"The sub-agent does not see this conversation, so give it a complete, self-contained prompt. " +
		"Start independent sub-agents in parallel with several Task calls in one response. " +
		"Set run_in_background to true to continue while it works; you get a notification when it finishes.\n\nAvailable agent types:\n")
	if t.Agents != nil {
		for _, a := range t.Agents() {
			tools := "all tools"
			if len(a.Tools) != 0 {
				tools = strings.Join(a.Tools, ", ")
			}
			fmt.Fprintf(&b, "- %s: %s (Tools: %s)\n", a.Name, a.Description, tools)
		}
	}
	return b.String()
}

func (Task) Schema() map[string]any {
	return schema(map[string]any{
		"description":       str("A short (3-5 word) description of the task"),
		"prompt":            str("The complete task for the agent"),
		"subagent_type":     str("The agent type to use (default: general-purpose)"),
		"run_in_background": boolean("Run the agent in the background"),
	}, "description", "prompt")
}

func (Task) Summary(input json.RawMessage) string {
	kind := field(input, "subagent_type")
	if kind == "" {
		kind = "general-purpose"
	}
	return kind + ": " + field(input, "description")
}

func (Task) Run(ctx context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		Description     string `json:"description"`
		Prompt          string `json:"prompt"`
		SubagentType    string `json:"subagent_type"`
		RunInBackground bool   `json:"run_in_background"`
	}](input)
	if err != nil {
		return "", err
	}
	if env.Spawn == nil {
		return "", errors.New("sub-agents cannot start other sub-agents")
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return "", errors.New("prompt is empty")
	}
	return env.Spawn(ctx, SpawnRequest{
		AgentType:   in.SubagentType,
		Description: in.Description,
		Prompt:      in.Prompt,
		Background:  in.RunInBackground,
	})
}
