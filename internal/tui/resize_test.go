package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shinucla/mini-golang-agent/internal/llm"
)

func TestWidthChangeRedrawsTheTranscriptAfterTheResizeSettles(t *testing.T) {
	app := newTestApp(t)
	app.history = []llm.Message{
		{Role: llm.RoleUser, Content: "first question"},
		{Role: llm.RoleAssistant, Content: "first answer"},
	}
	app.printQueue = nil

	if cmd := app.update(tea.WindowSizeMsg{Width: 100, Height: 30}); cmd != nil {
		t.Fatal("a height change keeps the transcript")
	}
	first := app.update(tea.WindowSizeMsg{Width: 80, Height: 30})
	second := app.update(tea.WindowSizeMsg{Width: 60, Height: 30})
	if first == nil || second == nil {
		t.Fatal("a width change waits for the resize to settle")
	}

	app.update(first())
	if app.clearScreen || len(app.printQueue) != 0 {
		t.Fatal("an older resize does not redraw")
	}

	app.update(second())
	out := printed(app)
	if !app.clearScreen || !strings.Contains(out, "first question") || !strings.Contains(out, "first answer") {
		t.Fatalf("the last resize clears the screen and prints the transcript again: %q", out)
	}
}

func TestResizeOnTheHomeViewSkipsTheRedraw(t *testing.T) {
	app := newTestApp(t)
	app.openHome()
	app.printQueue = nil
	app.update(app.update(tea.WindowSizeMsg{Width: 70, Height: 40})())
	if app.clearScreen || len(app.printQueue) != 0 {
		t.Fatal("the home view redraws itself on the alt screen")
	}
}
