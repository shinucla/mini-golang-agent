package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/tools"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewSkillsCommandsAndRulesLoadWithoutARestart(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	cfg := &config.Config{Providers: map[string]config.ProviderConfig{
		"fake": {Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", Local: true},
	}}
	rt := agent.NewRuntime(context.Background(), cfg, cwd, agent.NewPermissions(agent.ModeDefault, nil))
	applyExtensions(rt, nil)
	if len(rt.SkillList()) != 0 || len(rt.CommandList()) != 0 {
		t.Fatal("an empty project starts with no skills and no commands")
	}

	writeFile(t, filepath.Join(cwd, ".mga", "commands", "hello.md"), "---\ndescription: Say hello\n---\nSay hello.")
	writeFile(t, filepath.Join(cwd, ".claude", "skills", "tidy", "SKILL.md"), "---\nname: tidy\ndescription: Tidy the imports\n---\nTidy.")
	writeFile(t, filepath.Join(cwd, ".mga", "rules", "style.md"), "Use short names.")

	ag, err := rt.MainAgentWith(&tools.Env{Cwd: cwd}, "fake", "m")
	if err != nil {
		t.Fatal(err)
	}
	if commands := rt.CommandList(); len(commands) != 1 || commands[0].Name != "hello" {
		t.Fatalf("the next turn loads the new command: %+v", commands)
	}
	if skills := rt.SkillList(); len(skills) != 1 || skills[0].Name != "tidy" {
		t.Fatalf("the next turn loads the new skill: %+v", skills)
	}
	if !strings.Contains(ag.System, "- tidy: Tidy the imports") || !strings.Contains(ag.System, "Use short names.") {
		t.Fatal("the system prompt of the next turn lists the new skill and the new rule")
	}

	if err := os.RemoveAll(filepath.Join(cwd, ".mga", "commands")); err != nil {
		t.Fatal(err)
	}
	rt.ReloadExtensions()
	if len(rt.CommandList()) != 0 {
		t.Fatal("a deleted command goes away")
	}
}
