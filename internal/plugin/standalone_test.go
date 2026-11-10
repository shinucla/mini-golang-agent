package plugin

import (
	"path/filepath"
	"testing"
)

func TestLoadStandaloneReadsUserAndProjectFolders(t *testing.T) {
	mgaHome, claudeHome, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("MGA_HOME", mgaHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	cwd := filepath.Join(workspace, "repo")

	writeFile(t, filepath.Join(claudeHome, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: User review\n---\nUser body.\n")
	writeFile(t, filepath.Join(workspace, ".claude", "skills", "review", "SKILL.md"), "---\ndescription: Project review\n---\nProject body.\n")
	writeFile(t, filepath.Join(cwd, ".mga", "skills", "deploy", "SKILL.md"), "---\ndescription: Deploy\ndisable-model-invocation: true\nargument-hint: <env>\n---\nDeploy $ARGUMENTS.\n")
	writeFile(t, filepath.Join(cwd, ".claude", "skills", "style", "SKILL.md"), "---\nuser-invocable: false\n---\n# Style guide\nUse tabs.\n")
	writeFile(t, filepath.Join(mgaHome, "commands", "hello.md"), "Say hello to $1.\n")
	writeFile(t, filepath.Join(cwd, ".claude", "commands", "frontend", "component.md"), "---\ndescription: Make a component\nargument-hint: <name>\n---\nMake $ARGUMENTS.\n")
	writeFile(t, filepath.Join(cwd, ".claude", "commands", "hello.md"), "Project hello.\n")

	got, err := LoadStandalone(cwd)
	if err != nil {
		t.Fatal(err)
	}
	skills := map[string]Skill{}
	for _, s := range got.Skills {
		skills[s.Name] = s
	}
	if len(skills) != 3 {
		t.Fatalf("want 3 skills, got %+v", got.Skills)
	}
	if s := skills["review"]; s.Scope != ScopeUser || s.Description != "User review" {
		t.Fatalf("a user skill wins over a project skill of the same name: %+v", s)
	}
	if s := skills["deploy"]; !s.UserOnly || s.ModelOnly || s.ArgHint != "<env>" || s.Scope != ScopeProject {
		t.Fatalf("deploy: %+v", s)
	}
	if s := skills["style"]; !s.ModelOnly || s.UserOnly || s.Description != "Style guide" {
		t.Fatalf("style: %+v", s)
	}

	commands := map[string]Command{}
	for _, c := range got.Commands {
		commands[c.Name] = c
	}
	if len(commands) != 2 {
		t.Fatalf("want 2 commands, got %+v", got.Commands)
	}
	if c := commands["hello"]; c.Scope != ScopeUser || c.Description != "Say hello to $1." {
		t.Fatalf("hello: %+v", c)
	}
	if c := commands["component"]; c.Scope != ScopeProject || c.ArgHint != "<name>" {
		t.Fatalf("a command in a subfolder keeps its file name: %+v", c)
	}
}
