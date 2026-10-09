package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskStopped   TaskStatus = "stopped"
)

const maxTaskLog = 1000

type Task struct {
	ID          string
	Owner       string
	Agent       string
	Model       string
	Description string
	Prompt      string
	Background  bool
	NotifyMain  bool
	Status      TaskStatus
	Started     time.Time
	Ended       time.Time
	Log         []string
	Result      string
	Err         string
	ToolUses    int
	Tokens      int
}

func (t Task) Elapsed() time.Duration {
	end := t.Ended
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(t.Started).Round(time.Second)
}

func (t Task) LastActivity() string {
	if len(t.Log) == 0 {
		return ""
	}
	return t.Log[len(t.Log)-1]
}

type taskEntry struct {
	Task
	cancel  context.CancelFunc
	stopped bool
}

type TaskRegistry struct {
	mu       sync.Mutex
	entries  []*taskEntry
	seq      int
	OnChange func()
	OnFinish func(Task)
}

func (r *TaskRegistry) notify() {
	if r.OnChange != nil {
		r.OnChange()
	}
}

func (r *TaskRegistry) add(t Task, cancel context.CancelFunc) string {
	r.mu.Lock()
	r.seq++
	t.ID = fmt.Sprintf("a%d", r.seq)
	t.Status = TaskRunning
	t.Started = time.Now()
	r.entries = append(r.entries, &taskEntry{Task: t, cancel: cancel})
	r.mu.Unlock()
	r.notify()
	return t.ID
}

func (r *TaskRegistry) find(id string) *taskEntry {
	for _, e := range r.entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (r *TaskRegistry) Snapshot() []Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Task, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.Task
		out[i].Log = slices.Clone(e.Log)
	}
	return out
}

func (r *TaskRegistry) Get(id string) (Task, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.find(id)
	if e == nil {
		return Task{}, false
	}
	t := e.Task
	t.Log = slices.Clone(e.Log)
	return t, true
}

func (r *TaskRegistry) Running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.entries {
		if e.Status == TaskRunning {
			n++
		}
	}
	return n
}

func (r *TaskRegistry) Stop(id string) bool {
	r.mu.Lock()
	e := r.find(id)
	if e == nil || e.Status != TaskRunning {
		r.mu.Unlock()
		return false
	}
	e.stopped = true
	cancel := e.cancel
	r.mu.Unlock()
	cancel()
	return true
}

func (r *TaskRegistry) StopAll() {
	for _, t := range r.Snapshot() {
		if t.Status == TaskRunning {
			r.Stop(t.ID)
		}
	}
}

func (r *TaskRegistry) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := slices.IndexFunc(r.entries, func(e *taskEntry) bool { return e.ID == id && e.Status != TaskRunning })
	if i < 0 {
		return false
	}
	r.entries = slices.Delete(r.entries, i, i+1)
	return true
}

func (r *TaskRegistry) ClearFinished() {
	r.mu.Lock()
	r.entries = slices.DeleteFunc(r.entries, func(e *taskEntry) bool { return e.Status != TaskRunning })
	r.mu.Unlock()
}

func (r *TaskRegistry) finish(id string, result string, err error) {
	r.mu.Lock()
	e := r.find(id)
	if e == nil {
		r.mu.Unlock()
		return
	}
	e.Ended = time.Now()
	e.Result = result
	switch {
	case e.stopped:
		e.Status = TaskStopped
		e.Err = "stopped by the user"
	case err != nil:
		e.Status = TaskFailed
		e.Err = err.Error()
	default:
		e.Status = TaskCompleted
	}
	t := e.Task
	t.Log = slices.Clone(e.Log)
	r.mu.Unlock()
	r.notify()
	if r.OnFinish != nil {
		r.OnFinish(t)
	}
}

func (r *TaskRegistry) update(id string, fn func(e *taskEntry)) {
	r.mu.Lock()
	if e := r.find(id); e != nil {
		fn(e)
		if maxTaskLog < len(e.Log) {
			e.Log = slices.Clone(e.Log[len(e.Log)-maxTaskLog:])
		}
	}
	r.mu.Unlock()
}

type taskObserver struct {
	reg *TaskRegistry
	id  string
}

func (o taskObserver) Delta(llm.Delta) {}

func (o taskObserver) AssistantMessage(m llm.Message) {
	text := strings.TrimSpace(m.Content)
	if text == "" {
		return
	}
	o.reg.update(o.id, func(e *taskEntry) { e.Log = append(e.Log, strings.Split(text, "\n")...) })
	o.reg.notify()
}

func (o taskObserver) ToolStart(call llm.ToolCall, t tools.Tool) {
	line := fmt.Sprintf("→ %s(%s)", call.Name, t.Summary([]byte(call.Arguments)))
	o.reg.update(o.id, func(e *taskEntry) {
		e.ToolUses++
		e.Log = append(e.Log, line)
	})
	o.reg.notify()
}

func (o taskObserver) ToolResult(_ llm.ToolCall, result string, isError bool) {
	first, _, _ := strings.Cut(strings.TrimSpace(result), "\n")
	prefix := "  ⎿ "
	if isError {
		prefix = "  ⎿ ✗ "
	}
	o.reg.update(o.id, func(e *taskEntry) { e.Log = append(e.Log, prefix+tools.OneLine(first, 160)) })
	o.reg.notify()
}

func (o taskObserver) Usage(u llm.Usage) {
	o.reg.update(o.id, func(e *taskEntry) { e.Tokens += u.InputTokens + u.OutputTokens })
}
