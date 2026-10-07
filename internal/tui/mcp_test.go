package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/mcp"
)

func fakeMCPServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&msg)
		w.Header().Set("Content-Type", "application/json")
		switch msg.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"docs-server","version":"2.1"}}}`, msg.ID)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"search","description":"Search the docs","annotations":{"readOnlyHint":true}}]}}`, msg.ID)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMCPViewAndNotes(t *testing.T) {
	app := newTestApp(t)
	good := fakeMCPServer(t)
	if err := mcp.WriteServer(mcp.UserConfigPath(), "docs", &mcp.ServerConfig{Type: "http", URL: good.URL}); err != nil {
		t.Fatal(err)
	}
	if err := mcp.WriteServer(mcp.UserConfigPath(), "broken", &mcp.ServerConfig{Type: "http", URL: "http://127.0.0.1:1/mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := mcp.WriteServer(mcp.ProjectConfigPath(app.rt.Cwd), "repo-tool", &mcp.ServerConfig{Command: "echo"}); err != nil {
		t.Fatal(err)
	}
	servers := mcp.Load(app.rt.Cwd, nil)
	defer servers.Close()
	app.mcp = servers
	app.printing = true
	app.reportMCP()
	servers.ConnectAll(context.Background())
	app.Update(mcpChangedMsg{})

	notes := ansi.Strip(strings.Join(app.printQueue, "\n"))
	if !strings.Contains(notes, "MCP server broken failed") || !strings.Contains(notes, "MCP server repo-tool from .mcp.json needs your approval") {
		t.Fatalf("notes = %q", notes)
	}
	before := len(app.printQueue)
	app.Update(mcpChangedMsg{})
	if len(app.printQueue) != before {
		t.Fatal("a status must be reported only once")
	}

	typeText(app, "/mcp")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ansi.Strip(app.View())
	if app.view != viewMCP || !strings.Contains(view, "connected · 1 tools") || !strings.Contains(view, "needs approval") || !strings.Contains(view, "✗ failed") {
		t.Fatalf("mcp view:\n%s", view)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !strings.Contains(ansi.Strip(app.View()), "❯ docs") {
		t.Fatalf("servers must be sorted by name, docs second:\n%s", ansi.Strip(app.View()))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail := ansi.Strip(app.View())
	if !strings.Contains(detail, "docs-server 2.1") || !strings.Contains(detail, "search read-only") {
		t.Fatalf("detail view:\n%s", detail)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.view != viewChat {
		t.Fatal("esc must close the MCP view")
	}
	if !strings.Contains(app.mcpStatusLine(), "1 of 3 servers connected · 1 tools") {
		t.Fatalf("status = %q", app.mcpStatusLine())
	}

	block := ansi.Strip(formatToolBlock("mcp__docs__search", "{}", "{}", "3 hits", false, 100))
	if !strings.Contains(block, "docs - search (MCP)") {
		t.Fatalf("tool block = %q", block)
	}
}
