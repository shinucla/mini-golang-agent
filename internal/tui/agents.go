package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const (
	tabLibrary = 0
	tabRunning = 1
	logRows    = 16
)

type agentsMode int

const (
	modeList agentsMode = iota
	modeDetail
	modeForm
	modeConfirmDelete
	modeRunPrompt
	modeTaskDetail
)

type agentsView struct {
	tab        int
	mode       agentsMode
	defs       []agentdef.Definition
	defCursor  int
	taskCursor int
	taskID     string
	scroll     int
	form       *agentForm
	prompt     textinput.Model
	err        string
}

func (a *App) openAgents(tab int) tea.Cmd {
	v := &agentsView{tab: tab}
	v.reload(a)
	a.agents = v
	a.view = viewAgents
	return nil
}

func (v *agentsView) reload(a *App) {
	defs, err := a.rt.Defs.List()
	v.defs = defs
	v.err = ""
	if err != nil {
		v.err = err.Error()
	}
	v.defCursor = min(v.defCursor, max(len(v.defs)-1, 0))
}

func (v *agentsView) clampTask(a *App) {
	v.taskCursor = min(v.taskCursor, max(len(a.rt.Tasks.Snapshot())-1, 0))
}

func (v *agentsView) selectedDef() (agentdef.Definition, bool) {
	if len(v.defs) <= v.defCursor {
		return agentdef.Definition{}, false
	}
	return v.defs[v.defCursor], true
}

func (v *agentsView) selectedTask(a *App) (agent.Task, bool) {
	tasks := a.rt.Tasks.Snapshot()
	if len(tasks) <= v.taskCursor {
		return agent.Task{}, false
	}
	return tasks[v.taskCursor], true
}

func (v *agentsView) update(a *App, msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	switch v.mode {
	case modeForm:
		return v.form.update(a, v, msg)
	case modeRunPrompt:
		switch key {
		case "esc":
			v.mode = modeList
		case "enter":
			text := strings.TrimSpace(v.prompt.Value())
			def, ok := v.selectedDef()
			if text == "" || !ok {
				return nil
			}
			v.mode = modeList
			v.tab = tabRunning
			return startAgent(a, def.Name, text)
		default:
			var cmd tea.Cmd
			v.prompt, cmd = v.prompt.Update(msg)
			return cmd
		}
		return nil
	case modeConfirmDelete:
		if key == "y" {
			if def, ok := v.selectedDef(); ok {
				if err := a.rt.Defs.Delete(def); err != nil {
					v.err = err.Error()
				} else {
					a.emit(formatNote("Deleted agent " + def.Name))
				}
			}
			v.reload(a)
		}
		v.mode = modeList
		return nil
	case modeDetail:
		switch key {
		case "e":
			return v.edit(a)
		case "o":
			return v.openInEditor(a)
		case "r":
			return v.askRun()
		default:
			v.mode = modeList
		}
		return nil
	case modeTaskDetail:
		switch key {
		case "esc", "q", "enter", "left":
			v.mode = modeList
		case "x":
			a.rt.Tasks.Stop(v.taskID)
		case "up", "k":
			v.scroll++
		case "down", "j":
			v.scroll = max(v.scroll-1, 0)
		case "pgup":
			v.scroll += logRows
		case "pgdown":
			v.scroll = max(v.scroll-logRows, 0)
		case "end", "G":
			v.scroll = 0
		}
		return nil
	}

	switch key {
	case "esc", "q", "ctrl+c":
		a.closeOverlay()
		return nil
	case "tab", "left", "right":
		v.tab = 1 - v.tab
		return nil
	}
	if v.tab == tabRunning {
		return v.updateRunning(a, key)
	}
	switch key {
	case "up", "k":
		v.defCursor = max(v.defCursor-1, 0)
	case "down", "j":
		v.defCursor = min(v.defCursor+1, max(len(v.defs)-1, 0))
	case "enter":
		if _, ok := v.selectedDef(); ok {
			v.mode = modeDetail
		}
	case "n":
		v.form = newAgentForm(nil, a.width)
		v.mode = modeForm
		return v.form.focusCmd()
	case "e":
		return v.edit(a)
	case "o":
		return v.openInEditor(a)
	case "d":
		if def, ok := v.selectedDef(); ok {
			if def.Scope == agentdef.ScopeBuiltin {
				v.err = "built-in agents cannot be deleted"
				return nil
			}
			v.mode = modeConfirmDelete
		}
	case "r":
		return v.askRun()
	}
	return nil
}

func (v *agentsView) updateRunning(a *App, key string) tea.Cmd {
	tasks := a.rt.Tasks.Snapshot()
	switch key {
	case "up", "k":
		v.taskCursor = max(v.taskCursor-1, 0)
	case "down", "j":
		v.taskCursor = min(v.taskCursor+1, max(len(tasks)-1, 0))
	case "enter":
		if t, ok := v.selectedTask(a); ok {
			v.taskID = t.ID
			v.scroll = 0
			v.mode = modeTaskDetail
		}
	case "x":
		if t, ok := v.selectedTask(a); ok {
			a.rt.Tasks.Stop(t.ID)
		}
	case "c":
		a.rt.Tasks.ClearFinished()
		v.clampTask(a)
	}
	return nil
}

func (v *agentsView) edit(a *App) tea.Cmd {
	def, ok := v.selectedDef()
	if !ok {
		return nil
	}
	v.form = newAgentForm(&def, a.width)
	v.mode = modeForm
	return v.form.focusCmd()
}

func (v *agentsView) askRun() tea.Cmd {
	if _, ok := v.selectedDef(); !ok {
		return nil
	}
	v.prompt = textinput.New()
	v.prompt.Placeholder = "What should the agent do?"
	v.prompt.Prompt = "> "
	v.mode = modeRunPrompt
	return v.prompt.Focus()
}

func (v *agentsView) openInEditor(a *App) tea.Cmd {
	def, ok := v.selectedDef()
	if !ok {
		return nil
	}
	if def.Scope == agentdef.ScopeBuiltin {
		v.err = "built-in agents have no file; press e to save an editable copy"
		return nil
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], def.Path)...)
	v.mode = modeList
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err: err} })
}

func startAgent(a *App, name, prompt string) tea.Cmd {
	rt := a.rt
	return func() tea.Msg {
		_, err := rt.Spawn(rt.BaseCtx, tools.SpawnRequest{
			AgentType: name, Prompt: prompt, Description: tools.OneLine(prompt, 40), Background: true, Manual: true,
		})
		return agentStartedMsg{name: name, err: err}
	}
}

func (v *agentsView) view(a *App) string {
	width := max(a.width-2, 10)
	switch v.mode {
	case modeForm:
		return stylePanel.Width(width).Render(v.form.view(a))
	case modeTaskDetail:
		return stylePanel.Width(width).Render(v.taskDetailView(a))
	case modeDetail:
		if def, ok := v.selectedDef(); ok {
			return stylePanel.Width(width).Render(v.detailView(a, def))
		}
	}

	tasks := a.rt.Tasks.Snapshot()
	running := 0
	for _, t := range tasks {
		if t.Status == agent.TaskRunning {
			running++
		}
	}
	tabs := []string{" Library ", fmt.Sprintf(" Running (%d) ", running)}
	tabs[v.tab] = styleSelected.Reverse(true).Render(tabs[v.tab])
	tabs[1-v.tab] = styleDim.Render(tabs[1-v.tab])
	lines := []string{styleBold.Render("Agents") + "  " + tabs[0] + " " + tabs[1] + styleDim.Render("   tab to switch"), ""}

	if v.tab == tabLibrary {
		lines = append(lines, v.libraryLines(a)...)
	} else {
		lines = append(lines, v.runningLines(a, tasks)...)
	}
	if v.err != "" {
		lines = append(lines, "", styleErr.Render(clip(v.err, width-4)))
	}
	switch v.mode {
	case modeConfirmDelete:
		def, _ := v.selectedDef()
		lines = append(lines, "", styleErr.Render(fmt.Sprintf("Delete %s (%s)? y to confirm, any other key to cancel", def.Name, def.Path)))
	case modeRunPrompt:
		def, _ := v.selectedDef()
		lines = append(lines, "", "Run "+styleBold.Render(def.Name)+" in the background:", v.prompt.View(), styleDim.Render("enter start · esc cancel"))
	}
	return stylePanel.Width(width).Render(strings.Join(lines, "\n"))
}

func (v *agentsView) libraryLines(a *App) []string {
	var lines []string
	if len(v.defs) == 0 {
		lines = append(lines, styleDim.Render("No agents."))
	}
	start, end := window(len(v.defs), v.defCursor, pickerRows)
	for i := start; i < end; i++ {
		d := v.defs[i]
		row := fmt.Sprintf("%-20s %-9s %-24s ", clip(d.Name, 20), d.Scope, clip(d.ModelLabel(), 24))
		lines = append(lines, cursorLine(i == v.defCursor, clip(row+styleDim.Render(d.Description), a.width-8)))
	}
	lines = append(lines, "", styleDim.Render("enter details · n new · e edit · o open in $EDITOR · d delete · r run · esc close"))
	return lines
}

func (v *agentsView) runningLines(a *App, tasks []agent.Task) []string {
	var lines []string
	if len(tasks) == 0 {
		lines = append(lines, styleDim.Render("No sub-agents yet. The model starts them with the Task tool, or press r in the Library tab."))
	}
	start, end := window(len(tasks), v.taskCursor, pickerRows)
	for i := start; i < end; i++ {
		t := tasks[i]
		mode := "fg"
		if t.Background {
			mode = "bg"
		}
		row := fmt.Sprintf("%-4s %s %-18s %-3s %6s %3d tools  ", t.ID, statusIcon(a, t.Status), clip(t.Agent, 18), mode, t.Elapsed(), t.ToolUses)
		lines = append(lines, cursorLine(i == v.taskCursor, clip(row+styleDim.Render(t.Description), a.width-8)))
	}
	lines = append(lines, "", styleDim.Render("enter view log · x stop · c clear finished · esc close"))
	return lines
}

func statusIcon(a *App, s agent.TaskStatus) string {
	switch s {
	case agent.TaskRunning:
		return a.spin.View() + " " + fmt.Sprintf("%-9s", s)
	case agent.TaskCompleted:
		return styleOK.Render("✔ " + fmt.Sprintf("%-9s", s))
	case agent.TaskFailed:
		return styleErr.Render("✗ " + fmt.Sprintf("%-9s", s))
	}
	return styleDim.Render("■ " + fmt.Sprintf("%-9s", s))
}

func (v *agentsView) detailView(a *App, d agentdef.Definition) string {
	path := d.Path
	if path == "" {
		path = "(built-in)"
	}
	lines := []string{
		styleBold.Render(d.Name) + styleDim.Render("  "+string(d.Scope)),
		"",
		styleDim.Render("Description: ") + d.Description,
		styleDim.Render("Model:       ") + d.ModelLabel(),
		styleDim.Render("Tools:       ") + d.ToolsLabel(),
		styleDim.Render("File:        ") + path,
		"",
		styleDim.Render("System prompt:"),
		preview(d.Prompt, 12),
		"",
		styleDim.Render("e edit · o open in $EDITOR · r run · any other key back"),
	}
	return strings.Join(lines, "\n")
}

func (v *agentsView) taskDetailView(a *App) string {
	t, ok := a.rt.Tasks.Get(v.taskID)
	if !ok {
		return "The task no longer exists. Press esc."
	}
	lines := []string{
		fmt.Sprintf("%s %s  %s · %s · %d tool uses · %s tokens · %s",
			styleBold.Render(t.Agent), styleDim.Render("("+t.ID+")"), statusIcon(a, t.Status), t.Elapsed(), t.ToolUses, formatTokens(t.Tokens), t.Model),
		styleDim.Render("Task: ") + t.Description,
		styleDim.Render("Prompt: ") + clip(tools.OneLine(t.Prompt, 400), a.width-12),
		"",
	}
	log := t.Log
	end := max(len(log)-v.scroll, 0)
	start := max(end-logRows, 0)
	if 0 < start {
		lines = append(lines, styleDim.Render(fmt.Sprintf("… %d earlier lines (↑ to scroll)", start)))
	}
	for _, l := range log[start:end] {
		lines = append(lines, clip(l, a.width-8))
	}
	if t.Err != "" {
		lines = append(lines, styleErr.Render("Error: "+t.Err))
	}
	lines = append(lines, "", styleDim.Render("↑/↓ scroll · x stop · esc back"))
	return strings.Join(lines, "\n")
}
