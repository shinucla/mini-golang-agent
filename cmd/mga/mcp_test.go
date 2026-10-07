package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/mcp"
)

func TestMCPAddRemoveApprove(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	cwd := t.TempDir()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	if err := mcpCommand(cfg, cwd, []string{"add", "-e", "TOKEN=${GH_TOKEN}", "github", "--", "npx", "-y", "server-github"}); err != nil {
		t.Fatal(err)
	}
	user, _ := mcp.ReadServers(mcp.UserConfigPath())
	gh := user["github"]
	if gh.Command != "npx" || strings.Join(gh.Args, " ") != "-y server-github" || gh.Env["TOKEN"] != "${GH_TOKEN}" {
		t.Fatalf("stdio server = %+v", gh)
	}
	if info, _ := os.Stat(mcp.UserConfigPath()); info.Mode().Perm() != 0o600 {
		t.Fatal("the user MCP file can hold secrets and must be 0600")
	}

	if err := mcpCommand(cfg, cwd, []string{"add", "-s", "project", "-H", "Authorization: Bearer x", "docs", "https://example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	project, _ := mcp.ReadServers(mcp.ProjectConfigPath(cwd))
	if d := project["docs"]; d.Transport() != mcp.TransportHTTP || d.Headers["Authorization"] != "Bearer x" {
		t.Fatalf("http server = %+v", d)
	}
	if !slices.Contains(cfg.ApprovedMCP[cwd], "docs") {
		t.Fatal("a project server added by the user must count as approved")
	}

	if err := mcpCommand(cfg, cwd, []string{"add", "bad name", "x"}); err == nil {
		t.Fatal("an invalid name must fail")
	}
	if err := mcpCommand(cfg, cwd, []string{"approve", "nope"}); err == nil {
		t.Fatal("approving a missing server must fail")
	}
	if err := mcpCommand(cfg, cwd, []string{"remove", "docs"}); err != nil {
		t.Fatal(err)
	}
	if err := mcpCommand(cfg, cwd, []string{"remove", "github"}); err != nil {
		t.Fatal(err)
	}
	if err := mcpCommand(cfg, cwd, []string{"remove", "github"}); err == nil {
		t.Fatal("removing twice must fail")
	}
}
