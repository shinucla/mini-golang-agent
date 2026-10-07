package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type Todo struct {
	Content    string     `json:"content"`
	Status     TodoStatus `json:"status"`
	ActiveForm string     `json:"activeForm,omitempty"`
}

type TodoWrite struct{}

func (TodoWrite) Name() string   { return "TodoWrite" }
func (TodoWrite) ReadOnly() bool { return true }

func (TodoWrite) Description() string {
	return "Create or replace the task list for this session. Use it for work with three or more steps. " +
		"Keep exactly one task in_progress while you work, and mark each task completed as soon as it is done."
}

func (TodoWrite) Schema() map[string]any {
	return schema(map[string]any{
		"todos": map[string]any{
			"type":        "array",
			"description": "The full, updated task list",
			"items": schema(map[string]any{
				"content":    str("What to do, in imperative form"),
				"status":     map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
				"activeForm": str("Present continuous form, shown while the task runs"),
			}, "content", "status"),
		},
	}, "todos")
}

func (TodoWrite) Summary(input json.RawMessage) string {
	in, err := decode[struct {
		Todos []Todo `json:"todos"`
	}](input)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d tasks", len(in.Todos))
}

func (TodoWrite) Run(_ context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		Todos []Todo `json:"todos"`
	}](input)
	if err != nil {
		return "", err
	}
	env.setTodos(in.Todos)
	return "Task list updated:\n" + FormatTodos(in.Todos), nil
}

func FormatTodos(todos []Todo) string {
	var b strings.Builder
	for _, t := range todos {
		mark := "☐"
		switch t.Status {
		case TodoInProgress:
			mark = "◐"
		case TodoCompleted:
			mark = "☒"
		}
		fmt.Fprintf(&b, "%s %s\n", mark, t.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}
