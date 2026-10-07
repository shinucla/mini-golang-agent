package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const (
	fieldName = iota
	fieldDescription
	fieldModel
	fieldTools
	fieldScope
	fieldPrompt
	fieldCount
)

var fieldLabels = []string{"Name", "Description", "Model", "Tools", "Scope", "System prompt"}

type agentForm struct {
	inputs       []textinput.Model
	prompt       textarea.Model
	scope        agentdef.Scope
	focus        int
	previousPath string
	editing      bool
	err          string
}

func newAgentForm(d *agentdef.Definition, width int) *agentForm {
	f := &agentForm{scope: agentdef.ScopeProject}
	placeholders := []string{"code-reviewer", "When the main agent should use this agent", "inherit, provider:model, or a model id", "empty = all tools except Task"}
	for _, p := range placeholders {
		in := textinput.New()
		in.Placeholder = p
		in.Prompt = ""
		in.Width = max(width-24, 20)
		f.inputs = append(f.inputs, in)
	}
	f.prompt = textarea.New()
	f.prompt.ShowLineNumbers = false
	f.prompt.CharLimit = 0
	f.prompt.MaxHeight = 0
	f.prompt.Placeholder = "You are a specialist agent that…"
	f.prompt.FocusedStyle.CursorLine = lipgloss.NewStyle()
	f.prompt.SetWidth(max(width-8, 20))
	f.prompt.SetHeight(8)

	if d != nil {
		f.editing = d.Scope != agentdef.ScopeBuiltin
		f.inputs[fieldName].SetValue(d.Name)
		f.inputs[fieldDescription].SetValue(d.Description)
		f.inputs[fieldModel].SetValue(d.Model)
		f.inputs[fieldTools].SetValue(strings.Join(d.Tools, ", "))
		f.prompt.SetValue(d.Prompt)
		if f.editing {
			f.scope = d.Scope
			f.previousPath = d.Path
		}
	}
	return f
}

func (f *agentForm) focusCmd() tea.Cmd {
	for i := range f.inputs {
		f.inputs[i].Blur()
	}
	f.prompt.Blur()
	switch {
	case f.focus < len(f.inputs):
		return f.inputs[f.focus].Focus()
	case f.focus == fieldPrompt:
		return f.prompt.Focus()
	}
	return nil
}

func (f *agentForm) update(a *App, v *agentsView, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		v.mode = modeList
		return nil
	case "ctrl+s":
		return f.save(a, v)
	case "tab":
		f.focus = (f.focus + 1) % fieldCount
		return f.focusCmd()
	case "shift+tab":
		f.focus = (f.focus + fieldCount - 1) % fieldCount
		return f.focusCmd()
	}
	var cmd tea.Cmd
	switch {
	case f.focus < len(f.inputs):
		if msg.String() == "enter" {
			f.focus++
			return f.focusCmd()
		}
		f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	case f.focus == fieldScope:
		switch msg.String() {
		case "left", "right", " ", "h", "l":
			if f.scope == agentdef.ScopeProject {
				f.scope = agentdef.ScopeUser
			} else {
				f.scope = agentdef.ScopeProject
			}
		case "enter":
			f.focus++
			return f.focusCmd()
		}
	case f.focus == fieldPrompt:
		f.prompt, cmd = f.prompt.Update(msg)
	}
	return cmd
}

func (f *agentForm) save(a *App, v *agentsView) tea.Cmd {
	toolNames := agentdef.ParseTools(f.inputs[fieldTools].Value())
	known := tools.Names()
	for _, t := range toolNames {
		if !slices.Contains(known, t) {
			f.err = fmt.Sprintf("unknown tool %q; available: %s", t, strings.Join(known, ", "))
			return nil
		}
	}
	model := strings.TrimSpace(f.inputs[fieldModel].Value())
	if model == "inherit" {
		model = ""
	}
	d := agentdef.Definition{
		Name:        strings.TrimSpace(f.inputs[fieldName].Value()),
		Description: strings.TrimSpace(f.inputs[fieldDescription].Value()),
		Model:       model,
		Tools:       toolNames,
		Prompt:      f.prompt.Value(),
		Scope:       f.scope,
	}
	if strings.TrimSpace(d.Prompt) == "" {
		f.err = "the system prompt is empty"
		return nil
	}
	saved, err := a.rt.Defs.Save(d, f.previousPath)
	if err != nil {
		f.err = err.Error()
		return nil
	}
	a.emit(formatNote("Saved agent " + styleBold.Render(saved.Name) + styleDim.Render(" → "+saved.Path)))
	v.reload(a)
	for i, def := range v.defs {
		if def.Name == saved.Name {
			v.defCursor = i
		}
	}
	v.mode = modeList
	return nil
}

func (f *agentForm) view(a *App) string {
	title := "New agent"
	if f.editing {
		title = "Edit agent"
	}
	lines := []string{styleBold.Render(title) + styleDim.Render("   tab next field · ctrl+s save · esc cancel"), ""}
	for i := range fieldCount {
		label := fmt.Sprintf("%-14s", fieldLabels[i])
		if i == f.focus {
			label = styleSelected.Render(label)
		} else {
			label = styleDim.Render(label)
		}
		switch {
		case i < len(f.inputs):
			lines = append(lines, label+f.inputs[i].View())
		case i == fieldScope:
			project, user := " project ", " user "
			if f.scope == agentdef.ScopeProject {
				project = styleSelected.Reverse(true).Render(project)
			} else {
				user = styleSelected.Reverse(true).Render(user)
			}
			lines = append(lines, label+project+" "+user+styleDim.Render("  ←/→ ("+a.rt.Defs.ProjectDir+" or "+a.rt.Defs.UserDir+")"))
		default:
			lines = append(lines, label, f.prompt.View())
		}
	}
	lines = append(lines, "", styleDim.Render("Tools available: "+strings.Join(tools.Names(), ", ")))
	if f.err != "" {
		lines = append(lines, styleErr.Render(f.err))
	}
	return strings.Join(lines, "\n")
}
