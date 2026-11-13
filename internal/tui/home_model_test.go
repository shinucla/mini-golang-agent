package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shinucla/mini-golang-agent/internal/config"
)

func TestSessionListKeepsItsOwnModel(t *testing.T) {
	app := newTestApp(t)
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	saved.Providers = app.rt.Cfg.Providers
	app.rt.Cfg = saved
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	other := saveTestSession(t, app.rt.Cwd, "other work", "q")
	other.Provider, other.Model = "fake", "gpt-5"
	if err := other.Save(config.SessionsDir()); err != nil {
		t.Fatal(err)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	selectSession(t, app, other.ID)
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if app.homeModel != "m" || !strings.Contains(app.View(), "fake:m") {
		t.Fatalf("opening a session keeps the session list model: %q", app.homeModel)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	typeText(app, "/model fake:session-model")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.model != "session-model" || app.homeModel != "m" {
		t.Fatalf("/model in a session sets only that session: session=%q home=%q", app.model, app.homeModel)
	}
	if reloaded, _ := config.Load(); reloaded.DefaultModel == "session-model" {
		t.Fatal("/model in a session must not change the saved default")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "/model fake:home-model")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.view != viewHome || app.homeModel != "home-model" || app.model != "session-model" {
		t.Fatalf("/model on the list sets only the list model: view=%v home=%q session=%q", app.view, app.homeModel, app.model)
	}
	if reloaded, _ := config.Load(); reloaded.DefaultModel != "home-model" {
		t.Fatalf("/model on the list saves the default: %q", reloaded.DefaultModel)
	}

	app.rt.RegisterProvider("fake", echoProvider{})
	typeText(app, "start fresh")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.model != "home-model" || app.session.Model != "home-model" {
		t.Fatalf("a new session from the list uses the list model: %q", app.model)
	}
}

func TestModelPickerFromTheListSetsTheListModel(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	typeText(app, "/model")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.picker.provider = "fake"
	app.picker.stage = stageModels
	app.picker.filter = "picked-model"
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.view != viewHome || app.homeModel != "picked-model" || app.model != "m" {
		t.Fatalf("the picker from the list sets the list model only: view=%v home=%q session=%q", app.view, app.homeModel, app.model)
	}
}
