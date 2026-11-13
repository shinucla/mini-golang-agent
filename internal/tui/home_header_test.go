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
	app.rt.SetCurrent("fake", "gpt-5")
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.openHome()

	lines := strings.Split(ansi.Strip(app.View()), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "╭") {
		t.Fatalf("the panel border is the top line: %q", lines[0])
	}
	want := []string{"│ ✻ mga       mini Go agent", "│   Version   v9.9.9", "│   Sessions  " + app.rt.Cwd[:20], "│   Model     fake:gpt-5"}
	for i, w := range want {
		if line := lines[1+i]; !strings.Contains(line, w) || !strings.HasPrefix(strings.TrimSpace(line), "│") {
			t.Fatalf("line %d inside the border must show %q: %q", 1+i, w, line)
		}
	}
	if 30 < lipgloss.Height(app.View()) {
		t.Fatalf("the view must fit the terminal: %d lines", lipgloss.Height(app.View()))
	}

	app.Update(tea.WindowSizeMsg{Width: 30, Height: 30})
	for _, line := range strings.Split(app.View(), "\n")[:5] {
		if 30 < lipgloss.Width(line) {
			t.Fatalf("a narrow terminal cuts the header: %q", line)
		}
	}
}
