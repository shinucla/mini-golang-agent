package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type scripted struct {
	mu    sync.Mutex
	turns map[string][]llm.Message
	seen  map[string][]llm.Request
}

func (s *scripted) Name() string                                    { return "fake" }
func (s *scripted) ListModels(context.Context) ([]llm.Model, error) { return nil, nil }

func (s *scripted) Chat(ctx context.Context, req llm.Request, _ func(llm.Delta)) (*llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := "main"
	if strings.Contains(req.System, "You run as a sub-agent") {
		key = "sub"
	}
	s.seen[key] = append(s.seen[key], req)
	queue := s.turns[key]
	if len(queue) == 0 {
		return nil, errors.New("script exhausted for " + key)
	}
	s.turns[key] = queue[1:]
	return &llm.Response{Message: queue[0], Usage: llm.Usage{InputTokens: 1, OutputTokens: 1}}, nil
}

type nopObserver struct{}

func (nopObserver) Delta(llm.Delta)                       {}
func (nopObserver) AssistantMessage(llm.Message)          {}
func (nopObserver) ToolStart(llm.ToolCall, tools.Tool)    {}
func (nopObserver) ToolResult(llm.ToolCall, string, bool) {}
func (nopObserver) Usage(llm.Usage)                       {}

func call(id, name string, args any) llm.ToolCall {
	data, _ := json.Marshal(args)
	return llm.ToolCall{ID: id, Name: name, Arguments: string(data)}
}

func newTestRuntime(t *testing.T, p llm.Provider, mode Mode) *Runtime {
	t.Setenv("MGA_HOME", t.TempDir())
	cwd := t.TempDir()
	rt := NewRuntime(context.Background(), &config.Config{}, cwd, NewPermissions(mode, nil))
	rt.Defs = &agentdef.Store{UserDir: t.TempDir(), ProjectDir: t.TempDir()}
	rt.providers["fake"] = p
	rt.SetCurrent("fake", "m")
	return rt
}

func TestMainAgentRunsToolsAndSubAgent(t *testing.T) {
	p := &scripted{seen: map[string][]llm.Request{}, turns: map[string][]llm.Message{
		"main": {
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				call("t1", "Task", map[string]any{"description": "look", "prompt": "find things", "subagent_type": "Explore"}),
				call("t2", "Glob", map[string]any{"pattern": "*"}),
			}},
			{Role: llm.RoleAssistant, Content: "all done"},
		},
		"sub": {
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call("s1", "Grep", map[string]any{"pattern": "x"})}},
			{Role: llm.RoleAssistant, Content: "found it"},
		},
	}}
	rt := newTestRuntime(t, p, ModeDefault)
	ag, err := rt.MainAgent(&tools.Env{Cwd: rt.Cwd})
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := ag.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "go"}}, nopObserver{})
	if err != nil {
		t.Fatal(err)
	}
	if LastAssistantText(msgs) != "all done" || len(msgs) != 5 {
		t.Fatalf("messages = %+v", msgs)
	}
	if msgs[2].ToolCallID != "t1" || msgs[2].Content != "found it" || msgs[3].ToolCallID != "t2" {
		t.Fatalf("tool results out of order: %+v", msgs[2:4])
	}
	subTools := p.seen["sub"][0].Tools
	if len(subTools) != 3 {
		t.Fatalf("Explore must get only Read, Glob, Grep: %+v", subTools)
	}
	tasks := rt.Tasks.Snapshot()
	if len(tasks) != 1 || tasks[0].Status != TaskCompleted || tasks[0].ToolUses != 1 || tasks[0].Agent != "Explore" {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestDeniedToolStopsMainTurnWithValidHistory(t *testing.T) {
	p := &scripted{seen: map[string][]llm.Request{}, turns: map[string][]llm.Message{
		"main": {{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			call("b1", "Bash", map[string]any{"command": "rm -rf /tmp/x"}),
			call("b2", "Bash", map[string]any{"command": "ls"}),
		}}},
	}}
	rt := newTestRuntime(t, p, ModeDefault)
	var asked []string
	rt.Approve = func(_ context.Context, req ApprovalRequest) Decision {
		asked = append(asked, req.Summary)
		return Deny
	}
	ag, _ := rt.MainAgent(&tools.Env{Cwd: rt.Cwd})
	msgs, err := ag.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "go"}}, nopObserver{})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if len(asked) != 1 || len(msgs) != 4 || msgs[3].ToolCallID != "b2" || !msgs[3].IsError {
		t.Fatalf("asked=%v msgs=%+v", asked, msgs)
	}
}

func TestBackgroundSubAgentNotifies(t *testing.T) {
	p := &scripted{seen: map[string][]llm.Request{}, turns: map[string][]llm.Message{
		"sub": {{Role: llm.RoleAssistant, Content: "bg result"}},
	}}
	rt := newTestRuntime(t, p, ModeDefault)
	done := make(chan Task, 1)
	rt.Tasks.OnFinish = func(t Task) { done <- t }
	out, err := rt.Spawn(context.Background(), tools.SpawnRequest{Prompt: "work", Background: true})
	if err != nil || !strings.Contains(out, "Started background agent general-purpose") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	select {
	case task := <-done:
		if task.Result != "bg result" || !task.NotifyMain || task.Status != TaskCompleted {
			t.Fatalf("task = %+v", task)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background agent did not finish")
	}
}

func TestPermissionRules(t *testing.T) {
	cases := []struct {
		rule, tool, subject string
		want                bool
	}{
		{"Bash(go test:*)", "Bash", "go test ./...", true},
		{"Bash(go test:*)", "Bash", "go test ./... && rm -rf /", false},
		{"Bash(go:*)", "Bash", "gofmt -w .", false},
		{"Bash(make)", "Bash", "make", true},
		{"Edit", "Edit", "", true},
		{"WebFetch(domain:go.dev)", "WebFetch", "domain:go.dev", true},
		{"WebFetch(domain:go.dev)", "WebFetch", "domain:evil.dev", false},
	}
	for _, c := range cases {
		if got := RuleMatches(c.rule, c.tool, c.subject); got != c.want {
			t.Errorf("RuleMatches(%q, %q, %q) = %v", c.rule, c.tool, c.subject, got)
		}
	}

	perms := NewPermissions(ModePlan, nil)
	if v, _ := perms.check(tools.Bash{}, json.RawMessage(`{"command":"ls"}`)); v != verdictBlock {
		t.Fatal("plan mode must block Bash")
	}
	perms.SetMode(ModeAcceptEdits)
	if v, _ := perms.check(tools.Edit{}, json.RawMessage(`{}`)); v != verdictAllow {
		t.Fatal("acceptEdits must allow Edit")
	}
	perms.SetMode(ModeDefault)
	perms.remember(tools.Bash{}, json.RawMessage(`{"command":"go build ./..."}`))
	if v, _ := perms.check(tools.Bash{}, json.RawMessage(`{"command":"go vet ./..."}`)); v != verdictAllow {
		t.Fatal("remembered Bash prefix must allow")
	}
}

func TestParseReview(t *testing.T) {
	cases := []struct {
		text string
		want ReviewDecision
		ok   bool
	}{
		{`{"decision":"allow","reason":"runs tests"}`, ReviewAllow, true},
		{"```json\n{\"decision\": \"BLOCK\", \"reason\": \"wipes home\"}\n```", ReviewBlock, true},
		{`I think {"decision":"ask","reason":"pushes"} is right`, ReviewAsk, true},
		{`{"decision":"maybe"}`, ReviewAsk, false},
		{`looks fine`, ReviewAsk, false},
	}
	for _, c := range cases {
		got, _, err := ParseReview(c.text)
		if got != c.want || (err == nil) != c.ok {
			t.Errorf("ParseReview(%q) = %v, %v", c.text, got, err)
		}
	}
}

func TestAutoModeCheck(t *testing.T) {
	perms := NewPermissions(ModeAuto, []string{"Bash(docker ps:*)"})
	cases := []struct {
		tool tools.Tool
		in   string
		want verdict
	}{
		{tools.Read{}, `{"file_path":"x"}`, verdictAllow},
		{tools.Bash{}, `{"command":"go test ./..."}`, verdictAllow},
		{tools.Bash{}, `{"command":"docker ps -a"}`, verdictAllow},
		{tools.Bash{}, `{"command":"go test ./... && rm -rf /"}`, verdictReview},
		{tools.Bash{}, `{"command":"rm -rf build"}`, verdictReview},
		{tools.Edit{}, `{"file_path":"a.go"}`, verdictReview},
		{tools.WebFetch{}, `{"url":"https://go.dev"}`, verdictReview},
	}
	for _, c := range cases {
		if got, _ := perms.check(c.tool, json.RawMessage(c.in)); got != c.want {
			t.Errorf("check(%s %s) = %v, want %v", c.tool.Name(), c.in, got, c.want)
		}
	}
	if ModePlan.Next() != ModeAuto || ModeAuto.Next() != ModeBypass {
		t.Fatal("cycle must be plan → auto → bypass")
	}
	perms.remember(tools.Edit{}, json.RawMessage(`{}`))
	if perms.Mode() != ModeAuto {
		t.Fatal("allowing edits must not leave auto mode")
	}
}

func TestAutoModeReviewsRunsAsksAndBlocks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	p := &scripted{seen: map[string][]llm.Request{}, turns: map[string][]llm.Message{
		"main": {
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				call("e1", "Edit", map[string]any{"file_path": "new.txt", "old_string": "", "new_string": "hi"}),
				call("w1", "Write", map[string]any{"file_path": outside, "content": "x"}),
				call("b1", "Bash", map[string]any{"command": "echo reviewed-ok"}),
				call("b2", "Bash", map[string]any{"command": "rm -rf ~"}),
			}},
			{Role: llm.RoleAssistant, Content: "done"},
		},
	}}
	rt := newTestRuntime(t, p, ModeAuto)
	var asked []ApprovalRequest
	rt.Approve = func(_ context.Context, req ApprovalRequest) Decision {
		asked = append(asked, req)
		return Allow
	}
	ag, _ := rt.MainAgent(&tools.Env{Cwd: rt.Cwd})
	var reviewed []string
	ag.Review = func(_ context.Context, req ReviewRequest) (ReviewDecision, string, error) {
		reviewed = append(reviewed, req.Tool)
		if req.UserRequest != "clean up" {
			t.Errorf("user request = %q", req.UserRequest)
		}
		switch {
		case strings.Contains(string(req.Input), "rm -rf"):
			return ReviewBlock, "deletes the home directory.", nil
		case req.Tool == "Write":
			return ReviewAsk, "writes outside the project.", nil
		}
		return ReviewAllow, "harmless", nil
	}
	msgs, err := ag.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "clean up"}}, nopObserver{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(reviewed, ",") != "Write,Bash,Bash" {
		t.Fatalf("reviewed = %v (an edit inside the project must skip review)", reviewed)
	}
	if len(asked) != 1 || asked[0].Tool != "Write" || !strings.Contains(asked[0].Reason, "writes outside the project") {
		t.Fatalf("asked = %+v", asked)
	}
	results := map[string]llm.Message{}
	for _, m := range msgs {
		if m.Role == llm.RoleTool {
			results[m.ToolCallID] = m
		}
	}
	if results["e1"].IsError || results["w1"].IsError || results["b1"].Content != "reviewed-ok" {
		t.Fatalf("results = %+v", results)
	}
	if !results["b2"].IsError || !strings.Contains(results["b2"].Content, "Auto mode blocked this call: deletes the home directory.") {
		t.Fatalf("blocked result = %+v", results["b2"])
	}
}

func TestRuntimeReviewAsksTheModel(t *testing.T) {
	p := &scripted{seen: map[string][]llm.Request{}, turns: map[string][]llm.Message{
		"main": {
			{Role: llm.RoleAssistant, Content: `{"decision":"allow","reason":"a build"}`},
			{Role: llm.RoleAssistant, Content: "sure, go ahead"},
		},
	}}
	rt := newTestRuntime(t, p, ModeAuto)
	req := ReviewRequest{Tool: "Bash", Input: json.RawMessage(`{"command":"make release"}`), Cwd: rt.Cwd, UserRequest: "ship it"}
	decision, reason, err := rt.Review(context.Background(), req)
	if err != nil || decision != ReviewAllow || reason != "a build" {
		t.Fatalf("decision=%v reason=%q err=%v", decision, reason, err)
	}
	sent := p.seen["main"][0]
	if !strings.Contains(sent.System, "auto mode") || !strings.Contains(sent.Messages[0].Content, "make release") || len(sent.Tools) != 0 {
		t.Fatalf("review request = %+v", sent)
	}
	if decision, _, err := rt.Review(context.Background(), req); err == nil || decision != ReviewAsk {
		t.Fatalf("an unclear answer must fall back to ask: %v %v", decision, err)
	}
}
