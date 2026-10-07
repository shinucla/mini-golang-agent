package agentdef

import (
	"slices"
	"testing"
)

func TestParseFormatRoundTrip(t *testing.T) {
	src := "---\nname: reviewer\ndescription: Reviews code\nmodel: gemini:gemini-2.5-pro\ntools: [Read, Grep]\n---\n\nYou review code.\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "reviewer" || d.Model != "gemini:gemini-2.5-pro" || !slices.Equal(d.Tools, []string{"Read", "Grep"}) || d.Prompt != "You review code." {
		t.Fatalf("parsed %+v", d)
	}
	again, err := Parse(Format(d))
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != d.Name || again.Model != d.Model || !slices.Equal(again.Tools, d.Tools) || again.Prompt != d.Prompt {
		t.Fatalf("round trip %+v", again)
	}
}

func TestStoreSaveListDeleteAndOverride(t *testing.T) {
	s := &Store{UserDir: t.TempDir(), ProjectDir: t.TempDir()}
	if _, err := s.Save(Definition{Name: "x", Description: "user one", Prompt: "p", Scope: ScopeUser}, ""); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Save(Definition{Name: "x", Description: "project one", Prompt: "p", Scope: ScopeProject}, "")
	if err != nil {
		t.Fatal(err)
	}
	d, ok := s.Get("x")
	if !ok || d.Description != "project one" {
		t.Fatalf("project scope must win: %+v", d)
	}
	if err := s.Delete(saved); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Get("x"); d.Scope != ScopeUser {
		t.Fatalf("user definition must remain: %+v", d)
	}
	if _, ok := s.Get("Explore"); !ok {
		t.Fatal("built-in Explore missing")
	}
	if _, err := s.Save(Definition{Name: "bad name", Description: "d", Scope: ScopeUser}, ""); err == nil {
		t.Fatal("invalid name accepted")
	}
}
