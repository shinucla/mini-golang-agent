package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionListInputBoxAndBottomBar(t *testing.T) {
	h := newHarness(t)
	gate := make(chan struct{})
	defer close(gate)
	h.app.rt.RegisterProvider("fake", gatedProvider{gate: gate})
	h.app.provider, h.app.model = "fake", "m"
	h.update(tea.WindowSizeMsg{Width: 120, Height: 40})
	current := h.app.sessionRun

	h.key(tea.KeyLeft)
	lines := strings.Split(h.view(), "\n")
	if len(lines) != 40 {
		t.Fatalf("the list must fill the screen: %d lines", len(lines))
	}
	if last := lines[len(lines)-1]; strings.TrimSpace(last) != strings.TrimSpace(homeHint) {
		t.Fatalf("the hint line must be the last line: %q", last)
	}
	if !strings.Contains(lines[len(lines)-3], "Type a message and press enter to start a new session") {
		t.Fatalf("the input box must sit just above the hint line:\n%s", strings.Join(lines[len(lines)-5:], "\n"))
	}

	h.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	lines = strings.Split(h.view(), "\n")
	bottom := strings.Join(lines[len(lines)-14:], "\n")
	if len(lines) != 40 || !strings.Contains(bottom, "Session list keys") || !strings.Contains(bottom, "start a new session with that message") {
		t.Fatalf("? must show the help at the bottom:\n%s", h.view())
	}
	if strings.Contains(h.view(), strings.TrimSpace(homeHint)) {
		t.Fatal("the help replaces the hint line")
	}
	h.key(tea.KeyEsc)
	if h.app.home.showKeys || h.app.view != viewHome {
		t.Fatal("esc closes the help first")
	}

	h.send("slow background job")
	started := h.app.sessionRun
	if h.app.view != viewHome || started == current || h.app.input.Value() != "" {
		t.Fatal("enter with text starts a new session, makes it current, and stays in the list")
	}
	if !started.busy || started.history[0].Content != "slow background job" {
		t.Fatalf("the new session must run the message: %+v", started)
	}
	if _, kept := h.app.runs[current.session.ID]; kept || strings.Contains(h.view(), "(new session)") {
		t.Fatalf("the empty placeholder session must be removed:\n%s", h.view())
	}
	for _, l := range strings.Split(h.view(), "\n") {
		if strings.Contains(l, "● ") && !strings.Contains(l, "current") {
			t.Fatalf("the ● must mark the current session only: %q", l)
		}
	}
	if view := ansi.Strip(h.app.View()); !strings.Contains(view, "Working") {
		t.Fatalf("the new session must show under Working:\n%s", view)
	}
	if sel := h.app.home.items(h.app)[h.app.home.cursor]; sel.session != started.session {
		t.Fatal("the cursor must move to the new session")
	}

	h.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab?")})
	if h.app.home.showKeys || h.app.input.Value() != "ab?" {
		t.Fatal("? with text in the box types a question mark")
	}
	h.key(tea.KeyEsc)
	h.key(tea.KeyEsc)
	if h.app.view != viewChat || h.app.input.Placeholder != chatPlaceholder {
		t.Fatal("esc clears the input, then leaves the list and restores the chat placeholder")
	}
}

func TestStartFromListKeepsASessionWithMessages(t *testing.T) {
	h := newHarness(t)
	gate := make(chan struct{})
	defer close(gate)
	h.app.rt.RegisterProvider("fake", gatedProvider{gate: gate})
	h.app.provider, h.app.model = "fake", "m"
	h.send("first question")
	h.until("the first answer", func() bool { return !h.app.busy && 2 <= len(h.app.history) })
	previous := h.app.sessionRun

	h.key(tea.KeyLeft)
	h.send("slow second topic")
	if h.app.sessionRun == previous {
		t.Fatal("the new session must become current")
	}
	if _, kept := h.app.runs[previous.session.ID]; !kept {
		t.Fatal("a session with messages must stay")
	}
	if !strings.Contains(h.view(), "fast: first question") && !strings.Contains(strings.Join(listNames(h.app), ","), sessionName(previous.session)) {
		t.Fatalf("the previous session must stay in the list: %v", listNames(h.app))
	}

	h.app.printing = true
	h.app.printQueue = nil
	h.key(tea.KeyEsc)
	if h.app.view != viewChat {
		t.Fatal("esc leaves the list")
	}
	h.app.printing = false
	h.app.loadSession(h.app.session)
	if out := ansi.Strip(strings.Join(h.app.printQueue, "\n")); !strings.Contains(out, "slow second topic") {
		t.Fatalf("the chat must show the new session:\n%s", out)
	}
}
