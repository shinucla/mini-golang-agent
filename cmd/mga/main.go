package main

import (
	"context"
	"encoding/json"
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

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/agentdef"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/llm"
	"github.com/shinucla/mini-golang-agent/internal/mcp"
	"github.com/shinucla/mini-golang-agent/internal/plugin"
	"github.com/shinucla/mini-golang-agent/internal/tools"
	"github.com/shinucla/mini-golang-agent/internal/tui"
)

const usage = `mga - a mini coding agent for the terminal

Usage:
  mga [flags] [prompt]          start the interactive UI (optionally with a first prompt)
  mga -p [flags] "prompt"       run one prompt, print the answer, and exit (flags in any order)
  mga sessions [--dir DIR | --all] [--json]   list sessions (default: this folder)
  mga sessions show <id> [--json]             print one session
  mga plugin marketplace add <git url>        add a plugin marketplace (Claude Code format)
  mga plugin install <plugin>@<marketplace>   install a plugin; see: mga plugin help
  mga models [provider...]      list the models of each configured provider
  mga providers                 list providers and their status
  mga agents [list]             list agent definitions
  mga agents show <name>        print an agent definition
  mga agents new <name> [--user] create an agent file to edit
  mga agents delete <name>      delete an agent definition
  mga mcp list                  connect to each MCP server and show its status
  mga mcp get <name>            show one MCP server and its tools
  mga mcp add [flags] <name> -- <command> [args...]   add a stdio MCP server
  mga mcp add -t http [flags] <name> <url>             add an HTTP MCP server
  mga mcp remove [-s user|project] <name>              remove an MCP server
  mga mcp approve <name>        allow a server from this project's .mcp.json

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
		case "mcp":
			return mcpCommand(cfg, cwd, args[1:])
		case "sessions":
			return sessionsCommand(cwd, args[1:])
		case "plugin", "plugins":
			return pluginCommand(args[1:])
		}
	}

	fs := flag.NewFlagSet("mga", flag.ContinueOnError)
	var printMode bool
	fs.BoolVar(&printMode, "p", false, "run the prompt non-interactively, print the answer, and exit")
	fs.BoolVar(&printMode, "print", false, "same as -p")
	outputFormat := fs.String("output-format", "text", "with -p: text, json, or stream-json")
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
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	prompt := strings.Join(positional, " ")
	switch *outputFormat {
	case formatText, formatJSON, formatStream:
	default:
		return fmt.Errorf("unknown --output-format %q: use text, json, or stream-json", *outputFormat)
	}

	session, err := pickSession(cwd, *continueLast, *resumeID)
	if err != nil {
		return err
	}
	if *resumeID != "" && session.Cwd != "" && session.Cwd != cwd {
		if info, err := os.Stat(session.Cwd); err == nil && info.IsDir() {
			if err := os.Chdir(session.Cwd); err != nil {
				return err
			}
			cwd = session.Cwd
		}
	}

	mode, err := agent.ParseMode(*modeFlag)
	if err != nil {
		return err
	}
	if _, err := agent.ParseAskPolicy(cfg.AutoModeAsk); err != nil {
		return err
	}
	if *skip {
		mode = agent.ModeBypass
	}
	rules := append(slices.Clone(cfg.AllowedTools), splitRules(*allowed)...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rt := agent.NewRuntime(ctx, cfg, cwd, agent.NewPermissions(mode, rules))
	servers := mcp.Load(cwd, rt.ApprovedMCP())
	servers.PersistApproval = rt.ApproveMCPServer
	rt.MCP = servers
	defer servers.Close()
	applyPlugins(rt, servers)

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

	if printMode || !isTerminal(os.Stdin) {
		connectForPrint(ctx, servers)
		return runPrint(ctx, rt, session, prompt, printOptions{verbose: *verbose, format: *outputFormat})
	}
	return tui.Run(rt, session, prompt, servers)
}

func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var tail []string
	if i := slices.Index(args, "--"); 0 <= i {
		args, tail = args[:i], args[i+1:]
	}
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return append(positional, tail...), nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
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

type printObserver struct {
	verbose bool
	session *agent.Session
	events  *eventWriter
}

func (o printObserver) Delta(d llm.Delta) {
	if d.Text != "" {
		o.events.emit(map[string]any{"type": "text_delta", "text": d.Text})
	}
	if d.Reasoning != "" {
		o.events.emit(map[string]any{"type": "reasoning_delta", "text": d.Reasoning})
	}
}

func (o printObserver) AssistantMessage(m llm.Message) {
	calls := make([]map[string]any, 0, len(m.ToolCalls))
	for _, c := range m.ToolCalls {
		calls = append(calls, map[string]any{"id": c.ID, "name": c.Name, "input": json.RawMessage(validJSON(c.Arguments))})
	}
	o.events.emit(map[string]any{"type": "assistant", "text": m.Content, "tool_calls": calls})
}

func (o printObserver) Usage(u llm.Usage) {
	o.session.Usage.InputTokens += u.InputTokens
	o.session.Usage.OutputTokens += u.OutputTokens
	o.session.ContextTokens = u.InputTokens + u.OutputTokens
	o.events.emit(map[string]any{"type": "usage", "input_tokens": u.InputTokens, "output_tokens": u.OutputTokens})
}

func (o printObserver) ToolStart(call llm.ToolCall, t tools.Tool) {
	summary := t.Summary([]byte(call.Arguments))
	if o.verbose {
		fmt.Fprintf(os.Stderr, "⏺ %s(%s)\n", call.Name, summary)
	}
	o.events.emit(map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "summary": summary})
}

func (o printObserver) ToolResult(call llm.ToolCall, result string, isErr bool) {
	if o.verbose && isErr {
		fmt.Fprintf(os.Stderr, "  ⎿ %s\n", tools.OneLine(result, 200))
	}
	o.events.emit(map[string]any{"type": "tool_result", "id": call.ID, "name": call.Name, "is_error": isErr, "content": tools.Truncate(result, 4000)})
}

func validJSON(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}
	quoted, _ := json.Marshal(s)
	return string(quoted)
}

const stdinWait = 3 * time.Second

func addPipedInput(prompt string, stdin io.Reader, wait time.Duration) (string, error) {
	type chunk struct {
		data []byte
		err  error
	}
	first := make(chan chunk, 1)
	go func() {
		buf := make([]byte, 64*1024)
		n, err := stdin.Read(buf)
		first <- chunk{buf[:n], err}
	}()
	var c chunk
	if prompt == "" {
		c = <-first
	} else {
		select {
		case c = <-first:
		case <-time.After(wait):
			fmt.Fprintf(os.Stderr, "mga: no input on stdin after %s; going on with the -p prompt only\n", wait)
			return prompt, nil
		}
	}
	data := c.data
	switch {
	case c.err == nil:
		rest, err := io.ReadAll(stdin)
		if err != nil {
			return "", err
		}
		data = append(data, rest...)
	case !errors.Is(c.err, io.EOF):
		return "", c.err
	}
	if piped := strings.TrimSpace(string(data)); piped != "" {
		prompt = strings.TrimSpace(prompt + "\n\n" + piped)
	}
	return prompt, nil
}

type printOptions struct {
	verbose bool
	format  string
}

func runPrint(ctx context.Context, rt *agent.Runtime, session *agent.Session, prompt string, opts printOptions) error {
	if !isTerminal(os.Stdin) {
		var err error
		if prompt, err = addPipedInput(prompt, os.Stdin, stdinWait); err != nil {
			return err
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

	dir := config.SessionsDir()
	release, err := agent.LockSession(dir, session.ID)
	if err != nil {
		return err
	}
	defer release()
	if latest, ok := session.Reload(dir); ok {
		session = latest
	}

	var events *eventWriter
	if opts.format == formatStream {
		events = &eventWriter{out: os.Stdout}
	}
	provider, model := rt.Current()
	events.emit(map[string]any{"type": "init", "session_id": session.ID, "cwd": rt.Cwd, "provider": provider, "model": model})

	before := len(session.Messages)
	history := append(session.Messages, llm.Message{Role: llm.RoleUser, Content: prompt})
	msgs, runErr := ag.Run(ctx, history, printObserver{verbose: opts.verbose, session: session, events: events})
	session.Provider, session.Model = rt.Current()
	session.Cwd = rt.Cwd
	session.Messages = msgs
	if err := session.Save(dir); err != nil && opts.verbose {
		fmt.Fprintln(os.Stderr, "mga: could not save session:", err)
	}
	text := agent.LastAssistantText(msgs[min(before, len(msgs)):])
	if opts.format == formatText {
		if text != "" {
			fmt.Println(text)
		}
		return runErr
	}
	result := map[string]any{
		"type": "result", "session_id": session.ID, "result": text, "is_error": runErr != nil,
		"provider": session.Provider, "model": session.Model, "num_messages": len(msgs),
		"usage": map[string]any{"input_tokens": session.Usage.InputTokens, "output_tokens": session.Usage.OutputTokens},
	}
	if runErr != nil {
		result["error"] = runErr.Error()
	}
	(&eventWriter{out: os.Stdout}).emit(result)
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
	if loaded, err := plugin.LoadEnabled(); err == nil {
		store.Plugins = loaded.Agents
	}
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
