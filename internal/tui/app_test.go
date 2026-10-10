package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	return &llm.Response{
		Message: llm.Message{Role: llm.RoleAssistant, Content: "echo: " + last.Content},
		Usage:   llm.Usage{InputTokens: 1000, OutputTokens: 200},
	}, nil
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
	app.send = func(msg tea.Msg) { app.Update(msg) }

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
	saved, err := agent.LoadSession(config.SessionsDir(), app.session.ID)
	if err != nil {
		t.Fatalf("session not saved: %v", err)
	}
	if saved.Usage.InputTokens != 1000 || saved.Usage.OutputTokens != 200 || saved.ContextTokens != 1200 {
		t.Fatalf("usage not saved: %+v ctx=%d", saved.Usage, saved.ContextTokens)
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
	if app.view != viewHome || !strings.Contains(view, "older work") || !strings.Contains(view, "scratch") {
		t.Fatalf("home view:\n%s", view)
	}
	if strings.Contains(view, "(new session)") || strings.Contains(view, "current") || strings.Contains(ansi.Strip(view), "● ") {
		t.Fatalf("an unused placeholder must stay hidden when sessions exist:\n%s", view)
	}
	if first := app.home.items(app)[app.home.cursor]; app.home.cursor != 0 || first.session.ID != other.ID {
		t.Fatal("the cursor must start on the first session")
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
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
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
	app.Update(usageMsg{run: app.sessionRun, usage: llm.Usage{InputTokens: 40000, OutputTokens: 12000}})
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

func TestWrappedRowsMatchesTextarea(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	pieces := []string{"a", "go", "mga", "agent", "supercalifragilistic", "修复", "构建错误", " ", "  ", "x"}
	for i := range 2000 {
		var b strings.Builder
		for range rng.IntN(30) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
			if rng.IntN(3) == 0 {
				b.WriteString(" ")
			}
		}
		line := b.String()
		width := 5 + rng.IntN(60)
		ta := textarea.New()
		ta.ShowLineNumbers = false
		ta.CharLimit = 0
		ta.MaxHeight = 0
		ta.SetPromptFunc(2, func(int) string { return "> " })
		ta.SetWidth(width + 2)
		ta.SetHeight(100)
		ta.SetValue(line)
		if got, want := wrappedRows([]rune(line), ta.Width()), ta.LineInfo().Height; got != want {
			t.Fatalf("case %d width %d %q: wrappedRows = %d, textarea = %d", i, ta.Width(), line, got, want)
		}
	}
}

func TestInputBoxGrowsWithWrappedText(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 40, Height: 40})
	if app.input.Height() != 1 {
		t.Fatalf("empty input height = %d", app.input.Height())
	}
	text := "first please read the whole project and then explain how the agent loop works end"
	typeText(app, text)
	if app.input.Height() < 3 {
		t.Fatalf("height = %d for %d chars at width %d", app.input.Height(), len(text), app.input.Width())
	}
	view := app.View()
	if !strings.Contains(view, "> first please") || !strings.Contains(view, "works end") {
		t.Fatalf("wrapped input not fully visible:\n%s", view)
	}

	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if app.input.Height() != 1 {
		t.Fatalf("height after widening = %d", app.input.Height())
	}
	for range text {
		app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if app.input.Height() != 1 || app.input.Value() != "" {
		t.Fatalf("height after delete = %d", app.input.Height())
	}
	typeText(app, strings.Repeat("word ", 400))
	if app.input.Height() != maxInputHeight {
		t.Fatalf("height must stop at %d, got %d", maxInputHeight, app.input.Height())
	}
}

func TestOpeningASessionRestoresTokensAndContext(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	old := saveTestSession(t, app.rt.Cwd, "earlier work", "q")
	old.Provider, old.Model = "fake", "gpt-5"
	old.Usage = llm.Usage{InputTokens: 40000, OutputTokens: 12000}
	old.ContextTokens = 52000
	if err := old.Save(config.SessionsDir()); err != nil {
		t.Fatal(err)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	selectSession(t, app, old.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	if line := strings.Split(app.statusLine(), "\n")[0]; !strings.Contains(line, "gpt-5 (fake) | 52.0k tokens | 87% ctx remaining") {
		t.Fatalf("usage not restored: %q", line)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "/new")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if line := strings.Split(app.statusLine(), "\n")[0]; !strings.Contains(line, "| 0 tokens | 100% ctx remaining") {
		t.Fatalf("a new session must start at zero: %q", line)
	}

	resumed, err := agent.LoadSession(config.SessionsDir(), old.ID)
	if err != nil {
		t.Fatal(err)
	}
	started := newApp(app.rt, resumed, "", true)
	started.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if line := strings.Split(started.statusLine(), "\n")[0]; !strings.Contains(line, "52.0k tokens | 87% ctx remaining") {
		t.Fatalf("usage not restored at startup: %q", line)
	}
}

func TestNewSessionClearsAndOpenedSessionReplays(t *testing.T) {
	app := newTestApp(t)
	app.printing = false
	app.printQueue = nil
	app.emit("old output that must not survive")
	app.newSession()
	if !app.clearScreen || len(app.printQueue) != 1 || !strings.Contains(app.printQueue[0], "mga · mini Go agent") {
		t.Fatalf("new session: clear=%v queue=%q", app.clearScreen, app.printQueue)
	}
	if app.flush() == nil || app.clearScreen {
		t.Fatal("flush must send the clear once")
	}

	s := agent.NewSession(app.rt.Cwd)
	s.Title = "tool work"
	s.Messages = []llm.Message{
		{Role: llm.RoleUser, Content: "list files"},
		{Role: llm.RoleAssistant, Content: "Running it.", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "Bash", Arguments: `{"command":"echo replayed-output"}`}}},
		{Role: llm.RoleTool, ToolCallID: "c1", ToolName: "Bash", Content: "replayed-output"},
		{Role: llm.RoleUser, Content: "<task-notification>\nagent done\n</task-notification>"},
		{Role: llm.RoleAssistant, Content: "All done."},
	}
	app.printing = false
	app.loadSession(s)
	out := ansi.Strip(strings.Join(app.printQueue, "\n"))
	if !app.clearScreen || !strings.HasPrefix(app.printQueue[0], app.banner()) {
		t.Fatal("opening a session must clear the screen and show the banner first")
	}
	for _, want := range []string{"Opened session", "> list files", "Running it.", "Bash", "echo replayed-output", "⎿  replayed-output", "Background agent results", "All done."} {
		if !strings.Contains(out, want) {
			t.Fatalf("replay misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<task-notification>") {
		t.Fatal("notifications must not replay as raw user text")
	}
	for _, l := range strings.Split(strings.Join(app.printQueue, "\n"), "\n") {
		plain := ansi.Strip(l)
		if strings.HasSuffix(plain, " ") && (!strings.HasPrefix(plain, "> ") || strings.HasSuffix(plain, "  ")) {
			t.Fatalf("rendered line keeps trailing padding: %q", l)
		}
	}
}

func TestReplayWaitsForWidthAndLimitsLength(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	cfg, _ := config.Load()
	rt := agent.NewRuntime(context.Background(), cfg, t.TempDir(), agent.NewPermissions(agent.ModeDefault, nil))
	rt.SetCurrent("ollama", "m")
	s := agent.NewSession(rt.Cwd)
	for i := range 250 {
		s.Messages = append(s.Messages, llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("message %d", i)})
	}
	app := newApp(rt, s, "", true)
	app.Init()
	if len(app.printQueue) != 1 {
		t.Fatalf("replay must wait for the width, queue=%d", len(app.printQueue))
	}
	app.printing = true
	app.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	lines := map[string]bool{}
	for _, l := range strings.Split(ansi.Strip(strings.Join(app.printQueue, "\n")), "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	if !lines["… 50 earlier messages"] || lines["> message 49"] || !lines["> message 50"] || !lines["> message 249"] {
		t.Fatal("the replay must show only the last 200 messages after a note")
	}
}

type titleProvider struct {
	title string
	fail  bool
}

func (titleProvider) Name() string                                    { return "fake" }
func (titleProvider) ListModels(context.Context) ([]llm.Model, error) { return nil, nil }

func (p titleProvider) Chat(_ context.Context, req llm.Request, _ func(llm.Delta)) (*llm.Response, error) {
	if strings.Contains(req.System, "name chat sessions") {
		if p.fail {
			return nil, errors.New("title service down")
		}
		return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: p.title}}, nil
	}
	return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}, nil
}

func TestFirstMessageGetsModelTitle(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", titleProvider{title: "Explain the agent loop design."})
	request := "please read the whole project and then explain to me how the agent loop works"
	typeText(app, request)
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drain(app, cmd)
	if app.session.Title != "Explain the agent loop design" || app.session.TitleSource != agent.TitleFromModel {
		t.Fatalf("title = %q (%s)", app.session.Title, app.session.TitleSource)
	}
	saved, err := agent.LoadSession(config.SessionsDir(), app.session.ID)
	if err != nil || saved.Title != "Explain the agent loop design" {
		t.Fatalf("saved title = %+v, %v", saved, err)
	}
	if !strings.Contains(app.View(), "Explain the agent loop design ─╮") {
		t.Fatalf("title not on the input box:\n%s", app.View())
	}

	if app.needsTitle("a second request") {
		t.Fatal("only the first request asks for a title")
	}

	app.session.TitleSource = agent.TitleFromUser
	app.session.Title = "my own name"
	app.applyTitle(titleMsg{sessionID: app.session.ID, title: "Late model title"})
	if app.session.Title != "my own name" {
		t.Fatal("a model title must not replace a user name")
	}

	other := saveTestSession(t, app.rt.Cwd, "", "first words of another session")
	other.TitleSource = agent.TitleFromText
	app.applyTitle(titleMsg{sessionID: other.ID, title: "Summarized other session"})
	if reloaded, _ := agent.LoadSession(config.SessionsDir(), other.ID); reloaded.Title != "Summarized other session" {
		t.Fatalf("title of a session the user left = %q", reloaded.Title)
	}
}

func TestTitleFailureKeepsRequestText(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", titleProvider{fail: true})
	typeText(app, "fix the failing build")
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drain(app, cmd)
	if app.session.Title != "fix the failing build" || app.session.TitleSource != agent.TitleFromText {
		t.Fatalf("title = %q (%s)", app.session.Title, app.session.TitleSource)
	}
}
