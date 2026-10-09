package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type sessionRun struct {
	session       *agent.Session
	provider      string
	model         string
	history       []llm.Message
	pending       []llm.Message
	env           *tools.Env
	busy          bool
	cancel        context.CancelFunc
	turnStart     time.Time
	stream        strings.Builder
	reasoning     strings.Builder
	running       map[string]runningTool
	runOrder      []string
	queue         []string
	notifications []string
	todos         []tools.Todo
	usage         llm.Usage
	contextTokens int
	approvals     []*pendingApproval
}

func (a *App) newRun(s *agent.Session) *sessionRun {
	r := &sessionRun{
		session:       s,
		provider:      s.Provider,
		model:         s.Model,
		history:       slices.Clone(s.Messages),
		usage:         s.Usage,
		contextTokens: s.ContextTokens,
		env:           &tools.Env{Cwd: a.rt.Cwd},
		running:       map[string]runningTool{},
	}
	if r.provider == "" || r.model == "" {
		r.provider, r.model = a.rt.Current()
	}
	a.wire(r)
	a.runs[s.ID] = r
	return r
}

func (a *App) wire(r *sessionRun) {
	send := a.send
	r.env.OnTodos = func(t []tools.Todo) { send(todosMsg{run: r, todos: t}) }
}

func (r *sessionRun) waiting() bool {
	r.approvals = slices.DeleteFunc(r.approvals, func(p *pendingApproval) bool { return p.ctx.Err() != nil })
	return 0 < len(r.approvals)
}

func (r *sessionRun) empty() bool {
	return !r.busy && len(r.history) == 0 && len(r.pending) == 0 && len(r.session.Messages) == 0
}

func (r *sessionRun) transcript() []llm.Message {
	return append(slices.Clone(r.history), r.pending...)
}

func (a *App) runFor(ctx context.Context) *sessionRun {
	if r, ok := a.runs[agent.TurnOf(ctx).Owner]; ok {
		return r
	}
	return a.sessionRun
}

func (a *App) otherRuns() (working, waiting int) {
	for _, r := range a.runs {
		if r == a.sessionRun {
			continue
		}
		if r.waiting() {
			waiting++
		} else if r.busy {
			working++
		}
	}
	return working, waiting
}

func (a *App) anyBusy() bool {
	for _, r := range a.runs {
		if r.busy {
			return true
		}
	}
	return false
}
