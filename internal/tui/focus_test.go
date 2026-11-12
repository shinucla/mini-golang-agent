package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTerminalFocusHidesAndRestoresTheCursor(t *testing.T) {
	app := newTestApp(t)

	app.Update(tea.BlurMsg{})
	if app.input.Cursor.Mode() != cursor.CursorHide {
		t.Fatal("blur hides the cursor of the input")
	}
	typeText(app, "hi")
	if !app.input.Focused() || app.input.Value() != "hi" {
		t.Fatalf("blur keeps the input focused for keys: %q", app.input.Value())
	}

	_, cmd := app.Update(tea.FocusMsg{})
	if app.input.Cursor.Mode() != cursor.CursorBlink || cmd == nil {
		t.Fatal("focus makes the cursor blink again and starts the blink")
	}

	app.editInput(app.input.Reset)
	app.Update(tea.BlurMsg{})
	app.openHome()
	press(app, tea.KeyCtrlF)
	if !app.home.searching || app.home.search.Cursor.Mode() != cursor.CursorHide {
		t.Fatal("an input that opens without focus starts with the cursor hidden")
	}
	app.Update(tea.FocusMsg{})
	if app.home.search.Cursor.Mode() != cursor.CursorBlink || !app.home.search.Focused() {
		t.Fatal("focus makes the search cursor blink")
	}
}
