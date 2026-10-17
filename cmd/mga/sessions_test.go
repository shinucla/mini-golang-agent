package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"strings"
	"testing"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/llm"
)

func TestParseInterleavedAcceptsFlagsInAnyOrder(t *testing.T) {
	cases := []struct {
		args   []string
		prompt string
		resume string
		print  bool
	}{
		{[]string{"-p", "--resume", "abc", "what now?"}, "what now?", "abc", true},
		{[]string{"-p", "hello there", "--resume", "abc"}, "hello there", "abc", true},
		{[]string{"--resume", "abc", "-p", "hi"}, "hi", "abc", true},
		{[]string{"fix", "the", "bug"}, "fix the bug", "", false},
		{[]string{"-p", "--", "-starts with a dash"}, "-starts with a dash", "", true},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		p := fs.Bool("p", false, "")
		resume := fs.String("resume", "", "")
		positional, err := parseInterleaved(fs, c.args)
		if err != nil || strings.Join(positional, " ") != c.prompt || *resume != c.resume || *p != c.print {
			t.Errorf("%v: prompt=%q resume=%q print=%v err=%v", c.args, strings.Join(positional, " "), *resume, *p, err)
		}
	}
}

func TestSessionsCommandListsAndShows(t *testing.T) {
	t.Setenv("MGA_HOME", t.TempDir())
	here, elsewhere := t.TempDir(), t.TempDir()
	for _, cwd := range []string{here, elsewhere} {
		s := agent.NewSession(cwd)
		s.Messages = []llm.Message{{Role: llm.RoleUser, Content: "question in " + cwd}, {Role: llm.RoleAssistant, Content: "answer"}}
		if err := s.Save(config.SessionsDir()); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if err := sessionsTo(&out, here, []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	var list []sessionSummary
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || len(list) != 1 || list[0].Cwd != here || list[0].Messages != 2 {
		t.Fatalf("the list holds this folder's session: %s err=%v", out.String(), err)
	}

	out.Reset()
	if err := sessionsTo(&out, here, []string{"--all", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("--all lists every folder: %s", out.String())
	}

	out.Reset()
	if err := sessionsTo(&out, elsewhere, []string{"show", list[0].ID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "> question in") || !strings.Contains(out.String(), "⏺ answer") {
		t.Fatalf("show prints the conversation:\n%s", out.String())
	}

	out.Reset()
	if err := sessionsTo(&out, here, []string{"show", list[0].ID, "--json"}); err != nil {
		t.Fatal(err)
	}
	var full agent.Session
	if err := json.Unmarshal(out.Bytes(), &full); err != nil || len(full.Messages) != 2 {
		t.Fatalf("show --json prints the whole session: %s", out.String())
	}

	if err := sessionsTo(&out, here, []string{"show", "nope"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("an unknown id fails clearly: %v", err)
	}
}
