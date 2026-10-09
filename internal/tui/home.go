package tui

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
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
	sessions    []*agent.Session
	cursor      int
	err         string
	renaming    bool
	armedDelete string
	nameInput   textinput.Model
	task        *agentsView
	showKeys    bool
	searching   bool
	search      textinput.Model
}

var homeKeys = [][2]string{
	{"↑ ↓  k j", "move the cursor"},
	{"shift+↑ ↓", "move the session up or down within its group (Pinned or Recent)"},
	{"→  enter  l", "open the session, or the agent's live log"},
	{"ctrl+f", "search sessions by name; esc clears the search"},
	{"ctrl+r", "rename the session"},
	{"ctrl+n", "start a new session"},
	{"ctrl+t", "pin or unpin the session; pinned sessions stay on top"},
	{"ctrl+x", "stop a running agent; press twice on a session to delete it; remove a finished agent"},
	{"esc", "back to the current session"},
	{"?", "show this list"},
}

var validSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (a *App) resumeSession(id string) tea.Cmd {
	if id == a.session.ID {
		a.emit(formatNote("Already in session " + styleBold.Render(sessionName(a.session))))
		return nil
	}
	cmd := a.openHome()
	if a.busy {
		a.home.err = "A turn is running. Press esc first, then resume."
		return cmd
	}
	if !validSessionID.MatchString(id) {
		a.home.err = fmt.Sprintf("Session %q does not exist. Pick one from the list.", id)
		return cmd
	}
	s, err := agent.LoadSession(config.SessionsDir(), id)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		a.home.err = fmt.Sprintf("Session %q does not exist. Pick one from the list.", id)
		return cmd
	case err != nil:
		a.home.err = fmt.Sprintf("Session %q could not be opened: %v", id, err)
		return cmd
	}
	a.closeOverlay()
	a.loadSession(s)
	return a.learnContextWindow()
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
	slices.SortStableFunc(sessions, func(x, y *agent.Session) int {
		switch {
		case x.Pinned != y.Pinned && x.Pinned:
			return -1
		case x.Pinned != y.Pinned:
			return 1
		case x.Pinned:
			return cmp.Compare(x.PinOrder, y.PinOrder)
		case x.Order == y.Order:
			return 0
		case x.Order == 0:
			return -1
		case y.Order == 0:
			return 1
		}
		return cmp.Compare(x.Order, y.Order)
	})
	h.sessions = sessions
}

func (h *homeView) pinned() []*agent.Session {
	var out []*agent.Session
	for _, s := range h.sessions {
		if s.Pinned {
			out = append(out, s)
		}
	}
	return out
}

func (h *homeView) follow(a *App, s *agent.Session) {
	h.reload(a)
	h.cursor = max(slices.IndexFunc(h.items(a), func(x homeItem) bool { return x.isSession() && x.session.ID == s.ID }), 0)
}

func (h *homeView) query() string {
	if !h.searching {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(h.search.Value()))
}

func (h *homeView) togglePin(a *App, s *agent.Session) {
	order := 0
	if !s.Pinned {
		for _, p := range h.pinned() {
			order = max(order, p.PinOrder)
		}
		order++
	}
	if err := s.SetPin(config.SessionsDir(), !s.Pinned, order); err != nil {
		h.err = "Could not save the pin: " + err.Error()
		return
	}
	h.follow(a, s)
}

func (h *homeView) moveSession(a *App, s *agent.Session, step int) {
	var group, visible []*agent.Session
	for _, x := range h.sessions {
		if x.Pinned == s.Pinned {
			group = append(group, x)
		}
	}
	for _, item := range h.items(a) {
		if item.isSession() && item.session.Pinned == s.Pinned {
			visible = append(visible, item.session)
		}
	}
	vi := slices.Index(visible, s)
	vj := vi + step
	if vi < 0 || vj < 0 || len(visible) <= vj {
		return
	}
	i, j := slices.Index(group, s), slices.Index(group, visible[vj])
	group[i], group[j] = group[j], group[i]
	dir := config.SessionsDir()
	for k, x := range group {
		var err error
		switch {
		case s.Pinned && x.PinOrder != k+1:
			err = x.SetPin(dir, true, k+1)
		case !s.Pinned && x.Order != k+1:
			err = x.SetOrder(dir, k+1)
		}
		if err != nil {
			h.err = "Could not save the order: " + err.Error()
			break
		}
	}
	h.follow(a, s)
}

func (h *homeView) items(a *App) []homeItem {
	query := h.query()
	items := make([]homeItem, 0, len(h.sessions))
	for _, s := range h.sessions {
		if query == "" || strings.Contains(strings.ToLower(sessionName(s)), query) {
			items = append(items, homeItem{session: s})
		}
	}
	if query != "" {
		return items
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
	armed := h.armedDelete
	h.armedDelete = ""
	h.err = ""
	if h.showKeys {
		h.showKeys = false
		return nil
	}
	switch key {
	case "esc":
		if h.searching {
			h.searching = false
			if selected.isSession() {
				h.follow(a, selected.session)
			}
			return nil
		}
		a.closeOverlay()
		return nil
	case "ctrl+c":
		a.closeOverlay()
		return nil
	case "up":
		h.cursor = max(h.cursor-1, 0)
		return nil
	case "down":
		h.cursor = min(h.cursor+1, max(len(items)-1, 0))
		return nil
	case "right", "enter":
		return h.open(a, selected)
	case "shift+up", "shift+down":
		if !selected.isSession() {
			h.err = "Only sessions can be moved."
			return nil
		}
		step := 1
		if key == "shift+up" {
			step = -1
		}
		h.moveSession(a, selected.session, step)
		return nil
	case "ctrl+f":
		if !h.searching {
			h.search = textinput.New()
			h.search.Prompt = "search: "
			h.search.Placeholder = "session name"
			h.searching = true
			return h.search.Focus()
		}
		return nil
	case "ctrl+r":
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
	case "ctrl+n":
		if a.busy {
			h.err = "A turn is running. Go back and press esc first."
			return nil
		}
		a.closeOverlay()
		a.newSession()
		return nil
	case "ctrl+t":
		if !selected.isSession() {
			h.err = "Only sessions can be pinned."
			return nil
		}
		h.togglePin(a, selected.session)
		return nil
	case "ctrl+x":
		h.cut(a, selected, armed)
		return nil
	}
	if h.searching {
		var cmd tea.Cmd
		h.search, cmd = h.search.Update(msg)
		h.cursor = 0
		return cmd
	}
	switch key {
	case "?":
		h.showKeys = true
	case "k":
		h.cursor = max(h.cursor-1, 0)
	case "j":
		h.cursor = min(h.cursor+1, max(len(items)-1, 0))
	case "l":
		return h.open(a, selected)
	}
	return nil
}

func (h *homeView) cut(a *App, item homeItem, armed string) {
	if !item.isSession() {
		switch {
		case item.task.ID == "":
		case item.task.Status == agent.TaskRunning:
			a.rt.Tasks.Stop(item.task.ID)
		default:
			a.rt.Tasks.Remove(item.task.ID)
			h.cursor = min(h.cursor, max(len(h.items(a))-1, 0))
		}
		return
	}
	s := item.session
	if s.ID == a.session.ID && a.busy {
		h.err = "A turn is running in this session. Go back and press esc first."
		return
	}
	if armed != s.ID {
		h.armedDelete = s.ID
		h.err = fmt.Sprintf("Press ctrl+x again to delete %q. Any other key cancels.", sessionName(s))
		return
	}
	h.delete(a, s)
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
	return a.learnContextWindow()
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
	if h.showKeys {
		lines := []string{styleBold.Render("Session list keys"), ""}
		for _, k := range homeKeys {
			lines = append(lines, "  "+styleAccent.Render(fmt.Sprintf("%-14s", k[0]))+k[1])
		}
		lines = append(lines, "", styleDim.Render("press any key to go back to the list"))
		return stylePanel.Width(width).Render(strings.Join(lines, "\n"))
	}
	items := h.items(a)
	h.cursor = min(h.cursor, max(len(items)-1, 0))
	lines := []string{styleBold.Render("Sessions") + styleDim.Render("  "+a.rt.Cwd)}
	if h.searching {
		lines = append(lines, h.search.View()+styleDim.Render(fmt.Sprintf("  %d of %d sessions", len(items), len(h.sessions))))
		if len(items) == 0 {
			lines = append(lines, styleDim.Render("No session name matches."))
		}
	}
	lines = append(lines, "")
	start, end := window(len(items), h.cursor, homeRows)
	hasPinned := slices.ContainsFunc(h.sessions, func(s *agent.Session) bool { return s.Pinned })
	group := func(item homeItem) string {
		switch {
		case !item.isSession():
			return "Agents"
		case item.session.Pinned:
			return "Pinned"
		case hasPinned:
			return "Recent"
		}
		return ""
	}
	previous := ""
	for i := start; i < end; i++ {
		item := items[i]
		if g := group(item); g != previous {
			previous = g
			if 2 < len(lines) {
				lines = append(lines, "")
			}
			switch g {
			case "Agents":
				lines = append(lines, styleBold.Render("Agents")+styleDim.Render("  sub-agents of this run"))
			case "Pinned":
				lines = append(lines, styleAccent.Render("Pinned"))
			case "Recent":
				lines = append(lines, styleBold.Render("Recent"))
			}
		}
		lines = append(lines, cursorLine(i == h.cursor, clip(h.row(a, item), a.width-8)))
	}
	if !h.searching && len(items) == len(h.sessions) {
		lines = append(lines, "", styleBold.Render("Agents"), styleDim.Render("  No sub-agents in this run."))
	}
	switch {
	case h.renaming:
		lines = append(lines, "", h.nameInput.View(), styleDim.Render("enter save · esc cancel"))
	case h.searching:
		lines = append(lines, "", styleDim.Render("type to filter · ↑/↓ move · enter open · ctrl keys still work · esc clear search"))
	default:
		lines = append(lines, "", styleDim.Render("→/enter open · ctrl+f search · ctrl+r rename · ctrl+x stop/delete · esc back · ? help"))
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
