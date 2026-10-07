package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type fakeMCP struct{}

func (fakeMCP) Tools() []tools.Tool  { return []tools.Tool{fakeMCPTool{}} }
func (fakeMCP) Instructions() string { return "## docs\nAsk docs before you guess." }

type fakeMCPTool struct{}

func (fakeMCPTool) Name() string                   { return "mcp__docs__search" }
func (fakeMCPTool) Description() string            { return "[MCP server docs] Search docs" }
func (fakeMCPTool) ReadOnly() bool                 { return false }
func (fakeMCPTool) Summary(json.RawMessage) string { return "" }
func (fakeMCPTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (fakeMCPTool) Run(context.Context, *tools.Env, json.RawMessage) (string, error) {
	return "found 3 pages", nil
}

func TestMCPToolsReachAgents(t *testing.T) {
	p := &scripted{seen: map[string][]llm.Request{}, turns: map[string][]llm.Message{
		"main": {
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call("m1", "mcp__docs__search", map[string]any{})}},
			{Role: llm.RoleAssistant, Content: "done"},
		},
		"sub": {{Role: llm.RoleAssistant, Content: "sub done"}},
	}}
	rt := newTestRuntime(t, p, ModeDefault)
	rt.MCP = fakeMCP{}
	rt.Perms = NewPermissions(ModeDefault, []string{"mcp__docs"})
	ag, err := rt.MainAgent(&tools.Env{Cwd: rt.Cwd})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ag.System, "# MCP server instructions") || !strings.Contains(ag.System, "Ask docs before you guess") {
		t.Fatal("server instructions missing from the system prompt")
	}
	msgs, err := ag.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "look it up"}}, nopObserver{})
	if err != nil || msgs[2].Content != "found 3 pages" {
		t.Fatalf("MCP tool result = %+v, %v", msgs, err)
	}
	if !slices.ContainsFunc(p.seen["main"][0].Tools, func(s llm.ToolSpec) bool { return s.Name == "mcp__docs__search" }) {
		t.Fatal("the model did not see the MCP tool")
	}

	rt.Defs = &agentdef.Store{UserDir: t.TempDir(), ProjectDir: t.TempDir()}
	def := agentdef.Definition{Name: "doc-reader", Description: "d", Prompt: "p", Tools: []string{"Read", "mcp__docs__search"}, Scope: agentdef.ScopeUser}
	if _, err := rt.Defs.Save(def, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Spawn(context.Background(), tools.SpawnRequest{AgentType: "doc-reader", Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range p.seen["sub"][0].Tools {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "Read,mcp__docs__search" {
		t.Fatalf("sub-agent tools = %v", names)
	}

	if !RuleMatches("mcp__docs", "mcp__docs__search", "") || RuleMatches("mcp__doc", "mcp__docs__search", "") || !RuleMatches("mcp__docs__search", "mcp__docs__search", "") {
		t.Fatal("server-wide MCP rule matching is wrong")
	}
}
