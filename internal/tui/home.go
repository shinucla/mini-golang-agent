package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const homeRows = 14

type homeItem struct {
	session *agent.Session
	task    agent.Task
}

func (i homeItem) isSession() bool { return i.session != nil }

type homeView struct {
	sessions      []*agent.Session
	cursor        int
	err           string
	renaming      bool
	confirmDelete bool
	nameInput     textinput.Model
	task          *agentsView
}

func (a *App) openHome() tea.Cmd {
	h := &homeView{}
	h.reload(a)
	h.cursor = max(slices.IndexFunc(h.sessions, func(s *agent.Session) bool { return s.ID == a.session.ID }), 0)
	a.home = h
	a.view = viewHome
	return nil
}

func (h *homeView) reload(a *App) {
	sessions, err := agent.ListSessions(config.SessionsDir(), a.rt.Cwd)
	h.err = ""
	if err != nil {
		h.err = err.Error()
	}
	i := slices.IndexFunc(sessions, func(s *agent.Session) bool { return s.ID == a.session.ID })
	if i < 0 {
		sessions = append([]*agent.Session{a.session}, sessions...)
	} else {
		sessions[i] = a.session
	}
	h.sessions = sessions
}

func (h *homeView) items(a *App) []homeItem {
	items := make([]homeItem, 0, len(h.sessions))
	for _, s := range h.sessions {
		items = append(items, homeItem{session: s})
	}
	for _, t := range a.rt.Tasks.Snapshot() {
		items = append(items, homeItem{task: t})
	}
	return items
}

func (h *homeView) update(a *App, msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if h.task != nil {
		cmd := h.task.update(a, msg)
		if h.task.mode != modeTaskDetail {
			h.task = nil
		}
		return cmd
	}
	items := h.items(a)
	h.cursor = min(h.cursor, max(len(items)-1, 0))
	var selected homeItem
	if h.cursor < len(items) {
		selected = items[h.cursor]
	}
	if h.renaming {
		return h.updateRename(a, msg, selected)
	}
	if h.confirmDelete {
		h.confirmDelete = false
		if key == "y" {
			h.delete(a, selected.session)
		}
		return nil
	}

	h.err = ""
	switch key {
	case "esc", "q", "ctrl+c":
		a.closeOverlay()
	case "up", "k":
		h.cursor = max(h.cursor-1, 0)
	case "down", "j":
		h.cursor = min(h.cursor+1, max(len(items)-1, 0))
	case "right", "enter", "l":
		return h.open(a, selected)
	case "r":
		if !selected.isSession() {
			h.err = "Only sessions can be renamed."
			return nil
		}
		h.nameInput = textinput.New()
		h.nameInput.Prompt = "name: "
		h.nameInput.Placeholder = "a short name for this session"
		h.nameInput.CharLimit = 80
		h.nameInput.SetValue(selected.session.Title)
		h.nameInput.CursorEnd()
		h.renaming = true
		return h.nameInput.Focus()
	case "ctrl+n", "n":
		if a.busy {
			h.err = "A turn is running. Go back and press esc first."
			return nil
		}
		a.closeOverlay()
		a.newSession()
	case "d", "delete":
		if !selected.isSession() {
			return nil
		}
		if selected.session.ID == a.session.ID && a.busy {
			h.err = "A turn is running in this session. Go back and press esc first."
			return nil
		}
		h.confirmDelete = true
	case "x":
		if !selected.isSession() && selected.task.ID != "" {
			a.rt.Tasks.Stop(selected.task.ID)
		}
	}
	return nil
}

func (h *homeView) updateRename(a *App, msg tea.KeyMsg, selected homeItem) tea.Cmd {
	switch msg.String() {
	case "esc":
		h.renaming = false
		return nil
	case "enter":
		name := strings.TrimSpace(h.nameInput.Value())
		if name == "" {
			h.err = "The name is empty."
			return nil
		}
		h.renaming = false
		if !selected.isSession() {
			return nil
		}
		if err := selected.session.Rename(config.SessionsDir(), name); err != nil {
			h.err = "Could not rename: " + err.Error()
		}
		return nil
	}
	var cmd tea.Cmd
	h.nameInput, cmd = h.nameInput.Update(msg)
	return cmd
}

func (h *homeView) open(a *App, item homeItem) tea.Cmd {
	if !item.isSession() {
		if item.task.ID == "" {
			return nil
		}
		h.task = &agentsView{tab: tabRunning, mode: modeTaskDetail, taskID: item.task.ID}
		return nil
	}
	if item.session.ID == a.session.ID {
		a.closeOverlay()
		return nil
	}
	if a.busy {
		h.err = "A turn is running in this session. Go back and press esc first."
		return nil
	}
	a.closeOverlay()
	a.loadSession(item.session)
	return nil
}

func (h *homeView) delete(a *App, s *agent.Session) {
	if s == nil {
		return
	}
	if err := agent.DeleteSession(config.SessionsDir(), s.ID); err != nil {
		h.err = "Could not delete: " + err.Error()
		return
	}
	if s.ID == a.session.ID {
		a.newSession()
	}
	a.emit(formatNote("Deleted session " + sessionName(s)))
	h.reload(a)
	h.cursor = min(h.cursor, max(len(h.items(a))-1, 0))
}

func (a *App) newSession() {
	a.session = agent.NewSession(a.rt.Cwd)
	a.history = nil
	a.todos = nil
	a.usage = llm.Usage{}
	a.contextTokens = 0
	a.env = &tools.Env{Cwd: a.rt.Cwd}
	a.wireEnv()
	a.resetScreen()
}

func (a *App) loadSession(s *agent.Session) {
	a.session = s
	a.history = slices.Clone(s.Messages)
	a.todos = nil
	a.usage = s.Usage
	a.contextTokens = s.ContextTokens
	a.env = &tools.Env{Cwd: a.rt.Cwd}
	a.wireEnv()
	if s.Provider != "" && s.Model != "" {
		a.rt.SetCurrent(s.Provider, s.Model)
	}
	a.resetScreen()
	a.emit(formatNote("Opened session " + styleBold.Render(sessionName(s)) + styleDim.Render(" · "+s.ID)))
	a.replay(a.history)
}

func sessionName(s *agent.Session) string {
	if s.Title != "" {
		return s.Title
	}
	if len(s.Messages) == 0 {
		return "(new session)"
	}
	return "(untitled)"
}

func (h *homeView) view(a *App) string {
	width := max(a.width-2, 10)
	if h.task != nil {
		return stylePanel.Width(width).Render(h.task.taskDetailView(a))
	}
	items := h.items(a)
	h.cursor = min(h.cursor, max(len(items)-1, 0))
	lines := []string{styleBold.Render("Sessions") + styleDim.Render("  "+a.rt.Cwd), ""}
	start, end := window(len(items), h.cursor, homeRows)
	agentsHeader := false
	for i := start; i < end; i++ {
		item := items[i]
		if !item.isSession() && !agentsHeader {
			lines = append(lines, "", styleBold.Render("Agents")+styleDim.Render("  sub-agents of this run"), "")
			agentsHeader = true
		}
		lines = append(lines, cursorLine(i == h.cursor, clip(h.row(a, item), a.width-8)))
	}
	if len(items) == len(h.sessions) {
		lines = append(lines, "", styleBold.Render("Agents"), styleDim.Render("  No sub-agents in this run."))
	}
	switch {
	case h.renaming:
		lines = append(lines, "", h.nameInput.View(), styleDim.Render("enter save · esc cancel"))
	case h.confirmDelete:
		lines = append(lines, "", styleErr.Render("Delete this session? y to confirm, any other key to cancel"))
	default:
		lines = append(lines, "", styleDim.Render("→/enter open · r rename · n new session · d delete · x stop agent · esc back"))
	}
	if h.err != "" {
		lines = append(lines, styleErr.Render(h.err))
	}
	return stylePanel.Width(width).Render(strings.Join(lines, "\n"))
}

func (h *homeView) row(a *App, item homeItem) string {
	if !item.isSession() {
		t := item.task
		return fmt.Sprintf("%-4s %s %-18s %6s  ", t.ID, statusIcon(a, t.Status), clip(t.Agent, 18), t.Elapsed()) + styleDim.Render(t.Description)
	}
	s := item.session
	marker := "  "
	if s.ID == a.session.ID {
		marker = styleOK.Render("● ")
	}
	updated := "not saved yet"
	if !s.Updated.IsZero() {
		updated = s.Updated.Format("2006-01-02 15:04")
	}
	model := s.Model
	if s.Provider != "" {
		model = s.Provider + ":" + s.Model
	}
	row := marker + fmt.Sprintf("%-40s ", clip(sessionName(s), 40)) + styleDim.Render(fmt.Sprintf("%-16s %4d msgs  %s", updated, len(s.Messages), model))
	if s.ID == a.session.ID {
		row += styleOK.Render("  current")
	}
	return row
}
