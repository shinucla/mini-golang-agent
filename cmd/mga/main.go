package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
	"github.com/kzhuang/mini-golang-agent/internal/tui"
)

const usage = `mga - a mini coding agent for the terminal

Usage:
  mga [flags] [prompt]          start the interactive UI (optionally with a first prompt)
  mga -p "prompt" [flags]       run one prompt, print the answer, and exit
  mga models [provider...]      list the models of each configured provider
  mga providers                 list providers and their status
  mga agents [list]             list agent definitions
  mga agents show <name>        print an agent definition
  mga agents new <name> [--user] create an agent file to edit
  mga agents delete <name>      delete an agent definition

Flags:
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mga:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if 0 < len(args) {
		switch args[0] {
		case "models":
			return listModels(cfg, args[1:])
		case "providers":
			return listProviders(cfg)
		case "agents":
			return agentsCommand(cwd, args[1:])
		}
	}

	fs := flag.NewFlagSet("mga", flag.ContinueOnError)
	var printPrompt string
	fs.StringVar(&printPrompt, "p", "", "run one prompt non-interactively and print the answer")
	fs.StringVar(&printPrompt, "print", "", "same as -p")
	modelRef := fs.String("model", "", "provider:model, a provider name, or a model id for the default provider")
	continueLast := fs.Bool("c", false, "continue the most recent session in this directory")
	resumeID := fs.String("resume", "", "resume a session by id")
	modeFlag := fs.String("permission-mode", cfg.PermissionMode, "default, acceptEdits, plan, auto, or bypassPermissions")
	skip := fs.Bool("dangerously-skip-permissions", false, "run every tool without approval")
	allowed := fs.String("allowed-tools", "", `comma-separated allow rules, e.g. "Bash(go test:*),Edit,WebFetch(domain:go.dev)"`)
	verbose := fs.Bool("verbose", false, "with -p, print tool activity to stderr")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	mode, err := agent.ParseMode(*modeFlag)
	if err != nil {
		return err
	}
	if *skip {
		mode = agent.ModeBypass
	}
	rules := append(slices.Clone(cfg.AllowedTools), splitRules(*allowed)...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rt := agent.NewRuntime(ctx, cfg, cwd, agent.NewPermissions(mode, rules))

	session, err := pickSession(cwd, *continueLast, *resumeID)
	if err != nil {
		return err
	}
	provider, model := cfg.ResolveDefault()
	switch {
	case *modelRef != "":
		provider, model, err = cfg.ParseModelRef(*modelRef, provider)
		if err != nil {
			return err
		}
	case session.Provider != "" && session.Model != "":
		provider, model = session.Provider, session.Model
	}
	if model == "" {
		model = firstModel(ctx, rt, provider)
	}
	rt.SetCurrent(provider, model)

	if printPrompt != "" || !isTerminal(os.Stdin) {
		return runPrint(ctx, rt, session, printPrompt, *verbose)
	}
	return tui.Run(rt, session, strings.Join(fs.Args(), " "))
}

func splitRules(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

func pickSession(cwd string, continueLast bool, id string) (*agent.Session, error) {
	if id != "" {
		return agent.LoadSession(config.SessionsDir(), id)
	}
	if continueLast {
		sessions, err := agent.ListSessions(config.SessionsDir(), cwd)
		if err != nil {
			return nil, err
		}
		if 0 < len(sessions) {
			return sessions[0], nil
		}
	}
	return agent.NewSession(cwd), nil
}

func firstModel(ctx context.Context, rt *agent.Runtime, provider string) string {
	p, err := rt.Provider(provider)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	models, err := p.ListModels(ctx)
	if err != nil || len(models) == 0 {
		return ""
	}
	return models[0].ID
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

type printObserver struct{ verbose bool }

func (printObserver) Delta(llm.Delta)              {}
func (printObserver) AssistantMessage(llm.Message) {}
func (printObserver) Usage(llm.Usage)              {}

func (o printObserver) ToolStart(call llm.ToolCall, t tools.Tool) {
	if o.verbose {
		fmt.Fprintf(os.Stderr, "⏺ %s(%s)\n", call.Name, t.Summary([]byte(call.Arguments)))
	}
}

func (o printObserver) ToolResult(_ llm.ToolCall, result string, isErr bool) {
	if o.verbose && isErr {
		fmt.Fprintf(os.Stderr, "  ⎿ %s\n", tools.OneLine(result, 200))
	}
}

func runPrint(ctx context.Context, rt *agent.Runtime, session *agent.Session, prompt string, verbose bool) error {
	if !isTerminal(os.Stdin) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		if piped := strings.TrimSpace(string(data)); piped != "" {
			prompt = strings.TrimSpace(prompt + "\n\n" + piped)
		}
	}
	if prompt == "" {
		return errors.New("no prompt: pass -p \"...\" or pipe text on stdin")
	}
	rt.BackgroundAllowed = false
	rt.Approve = func(_ context.Context, req agent.ApprovalRequest) agent.Decision {
		fmt.Fprintf(os.Stderr, "mga: denied %s(%s); allow it with --allowed-tools or --permission-mode\n", req.Tool, req.Summary)
		return agent.Deny
	}
	ag, err := rt.MainAgent(&tools.Env{Cwd: rt.Cwd})
	if err != nil {
		return err
	}
	ag.StopOnDeny = false
	before := len(session.Messages)
	history := append(session.Messages, llm.Message{Role: llm.RoleUser, Content: prompt})
	msgs, runErr := ag.Run(ctx, history, printObserver{verbose: verbose})
	session.Provider, session.Model = rt.Current()
	session.Messages = msgs
	if err := session.Save(config.SessionsDir()); err != nil && verbose {
		fmt.Fprintln(os.Stderr, "mga: could not save session:", err)
	}
	if text := agent.LastAssistantText(msgs[min(before, len(msgs)):]); text != "" {
		fmt.Println(text)
	}
	return runErr
}

func listProviders(cfg *config.Config) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tTYPE\tSTATUS\tDEFAULT MODEL\tBASE URL")
	for _, name := range cfg.ProviderNames() {
		pc, _ := cfg.Provider(name)
		status := "no key (" + strings.Split(pc.APIKeyEnv, ",")[0] + ")"
		switch {
		case pc.Local:
			status = "local"
		case pc.Key() != "":
			status = "ready"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, pc.Type, status, pc.DefaultModel, pc.BaseURL)
	}
	fmt.Fprintf(w, "\nConfig file: %s\n", cfg.Path())
	return w.Flush()
}

func listModels(cfg *config.Config, names []string) error {
	explicit := 0 < len(names)
	if !explicit {
		names = cfg.ProviderNames()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var errs []error
	for _, name := range names {
		pc, ok := cfg.Provider(name)
		if !ok {
			errs = append(errs, fmt.Errorf("unknown provider %q", name))
			continue
		}
		if !pc.Configured() && !explicit {
			continue
		}
		p, err := llm.New(name, pc)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		models, err := p.ListModels(ctx)
		if err != nil {
			if explicit || !pc.Local {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			}
			continue
		}
		for _, m := range models {
			fmt.Printf("%s:%s\n", name, m.ID)
		}
	}
	return errors.Join(errs...)
}

func agentsCommand(cwd string, args []string) error {
	store := &agentdef.Store{UserDir: config.UserAgentsDir(), ProjectDir: config.ProjectAgentsDir(cwd)}
	sub := "list"
	if 0 < len(args) {
		sub = args[0]
	}
	switch sub {
	case "list", "ls":
		defs, err := store.List()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSCOPE\tMODEL\tTOOLS\tDESCRIPTION")
		for _, d := range defs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", d.Name, d.Scope, d.ModelLabel(), d.ToolsLabel(), tools.OneLine(d.Description, 60))
		}
		w.Flush()
		return err
	case "show", "delete", "rm", "new":
		if len(args) < 2 {
			return fmt.Errorf("usage: mga agents %s <name>", sub)
		}
	default:
		return fmt.Errorf("unknown agents command %q", sub)
	}
	name := args[1]
	if sub == "new" {
		scope := agentdef.ScopeProject
		if 2 < len(args) && args[2] == "--user" {
			scope = agentdef.ScopeUser
		}
		if _, exists := store.Get(name); exists {
			return fmt.Errorf("agent %q already exists", name)
		}
		path, err := store.Draft(name, scope)
		if err != nil {
			return err
		}
		fmt.Println("Created", path, "- edit it to finish the agent.")
		return nil
	}
	def, ok := store.Get(name)
	if !ok {
		return fmt.Errorf("no agent named %q", name)
	}
	if sub == "show" {
		fmt.Print(string(agentdef.Format(def)))
		return nil
	}
	if err := store.Delete(def); err != nil {
		return err
	}
	fmt.Println("Deleted", def.Path)
	return nil
}
