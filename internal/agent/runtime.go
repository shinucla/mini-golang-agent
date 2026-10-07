package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const (
	mainMaxSteps = 200
	subMaxSteps  = 150
)

type Runtime struct {
	Cfg               *config.Config
	Cwd               string
	Defs              *agentdef.Store
	Perms             *Permissions
	Tasks             *TaskRegistry
	Approve           Approver
	BaseCtx           context.Context
	BackgroundAllowed bool

	mu             sync.Mutex
	providers      map[string]llm.Provider
	contextWindows map[string]int
	provider       string
	model          string
}

func NewRuntime(ctx context.Context, cfg *config.Config, cwd string, perms *Permissions) *Runtime {
	return &Runtime{
		Cfg:               cfg,
		Cwd:               cwd,
		Defs:              &agentdef.Store{UserDir: config.UserAgentsDir(), ProjectDir: config.ProjectAgentsDir(cwd)},
		Perms:             perms,
		Tasks:             &TaskRegistry{},
		BaseCtx:           ctx,
		BackgroundAllowed: true,
		providers:         map[string]llm.Provider{},
		contextWindows:    map[string]int{},
	}
}

func (r *Runtime) SetCurrent(provider, model string) {
	r.mu.Lock()
	r.provider, r.model = provider, model
	r.mu.Unlock()
}

func (r *Runtime) Current() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.provider, r.model
}

func (r *Runtime) RegisterProvider(name string, p llm.Provider) {
	r.mu.Lock()
	r.providers[name] = p
	r.mu.Unlock()
}

func (r *Runtime) SetProviderKey(name, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Cfg.SetProviderKey(name, key)
	delete(r.providers, name)
	return r.Cfg.Save()
}

func (r *Runtime) SaveDefault(provider, model string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Cfg.DefaultProvider = provider
	r.Cfg.DefaultModel = model
	return r.Cfg.Save()
}

func (r *Runtime) SetMode(m Mode) error {
	r.Perms.SetMode(m)
	if !m.Persistent() {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Cfg.PermissionMode = string(m)
	return r.Cfg.Save()
}

func (r *Runtime) RememberModels(provider string, models []llm.Model) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range models {
		if 0 < m.ContextWindow {
			r.contextWindows[provider+"/"+m.ID] = m.ContextWindow
		}
	}
}

func (r *Runtime) ContextWindow(provider, model string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pc, ok := r.Cfg.Provider(provider); ok && 0 < pc.ContextWindow {
		return pc.ContextWindow
	}
	if n := r.contextWindows[provider+"/"+model]; 0 < n {
		return n
	}
	return llm.KnownContextWindow(model)
}

func (r *Runtime) askPolicy() AskPolicy {
	r.mu.Lock()
	defer r.mu.Unlock()
	policy, _ := ParseAskPolicy(r.Cfg.AutoModeAsk)
	return policy
}

func (r *Runtime) ProviderConfig(name string) (config.ProviderConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Cfg.Provider(name)
}

func (r *Runtime) Provider(name string) (llm.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	pc, ok := r.Cfg.Provider(name)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	p, err := llm.New(name, pc)
	if err != nil {
		return nil, err
	}
	r.providers[name] = p
	return p, nil
}

func (r *Runtime) agentInfos() []tools.AgentInfo {
	defs, _ := r.Defs.List()
	infos := make([]tools.AgentInfo, len(defs))
	for i, d := range defs {
		infos[i] = tools.AgentInfo{Name: d.Name, Description: d.Description, Tools: d.Tools}
	}
	return infos
}

func (r *Runtime) MainAgent(env *tools.Env) (*Agent, error) {
	providerName, model := r.Current()
	if model == "" {
		return nil, fmt.Errorf("no model selected for %s; use /model", providerName)
	}
	p, err := r.Provider(providerName)
	if err != nil {
		return nil, err
	}
	env.Spawn = r.Spawn
	return &Agent{
		Name:       "main",
		Provider:   p,
		Model:      model,
		System:     MainPrompt(r.Cwd, providerName, model),
		Tools:      tools.All(r.agentInfos),
		Env:        env,
		Perms:      r.Perms,
		Approve:    r.Approve,
		Review:     r.Review,
		AskPolicy:  r.askPolicy(),
		StopOnDeny: true,
		MaxSteps:   mainMaxSteps,
	}, nil
}

func (r *Runtime) resolveModel(ref string) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "inherit" {
		return r.provider, r.model, nil
	}
	return r.Cfg.ParseModelRef(ref, r.provider)
}

func (r *Runtime) Spawn(ctx context.Context, req tools.SpawnRequest) (string, error) {
	agentType := req.AgentType
	if agentType == "" {
		agentType = "general-purpose"
	}
	def, ok := r.Defs.Get(agentType)
	if !ok {
		var names []string
		for _, info := range r.agentInfos() {
			names = append(names, info.Name)
		}
		return "", fmt.Errorf("unknown agent type %q; available: %s", agentType, strings.Join(names, ", "))
	}
	providerName, model, err := r.resolveModel(def.Model)
	if err != nil {
		return "", err
	}
	p, err := r.Provider(providerName)
	if err != nil {
		return "", err
	}
	a := &Agent{
		Name:      def.Name,
		Provider:  p,
		Model:     model,
		System:    SubAgentPrompt(def, r.Cwd),
		Tools:     tools.Without(tools.Select(tools.All(nil), def.Tools), "Task"),
		Env:       &tools.Env{Cwd: r.Cwd},
		Perms:     r.Perms,
		Approve:   r.Approve,
		Review:    r.Review,
		AskPolicy: r.askPolicy(),
		MaxSteps:  subMaxSteps,
	}

	background := req.Background && r.BackgroundAllowed
	parent := ctx
	if background {
		parent = r.BaseCtx
	}
	runCtx, cancel := context.WithCancel(parent)
	id := r.Tasks.add(Task{
		Agent: def.Name, Model: providerName + ":" + model, Description: req.Description,
		Prompt: req.Prompt, Background: background, NotifyMain: background && !req.Manual,
	}, cancel)

	run := func() (string, error) {
		defer cancel()
		msgs, err := a.Run(runCtx, []llm.Message{{Role: llm.RoleUser, Content: req.Prompt}}, taskObserver{reg: r.Tasks, id: id})
		result := LastAssistantText(msgs)
		r.Tasks.finish(id, result, err)
		return result, err
	}

	if background {
		go run()
		return fmt.Sprintf("Started background agent %s (id %s). You will get a notification when it finishes. Do not poll for it.", def.Name, id), nil
	}
	result, err := run()
	if err != nil {
		return result, fmt.Errorf("agent %s (%s) failed: %w", def.Name, id, err)
	}
	if result == "" {
		result = "(the agent returned no text)"
	}
	return result, nil
}
