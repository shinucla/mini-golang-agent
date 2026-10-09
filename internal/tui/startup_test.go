package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
)

func TestStartupOpensTheSessionListWhenSessionsExist(t *testing.T) {
	app := newTestApp(t)
	app.Init()
	if app.view != viewChat {
		t.Fatal("with no sessions in this folder, mga starts in the chat")
	}

	saved := saveTestSession(t, app.rt.Cwd, "earlier work", "q")
	app = newApp(app.rt, agent.NewSession(app.rt.Cwd), "", true)
	app.Init()
	view := ansi.Strip(app.View())
	if app.view != viewHome || !app.alt || !strings.Contains(view, "earlier work") || strings.Contains(view, "(new session)") {
		t.Fatalf("with sessions, mga starts in the full-screen list:\n%s", view)
	}
	if first := app.home.items(app)[app.home.cursor]; first.session.ID != saved.ID {
		t.Fatal("the cursor must start on the first session")
	}

	app = newApp(app.rt, agent.NewSession(app.rt.Cwd), "a first prompt", true)
	app.Init()
	if app.view != viewChat {
		t.Fatal("a prompt on the command line starts in the chat")
	}

	app = newApp(app.rt, saved, "", true)
	app.Init()
	if app.view != viewChat {
		t.Fatal("-c and --resume start in the resumed session")
	}
}
