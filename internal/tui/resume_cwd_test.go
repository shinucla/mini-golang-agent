package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
)

func TestSessionListShowsSubfoldersWithRelativePaths(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	parent := app.rt.Cwd
	child := filepath.Join(parent, "projects", "child")
	saveTestSession(t, parent, "parent work", "a")
	childSession := saveTestSession(t, child, "child work", "b")
	saveTestSession(t, parent+"-sibling", "sibling work", "c")
	saveTestSession(t, filepath.Dir(parent), "ancestor work", "d")

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	view := ansi.Strip(app.View())
	names := strings.Join(listNames(app), ",")
	if !strings.Contains(names, "parent work") || !strings.Contains(names, "child work") {
		t.Fatalf("the list must include this folder and its subfolders: %s", names)
	}
	if strings.Contains(names, "sibling work") || strings.Contains(names, "ancestor work") {
		t.Fatalf("the list must not include other folders: %s", names)
	}
	for _, l := range strings.Split(view, "\n") {
		switch {
		case strings.Contains(l, "child work") && !strings.Contains(l, "projects/child"):
			t.Fatalf("a subfolder session shows its relative path: %q", l)
		case strings.Contains(l, "parent work") && !strings.Contains(l, "cwd: ./"):
			t.Fatalf("a session of this folder shows \"cwd: ./\": %q", l)
		case strings.Contains(l, "msgs") && strings.Contains(l, "fake"):
			t.Fatalf("the model name is replaced by the path: %q", l)
		}
	}

	resumed, err := agent.LoadSession(config.SessionsDir(), childSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	app = newApp(app.rt, resumed, "", true)
	app.saveSession()
	if saved, _ := agent.LoadSession(config.SessionsDir(), childSession.ID); saved.Cwd != child {
		t.Fatalf("a session keeps its own folder on save, got %q", saved.Cwd)
	}
}
