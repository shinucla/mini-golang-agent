package tui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type echoProvider struct{}

func (echoProvider) Name() string { return "fake" }
func (echoProvider) ListModels(context.Context) ([]llm.Model, error) {
	return []llm.Model{{ID: "m"}}, nil
}

func (echoProvider) Chat(_ context.Context, req llm.Request, onDelta func(llm.Delta)) (*llm.Response, error) {
	last := req.Messages[len(req.Messages)-1]
	onDelta(llm.Delta{Text: "echo"})
	return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: "echo: " + last.Content}}, nil
}

func newTestApp(t *testing.T) *App {
	t.Setenv("MGA_HOME", t.TempDir())
	cwd := t.TempDir()
	rt := agent.NewRuntime(context.Background(), &config.Config{Providers: map[string]config.ProviderConfig{
		"fake": {Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", Local: true},
	}}, cwd, agent.NewPermissions(agent.ModeDefault, nil))
	rt.Defs = &agentdef.Store{UserDir: t.TempDir(), ProjectDir: filepath.Join(cwd, ".mga", "agents")}
	rt.SetCurrent("fake", "m")
	app := newApp(rt, agent.NewSession(cwd), "", true)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return app
}

func drain(app *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drain(app, c)
		}
	case nil, spinner.TickMsg, cursor.BlinkMsg:
	default:
		if strings.HasPrefix(fmt.Sprintf("%T", msg), "cursor.") {
			return
		}
		_, next := app.Update(msg)
		drain(app, next)
	}
}

func typeText(app *App, s string) {
	for _, r := range s {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestTurnRunsAndStoresHistory(t *testing.T) {
	app := newTestApp(t)
	app.rt.Cfg.Providers["fake"] = config.ProviderConfig{Local: true}
	app.rt.RegisterProvider("fake", echoProvider{})

	typeText(app, "hello")
	if !strings.Contains(app.View(), "hello") {
		t.Fatalf("input not shown:\n%s", app.View())
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !app.busy {
		t.Fatal("turn did not start")
	}
	drain(app, cmd)
	if app.busy || len(app.history) != 2 || app.history[1].Content != "echo: hello" {
		t.Fatalf("busy=%v history=%+v", app.busy, app.history)
	}
	if _, err := os.Stat(filepath.Join(config.SessionsDir(), app.session.ID+".json")); err != nil {
		t.Fatalf("session not saved: %v", err)
	}
}

func TestApprovalKeysReplyAndClear(t *testing.T) {
	app := newTestApp(t)
	reply := make(chan agent.Decision, 1)
	app.Update(approvalMsg{
		req:   agent.ApprovalRequest{Tool: "Bash", Summary: "rm -rf build", AlwaysLabel: "Yes, and don't ask again for `rm` commands this session"},
		reply: reply, ctx: context.Background(),
	})
	view := app.View()
	if !strings.Contains(view, "rm -rf build") || !strings.Contains(view, "Do you want to proceed?") {
		t.Fatalf("approval not shown:\n%s", view)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-reply; got != agent.AllowAlways {
		t.Fatalf("decision = %v", got)
	}
	if strings.Contains(app.View(), "Do you want to proceed?") {
		t.Fatal("approval still shown")
	}
}

func TestAgentsFormCreatesDefinition(t *testing.T) {
	app := newTestApp(t)
	typeText(app, "/agents")
	drain(app, func() tea.Msg { _, c := app.Update(tea.KeyMsg{Type: tea.KeyEnter}); return c })
	if app.view != viewAgents || !strings.Contains(app.View(), "general-purpose") {
		t.Fatalf("agents view not open:\n%s", app.View())
	}
	typeText(app, "n")
	typeText(app, "reviewer")
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeText(app, "Reviews diffs")
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeText(app, "Read, Grep")
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	typeText(app, "You review code.")
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	d, ok := app.rt.Defs.Get("reviewer")
	if !ok || d.Scope != agentdef.ScopeProject || d.Prompt != "You review code." || len(d.Tools) != 2 {
		t.Fatalf("definition = %+v ok=%v\n%s", d, ok, app.View())
	}
	if !strings.Contains(app.View(), "reviewer") {
		t.Fatalf("new agent not listed:\n%s", app.View())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(app.View(), "No sub-agents yet") {
		t.Fatalf("running tab not shown:\n%s", app.View())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewChat {
		t.Fatal("esc did not close the agents view")
	}
}

func TestModelPickerFiltersAndSelects(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", echoProvider{})
	app.openPicker()
	for app.picker.providers[app.picker.cursor] != "fake" {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drain(app, cmd)
	if len(app.picker.models) != 1 {
		t.Fatalf("models not loaded: %+v", app.picker)
	}
	typeText(app, "custom-model")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p, m := app.rt.Current(); p != "fake" || m != "custom-model" || app.view != viewChat {
		t.Fatalf("current = %s:%s view=%v", p, m, app.view)
	}
}

func keyServer(t *testing.T, goodKey string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
			return
		}
		io.WriteString(w, `{"data":[{"id":"m1"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newKeyApp(t *testing.T, pc config.ProviderConfig) *App {
	t.Setenv("MGA_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers = map[string]config.ProviderConfig{"acme": pc}
	rt := agent.NewRuntime(context.Background(), cfg, t.TempDir(), agent.NewPermissions(agent.ModeDefault, nil))
	rt.SetCurrent("acme", "")
	app := newApp(rt, agent.NewSession(rt.Cwd), "", true)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.openPicker()
	for app.picker.providers[app.picker.cursor] != "acme" {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	return app
}

func press(app *App, k tea.KeyType) {
	_, cmd := app.Update(tea.KeyMsg{Type: k})
	drain(app, cmd)
}

func TestPickerAsksForKeyChecksAndSaves(t *testing.T) {
	srv := keyServer(t, "good-key")
	t.Setenv("ACME_TEST_KEY", "")
	app := newKeyApp(t, config.ProviderConfig{Type: config.TypeOpenAI, BaseURL: srv.URL, APIKeyEnv: "ACME_TEST_KEY", KeyURL: "https://example.com/keys"})

	press(app, tea.KeyEnter)
	view := app.View()
	if app.picker.stage != stageKey || !strings.Contains(view, "Connect acme") || !strings.Contains(view, "https://example.com/keys") {
		t.Fatalf("key screen not shown:\n%s", view)
	}

	typeText(app, "bad-key")
	if strings.Contains(app.View(), "bad-key") {
		t.Fatal("the key must be masked")
	}
	press(app, tea.KeyEnter)
	if app.picker.stage != stageKey || !strings.Contains(app.picker.keyErr, "rejected") {
		t.Fatalf("bad key not rejected: stage=%v err=%q", app.picker.stage, app.picker.keyErr)
	}
	if data, _ := os.ReadFile(app.rt.Cfg.Path()); strings.Contains(string(data), "bad-key") {
		t.Fatal("a rejected key was saved")
	}

	for range "bad-key" {
		app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeText(app, "good-key")
	press(app, tea.KeyEnter)
	if app.picker.stage != stageModels || len(app.picker.models) != 1 || !strings.Contains(app.View(), "Key saved") {
		t.Fatalf("models not shown after a good key: stage=%v err=%q\n%s", app.picker.stage, app.picker.keyErr, app.View())
	}
	data, err := os.ReadFile(app.rt.Cfg.Path())
	if err != nil || !strings.Contains(string(data), `"api_key": "good-key"`) {
		t.Fatalf("key not saved: %s err=%v", data, err)
	}
	if info, _ := os.Stat(app.rt.Cfg.Path()); info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v", info.Mode().Perm())
	}

	press(app, tea.KeyEnter)
	if p, m := app.rt.Current(); p != "acme" || m != "m1" || app.view != viewChat {
		t.Fatalf("current = %s:%s view=%v", p, m, app.view)
	}
}

func TestPickerSendsRejectedSavedKeyToKeyScreen(t *testing.T) {
	srv := keyServer(t, "good-key")
	app := newKeyApp(t, config.ProviderConfig{Type: config.TypeOpenAI, BaseURL: srv.URL, APIKey: "stale-key"})
	press(app, tea.KeyEnter)
	if app.picker.stage != stageKey || !strings.Contains(app.picker.keyErr, "rejected the current key") {
		t.Fatalf("stage=%v err=%q", app.picker.stage, app.picker.keyErr)
	}
	if !strings.Contains(app.View(), "Replace the API key for acme") {
		t.Fatalf("view:\n%s", app.View())
	}
}

func saveTestSession(t *testing.T, cwd, title string, msgs ...string) *agent.Session {
	t.Helper()
	s := agent.NewSession(cwd)
	s.ID += "-" + strings.ReplaceAll(title, " ", "")
	s.Title = title
	for _, m := range msgs {
		s.Messages = append(s.Messages, llm.Message{Role: llm.RoleUser, Content: m}, llm.Message{Role: llm.RoleAssistant, Content: "re: " + m})
	}
	if err := s.Save(config.SessionsDir()); err != nil {
		t.Fatal(err)
	}
	return s
}

func selectSession(t *testing.T, app *App, id string) {
	t.Helper()
	for range 50 {
		app.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	for range 50 {
		if s := app.home.items(app)[app.home.cursor].session; s != nil && s.ID == id {
			return
		}
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	t.Fatalf("session %s not in the home view:\n%s", id, app.View())
}

func TestHomeViewOpensRenamesAndDeletesSessions(t *testing.T) {
	app := newTestApp(t)
	older := saveTestSession(t, app.rt.Cwd, "older work", "first question")
	other := saveTestSession(t, app.rt.Cwd, "scratch", "x")
	saveTestSession(t, t.TempDir(), "other directory", "y")

	typeText(app, "draft")
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if app.view != viewChat {
		t.Fatal("left must move the cursor when the input has text")
	}
	app.input.Reset()

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	view := app.View()
	if app.view != viewHome || !strings.Contains(view, "(new session)") || !strings.Contains(view, "older work") || !strings.Contains(view, "scratch") {
		t.Fatalf("home view:\n%s", view)
	}
	if strings.Contains(view, "other directory") {
		t.Fatal("sessions of another directory must not show")
	}

	selectSession(t, app, older.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.view != viewChat || app.session.ID != older.ID || len(app.history) != 2 {
		t.Fatalf("view=%v session=%s history=%d", app.view, app.session.ID, len(app.history))
	}

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if cur := app.home.items(app)[app.home.cursor].session; cur.ID != older.ID {
		t.Fatalf("cursor must start on the current session, got %s", cur.ID)
	}
	updated := older.Updated
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for range "older work" {
		app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeText(app, "auth refactor")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	reloaded, err := agent.LoadSession(config.SessionsDir(), older.ID)
	if err != nil || reloaded.Title != "auth refactor" || !reloaded.Updated.Equal(updated) {
		t.Fatalf("rename: %+v err=%v", reloaded, err)
	}
	if !strings.Contains(app.View(), "auth refactor") {
		t.Fatalf("new name not shown:\n%s", app.View())
	}

	selectSession(t, app, other.ID)
	typeText(app, "d")
	typeText(app, "y")
	if _, err := agent.LoadSession(config.SessionsDir(), other.ID); err == nil {
		t.Fatal("session not deleted")
	}
	if strings.Contains(app.View(), "scratch") {
		t.Fatal("deleted session still listed")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewChat || app.session.ID != older.ID {
		t.Fatal("esc must return to the current session")
	}
}

func TestHomeViewShowsAgentLog(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", echoProvider{})
	if _, err := app.rt.Spawn(context.Background(), tools.SpawnRequest{AgentType: "Explore", Description: "look around", Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !strings.Contains(app.View(), "look around") {
		t.Fatalf("agent not listed:\n%s", app.View())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if app.home.task == nil || !strings.Contains(app.View(), "echo: hello") {
		t.Fatalf("agent log not shown:\n%s", app.View())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if app.home.task != nil || app.view != viewHome {
		t.Fatal("left must return from the agent log to the list")
	}
}

func TestModeCycleKeepsTwoStatusLinesAndPersists(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers = map[string]config.ProviderConfig{"fake": {Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", Local: true}}
	rt := agent.NewRuntime(context.Background(), cfg, t.TempDir(), agent.NewPermissions(agent.ModeDefault, nil))
	rt.SetCurrent("fake", "a-rather-long-model-name-for-the-status-bar")
	app := newApp(rt, agent.NewSession(rt.Cwd), "", true)

	for _, width := range []int{60, 80, 120} {
		app.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		for range agent.Modes {
			lines := strings.Split(app.statusLine(), "\n")
			if len(lines) != 2 {
				t.Fatalf("mode %s width %d: %d status lines", rt.Perms.Mode(), width, len(lines))
			}
			for _, l := range lines {
				if width <= lipgloss.Width(l) {
					t.Fatalf("mode %s: status line is %d wide on a %d terminal: %q", rt.Perms.Mode(), lipgloss.Width(l), width, l)
				}
			}
			if !strings.Contains(lines[1], rt.Perms.Mode().Label()) {
				t.Fatalf("mode label missing on line 2: %q", lines[1])
			}
			app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		}
	}

	for rt.Perms.Mode() != agent.ModeAuto {
		app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	}
	reloaded, _ := config.Load()
	if reloaded.PermissionMode != "auto" {
		t.Fatalf("saved mode = %q", reloaded.PermissionMode)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if rt.Perms.Mode() != agent.ModeBypass {
		t.Fatalf("mode = %s", rt.Perms.Mode())
	}
	reloaded, _ = config.Load()
	if reloaded.PermissionMode != "auto" {
		t.Fatalf("bypass must not be saved, got %q", reloaded.PermissionMode)
	}
}

func TestStatusLineShowsModelTokensAndContext(t *testing.T) {
	app := newTestApp(t)
	app.rt.SetCurrent("fake", "gpt-5")
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.Update(usageMsg{InputTokens: 40000, OutputTokens: 12000})
	app.rt.Perms.SetMode(agent.ModeAuto)
	lines := strings.Split(app.statusLine(), "\n")
	if !strings.Contains(lines[0], "gpt-5 (fake) | 52.0k tokens | 87% ctx remaining") {
		t.Fatalf("line 1 = %q", lines[0])
	}
	if !strings.Contains(lines[1], "⏵⏵ auto mode on (shift+tab to cycle)") {
		t.Fatalf("line 2 = %q", lines[1])
	}

	app.rt.SetCurrent("fake", "my-local-model")
	if strings.Contains(app.statusLine(), "ctx remaining") {
		t.Fatal("an unknown context size must hide the percentage")
	}
	app.rt.RememberModels("fake", []llm.Model{{ID: "my-local-model", ContextWindow: 104000}})
	if !strings.Contains(app.statusLine(), "50% ctx remaining") {
		t.Fatalf("listed context size not used: %q", app.statusLine())
	}
	app.rt.Cfg.Providers["fake"] = config.ProviderConfig{Type: config.TypeOpenAI, Local: true, ContextWindow: 208000}
	if !strings.Contains(app.statusLine(), "75% ctx remaining") {
		t.Fatalf("config context_window not used: %q", app.statusLine())
	}

	app.Update(tea.WindowSizeMsg{Width: 50, Height: 40})
	app.notice = "Press Ctrl+C again to exit"
	second := strings.Split(app.statusLine(), "\n")[1]
	if !strings.Contains(second, "Press Ctrl+C again to exit") || !strings.Contains(second, "auto mode on") {
		t.Fatalf("notice or mode lost on a narrow terminal: %q", second)
	}
}

func TestInputBoxShowsSessionNameOnTopBorder(t *testing.T) {
	for _, width := range []int{30, 80, 120} {
		box := titledBox("> hi", "z - golang mini agent", width)
		lines := strings.Split(box, "\n")
		top := lines[0]
		for _, l := range lines {
			if lipgloss.Width(l) != lipgloss.Width(top) {
				t.Fatalf("width %d: uneven box lines:\n%s", width, box)
			}
		}
		if !strings.HasPrefix(top, "╭") || !strings.HasSuffix(top, " ─╮") || len(lines) != 3 {
			t.Fatalf("width %d: top border = %q", width, top)
		}
		if 40 < width && !strings.HasSuffix(top, "── z - golang mini agent ─╮") {
			t.Fatalf("width %d: name not right-aligned: %q", width, top)
		}
	}
	if strings.Contains(titledBox("> hi", "", 80), " ─╮") {
		t.Fatal("an unnamed session must keep the plain border")
	}
	long := titledBox("> hi", strings.Repeat("very long name ", 20), 60)
	if lipgloss.Width(strings.Split(long, "\n")[0]) != 60+2 || !strings.Contains(long, "…") {
		t.Fatalf("long name not shortened:\n%s", long)
	}

	app := newTestApp(t)
	app.session.Title = "z - golang mini agent"
	if !strings.Contains(app.View(), "z - golang mini agent ─╮") {
		t.Fatalf("session name not on the input box:\n%s", app.View())
	}
}
