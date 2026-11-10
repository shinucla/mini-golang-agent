package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstructionFilesFollowClaudeCode(t *testing.T) {
	mgaHome, claudeHome, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("MGA_HOME", mgaHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	cwd := filepath.Join(workspace, "repo")

	write(t, filepath.Join(claudeHome, "CLAUDE.md"), "User rules.")
	write(t, filepath.Join(claudeHome, "rules", "tone.md"), "Be brief.")
	write(t, filepath.Join(workspace, "CLAUDE.md"), "Workspace rules. See @docs/style.md and `@not/imported.md`.\n```\n@also/not.md\n```\nMail a@b.com.")
	write(t, filepath.Join(workspace, "docs", "style.md"), "Imported style.")
	write(t, filepath.Join(cwd, "AGENTS.md"), "Same text.")
	write(t, filepath.Join(cwd, "CLAUDE.md"), "Same text.")
	write(t, filepath.Join(cwd, ".claude", "CLAUDE.md"), "Repo rules.")
	write(t, filepath.Join(cwd, "CLAUDE.local.md"), "Local rules.")
	write(t, filepath.Join(cwd, ".claude", "rules", "go", "tests.md"), "---\npaths:\n  - \"**/*_test.go\"\n---\nTable tests.")
	write(t, filepath.Join(cwd, ".mga", "rules", "empty.md"), "  \n")

	var got []string
	var scoped InstructionFile
	for _, f := range InstructionFiles(cwd) {
		rel, err := filepath.Rel(filepath.Dir(workspace), f.Path)
		if strings.HasPrefix(f.Path, claudeHome) {
			rel, err = filepath.Rel(claudeHome, f.Path)
			rel = "~claude/" + rel
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.ToSlash(rel))
		if 0 < len(f.Paths) {
			scoped = f
		}
	}
	base := filepath.Base(workspace)
	want := []string{
		"~claude/CLAUDE.md",
		"~claude/rules/tone.md",
		base + "/CLAUDE.md",
		base + "/docs/style.md",
		base + "/repo/AGENTS.md",
		base + "/repo/CLAUDE.local.md",
		base + "/repo/.claude/CLAUDE.md",
		base + "/repo/.claude/rules/go/tests.md",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("files:\n got  %q\n want %q", got, want)
	}
	if scoped.Text != "Table tests." || !slices.Equal(scoped.Paths, []string{"**/*_test.go"}) {
		t.Fatalf("a rule with paths keeps them and drops its frontmatter: %+v", scoped)
	}

	prompt := MainPrompt(cwd, "", "")
	if !strings.Contains(prompt, "# Extend mga") || !strings.Contains(prompt, mgaHome+"/commands/<name>.md") || !strings.Contains(prompt, mgaHome+"/skills/<name>/SKILL.md") {
		t.Fatalf("the prompt says where mga commands and skills go:\n%s", prompt)
	}
	if !strings.Contains(prompt, "(apply these rules only to files that match: **/*_test.go)") || !strings.Contains(prompt, "Imported style.") {
		t.Fatalf("prompt misses the scoped rule or the import:\n%s", prompt)
	}
}
