package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestUserMessageIsAShadedBlock(t *testing.T) {
	if _, ok := styleUser.GetBackground().(lipgloss.AdaptiveColor); !ok {
		t.Fatal("user lines have a background that follows the dark or light theme")
	}

	out := strings.TrimPrefix(formatUser("fix the login bug\nthen run the tests", 80), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	first, second := ansi.Strip(lines[0]), ansi.Strip(lines[1])
	if ansi.StringWidth(first) != 79 || ansi.StringWidth(second) != 79 {
		t.Fatalf("the block covers the whole line but the last column: %d %d", ansi.StringWidth(first), ansi.StringWidth(second))
	}
	if strings.TrimRight(first, " ") != "> fix the login bug" || strings.TrimRight(second, " ") != "  then run the tests" {
		t.Fatalf("lines = %q %q", first, second)
	}

	long := formatUser(strings.Repeat("word ", 40), 40)
	for _, l := range strings.Split(strings.TrimPrefix(long, "\n"), "\n") {
		if 40 <= ansi.StringWidth(l) {
			t.Fatalf("a long message wraps inside the width: %q", ansi.Strip(l))
		}
	}
	if !strings.Contains(ansi.Strip(long), "word word") || strings.Count(ansi.Strip(long), "word") != 40 {
		t.Fatal("wrapping keeps every word")
	}
}
