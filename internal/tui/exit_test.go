package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestExitClearsTheInputBoxAndStatusLine(t *testing.T) {
	for _, quit := range []func(app *App) tea.Cmd{
		func(app *App) tea.Cmd {
			typeText(app, "/exit")
			_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			return cmd
		},
		func(app *App) tea.Cmd {
			app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			return cmd
		},
	} {
		app := newTestApp(t)
		if app.View() == "" {
			t.Fatal("the input box shows before the exit")
		}
		if cmd := quit(app); cmd == nil {
			t.Fatal("the exit returns a command that quits")
		}
		if app.View() != "" {
			t.Fatalf("the last frame is empty, so no input box or status line stays on the screen: %q", app.View())
		}
	}
}

func TestScreenRowsCountsWrappedLines(t *testing.T) {
	for _, c := range []struct {
		text string
		want int
	}{
		{"", 1},
		{"one\ntwo", 2},
		{strings.Repeat("x", 10), 1},
		{strings.Repeat("x", 11), 2},
		{"\x1b[1m" + strings.Repeat("x", 10) + "\x1b[0m", 1},
	} {
		if got := screenRows(c.text, 10); got != c.want {
			t.Fatalf("screenRows(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}

func TestExitErasesWhatMgaPrinted(t *testing.T) {
	app := newTestApp(t)
	app.Update(printDoneMsg{})
	app.printedRows = 0
	app.emit("first\nsecond")
	app.flush()
	if app.printedRows != 0 {
		t.Fatal("rows count only after the print is done")
	}
	app.Update(printDoneMsg{})
	app.emit("third")
	app.flush()
	app.Update(printDoneMsg{})
	if app.printedRows != 3 || app.eraseOutput() != ansi.CursorUp(3)+ansi.EraseScreenBelow {
		t.Fatalf("mga moves up over its 3 printed rows and clears them: rows=%d %q", app.printedRows, app.eraseOutput())
	}

	app.resetScreen()
	app.flush()
	app.Update(printDoneMsg{})
	if app.printedRows != screenRows(app.banner(), app.width) {
		t.Fatalf("a cleared screen counts only what mga printed after it: %d", app.printedRows)
	}

	if fresh := newTestApp(t); fresh.eraseOutput() != "" {
		t.Fatal("with nothing printed, the exit erases nothing")
	}
}

func TestScreenSwitchDrawsAnEmptyFrameFirst(t *testing.T) {
	screenSwitchDelay = resizeSettle
	defer func() { screenSwitchDelay = 0 }()
	app := newTestApp(t)

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if app.alt || !app.switching || cmd == nil || app.View() != "" {
		t.Fatalf("before the alternate screen, mga draws an empty frame and waits: alt=%v switching=%v", app.alt, app.switching)
	}
	app.Update(screenSwitchMsg{})
	if !app.alt || app.switching || app.View() == "" {
		t.Fatalf("after the wait, mga switches and draws the list: alt=%v switching=%v", app.alt, app.switching)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !app.alt || !app.switching || app.View() != "" {
		t.Fatal("before the normal screen, mga draws an empty frame on the alternate screen")
	}
	app.Update(screenSwitchMsg{})
	if app.alt || app.switching || app.View() == "" {
		t.Fatal("after the wait, mga is back on the normal screen with the chat")
	}
}

func TestQuitDropsQueuedOutputWithoutCountingIt(t *testing.T) {
	app := newTestApp(t)
	app.Update(printDoneMsg{})
	app.printedRows = 4
	app.openHome()
	typeText(app, "/exit")
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msg := cmd(); msg != tea.Quit() {
		t.Fatalf("the exit quits at once, with no print and no screen switch first: %T", msg)
	}
	if len(app.printQueue) != 0 || app.printedRows != 4 {
		t.Fatalf("queued text is dropped and never counted, so the erase cannot reach the shell history: queue=%d rows=%d", len(app.printQueue), app.printedRows)
	}
}
