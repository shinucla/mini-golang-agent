package tui

import "testing"

func TestShortPath(t *testing.T) {
	cases := []struct {
		path  string
		width int
		want  string
	}{
		{"cwd: ./", 30, "cwd: ./"},
		{"ring/web", 30, "ring/web"},
		{"services/billing/internal/handlers/v2", 30, "services/.../v2"},
		{"services/billing/internal/handlers/v2", 37, "services/billing/internal/handlers/v2"},
		{"a-very-long-folder-name/b/c/another-long-leaf-name", 30, "a-very-long-folder-name/.../a…"},
		{"a-very-long-folder-name-of-forty-letters/leaf", 20, "a-very-long-folder-…"},
	}
	for _, c := range cases {
		if got := shortPath(c.path, c.width); got != c.want {
			t.Errorf("shortPath(%q, %d) = %q, want %q", c.path, c.width, got, c.want)
		}
	}
}
