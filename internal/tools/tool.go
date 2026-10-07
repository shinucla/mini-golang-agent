package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	ReadOnly() bool
	Summary(input json.RawMessage) string
	Run(ctx context.Context, env *Env, input json.RawMessage) (string, error)
}

type SpawnRequest struct {
	AgentType   string
	Description string
	Prompt      string
	Background  bool
	Manual      bool
}

type Env struct {
	Cwd     string
	OnTodos func([]Todo)
	Spawn   func(ctx context.Context, req SpawnRequest) (string, error)

	mu        sync.Mutex
	readFiles map[string]time.Time
	todos     []Todo
}

func (e *Env) Resolve(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(e.Cwd, path)
}

func (e *Env) markRead(path string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.readFiles == nil {
		e.readFiles = map[string]time.Time{}
	}
	e.readFiles[path] = time.Now()
}

func (e *Env) checkFresh(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	e.mu.Lock()
	readAt, ok := e.readFiles[path]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("read %s with the Read tool before you change it", path)
	}
	if readAt.Before(info.ModTime()) {
		return fmt.Errorf("%s changed after you read it; read it again first", path)
	}
	return nil
}

func (e *Env) Todos() []Todo {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.todos)
}

func (e *Env) setTodos(todos []Todo) {
	e.mu.Lock()
	e.todos = slices.Clone(todos)
	e.mu.Unlock()
	if e.OnTodos != nil {
		e.OnTodos(todos)
	}
}

func All(agents func() []AgentInfo) []Tool {
	return []Tool{
		Bash{}, Read{}, Write{}, Edit{}, Glob{}, Grep{}, WebFetch{}, TodoWrite{}, Task{Agents: agents},
	}
}

func Names() []string {
	var names []string
	for _, t := range All(nil) {
		names = append(names, t.Name())
	}
	return names
}

func Select(all []Tool, names []string) []Tool {
	if len(names) == 0 {
		return all
	}
	var out []Tool
	for _, t := range all {
		if slices.Contains(names, t.Name()) {
			out = append(out, t)
		}
	}
	return out
}

func Without(all []Tool, name string) []Tool {
	return slices.DeleteFunc(slices.Clone(all), func(t Tool) bool { return t.Name() == name })
}

func decode[T any](input json.RawMessage) (T, error) {
	var v T
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	if err := json.Unmarshal(input, &v); err != nil {
		return v, fmt.Errorf("invalid input: %w", err)
	}
	return v, nil
}

func field(input json.RawMessage, name string) string {
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	if v, ok := m[name].(string); ok {
		return v
	}
	return ""
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) != 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func Truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("\n... [truncated %d characters]", len(s)-limit)
}

func OneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	for 0 < limit && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit] + "…"
}
