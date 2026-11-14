package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func lineIndex(view, text string) int {
	for i, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(line, text) {
			return i
		}
	}
	return -1
}

func TestSearchLineUsesTheRenameLine(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	saveTestSession(t, app.rt.Cwd, "Fix login bug", "a")
	saveTestSession(t, app.rt.Cwd, "Agent docs", "b")
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	rename := lineIndex(app.View(), "enter save · esc cancel") - 1
	if rename != lineIndex(app.View(), "Fix login bug")+2 {
		t.Fatalf("the rename field comes one blank line after the last session:\n%s", ansi.Strip(app.View()))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	search := lineIndex(app.View(), "of 2 sessions")
	if search != rename || lineIndex(app.View(), "esc cancel") != search+1 {
		t.Fatalf("the search line (%d) uses the rename line (%d), with esc cancel under it:\n%s", search, rename, ansi.Strip(app.View()))
	}

	typeText(app, "zzz")
	view := app.View()
	if lineIndex(view, "No session name matches.") <= 0 || lineIndex(view, "0 of 2 sessions") <= lineIndex(view, "No session name matches.") {
		t.Fatalf("with no match the note is in the list and the search line stays at the bottom:\n%s", ansi.Strip(view))
	}
}

func panelBottom(view string) int {
	for i, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.HasPrefix(line, "╰") {
			return i
		}
	}
	return -1
}

func TestLongSessionListKeepsTheFieldsInsideThePanel(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	for i := range 40 {
		saveTestSession(t, app.rt.Cwd, fmt.Sprintf("session %02d", i), "q")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app.home.cursor = 0
	if lineIndex(app.View(), "❯") <= 0 || 30 < len(strings.Split(app.View(), "\n")) {
		t.Fatalf("the first session shows with the cursor, and the view fits the screen:\n%s", ansi.Strip(app.View()))
	}

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	bottom := panelBottom(app.View())
	if lineIndex(app.View(), "esc cancel") != bottom-1 || lineIndex(app.View(), "of 40 sessions") != bottom-2 {
		t.Fatalf("the hint is on the last line in the border and the search line is above it:\n%s", ansi.Strip(app.View()))
	}
	for range 45 {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
		if row := lineIndex(app.View(), "❯"); row <= 0 || bottom-3 <= row {
			t.Fatalf("down scrolls the list and keeps the cursor above the search line:\n%s", ansi.Strip(app.View()))
		}
	}
	if app.home.cursor != 39 {
		t.Fatalf("down reaches the last session: %d", app.home.cursor)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if lineIndex(app.View(), "enter save · esc cancel") != bottom-1 || lineIndex(app.View(), "name: ") != bottom-2 {
		t.Fatalf("the rename field has the same lowest place:\n%s", ansi.Strip(app.View()))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	for range 45 {
		app.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if app.home.cursor != 0 || lineIndex(app.View(), "❯") <= 0 {
		t.Fatalf("up scrolls back to the first session:\n%s", ansi.Strip(app.View()))
	}
}
