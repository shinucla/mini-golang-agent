package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("MGA_FAKE_MCP") == "1" {
		runFakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeServer() {
	fmt.Fprintln(os.Stderr, "fake server starting")
	var outMu sync.Mutex
	write := func(v any) {
		data, _ := json.Marshal(v)
		outMu.Lock()
		os.Stdout.Write(append(data, '\n'))
		outMu.Unlock()
	}
	reply := func(id json.RawMessage, result any) {
		write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	text := func(s string, isError bool) map[string]any {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}, "isError": isError}
	}
	var mu sync.Mutex
	cancelled := map[string]chan struct{}{}
	pingReplies := make(chan struct{}, 1)
	readOnly := true

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		switch msg.Method {
		case "":
			if string(msg.ID) == `"s1"` {
				pingReplies <- struct{}{}
			}
		case "initialize":
			reply(msg.ID, map[string]any{
				"protocolVersion": ProtocolVersion,
				"serverInfo":      map[string]any{"name": "fake", "version": "1.0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"instructions":    "Use fake tools for fake work.",
			})
		case "tools/list":
			var p struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if p.Cursor == "" {
				reply(msg.ID, map[string]any{"nextCursor": "p2", "tools": []any{
					map[string]any{"name": "echo", "description": "Echo text", "annotations": map[string]any{"readOnlyHint": readOnly},
						"inputSchema": map[string]any{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object",
							"properties": map[string]any{"text": map[string]any{"type": "string"}}}},
					map[string]any{"name": "fail", "inputSchema": map[string]any{"type": "object"}},
				}})
			} else {
				reply(msg.ID, map[string]any{"tools": []any{
					map[string]any{"name": "env"}, map[string]any{"name": "slow"}, map[string]any{"name": "ping_me"}, map[string]any{"name": "crash"},
				}})
			}
		case "notifications/cancelled":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			mu.Lock()
			if ch, ok := cancelled[string(p.RequestID)]; ok {
				close(ch)
			}
			mu.Unlock()
		case "tools/call":
			var p struct {
				Name      string `json:"name"`
				Arguments struct {
					Text string `json:"text"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			switch p.Name {
			case "echo":
				reply(msg.ID, text("echo: "+p.Arguments.Text, false))
			case "fail":
				reply(msg.ID, text("bad input", true))
			case "env":
				reply(msg.ID, text(os.Getenv("FAKE_TOKEN"), false))
			case "slow":
				ch := make(chan struct{})
				mu.Lock()
				cancelled[string(msg.ID)] = ch
				mu.Unlock()
				go func(id json.RawMessage) {
					select {
					case <-ch:
						fmt.Fprintln(os.Stderr, "slow call cancelled")
					case <-time.After(10 * time.Second):
						reply(id, text("slow done", false))
					}
				}(msg.ID)
			case "ping_me":
				go func(id json.RawMessage) {
					write(map[string]any{"jsonrpc": "2.0", "id": "s1", "method": "ping"})
					<-pingReplies
					reply(id, text("pong received", false))
				}(msg.ID)
			case "crash":
				fmt.Fprintln(os.Stderr, "fatal: crash requested")
				os.Exit(3)
			}
		default:
			if len(msg.ID) != 0 {
				write(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "unknown"}})
			}
		}
	}
}

func waitFor(t *testing.T, m *Manager, name string, want Status) ServerInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range m.Snapshot() {
			if s.Name == name && s.Status == want {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server %s never reached %q: %+v", name, want, m.Snapshot())
	return ServerInfo{}
}

func findTool(t *testing.T, m *Manager, name string) *Tool {
	t.Helper()
	for _, tool := range m.Tools() {
		if tool.Name() == name {
			return tool.(*Tool)
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}

func TestStdioServerLifecycle(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	cwd := t.TempDir()
	cfg := &ServerConfig{Command: os.Args[0], Env: map[string]string{"MGA_FAKE_MCP": "1", "FAKE_TOKEN": "${MGA_TEST_UNSET_TOKEN:-default-token}"}}
	if err := WriteServer(ProjectConfigPath(cwd), "fake", cfg); err != nil {
		t.Fatal(err)
	}

	m := Load(cwd, nil)
	var approved []string
	m.PersistApproval = func(name string) error { approved = append(approved, name); return nil }
	defer m.Close()
	m.ConnectAll(context.Background())
	if s := m.Snapshot()[0]; s.Status != StatusNeedsApproval || s.Scope != ScopeProject {
		t.Fatalf("a project server must wait for approval: %+v", s)
	}
	if len(m.Tools()) != 0 {
		t.Fatal("an unapproved server must not expose tools")
	}

	if err := m.Approve(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	info := waitFor(t, m, "fake", StatusConnected)
	if len(approved) != 1 || info.ServerName != "fake" || len(info.Tools) != 6 || !strings.Contains(m.Instructions(), "Use fake tools") {
		t.Fatalf("connected info = %+v approved=%v", info, approved)
	}

	echo := findTool(t, m, "mcp__fake__echo")
	if !echo.ReadOnly() || findTool(t, m, "mcp__fake__fail").ReadOnly() {
		t.Fatal("readOnlyHint not honored")
	}
	if _, ok := echo.Schema()["$schema"]; ok {
		t.Fatal("$schema must be removed")
	}
	if props, ok := findTool(t, m, "mcp__fake__env").Schema()["properties"].(map[string]any); !ok || len(props) != 0 {
		t.Fatal("a missing schema must become an empty object schema")
	}

	ctx := context.Background()
	if out, err := echo.Run(ctx, nil, json.RawMessage(`{"text":"hi"}`)); err != nil || out != "echo: hi" {
		t.Fatalf("echo = %q, %v", out, err)
	}
	if out, err := findTool(t, m, "mcp__fake__fail").Run(ctx, nil, nil); err == nil || out != "bad input" {
		t.Fatalf("fail = %q, %v", out, err)
	}
	if out, _ := findTool(t, m, "mcp__fake__env").Run(ctx, nil, nil); out != "default-token" {
		t.Fatalf("env expansion = %q", out)
	}
	if out, err := findTool(t, m, "mcp__fake__ping_me").Run(ctx, nil, nil); err != nil || out != "pong received" {
		t.Fatalf("server ping = %q, %v", out, err)
	}

	cancelCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := findTool(t, m, "mcp__fake__slow").Run(cancelCtx, nil, nil); !errors.Is(err, context.DeadlineExceeded) || 3*time.Second < time.Since(start) {
		t.Fatalf("cancel = %v after %s", err, time.Since(start))
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(m.Snapshot()[0].Stderr, "slow call cancelled") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(m.Snapshot()[0].Stderr, "slow call cancelled") {
		t.Fatal("the server did not get notifications/cancelled")
	}

	if _, err := findTool(t, m, "mcp__fake__crash").Run(ctx, nil, nil); err == nil {
		t.Fatal("a crashed server must fail the call")
	}
	failed := waitFor(t, m, "fake", StatusFailed)
	if !strings.Contains(failed.Err, "crash requested") || len(m.Tools()) != 0 {
		t.Fatalf("crash info = %+v", failed)
	}
	if _, err := echo.Run(ctx, nil, nil); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("a tool of a failed server = %v", err)
	}

	if err := m.Reconnect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, m, "fake", StatusConnected)
}

func TestHTTPServer(t *testing.T) {
	var mu sync.Mutex
	var deleted bool
	var seenHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deleted = r.Header.Get("Mcp-Session-Id") == "sess-1"
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "missing token")
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&msg)
		mu.Lock()
		seenHeaders = append(seenHeaders, msg.Method+"|"+r.Header.Get("Mcp-Session-Id")+"|"+r.Header.Get("MCP-Protocol-Version"))
		mu.Unlock()
		switch msg.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-1")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-03-26","serverInfo":{"name":"web"}}}`, msg.ID)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\n", msg.ID)
			fmt.Fprint(w, "data: \"result\":{\"tools\":[{\"name\":\"search docs\",\"inputSchema\":{\"type\":\"object\"}}]}}\n\n")
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"content":[],"structuredContent":{"hits":2}}}`, msg.ID)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	t.Setenv("MGA_HOME", t.TempDir())
	t.Setenv("MGA_TEST_TOKEN", "secret")
	cfg := &ServerConfig{Type: "http", URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer ${MGA_TEST_TOKEN}"}}
	if err := WriteServer(UserConfigPath(), "web", cfg); err != nil {
		t.Fatal(err)
	}
	m := Load(t.TempDir(), nil)
	m.ConnectAll(context.Background())
	waitFor(t, m, "web", StatusConnected)
	tool := findTool(t, m, "mcp__web__search_docs")
	if out, err := tool.Run(context.Background(), nil, json.RawMessage(`{}`)); err != nil || out != `{"hits":2}` {
		t.Fatalf("call = %q, %v", out, err)
	}
	m.Close()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"initialize||", "notifications/initialized|sess-1|2025-03-26", "tools/list|sess-1|2025-03-26", "tools/call|sess-1|2025-03-26"}
	if strings.Join(seenHeaders, ",") != strings.Join(want, ",") || !deleted {
		t.Fatalf("headers = %v deleted=%v", seenHeaders, deleted)
	}

	t.Setenv("MGA_TEST_TOKEN", "wrong")
	bad := Load(t.TempDir(), nil)
	bad.ConnectAll(context.Background())
	if info := waitFor(t, bad, "web", StatusFailed); !strings.Contains(info.Err, "HTTP 401") {
		t.Fatalf("auth failure = %+v", info)
	}
}

func TestConfigFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	os.WriteFile(path, []byte(`{"other": true, "mcpServers": {"a": {"command": "x"}}}`), 0o644)
	if err := WriteServer(path, "b", &ServerConfig{Type: "http", URL: "https://example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	servers, err := ReadServers(path)
	if err != nil || len(servers) != 2 || servers["b"].Transport() != TransportHTTP || servers["a"].Transport() != TransportStdio {
		t.Fatalf("servers = %+v, %v", servers, err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"other": true`) {
		t.Fatal("other keys must survive a write")
	}
	if err := WriteServer(path, "a", nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteServer(path, "missing", nil); err == nil {
		t.Fatal("removing a missing server must fail")
	}
	if servers, _ := ReadServers(path); len(servers) != 1 {
		t.Fatalf("after remove = %+v", servers)
	}
	if servers, err := ReadServers(filepath.Join(t.TempDir(), "none.json")); err != nil || servers != nil {
		t.Fatal("a missing file is not an error")
	}
	if err := ValidateName("bad name"); err == nil {
		t.Fatal("invalid name accepted")
	}
}

func TestNamesAndResults(t *testing.T) {
	if got := ExposedName("git hub", "create.issue"); got != "mcp__git_hub__create_issue" {
		t.Fatalf("name = %q", got)
	}
	long := ExposedName(strings.Repeat("s", 40), strings.Repeat("t", 40))
	if len(long) != maxToolNameLength || long == ExposedName(strings.Repeat("s", 40), strings.Repeat("t", 41)) {
		t.Fatalf("long name = %q", long)
	}
	if server, tool, ok := SplitName("mcp__github__create_issue"); !ok || server != "github" || tool != "create_issue" {
		t.Fatal("SplitName failed")
	}
	res := &CallResult{Content: []ContentItem{
		{Type: "text", Text: "first"},
		{Type: "image", MimeType: "image/png", Data: "AAAA"},
		{Type: "resource_link", Name: "doc", URI: "file:///doc"},
	}}
	if got := FormatResult(res); got != "first\n[image: image/png, 4 bytes of base64]\n[resource link doc file:///doc]" {
		t.Fatalf("FormatResult = %q", got)
	}
	if FormatResult(&CallResult{}) != "(no output)" {
		t.Fatal("empty result")
	}
}
