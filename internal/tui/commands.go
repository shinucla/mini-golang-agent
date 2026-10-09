package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
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
	{"clear", "", "Start a new conversation"},
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
	for _, c := range commands {
		if strings.HasPrefix(c.name, value[1:]) {
			out = append(out, c)
		}
	}
	return out
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
		a.emit(helpText())
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
	case "clear", "new":
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
	default:
		a.emit(formatError(fmt.Sprintf("Unknown command /%s. Type /help for the list.", name)))
	}
	return nil
}

func helpText() string {
	var b strings.Builder
	b.WriteString(styleBold.Render("Commands") + "\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %s %s\n", styleAccent.Render(fmt.Sprintf("/%-12s", c.name)), c.desc+styleDim.Render(" "+c.args))
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

func (a *App) compactDone(msg compactDoneMsg) tea.Cmd {
	r := msg.run
	r.busy = false
	r.cancel = nil
	current := r == a.sessionRun
	if msg.err != nil {
		if current {
			a.emit(formatError("compact failed: " + msg.err.Error()))
		}
		return a.nextTurn(r)
	}
	r.history = []llm.Message{
		{Role: llm.RoleUser, Content: "This session continues from an earlier conversation. Summary of the earlier conversation:\n\n" + msg.summary},
		{Role: llm.RoleAssistant, Content: "I have the summary and can continue from here."},
	}
	r.contextTokens = 0
	a.saveRun(r)
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
