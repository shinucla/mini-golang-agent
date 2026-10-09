package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/mcp"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const (
	maxInputHeight  = 10
	liveStreamLines = 14
	replayLimit     = 200
	clearScrollback = "\x1b[3J"
)

type viewKind int

const (
	viewChat viewKind = iota
	viewModels
	viewAgents
	viewHome
	viewMCP
)

type runningTool struct {
	name    string
	summary string
	args    string
}

type pendingApproval struct {
	req    agent.ApprovalRequest
	reply  chan agent.Decision
	ctx    context.Context
	cursor int
}

type App struct {
	rt   *agent.Runtime
	send func(tea.Msg)
	md   markdown

	width  int
	height int

	*sessionRun
	runs map[string]*sessionRun

	input        textarea.Model
	spin         spinner.Model
	inputHistory []string
	historyPos   int

	view   viewKind
	picker *modelPicker
	agents *agentsView
	home   *homeView

	mcp         *mcp.Manager
	mcpView     *mcpView
	mcpReported map[string]mcp.Status

	printQueue    []string
	printing      bool
	alt           bool
	clearScreen   bool
	replayPending bool
	notice        string
	lastCtrlC     time.Time
	initialPrompt string
}

func Run(rt *agent.Runtime, session *agent.Session, initialPrompt string, servers *mcp.Manager) error {
	dark := lipgloss.HasDarkBackground()
	switch os.Getenv("MGA_THEME") {
	case "light":
		dark = false
	case "dark":
		dark = true
	}
	lipgloss.SetHasDarkBackground(dark)
	app := newApp(rt, session, initialPrompt, dark)
	app.mcp = servers
	p := tea.NewProgram(app)
	app.attach(p)
	if servers != nil {
		servers.OnChange = func() { go p.Send(mcpChangedMsg{}) }
		go servers.ConnectAll(rt.BaseCtx)
	}
	_, err := p.Run()
	rt.Tasks.StopAll()
	for _, r := range app.runs {
		if r.cancel != nil {
			r.cancel()
		}
		app.saveRun(r)
	}
	if 0 < len(app.session.Messages) {
		fmt.Printf("\nResume this session with: mga --resume %s\n", app.session.ID)
	}
	return err
}

func newApp(rt *agent.Runtime, session *agent.Session, initialPrompt string, dark bool) *App {
	ta := textarea.New()
	ta.Placeholder = chatPlaceholder
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "> "
		}
		return "  "
	})
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = styleDim
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"))
	ta.SetHeight(1)
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Spinner{Frames: []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}, FPS: 120 * time.Millisecond}
	sp.Style = styleAccent

	a := &App{
		rt:            rt,
		send:          func(tea.Msg) {},
		md:            markdown{dark: dark},
		width:         100,
		height:        40,
		runs:          map[string]*sessionRun{},
		input:         ta,
		spin:          sp,
		initialPrompt: initialPrompt,
	}
	a.sessionRun = a.newRun(session)
	return a
}

func (a *App) attach(p *tea.Program) {
	a.send = p.Send
	a.rt.Approve = a.approver
	a.rt.Tasks.OnChange = func() { go p.Send(tasksChangedMsg{}) }
	a.rt.Tasks.OnFinish = func(t agent.Task) { go p.Send(taskFinishedMsg(t)) }
	for _, r := range a.runs {
		a.wire(r)
	}
}

func (a *App) approver(ctx context.Context, req agent.ApprovalRequest) agent.Decision {
	reply := make(chan agent.Decision, 1)
	a.send(approvalMsg{req: req, reply: reply, ctx: ctx})
	select {
	case d := <-reply:
		return d
	case <-ctx.Done():
		return agent.Deny
	}
}

func (a *App) banner() string {
	provider, model := a.rt.Current()
	return stylePanel.Render(fmt.Sprintf("%s mga · mini Go agent\n\n%s\n%s\n\n%s",
		styleAccent.Render("✻"),
		styleDim.Render("cwd:   ")+a.rt.Cwd,
		styleDim.Render("model: ")+provider+":"+model,
		styleDim.Render("/help for commands · /model to switch · ← sessions")))
}

func (a *App) resetScreen() {
	a.printQueue = nil
	a.clearScreen = true
	a.emit(a.banner())
}

func (a *App) Init() tea.Cmd {
	a.emit(a.banner())
	a.replayPending = 0 < len(a.history)
	if a.mcp != nil {
		a.reportMCP()
	}
	cmds := []tea.Cmd{textarea.Blink, a.learnContextWindow()}
	if a.initialPrompt != "" {
		cmds = append(cmds, a.submit(a.initialPrompt))
	}
	return tea.Batch(cmds...)
}

func (a *App) learnContextWindow() tea.Cmd {
	provider, model := a.rt.Current()
	if model == "" || 0 < a.rt.ContextWindow(provider, model) {
		return nil
	}
	rt := a.rt
	return func() tea.Msg {
		p, err := rt.Provider(provider)
		if err != nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(rt.BaseCtx, listTimeout)
		defer cancel()
		models, err := p.ListModels(ctx)
		if err != nil {
			return nil
		}
		rt.RememberModels(provider, models)
		return contextWindowMsg{}
	}
}

func (a *App) emit(s string) {
	a.printQueue = append(a.printQueue, s)
}

func (a *App) flush() tea.Cmd {
	var steps []tea.Cmd
	if wantAlt := a.view == viewHome; wantAlt != a.alt {
		a.alt = wantAlt
		if wantAlt {
			steps = append(steps, tea.EnterAltScreen)
		} else {
			steps = append(steps, tea.ExitAltScreen)
		}
	}
	if a.alt || a.printing || (len(a.printQueue) == 0 && !a.clearScreen) {
		if len(steps) == 0 {
			return nil
		}
		return tea.Sequence(steps...)
	}
	text := strings.Join(a.printQueue, "\n")
	a.printQueue = nil
	a.printing = true
	if a.clearScreen {
		a.clearScreen = false
		text = clearScrollback + text
		steps = append(steps, tea.ClearScreen)
	}
	steps = append(steps, tea.Println(text), func() tea.Msg { return printDoneMsg{} })
	return tea.Sequence(steps...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	return a, tea.Batch(cmd, a.flush())
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.input.SetWidth(max(a.width-6, 10))
		a.editInput(func() {})
		if a.replayPending {
			a.replayPending = false
			a.replay(a.history)
		}
		return nil
	case printDoneMsg:
		a.printing = false
		return nil
	case spinner.TickMsg:
		if !a.animating() {
			return nil
		}
		var cmd tea.Cmd
		a.spin, cmd = a.spin.Update(msg)
		return cmd
	case tea.KeyMsg:
		return a.handleKey(msg)
	case deltaMsg:
		msg.run.stream.WriteString(msg.delta.Text)
		msg.run.reasoning.WriteString(msg.delta.Reasoning)
		return nil
	case assistantMsg:
		r := msg.run
		r.stream.Reset()
		r.reasoning.Reset()
		r.pending = append(r.pending, msg.message)
		if text := strings.TrimSpace(msg.message.Content); text != "" && r == a.sessionRun {
			a.emit(formatAssistant(&a.md, text, a.width))
		}
		return nil
	case toolStartMsg:
		msg.run.running[msg.call.ID] = runningTool{name: msg.call.Name, summary: msg.summary, args: msg.call.Arguments}
		msg.run.runOrder = append(msg.run.runOrder, msg.call.ID)
		return nil
	case toolResultMsg:
		r := msg.run
		tool, ok := r.running[msg.call.ID]
		if !ok {
			tool = runningTool{name: msg.call.Name, summary: tools.OneLine(msg.call.Arguments, 80), args: msg.call.Arguments}
		}
		delete(r.running, msg.call.ID)
		r.runOrder = slices.DeleteFunc(r.runOrder, func(id string) bool { return id == msg.call.ID })
		r.pending = append(r.pending, llm.Message{Role: llm.RoleTool, Content: msg.result, ToolCallID: msg.call.ID, ToolName: msg.call.Name, IsError: msg.isErr})
		if r == a.sessionRun {
			a.emit(formatToolBlock(tool.name, tool.summary, tool.args, msg.result, msg.isErr, a.width))
		}
		return nil
	case usageMsg:
		r := msg.run
		r.usage.InputTokens += msg.usage.InputTokens
		r.usage.OutputTokens += msg.usage.OutputTokens
		r.contextTokens = msg.usage.InputTokens + msg.usage.OutputTokens
		return nil
	case todosMsg:
		msg.run.todos = msg.todos
		return nil
	case turnDoneMsg:
		return a.finishTurn(msg)
	case approvalMsg:
		r := a.runFor(msg.ctx)
		r.approvals = append(r.approvals, &pendingApproval{req: msg.req, reply: msg.reply, ctx: msg.ctx})
		if r != a.sessionRun {
			a.notice = "Session " + sessionName(r.session) + " needs your input · ← to switch"
		}
		return nil
	case mcpChangedMsg:
		a.reportMCP()
		return a.spin.Tick
	case tasksChangedMsg:
		if a.agents != nil {
			a.agents.clampTask(a)
		}
		return a.spin.Tick
	case taskFinishedMsg:
		return a.taskFinished(agent.Task(msg))
	case modelsLoadedMsg:
		if a.picker != nil {
			return a.picker.loaded(a, msg)
		}
		return nil
	case keyCheckedMsg:
		if a.picker != nil {
			a.picker.keyChecked(a, msg)
		}
		return nil
	case compactDoneMsg:
		return a.compactDone(msg)
	case titleMsg:
		a.applyTitle(msg)
		return nil
	case contextWindowMsg:
		return nil
	case editorDoneMsg:
		if msg.err != nil {
			a.emit(formatError("editor: " + msg.err.Error()))
		}
		if a.agents != nil {
			a.agents.reload(a)
		}
		return nil
	case agentStartedMsg:
		if msg.err != nil {
			a.emit(formatError(msg.err.Error()))
		} else {
			a.notice = "Started agent " + msg.name
		}
		return a.spin.Tick
	}
	var cmd tea.Cmd
	if a.picker != nil && a.picker.stage == stageKey {
		a.picker.keyInput, cmd = a.picker.keyInput.Update(msg)
		return cmd
	}
	if a.home != nil && a.home.searching {
		a.home.search, cmd = a.home.search.Update(msg)
		return cmd
	}
	a.input, cmd = a.input.Update(msg)
	return cmd
}

func (a *App) animating() bool {
	pickerWaits := a.picker != nil && (a.picker.loading || a.picker.checking)
	return a.anyBusy() || pickerWaits || a.view == viewMCP || 0 < a.rt.Tasks.Running()
}

func (a *App) activeApproval() *pendingApproval {
	a.approvals = slices.DeleteFunc(a.approvals, func(p *pendingApproval) bool { return p.ctx.Err() != nil })
	if len(a.approvals) == 0 {
		return nil
	}
	return a.approvals[0]
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	a.notice = ""
	if ap := a.activeApproval(); ap != nil && a.view != viewHome {
		return a.approvalKey(ap, msg)
	}
	switch a.view {
	case viewModels:
		return a.picker.update(a, msg)
	case viewAgents:
		return a.agents.update(a, msg)
	case viewHome:
		return a.home.update(a, msg)
	case viewMCP:
		return a.mcpView.update(a, msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if a.busy {
			a.interrupt()
			return nil
		}
		if a.input.Value() != "" {
			a.editInput(a.input.Reset)
			return nil
		}
		if time.Since(a.lastCtrlC) < 2*time.Second {
			return a.quit()
		}
		a.lastCtrlC = time.Now()
		a.notice = "Press Ctrl+C again to exit"
		return nil
	case "ctrl+d":
		if a.input.Value() == "" {
			return a.quit()
		}
	case "esc":
		if a.busy {
			a.interrupt()
			return nil
		}
		a.editInput(a.input.Reset)
		return nil
	case "left":
		if a.input.Value() == "" {
			return a.openHome()
		}
	case "shift+tab":
		a.setMode(a.rt.Perms.Mode().Next())
		return nil
	case "enter":
		value := a.input.Value()
		if strings.HasSuffix(value, "\\") {
			a.editInput(func() { a.input.SetValue(strings.TrimSuffix(value, "\\") + "\n") })
			return nil
		}
		return a.submit(value)
	case "tab":
		if s := a.suggestions(); len(s) != 0 {
			a.editInput(func() { a.input.SetValue("/" + s[0].name + " ") })
			return nil
		}
	case "up":
		if a.recallHistory(-1) {
			return nil
		}
	case "down":
		if a.recallHistory(1) {
			return nil
		}
	}
	var cmd tea.Cmd
	a.editInput(func() { a.input, cmd = a.input.Update(msg) })
	return cmd
}

func (a *App) recallHistory(step int) bool {
	value := a.input.Value()
	if strings.Contains(value, "\n") || len(a.inputHistory) == 0 {
		return false
	}
	browsing := a.historyPos < len(a.inputHistory) && value == a.inputHistory[a.historyPos]
	if value != "" && !browsing {
		return false
	}
	pos := a.historyPos + step
	if pos < 0 || len(a.inputHistory) < pos {
		return true
	}
	a.historyPos = pos
	a.editInput(func() {
		if pos == len(a.inputHistory) {
			a.input.Reset()
		} else {
			a.input.SetValue(a.inputHistory[pos])
		}
	})
	return true
}

func (a *App) editInput(edit func()) {
	a.input.SetHeight(maxInputHeight)
	edit()
	rows := 0
	for _, line := range strings.Split(a.input.Value(), "\n") {
		rows += wrappedRows([]rune(line), a.input.Width())
	}
	a.input.SetHeight(min(max(rows, 1), maxInputHeight))
}

func (a *App) approvalKey(ap *pendingApproval, msg tea.KeyMsg) tea.Cmd {
	choices := []agent.Decision{agent.Allow, agent.AllowAlways, agent.Deny}
	decide := func(d agent.Decision) tea.Cmd {
		ap.reply <- d
		a.approvals = a.approvals[1:]
		return nil
	}
	switch msg.String() {
	case "up", "k", "shift+tab":
		ap.cursor = max(ap.cursor-1, 0)
	case "down", "j", "tab":
		ap.cursor = min(ap.cursor+1, len(choices)-1)
	case "1", "y":
		return decide(agent.Allow)
	case "2":
		return decide(agent.AllowAlways)
	case "3", "n", "esc":
		return decide(agent.Deny)
	case "enter":
		return decide(choices[ap.cursor])
	case "ctrl+c":
		decide(agent.Deny)
		a.interrupt()
	}
	return nil
}

func (a *App) submit(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	a.editInput(a.input.Reset)
	if len(a.inputHistory) == 0 || a.inputHistory[len(a.inputHistory)-1] != text {
		a.inputHistory = append(a.inputHistory, text)
	}
	a.historyPos = len(a.inputHistory)
	if isCommand(text) {
		return a.runCommand(text)
	}
	if a.busy {
		a.queue = append(a.queue, text)
		return nil
	}
	return a.startTurn(text, formatUser(text, a.width))
}

func (a *App) startTurn(text, display string) tea.Cmd {
	return a.startRunTurn(a.sessionRun, text, display)
}

func (a *App) startRunTurn(r *sessionRun, text, display string) tea.Cmd {
	current := r == a.sessionRun
	if display != "" && current {
		a.emit(display)
	}
	ag, err := a.rt.MainAgentWith(r.env, r.provider, r.model)
	if err != nil {
		if current {
			a.emit(formatError(err.Error()))
		} else {
			a.notice = "Session " + sessionName(r.session) + ": " + err.Error()
		}
		return nil
	}
	firstRequest := needsTitle(r, text)
	if firstRequest && r.session.Title == "" {
		r.session.Title, r.session.TitleSource = tools.OneLine(text, 80), agent.TitleFromText
	}
	r.history = append(r.history, llm.Message{Role: llm.RoleUser, Content: text})
	r.pending = nil
	turn := agent.Turn{Owner: r.session.ID, Provider: r.provider, Model: r.model}
	ctx, cancel := context.WithCancel(agent.WithTurn(a.rt.BaseCtx, turn))
	r.cancel = cancel
	r.busy = true
	r.turnStart = time.Now()
	history := slices.Clone(r.history)
	obs := uiObserver{send: a.send, run: r}
	cmds := []tea.Cmd{a.spin.Tick, func() tea.Msg {
		msgs, err := ag.Run(ctx, history, obs)
		cancel()
		return turnDoneMsg{run: r, msgs: msgs, err: err}
	}}
	if firstRequest {
		cmds = append(cmds, a.titleCmd(r.session.ID, text))
	}
	return tea.Batch(cmds...)
}

func (a *App) needsTitle(text string) bool {
	return needsTitle(a.sessionRun, text)
}

func needsTitle(r *sessionRun, text string) bool {
	if r.session.TitleSource == agent.TitleFromUser || strings.HasPrefix(text, "<task-notification>") {
		return false
	}
	return !slices.ContainsFunc(r.history, func(m llm.Message) bool { return m.Role == llm.RoleUser })
}

func (a *App) titleCmd(sessionID, request string) tea.Cmd {
	rt := a.rt
	return func() tea.Msg {
		title, err := rt.SessionTitle(rt.BaseCtx, request)
		return titleMsg{sessionID: sessionID, title: title, err: err}
	}
}

func (a *App) applyTitle(msg titleMsg) {
	if msg.err != nil || msg.title == "" {
		return
	}
	dir := config.SessionsDir()
	s := a.session
	if r, ok := a.runs[msg.sessionID]; ok {
		s = r.session
	}
	if s.ID != msg.sessionID {
		loaded, err := agent.LoadSession(dir, msg.sessionID)
		if err != nil {
			return
		}
		s = loaded
	}
	if s.TitleSource == agent.TitleFromUser {
		return
	}
	if err := s.SetTitle(dir, msg.title, agent.TitleFromModel); err != nil {
		a.notice = "could not save the session name: " + err.Error()
	}
}

func (a *App) interrupt() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) finishTurn(msg turnDoneMsg) tea.Cmd {
	r := msg.run
	current := r == a.sessionRun
	r.busy = false
	r.cancel = nil
	if 0 < len(msg.msgs) {
		r.history = msg.msgs
	}
	r.pending = nil
	if partial := strings.TrimSpace(r.stream.String()); partial != "" && current {
		a.emit(styleDim.Render(indent(partial, "⏺ ", "  ")))
	}
	r.stream.Reset()
	r.reasoning.Reset()
	r.running = map[string]runningTool{}
	r.runOrder = nil
	if current {
		switch {
		case msg.err == nil, errors.Is(msg.err, agent.ErrDenied):
		case errors.Is(msg.err, context.Canceled):
			a.emit(formatError("Interrupted · tell mga what to do instead"))
		default:
			a.emit(formatError(msg.err.Error()))
		}
	} else if a.view != viewHome {
		a.notice = "Session " + sessionName(r.session) + " finished · ← to switch"
	}
	a.saveRun(r)
	return a.nextTurn(r)
}

func (a *App) nextTurn(r *sessionRun) tea.Cmd {
	if r.busy {
		return nil
	}
	if 0 < len(r.queue) {
		text := strings.Join(r.queue, "\n\n")
		r.queue = nil
		return a.startRunTurn(r, text, formatUser(text, a.width))
	}
	if 0 < len(r.notifications) {
		text := strings.Join(r.notifications, "\n\n")
		r.notifications = nil
		return a.startRunTurn(r, text, formatNote(styleDim.Render("Background agent results sent to the model")))
	}
	return nil
}

func (a *App) taskFinished(t agent.Task) tea.Cmd {
	r := a.sessionRun
	if owner, ok := a.runs[t.Owner]; ok {
		r = owner
	}
	status := styleOK.Render(string(t.Status))
	if t.Status != agent.TaskCompleted {
		status = styleErr.Render(string(t.Status))
	}
	if r == a.sessionRun {
		a.emit(formatNote(fmt.Sprintf("Agent %s (%s) %s · %s · %d tool uses", t.Agent, t.ID, status, t.Elapsed(), t.ToolUses)))
	}
	if !t.NotifyMain {
		return nil
	}
	body := t.Result
	if t.Err != "" {
		body = "Error: " + t.Err + "\n" + body
	}
	r.notifications = append(r.notifications, fmt.Sprintf(
		"<task-notification>\nBackground agent %s (id %s, task %q) finished with status %s.\nResult:\n%s\n</task-notification>",
		t.Agent, t.ID, t.Description, t.Status, body))
	return a.nextTurn(r)
}

func (a *App) saveSession() {
	a.saveRun(a.sessionRun)
}

func (a *App) saveRun(r *sessionRun) {
	r.session.Provider, r.session.Model = r.provider, r.model
	r.session.Cwd = a.rt.Cwd
	r.session.Messages = r.history
	r.session.Usage = r.usage
	r.session.ContextTokens = r.contextTokens
	if err := r.session.Save(config.SessionsDir()); err != nil {
		a.notice = "could not save session: " + err.Error()
	}
}

func (a *App) quit() tea.Cmd {
	for _, r := range a.runs {
		if r.cancel != nil {
			r.cancel()
		}
	}
	return tea.Quit
}

func (a *App) replay(msgs []llm.Message) {
	start := max(len(msgs)-replayLimit, 0)
	if 0 < start {
		a.emit(styleDim.Render(fmt.Sprintf("… %d earlier messages", start)))
	}
	calls := map[string]llm.ToolCall{}
	for _, m := range msgs[start:] {
		switch m.Role {
		case llm.RoleUser:
			if strings.HasPrefix(m.Content, "<task-notification>") {
				a.emit(formatNote(styleDim.Render("Background agent results sent to the model")))
				continue
			}
			a.emit(formatUser(m.Content, a.width))
		case llm.RoleAssistant:
			if text := strings.TrimSpace(m.Content); text != "" {
				a.emit(formatAssistant(&a.md, text, a.width))
			}
			for _, c := range m.ToolCalls {
				calls[c.ID] = c
			}
		case llm.RoleTool:
			c, ok := calls[m.ToolCallID]
			if !ok {
				c = llm.ToolCall{Name: m.ToolName}
			}
			a.emit(formatToolBlock(c.Name, toolSummary(c.Name, c.Arguments), c.Arguments, m.Content, m.IsError, a.width))
		}
	}
}

func toolSummary(name, args string) string {
	for _, t := range tools.All(nil) {
		if t.Name() == name {
			return t.Summary(json.RawMessage(args))
		}
	}
	return tools.OneLine(args, 80)
}

func (a *App) View() string {
	if a.view == viewHome {
		return a.home.view(a)
	}
	var parts []string
	if live := a.liveView(); live != "" {
		parts = append(parts, live)
	}
	if ap := a.activeApproval(); ap != nil {
		parts = append(parts, a.approvalView(ap))
		return strings.Join(parts, "\n")
	}
	switch a.view {
	case viewModels:
		parts = append(parts, a.picker.view(a))
	case viewAgents:
		parts = append(parts, a.agents.view(a))
	case viewHome:
		parts = append(parts, a.home.view(a))
	case viewMCP:
		parts = append(parts, a.mcpView.view(a))
	default:
		parts = append(parts, a.chatView())
	}
	return strings.Join(parts, "\n")
}

func (a *App) liveView() string {
	var lines []string
	if text := strings.TrimSpace(a.stream.String()); text != "" {
		all := strings.Split(text, "\n")
		if liveStreamLines < len(all) {
			all = all[len(all)-liveStreamLines:]
		}
		for i, l := range all {
			prefix := "  "
			if i == 0 {
				prefix = "⏺ "
			}
			lines = append(lines, clip(prefix+l, a.width))
		}
	} else if thinking := strings.TrimSpace(a.reasoning.String()); thinking != "" {
		all := strings.Split(thinking, "\n")
		lines = append(lines, styleDim.Render(clip("✻ "+all[len(all)-1], a.width)))
	}
	for _, id := range a.runOrder {
		rt := a.running[id]
		lines = append(lines, a.spin.View()+" "+clip(styleBold.Render(rt.name)+"("+rt.summary+")", a.width-2))
		if rt.name == "Task" {
			for _, t := range a.rt.Tasks.Snapshot() {
				if t.Status == agent.TaskRunning && !t.Background {
					lines = append(lines, styleDim.Render(clip(fmt.Sprintf("  ⎿  %s: %s", t.Agent, t.LastActivity()), a.width)))
				}
			}
		}
	}
	if a.busy {
		elapsed := time.Since(a.turnStart).Round(time.Second)
		status := fmt.Sprintf("%s %s", a.spin.View(), styleAccent.Render("Working…"))
		status += styleDim.Render(fmt.Sprintf(" (%s · ↓ %s tokens · esc to interrupt)", elapsed, formatTokens(a.usage.OutputTokens)))
		lines = append(lines, "", status)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

func (a *App) chatView() string {
	var parts []string
	if hasOpenTodos(a.todos) {
		parts = append(parts, indent(formatTodos(a.todos), "  ⎿  ", "     "))
	}
	for _, q := range a.queue {
		parts = append(parts, styleDim.Render(clip("  queued: "+tools.OneLine(q, 200), a.width)))
	}
	box := titledBox(a.input.View(), a.session.Title, max(a.width-2, 10))
	parts = append(parts, box, a.statusLine())
	parts = append(parts, a.suggestionLines()...)
	return strings.Join(parts, "\n")
}

func hasOpenTodos(todos []tools.Todo) bool {
	return slices.ContainsFunc(todos, func(t tools.Todo) bool { return t.Status != tools.TodoCompleted })
}

func (a *App) setMode(m agent.Mode) {
	if err := a.rt.SetMode(m); err != nil {
		a.notice = "could not save the mode: " + err.Error()
	}
}

func (a *App) statusLine() string {
	provider, model := a.rt.Current()
	sep := styleDim.Render(" | ")
	name := styleBold.Render(model) + styleDim.Render(" ("+provider+")")
	if model == "" {
		name = styleErr.Render("no model") + styleDim.Render(" ("+provider+", use /model)")
	}
	parts := []string{name, formatTokens(a.usage.InputTokens+a.usage.OutputTokens) + " tokens"}
	if window := a.rt.ContextWindow(provider, model); 0 < window {
		parts = append(parts, fmt.Sprintf("%d%% ctx remaining", remainingPercent(a.contextTokens, window)))
	}
	if n := a.rt.Tasks.Running(); 0 < n {
		parts = append(parts, fmt.Sprintf("%d agent(s) running", n))
	}
	working, waiting := a.otherRuns()
	if 0 < working {
		parts = append(parts, styleAccent.Render(fmt.Sprintf("%d other session(s) working", working)))
	}
	if 0 < waiting {
		parts = append(parts, styleErr.Render(fmt.Sprintf("%d session(s) need your input", waiting)))
	}
	usage := clip("  "+strings.Join(parts, sep), a.width-1)

	mode := a.rt.Perms.Mode()
	label := styleAccent.Render(mode.Label())
	if mode == agent.ModeDefault {
		label = styleDim.Render(mode.Label())
	}
	right := styleDim.Render("← sessions · /help ")
	if a.notice != "" {
		right = styleInfo.Render(a.notice + " ")
	}
	left := "  " + label + styleDim.Render(" (shift+tab to cycle)")
	if a.width-1 < lipgloss.Width(left)+lipgloss.Width(right)+1 {
		left = "  " + label
	}
	return usage + "\n" + statusRow(left, right, a.width)
}

func remainingPercent(used, window int) int {
	return max(100-used*100/window, 0)
}

func statusRow(left, right string, width int) string {
	usable := width - 1
	gap := usable - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return clip(left, usable)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (a *App) approvalView(ap *pendingApproval) string {
	req := ap.req
	title := styleBold.Render(toolTitle(req.Tool))
	if req.Agent != "" && req.Agent != "main" {
		title += styleDim.Render("  (agent: " + req.Agent + ")")
	}
	body := []string{title, "", "  " + clip(req.Summary, a.width-10)}
	if req.Reason != "" {
		body = append(body, "", styleInfo.Render(clip(req.Reason, a.width-8)))
	}
	if req.Tool == "Edit" || req.Tool == "Write" {
		body = append(body, approvalPreview(req)...)
	}
	body = append(body, "", "Do you want to proceed?")
	options := []string{"Yes", req.AlwaysLabel, "No, and tell mga what to do differently (esc)"}
	for i, o := range options {
		body = append(body, cursorLine(i == ap.cursor, fmt.Sprintf("%d. %s", i+1, o)))
	}
	if 1 < len(a.approvals) {
		body = append(body, styleDim.Render(fmt.Sprintf("%d more request(s) waiting", len(a.approvals)-1)))
	}
	return stylePanel.Width(max(a.width-2, 10)).Render(strings.Join(body, "\n"))
}

func toolTitle(tool string) string {
	switch tool {
	case "Bash":
		return "Bash command"
	case "Edit":
		return "Edit file"
	case "Write":
		return "Write file"
	case "WebFetch":
		return "Fetch URL"
	}
	return displayToolName(tool)
}

func isCommand(text string) bool {
	return strings.HasPrefix(text, "/") && !strings.Contains(strings.Fields(text)[0][1:], "/")
}

func (a *App) suggestionLines() []string {
	var lines []string
	for i, c := range a.suggestions() {
		if i == 8 {
			break
		}
		name := fmt.Sprintf("/%-12s", c.name)
		if i == 0 {
			name = styleAccent.Render(name)
		}
		lines = append(lines, clip("  "+name+" "+styleDim.Render(c.desc), a.width))
	}
	return lines
}
