package agent

import (
	"strings"
	"testing"

	"github.com/shinucla/mini-golang-agent/internal/llm"
)

func TestCleanSuggestion(t *testing.T) {
	for in, want := range map[string]string{
		"\n\"commit and push\"\n":   "commit and push",
		"User: run the tests":       "run the tests",
		"NONE":                      "",
		"none.":                     "",
		"Go with option 2\nbecause": "Go with option 2",
	} {
		if got := CleanSuggestion(in); got != want {
			t.Errorf("CleanSuggestion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSuggestTranscriptNeedsAReplyLast(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "fix it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "Read"}}},
		{Role: llm.RoleTool, Content: "file text", ToolCallID: "1"},
		{Role: llm.RoleAssistant, Content: "Fixed. Want me to add a test?"},
	}
	got := suggestTranscript(history)
	if got != "User: fix it\n\nAgent: Fixed. Want me to add a test?" {
		t.Fatalf("transcript: %q", got)
	}
	if suggestTranscript(history[:1]) != "" {
		t.Fatal("no prediction before the agent replies")
	}
	long := strings.Repeat("a", maxSuggestReplyText) + "RECOMMEND"
	if got := suggestTranscript([]llm.Message{{Role: llm.RoleAssistant, Content: long}}); !strings.HasSuffix(got, "RECOMMEND") {
		t.Fatal("a long reply keeps its end, where the recommendation is")
	}
}
