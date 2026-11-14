package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/shinucla/mini-golang-agent/internal/version"
)

func TestSessionListShowsTheHeaderInsideThePanel(t *testing.T) {
	version.Override = "v9.9.9"
	defer func() { version.Override = "" }()
	app := newTestApp(t)
	app.setModel("fake", "gpt-5", true)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.openHome()

	lines := strings.Split(ansi.Strip(app.View()), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "╭") {
		t.Fatalf("the panel border is the top line: %q", lines[0])
	}
	want := []string{"│ ✻ mga       mini Go agent - v9.9.9", "│   Model     fake:gpt-5"}
	for i, w := range want {
		if line := lines[1+i]; !strings.Contains(line, w) || !strings.HasPrefix(strings.TrimSpace(line), "│") {
			t.Fatalf("line %d inside the border must show %q: %q", 1+i, w, line)
		}
	}
	if strings.Contains(lines[3], "│ ✻") || strings.Contains(lines[3], "Model") || strings.Contains(ansi.Strip(app.View()), "Sessions  ") || strings.Contains(ansi.Strip(app.View()), "Version  ") {
		t.Fatalf("the header has two lines only, with no Sessions or Version line:\n%s", ansi.Strip(app.View()))
	}
	if 30 < lipgloss.Height(app.View()) {
		t.Fatalf("the view must fit the terminal: %d lines", lipgloss.Height(app.View()))
	}

	app.Update(tea.WindowSizeMsg{Width: 30, Height: 30})
	for _, line := range strings.Split(app.View(), "\n")[:3] {
		if 30 < lipgloss.Width(line) {
			t.Fatalf("a narrow terminal cuts the header: %q", line)
		}
	}
}
