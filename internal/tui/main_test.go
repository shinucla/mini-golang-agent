package tui

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	syncEnabled = false
	predictEnabled = false
	claudeHome, err := os.MkdirTemp("", "mga-claude-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	code := m.Run()
	os.RemoveAll(claudeHome)
	os.Exit(code)
}
