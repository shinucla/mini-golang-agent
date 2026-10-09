package tui

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/config"
)

func TestResumedSessionMovesToLaunchFolder(t *testing.T) {
	app := newTestApp(t)
	parent := app.rt.Cwd
	child := filepath.Join(parent, "projects", "child")
	s := saveTestSession(t, child, "child work", "fix the child build")

	resumed, err := agent.LoadSession(config.SessionsDir(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	app = newApp(app.rt, resumed, "", true)
	app.saveSession()

	inParent, _ := agent.ListSessions(config.SessionsDir(), parent)
	if !slices.ContainsFunc(inParent, func(x *agent.Session) bool { return x.ID == s.ID }) {
		t.Fatal("a session resumed from an ancestor folder must show up in that folder's list")
	}
	if reloaded, _ := agent.LoadSession(config.SessionsDir(), s.ID); reloaded.Cwd != parent || len(reloaded.Messages) != 2 {
		t.Fatalf("saved session = cwd %q, %d messages", reloaded.Cwd, len(reloaded.Messages))
	}
}
