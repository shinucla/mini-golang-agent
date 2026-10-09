package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/config"
)

func headers(view string) []string {
	var out []string
	for _, l := range strings.Split(view, "\n") {
		l = strings.Trim(l, "│ ")
		for _, h := range []string{"Pinned", "Working", "Recent"} {
			if l == h || strings.HasSuffix(l, " "+h) || strings.HasPrefix(l, h+"  ") || strings.Contains(l, " "+h+"  sessions") {
				out = append(out, h)
			}
		}
	}
	return out
}

func TestWorkingSessionsReturnToTheirGroup(t *testing.T) {
	h := newHarness(t)
	gate := make(chan struct{})
	h.app.rt.RegisterProvider("fake", gatedProvider{gate: gate})
	h.app.provider, h.app.model = "fake", "m"
	dir := config.SessionsDir()
	cwd := h.app.rt.Cwd
	rec2 := saveTestSession(t, cwd, "recent two", "x")
	rec1 := saveTestSession(t, cwd, "recent one", "x")
	pinB := saveTestSession(t, cwd, "pinned b", "x")
	pinA := saveTestSession(t, cwd, "pinned a", "x")
	pinA.SetPin(dir, true, 1)
	pinB.SetPin(dir, true, 2)

	h.app.loadSession(pinB)
	h.send("slow pinned work")
	h.app.loadSession(rec1)
	h.send("slow recent work")
	h.app.loadSession(rec2)
	h.key(tea.KeyLeft)

	if got := strings.Join(headers(h.view()), ","); got != "Pinned,Working,Recent" {
		t.Fatalf("group order = %s\n%s", got, h.view())
	}
	if got := strings.Join(listNames(h.app), ","); !strings.HasPrefix(got, "pinned a,pinned b,recent one,") {
		t.Fatalf("working sessions must sit between Pinned and Recent: %s", got)
	}

	close(gate)
	h.until("both turns", func() bool { return !h.app.anyBusy() })
	if got := strings.Join(headers(h.view()), ","); got != "Pinned,Recent" {
		t.Fatalf("after the turns, groups = %s (one Recent header only)\n%s", got, h.view())
	}
	names := listNames(h.app)
	if strings.Join(names[:2], ",") != "pinned a,pinned b" {
		t.Fatalf("the pinned session must return to its pin order: %v", names)
	}
	if i, j := indexOf(names, "recent one"), indexOf(names, "recent two"); i < 0 || j < 0 || j < i {
		t.Fatalf("the recent session must return to its place in Recent: %v", names)
	}

}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}
