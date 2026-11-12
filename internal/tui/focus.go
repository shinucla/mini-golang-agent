package tui

import (
	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) cursors() []*cursor.Model {
	all := []*cursor.Model{&a.input.Cursor}
	if a.home != nil {
		all = append(all, &a.home.search.Cursor, &a.home.nameInput.Cursor)
	}
	if a.picker != nil {
		all = append(all, &a.picker.keyInput.Cursor)
	}
	if a.agents != nil {
		all = append(all, &a.agents.prompt.Cursor)
		if f := a.agents.form; f != nil {
			for i := range f.inputs {
				all = append(all, &f.inputs[i].Cursor)
			}
			all = append(all, &f.prompt.Cursor)
		}
	}
	return all
}

func (a *App) setTerminalFocus(focused bool) tea.Cmd {
	a.focused = focused
	if !focused {
		a.hideCursors()
		return nil
	}
	for _, c := range a.cursors() {
		c.SetMode(cursor.CursorBlink)
	}
	return cursor.Blink
}

func (a *App) hideCursors() {
	for _, c := range a.cursors() {
		if c.Mode() != cursor.CursorHide {
			c.SetMode(cursor.CursorHide)
		}
	}
}
