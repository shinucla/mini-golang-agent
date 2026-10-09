package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
)

const (
	pickerRows   = 12
	listTimeout  = 20 * time.Second
	keyInputSize = 60
)

type pickerStage int

const (
	stageProviders pickerStage = iota
	stageKey
	stageModels
)

type modelPicker struct {
	stage     pickerStage
	providers []string
	cursor    int
	provider  string
	models    []llm.Model
	filter    string
	loading   bool
	err       error
	keyInput  textinput.Model
	checking  bool
	keyErr    string
	notice    string
}

func (a *App) openPicker() tea.Cmd {
	current, _ := a.rt.Current()
	names := a.rt.Cfg.ProviderNames()
	a.picker = &modelPicker{providers: names, cursor: max(slices.Index(names, current), 0)}
	a.view = viewModels
	return nil
}

func (a *App) closeOverlay() {
	a.input.Placeholder = chatPlaceholder
	a.view = viewChat
	a.picker = nil
	a.agents = nil
	a.home = nil
	a.mcpView = nil
}

func loadModels(rt *agent.Runtime, name string) tea.Cmd {
	return func() tea.Msg {
		p, err := rt.Provider(name)
		if err != nil {
			return modelsLoadedMsg{provider: name, err: err}
		}
		ctx, cancel := context.WithTimeout(rt.BaseCtx, listTimeout)
		defer cancel()
		models, err := p.ListModels(ctx)
		return modelsLoadedMsg{provider: name, models: models, err: err}
	}
}

func checkKey(rt *agent.Runtime, name, key string) tea.Cmd {
	return func() tea.Msg {
		pc, _ := rt.ProviderConfig(name)
		pc.APIKey = key
		p, err := llm.New(name, pc)
		if err != nil {
			return keyCheckedMsg{provider: name, key: key, err: err}
		}
		ctx, cancel := context.WithTimeout(rt.BaseCtx, listTimeout)
		defer cancel()
		models, err := p.ListModels(ctx)
		return keyCheckedMsg{provider: name, key: key, models: models, err: err}
	}
}

func isAuthError(err error) bool {
	var apiErr *llm.APIError
	return errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden)
}

func openURL(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func (m *modelPicker) filtered() []llm.Model {
	if m.filter == "" {
		return m.models
	}
	needle := strings.ToLower(m.filter)
	var out []llm.Model
	for _, model := range m.models {
		if strings.Contains(strings.ToLower(model.ID), needle) {
			out = append(out, model)
		}
	}
	return out
}

func (m *modelPicker) showModels(a *App, models []llm.Model) {
	m.stage = stageModels
	m.loading = false
	m.models, m.err, m.filter = models, nil, ""
	current, model := a.rt.Current()
	m.cursor = 0
	if current == m.provider {
		m.cursor = max(slices.IndexFunc(m.models, func(x llm.Model) bool { return x.ID == model }), 0)
	}
}

func (m *modelPicker) chooseProvider(a *App, name string) tea.Cmd {
	m.provider = name
	m.notice = ""
	pc, _ := a.rt.ProviderConfig(name)
	if !pc.Configured() {
		return m.askKey("")
	}
	m.stage = stageModels
	m.loading = true
	m.models, m.err, m.filter, m.cursor = nil, nil, "", 0
	return tea.Batch(loadModels(a.rt, name), a.spin.Tick)
}

func (m *modelPicker) askKey(reason string) tea.Cmd {
	in := textinput.New()
	in.Placeholder = "paste the API key"
	in.Prompt = "> "
	in.EchoMode = textinput.EchoPassword
	in.EchoCharacter = '•'
	in.Width = keyInputSize
	m.keyInput = in
	m.stage = stageKey
	m.checking = false
	m.keyErr = reason
	return m.keyInput.Focus()
}

func (m *modelPicker) loaded(a *App, msg modelsLoadedMsg) tea.Cmd {
	if msg.provider != m.provider || m.stage != stageModels {
		return nil
	}
	pc, _ := a.rt.ProviderConfig(m.provider)
	if isAuthError(msg.err) && !pc.Local {
		return m.askKey(fmt.Sprintf("The provider rejected the current key (%s). Enter a new key.", msg.err))
	}
	a.rt.RememberModels(m.provider, msg.models)
	m.showModels(a, msg.models)
	m.err = msg.err
	return nil
}

func (m *modelPicker) keyChecked(a *App, msg keyCheckedMsg) {
	if msg.provider != m.provider || m.stage != stageKey {
		return
	}
	m.checking = false
	if msg.err != nil {
		if isAuthError(msg.err) {
			m.keyErr = "The provider rejected this key: " + msg.err.Error()
		} else {
			m.keyErr = "Could not check the key: " + msg.err.Error()
		}
		return
	}
	if err := a.rt.SetProviderKey(m.provider, msg.key); err != nil {
		m.keyErr = "Could not save the key: " + err.Error()
		return
	}
	a.rt.RememberModels(m.provider, msg.models)
	m.showModels(a, msg.models)
	m.notice = "✔ Key saved to " + a.rt.Cfg.Path()
}

func (m *modelPicker) update(a *App, msg tea.KeyMsg) tea.Cmd {
	switch m.stage {
	case stageKey:
		return m.updateKey(a, msg)
	case stageModels:
		return m.updateModels(a, msg)
	}
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		a.closeOverlay()
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, len(m.providers)-1)
	case "enter", "right":
		return m.chooseProvider(a, m.providers[m.cursor])
	case "e":
		name := m.providers[m.cursor]
		if pc, _ := a.rt.ProviderConfig(name); !pc.Local {
			m.provider = name
			return m.askKey("")
		}
	}
	return nil
}

func (m *modelPicker) updateKey(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.stage = stageProviders
		m.cursor = max(slices.Index(m.providers, m.provider), 0)
		return nil
	case "ctrl+c":
		a.closeOverlay()
		return nil
	case "ctrl+o":
		pc, _ := a.rt.ProviderConfig(m.provider)
		if pc.KeyURL == "" {
			return nil
		}
		if err := openURL(pc.KeyURL); err != nil {
			m.keyErr = "Could not open a browser. Open the URL yourself."
		}
		return nil
	case "enter":
		if m.checking {
			return nil
		}
		key := strings.TrimSpace(m.keyInput.Value())
		if key == "" {
			m.keyErr = "Paste a key first."
			return nil
		}
		m.checking = true
		m.keyErr = ""
		return tea.Batch(checkKey(a.rt, m.provider, key), a.spin.Tick)
	}
	var cmd tea.Cmd
	m.keyInput, cmd = m.keyInput.Update(msg)
	return cmd
}

func (m *modelPicker) updateModels(a *App, msg tea.KeyMsg) tea.Cmd {
	list := m.filtered()
	switch msg.String() {
	case "esc", "left":
		m.stage = stageProviders
		m.cursor = max(slices.Index(m.providers, m.provider), 0)
	case "ctrl+c":
		a.closeOverlay()
	case "up":
		m.cursor = max(m.cursor-1, 0)
	case "down":
		m.cursor = min(m.cursor+1, max(len(list)-1, 0))
	case "pgup":
		m.cursor = max(m.cursor-pickerRows, 0)
	case "pgdown":
		m.cursor = min(m.cursor+pickerRows, max(len(list)-1, 0))
	case "backspace":
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.cursor = 0
		}
	case "enter":
		model := strings.TrimSpace(m.filter)
		if m.cursor < len(list) {
			model = list[m.cursor].ID
		}
		if model == "" {
			return nil
		}
		provider := m.provider
		a.closeOverlay()
		a.setModel(provider, model)
	default:
		if msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
			m.cursor = 0
		}
	}
	return nil
}

func (m *modelPicker) view(a *App) string {
	var body string
	switch m.stage {
	case stageKey:
		body = m.keyView(a)
	case stageModels:
		body = m.modelsView(a)
	default:
		body = m.providersView(a)
	}
	return stylePanel.Width(max(a.width-2, 10)).Render(body)
}

func (m *modelPicker) providersView(a *App) string {
	lines := []string{styleBold.Render("Select a provider"), ""}
	start, end := window(len(m.providers), m.cursor, pickerRows)
	for i := start; i < end; i++ {
		name := m.providers[i]
		pc, _ := a.rt.ProviderConfig(name)
		lines = append(lines, cursorLine(i == m.cursor, fmt.Sprintf("%-12s %s", name, providerStatus(pc))))
	}
	lines = append(lines, "", styleDim.Render("enter select · e enter or replace API key · esc close"))
	return strings.Join(lines, "\n")
}

func (m *modelPicker) keyView(a *App) string {
	pc, _ := a.rt.ProviderConfig(m.provider)
	title := "Connect " + m.provider
	if pc.Key() != "" {
		title = "Replace the API key for " + m.provider
	}
	lines := []string{styleBold.Render(title), "", m.provider + " needs an API key."}
	if pc.KeyURL != "" {
		lines = append(lines, "Get a key: "+styleInfo.Render(pc.KeyURL)+styleDim.Render("  (ctrl+o opens it in the browser)"))
	} else {
		lines = append(lines, "Get a key from the provider's dashboard ("+pc.BaseURL+").")
	}
	lines = append(lines, "", m.keyInput.View())
	switch {
	case m.checking:
		lines = append(lines, a.spin.View()+" checking the key…")
	case m.keyErr != "":
		lines = append(lines, styleErr.Render(clip(m.keyErr, a.width-8)))
	}
	note := "The key is saved in " + a.rt.Cfg.Path() + " (only your user can read it)."
	if env := pc.KeyEnvName(); env != "" {
		note += "\nA saved key takes priority over the " + env + " environment variable."
	}
	lines = append(lines, "", styleDim.Render(note), "", styleDim.Render("enter check and save · esc back"))
	return strings.Join(lines, "\n")
}

func (m *modelPicker) modelsView(a *App) string {
	lines := []string{styleBold.Render("Select a model for " + m.provider)}
	if m.notice != "" {
		lines = append(lines, styleOK.Render(m.notice))
	}
	lines = append(lines, "filter: "+m.filter+styleDim.Render("▏ type to filter, or type a model id"), "")
	list := m.filtered()
	pc, _ := a.rt.ProviderConfig(m.provider)
	switch {
	case m.loading:
		lines = append(lines, a.spin.View()+" loading models…")
	case m.err != nil:
		lines = append(lines, styleErr.Render(clip("could not list models: "+m.err.Error(), a.width-8)))
		if pc.Local {
			lines = append(lines, styleDim.Render("Is the server running at "+pc.BaseURL+"? For Ollama, run `ollama serve`."))
		}
		lines = append(lines, styleDim.Render("Type a model id and press enter to use it anyway."))
	case len(list) == 0 && m.filter != "":
		lines = append(lines, styleDim.Render("No match. Press enter to use \""+m.filter+"\" as the model id."))
	case len(list) == 0:
		lines = append(lines, styleDim.Render("The provider returned no models."))
	}
	start, end := window(len(list), m.cursor, pickerRows)
	currentProvider, currentModel := a.rt.Current()
	for i := start; i < end; i++ {
		model := list[i]
		label := model.ID
		if currentProvider == m.provider && model.ID == currentModel {
			label += styleOK.Render(" ✔")
		}
		if model.Description != "" && model.Description != model.ID {
			label += styleDim.Render("  " + model.Description)
		}
		if 0 < model.ContextWindow {
			label += styleDim.Render(fmt.Sprintf("  %s ctx", formatTokens(model.ContextWindow)))
		}
		lines = append(lines, cursorLine(i == m.cursor, clip(label, a.width-10)))
	}
	if 0 < len(list) {
		lines = append(lines, styleDim.Render(fmt.Sprintf("%d of %d models", m.cursor+1, len(list))))
	}
	lines = append(lines, "", styleDim.Render("enter use and save as default · esc back"))
	return strings.Join(lines, "\n")
}

func providerStatus(pc config.ProviderConfig) string {
	switch {
	case pc.Local:
		return styleInfo.Render("local") + styleDim.Render("  "+pc.BaseURL)
	case pc.Key() != "":
		return styleOK.Render("✔ key set") + styleDim.Render("  "+pc.BaseURL)
	}
	return styleErr.Render("✗ no key") + styleDim.Render("  enter to add a key")
}
