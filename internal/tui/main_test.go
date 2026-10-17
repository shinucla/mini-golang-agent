package tui

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	syncEnabled = false
	os.Exit(m.Run())
}
