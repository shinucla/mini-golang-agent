package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shinucla/mini-golang-agent/internal/agentdef"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
}

func remoteRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "skills", "remote-skill", "SKILL.md"), "---\nname: remote-skill\ndescription: A skill from another repo\n---\nRemote body.\n")
	run(t, dir, "init", "-q")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "init")
	return "file://" + dir
}

func marketplace(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".claude-plugin", "marketplace.json"), `{
  "name": "acme-market",
  "plugins": [
    {"name": "acme", "source": "./plugins/acme", "description": "Acme tools", "skills": ["./extra/special"]},
    {"name": "remote", "source": {"source": "git", "url": "`+remote+`"}, "description": "From another repo"},
    {"name": "escape", "source": "../outside"}
  ]
}`)
	root := filepath.Join(dir, "plugins", "acme")
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name": "acme", "version": "1.2.3",
  "mcpServers": {"tool": {"command": "${CLAUDE_PLUGIN_ROOT}/bin/run.sh", "args": ["--root", "${CLAUDE_PLUGIN_ROOT}"]}}}`)
	writeFile(t, filepath.Join(root, "skills", "lint", "SKILL.md"), "---\nname: lint\ndescription: Use when: linting Go code: it fixes errors\nallowed-tools: Bash\n---\n\n# Lint\nRun the linter.\n")
	writeFile(t, filepath.Join(root, "extra", "special", "SKILL.md"), "---\nname: special\ndescription: A listed skill\n---\nSpecial body.\n")
	writeFile(t, filepath.Join(root, "commands", "greet.md"), "---\ndescription: Say hello\nargument-hint: <name>\n---\nSay hello to $ARGUMENTS.\n")
	writeFile(t, filepath.Join(root, "agents", "reviewer.md"), "---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\nYou review code.\n")
	writeFile(t, filepath.Join(root, "hooks", "hooks.json"), "{}")
	return dir
}

func names[T any](items []T, name func(T) string) []string {
	var out []string
	for _, it := range items {
		out = append(out, name(it))
	}
	slices.Sort(out)
	return out
}

func TestMarketplaceInstallAndLoad(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	mk, err := AddMarketplace(marketplace(t, "file:///nowhere"))
	if err != nil || mk.Name != "acme-market" {
		t.Fatalf("add: %+v err=%v", mk, err)
	}
	rec, err := Install("acme")
	if err != nil || rec.ID != "acme@acme-market" || rec.Version != "1.2.3" || !rec.Enabled {
		t.Fatalf("install: %+v err=%v", rec, err)
	}

	loaded, err := LoadEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if got := names(loaded.Skills, func(s Skill) string { return s.Name }); !slices.Equal(got, []string{"acme:lint", "acme:special"}) {
		t.Fatalf("skills = %v", got)
	}
	lint := loaded.Skills[slices.IndexFunc(loaded.Skills, func(s Skill) bool { return s.Name == "acme:lint" })]
	if lint.Description != "Use when: linting Go code: it fixes errors" {
		t.Fatalf("a description with colons still parses: %q", lint.Description)
	}
	if body, _ := Body(lint.File); body != "# Lint\nRun the linter." {
		t.Fatalf("body = %q", body)
	}
	if len(loaded.Commands) != 1 || loaded.Commands[0].Name != "acme:greet" || loaded.Commands[0].ArgHint != "<name>" {
		t.Fatalf("commands = %+v", loaded.Commands)
	}
	if len(loaded.Agents) != 1 || loaded.Agents[0].Name != "acme:reviewer" || loaded.Agents[0].Scope != agentdef.ScopePlugin {
		t.Fatalf("agents = %+v", loaded.Agents)
	}
	tool, ok := loaded.MCP["tool"]
	if !ok || tool.Command != filepath.Join(rec.Path, "bin", "run.sh") || tool.Args[1] != rec.Path {
		t.Fatalf("the plugin root is filled in: %+v", tool)
	}
	if !loaded.Plugins[0].Hooks || loaded.Plugins[0].Err != nil {
		t.Fatalf("plugin info = %+v", loaded.Plugins[0])
	}

	if _, err := SetEnabled("acme", false); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := LoadEnabled(); len(loaded.Skills) != 0 || len(loaded.MCP) != 0 {
		t.Fatal("a disabled plugin adds nothing")
	}
	if all, _ := LoadAll(); len(all.Plugins) != 1 || all.Plugins[0].Enabled {
		t.Fatal("a disabled plugin is still listed")
	}

	if _, err := Install("escape@acme-market"); err == nil || !strings.Contains(err.Error(), "outside the marketplace") {
		t.Fatalf("a source outside the marketplace is refused: %v", err)
	}
	if _, err := Install("missing@acme-market"); err == nil {
		t.Fatal("an unknown plugin fails")
	}
}

func TestPluginFromItsOwnGitRepo(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	if _, err := AddMarketplace(marketplace(t, remoteRepo(t))); err != nil {
		t.Fatal(err)
	}
	rec, err := Install("remote@acme-market")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.Path, filepath.Join(Root(), "cache")) || len(rec.Commit) != 40 {
		t.Fatalf("the plugin is cloned into the cache: %+v", rec)
	}
	loaded, _ := LoadEnabled()
	if len(loaded.Skills) != 1 || loaded.Skills[0].Name != "remote:remote-skill" {
		t.Fatalf("skills = %+v", loaded.Skills)
	}
	if _, err := Install("acme@acme-market"); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveMarketplace("acme-market")
	if err != nil || len(removed) != 2 {
		t.Fatalf("removing the marketplace uninstalls its plugins: %v err=%v", removed, err)
	}
	if _, err := os.Stat(rec.Path); !os.IsNotExist(err) {
		t.Fatal("the cached clone is deleted")
	}
	if all, _ := InstalledPlugins(); len(all) != 0 {
		t.Fatalf("installed = %v", all)
	}
}
