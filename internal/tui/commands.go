package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/llm"
	"github.com/shinucla/mini-golang-agent/internal/plugin"
	"github.com/shinucla/mini-golang-agent/internal/tools"
)

type command struct {
	name string
	args string
	desc string
}

var commands = []command{
	{"help", "", "Show commands and keys"},
	{"model", "[provider:model]", "Pick or set the provider and model"},
	{"agents", "", "Manage agent definitions and running agents"},
	{"mcp", "", "Show MCP servers, their status, and their tools"},
	{"tasks", "", "Show running and finished sub-agents"},
	{"mode", "[default|acceptEdits|plan|auto|bypassPermissions]", "Show or set the permission mode"},
	{"permissions", "", "Show the permission mode and rules"},
	{"clear", "", "Clear the conversation and its context; keep this session"},
	{"plugins", "", "List installed plugins and what they add"},
	{"compact", "[focus]", "Summarize the conversation to free context"},
	{"resume", "[session id]", "Open a session by id, or list sessions and agents (← on an empty input)"},
	{"init", "", "Create an MGA.md file with notes about this project"},
	{"status", "", "Show provider, model, session, and token usage"},
	{"exit", "", "Quit mga"},
}

const initPrompt = `Analyze this codebase and create a file named MGA.md in the working directory. Future sessions read it as project instructions.
Include:
1. The commands to build, lint, and test, and how to run a single test.
2. The high-level architecture: the parts that you understand only after you read several files.
3. Code style and conventions that the code follows.
Keep it short. Do not list every file. Do not invent facts. If MGA.md exists, improve it instead.`

const compactPrompt = `Summarize the conversation below so that work can continue in a new context window. Include:
- The user's requests and intent, in order.
- Key technical facts, decisions, and constraints.
- Files read or changed, with the important details.
- Errors and how they were fixed.
- Work that is still pending, and the exact next step.
Write the summary as plain Markdown.`

func (a *App) suggestions() []command {
	value := a.input.Value()
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \n") {
		return nil
	}
	var out []command
	for _, c := range a.allCommands() {
		if strings.HasPrefix(c.name, value[1:]) {
			out = append(out, c)
		}
	}
	return out
}

func (a *App) allCommands() []command {
	all := slices.Clone(commands)
	for _, c := range a.rt.Commands {
		all = append(all, command{name: c.Name, args: c.ArgHint, desc: tools.OneLine(c.Description, 80)})
	}
	for _, s := range a.rt.Skills {
		all = append(all, command{name: s.Name, desc: tools.OneLine(s.Description, 80)})
	}
	return all
}

func (a *App) pluginPrompt(name, arg string) (string, bool, error) {
	for _, c := range a.rt.Commands {
		if c.Name != name {
			continue
		}
		body, err := c.Load()
		if err != nil {
			return "", true, err
		}
		switch {
		case strings.Contains(body, "$ARGUMENTS"):
			body = strings.ReplaceAll(body, "$ARGUMENTS", arg)
		case arg != "":
			body += "\n\nARGUMENTS: " + arg
		}
		return body, true, nil
	}
	for _, s := range a.rt.Skills {
		if s.Name == name {
			text, err := tools.ExpandSkill(s, arg)
			return text, true, err
		}
	}
	return "", false, nil
}

func pluginsText() string {
	loaded, err := plugin.LoadAll()
	if err != nil {
		return formatError(err.Error())
	}
	if len(loaded.Plugins) == 0 {
		return styleDim.Render("  ⎿  No plugins. Add one with: mga plugin marketplace add <git url>, then mga plugin install <name>@<marketplace>")
	}
	var b strings.Builder
	b.WriteString(styleBold.Render("Plugins") + styleDim.Render("  (manage them with mga plugin … in a shell; restart mga to apply changes)") + "\n")
	for _, p := range loaded.Plugins {
		state := styleOK.Render("enabled")
		if !p.Enabled {
			state = styleDim.Render("disabled")
		}
		fmt.Fprintf(&b, "  %s %s %s\n", styleAccent.Render(p.ID), styleDim.Render(p.Version), state)
		fmt.Fprintf(&b, "    %s\n", styleDim.Render(pluginCounts(p)))
		if p.Err != nil {
			fmt.Fprintf(&b, "    %s\n", styleErr.Render(tools.OneLine(p.Err.Error(), 200)))
		}
	}
	return "\n" + strings.TrimRight(b.String(), "\n")
}

func pluginCounts(p plugin.Info) string {
	parts := []string{fmt.Sprintf("%d skills", len(p.Skills)), fmt.Sprintf("%d commands", len(p.Commands)), fmt.Sprintf("%d agents", len(p.Agents)), fmt.Sprintf("%d MCP servers", len(p.MCP))}
	if p.Hooks {
		parts = append(parts, "hooks (not supported in mga)")
	}
	return strings.Join(parts, " · ")
}

func (a *App) suggestIndex(n int) int {
	if a.suggestFor != a.input.Value() {
		return 0
	}
	return min(a.suggestPos, max(n-1, 0))
}

func (a *App) selectedSuggestion() (command, bool) {
	s := a.suggestions()
	if len(s) == 0 {
		return command{}, false
	}
	return s[a.suggestIndex(len(s))], true
}

func (a *App) moveSuggestion(step int) bool {
	s := a.suggestions()
	if len(s) == 0 || a.browsingHistory() {
		return false
	}
	a.suggestPos = (a.suggestIndex(len(s)) + step + len(s)) % len(s)
	a.suggestFor = a.input.Value()
	return true
}

func (a *App) completeSuggestion() bool {
	c, ok := a.selectedSuggestion()
	if ok {
		a.editInput(func() { a.input.SetValue("/" + c.name + " ") })
	}
	return ok
}

func (a *App) completeOnEnter() bool {
	c, ok := a.selectedSuggestion()
	return ok && a.input.Value() != "/"+c.name && a.completeSuggestion()
}

func (a *App) browsingHistory() bool {
	return a.historyPos < len(a.inputHistory) && a.input.Value() == a.inputHistory[a.historyPos]
}

func (a *App) runCommand(text string) tea.Cmd {
	name, arg, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	arg = strings.TrimSpace(arg)
	a.emit(formatUser(text, a.width))
	switch name {
	case "help", "?":
		a.emit(a.helpText())
	case "model", "models":
		if arg == "" {
			return a.openPicker()
		}
		current, _ := a.rt.Current()
		provider, model, err := a.rt.Cfg.ParseModelRef(arg, current)
		if err != nil {
			a.emit(formatError(err.Error()))
			return nil
		}
		a.setModel(provider, model)
	case "mcp":
		return a.openMCP()
	case "agents":
		return a.openAgents(0)
	case "tasks", "bashes":
		return a.openAgents(1)
	case "mode":
		if arg != "" {
			mode, err := agent.ParseMode(arg)
			if err != nil {
				a.emit(formatError(err.Error()))
				return nil
			}
			a.setMode(mode)
		}
		a.emit(indent("Permission mode: "+string(a.rt.Perms.Mode()), "  ⎿  ", "     "))
	case "permissions":
		a.emit(a.permissionsText())
	case "clear":
		a.clearConversation()
	case "new":
		a.newSession()
	case "compact":
		return a.compact(arg)
	case "resume", "continue", "sessions":
		if arg == "" {
			return a.openHome()
		}
		return a.resumeSession(arg)
	case "init":
		if a.busy {
			a.queue = append(a.queue, initPrompt)
			return nil
		}
		return a.startTurn(initPrompt, "")
	case "status":
		a.emit(a.statusText())
	case "exit", "quit", "q":
		return a.quit()
	case "plugins", "plugin":
		a.emit(pluginsText())
	default:
		text, ok, err := a.pluginPrompt(name, arg)
		switch {
		case !ok:
			a.emit(formatError(fmt.Sprintf("Unknown command /%s. Type /help for the list.", name)))
		case err != nil:
			a.emit(formatError(err.Error()))
		case a.busy:
			a.queue = append(a.queue, text)
		default:
			return a.startTurn(text, "")
		}
	}
	return nil
}

func (a *App) helpText() string {
	var b strings.Builder
	b.WriteString(styleBold.Render("Commands") + "\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %s %s\n", styleAccent.Render(fmt.Sprintf("/%-12s", c.name)), c.desc+styleDim.Render(" "+c.args))
	}
	if extra := a.allCommands()[len(commands):]; 0 < len(extra) {
		b.WriteString("\n" + styleBold.Render("Plugin commands and skills") + "\n")
		for _, c := range extra {
			fmt.Fprintf(&b, "  %s %s\n", styleAccent.Render("/"+c.name), styleDim.Render(c.desc))
		}
	}
	b.WriteString("\n" + styleBold.Render("Keys") + "\n")
	for _, k := range [][2]string{
		{"enter", "send the message (queued while the agent works)"},
		{"ctrl+j, \\+enter", "insert a newline"},
		{"esc", "interrupt the agent, or clear the input"},
		{"shift+tab", "cycle the permission mode: default → accept edits → plan → auto → bypass"},
		{"left", "open the session and agent list (when the input is empty)"},
		{"up/down", "browse earlier inputs, or select a command in the slash command list"},
		{"tab", "complete the selected slash command"},
		{"ctrl+c twice", "quit"},
	} {
		fmt.Fprintf(&b, "  %s %s\n", styleAccent.Render(fmt.Sprintf("%-16s", k[0])), k[1])
	}
	return "\n" + strings.TrimRight(b.String(), "\n")
}

func (a *App) permissionsText() string {
	lines := []string{"Permission mode: " + string(a.rt.Perms.Mode())}
	rules := a.rt.Perms.Rules()
	if len(rules) == 0 {
		lines = append(lines, styleDim.Render("No allow rules. Add them to allowed_tools in ~/.mga/config.json or answer \"don't ask again\"."))
	}
	for _, r := range rules {
		lines = append(lines, "allow "+r)
	}
	return indent(strings.Join(lines, "\n"), "  ⎿  ", "     ")
}

func (a *App) statusText() string {
	provider, model := a.rt.Current()
	pc, _ := a.rt.Cfg.Provider(provider)
	defs, _ := a.rt.Defs.List()
	lines := []string{
		"Provider:  " + provider + styleDim.Render(" ("+pc.BaseURL+")"),
		"Model:     " + model,
		"Directory: " + a.rt.Cwd,
		"Session:   " + a.session.ID,
		"Mode:      " + string(a.rt.Perms.Mode()),
		fmt.Sprintf("Tokens:    %s in · %s out", formatTokens(a.usage.InputTokens), formatTokens(a.usage.OutputTokens)),
		fmt.Sprintf("Agents:    %d defined · %d running", len(defs), a.rt.Tasks.Running()),
		a.mcpStatusLine(),
		"Config:    " + a.rt.Cfg.Path(),
	}
	return indent(strings.Join(lines, "\n"), "  ⎿  ", "     ")
}

func (a *App) setModel(provider, model string) {
	if _, err := a.rt.Provider(provider); err != nil {
		a.emit(formatError(err.Error()))
		return
	}
	a.rt.SetCurrent(provider, model)
	a.provider, a.model = provider, model
	note := "Set model to " + styleBold.Render(provider+":"+model)
	if err := a.rt.SaveDefault(provider, model); err != nil {
		note += styleErr.Render(" (not saved: " + err.Error() + ")")
	}
	a.emit(indent(note, "  ⎿  ", "     "))
}

func (a *App) compact(focus string) tea.Cmd {
	if a.busy {
		a.emit(formatError("A turn is running. Press esc first."))
		return nil
	}
	if len(a.history) == 0 {
		a.emit(styleDim.Render("  ⎿  Nothing to compact"))
		return nil
	}
	r := a.sessionRun
	providerName, model := r.provider, r.model
	p, err := a.rt.Provider(providerName)
	if err != nil {
		a.emit(formatError(err.Error()))
		return nil
	}
	prompt := compactPrompt
	if focus != "" {
		prompt += "\nFocus on: " + focus
	}
	prompt += "\n\n<conversation>\n" + transcript(a.history) + "\n</conversation>"
	if err := a.lockRun(r); err != nil {
		a.emit(formatError(err.Error()))
		return nil
	}
	ctx, cancel := context.WithCancel(a.rt.BaseCtx)
	a.cancel = cancel
	a.busy = true
	a.turnStart = time.Now()
	return tea.Batch(a.spin.Tick, func() tea.Msg {
		defer cancel()
		resp, err := p.Chat(ctx, llm.Request{
			Model:    model,
			System:   "You write precise summaries of coding sessions.",
			Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}},
		}, func(llm.Delta) {})
		if err != nil {
			return compactDoneMsg{run: r, err: err}
		}
		return compactDoneMsg{run: r, summary: resp.Message.Content}
	})
}

func (a *App) clearConversation() {
	r := a.sessionRun
	if r.busy {
		a.emit(formatError("mga is still working. Press esc to stop the turn, then run /clear again."))
		return
	}
	r.history, r.pending, r.queue, r.notifications, r.todos = nil, nil, nil, nil, nil
	r.usage, r.contextTokens = llm.Usage{}, 0
	r.env = &tools.Env{Cwd: a.rt.Cwd}
	a.wire(r)
	a.resetScreen()
	if 0 < len(r.session.Messages) || !r.session.Updated.IsZero() {
		if err := r.session.Clear(config.SessionsDir()); err != nil {
			a.emit(formatError("could not save the cleared session: " + err.Error()))
		}
		a.markSeen(r)
	}
	a.emit(formatNote("Cleared the conversation. This session keeps its id and its place in the list."))
}

func (a *App) compactDone(msg compactDoneMsg) tea.Cmd {
	r := msg.run
	r.busy = false
	r.cancel = nil
	current := r == a.sessionRun
	if msg.err != nil {
		if current {
			a.emit(formatError("compact failed: " + msg.err.Error()))
		}
		r.release()
		return a.nextTurn(r)
	}
	r.history = []llm.Message{
		{Role: llm.RoleUser, Content: "This session continues from an earlier conversation. Summary of the earlier conversation:\n\n" + msg.summary},
		{Role: llm.RoleAssistant, Content: "I have the summary and can continue from here."},
	}
	r.contextTokens = 0
	a.saveRun(r)
	r.release()
	if current {
		a.emit(styleDim.Render("  ⎿  Compacted the conversation"))
	}
	return a.nextTurn(r)
}

func transcript(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			fmt.Fprintf(&b, "User: %s\n\n", m.Content)
		case llm.RoleAssistant:
			if m.Content != "" {
				fmt.Fprintf(&b, "Assistant: %s\n\n", m.Content)
			}
			for _, c := range m.ToolCalls {
				fmt.Fprintf(&b, "[Tool call %s: %s]\n", c.Name, tools.Truncate(c.Arguments, 2000))
			}
		case llm.RoleTool:
			fmt.Fprintf(&b, "[Tool result %s: %s]\n\n", m.ToolName, tools.Truncate(m.Content, 2000))
		}
	}
	return b.String()
}

func (a *App) mcpStatusLine() string {
	if a.mcp == nil {
		return "MCP:       off"
	}
	connected, total := a.mcp.Count()
	return fmt.Sprintf("MCP:       %d of %d servers connected · %d tools", connected, total, len(a.mcp.Tools()))
}
