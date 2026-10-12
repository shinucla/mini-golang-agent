package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/config"
)

func TestPickerDeletesSavedKeyAfterConfirm(t *testing.T) {
	t.Setenv("ACME_TEST_KEY", "")
	app := newKeyApp(t, config.ProviderConfig{Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", APIKeyEnv: "ACME_TEST_KEY"})
	if err := app.rt.SetProviderKey("acme", "old-key"); err != nil {
		t.Fatal(err)
	}
	view := func() string { return ansi.Strip(app.View()) }
	if !strings.Contains(view(), "d delete saved key") {
		t.Fatalf("the hint names the d key:\n%s", view())
	}

	typeText(app, "d")
	if pc, _ := app.rt.ProviderConfig("acme"); pc.Key() != "old-key" || !strings.Contains(view(), "Press d again") {
		t.Fatalf("the first d only asks:\n%s", view())
	}
	typeText(app, "x")
	typeText(app, "d")
	if pc, _ := app.rt.ProviderConfig("acme"); pc.Key() != "old-key" {
		t.Fatal("another key between the two presses cancels the delete")
	}

	typeText(app, "d")
	if pc, _ := app.rt.ProviderConfig("acme"); pc.Key() != "" {
		t.Fatalf("the second d deletes the key: %q", pc.Key())
	}
	if data, _ := os.ReadFile(app.rt.Cfg.Path()); strings.Contains(string(data), "old-key") {
		t.Fatalf("the key is gone from the config file: %s", data)
	}
	if !strings.Contains(view(), "Deleted the key for acme") || !strings.Contains(view(), "✗ no key") {
		t.Fatalf("the list shows the deleted key:\n%s", view())
	}

	typeText(app, "d")
	if !strings.Contains(view(), "mga has no saved key for acme.") {
		t.Fatalf("a provider without a saved key says so:\n%s", view())
	}
}

func TestPickerDeleteNamesTheEnvironmentKey(t *testing.T) {
	t.Setenv("ACME_TEST_KEY", "env-key")
	app := newKeyApp(t, config.ProviderConfig{Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", APIKeyEnv: "ACME_TEST_KEY"})
	typeText(app, "d")
	if !strings.Contains(ansi.Strip(app.View()), "The key comes from $ACME_TEST_KEY") {
		t.Fatalf("an environment key cannot be deleted here:\n%s", ansi.Strip(app.View()))
	}

	if err := app.rt.SetProviderKey("acme", "saved-key"); err != nil {
		t.Fatal(err)
	}
	typeText(app, "d")
	typeText(app, "d")
	if !strings.Contains(ansi.Strip(app.View()), "$ACME_TEST_KEY is still set") {
		t.Fatalf("after a delete, the note names the environment key:\n%s", ansi.Strip(app.View()))
	}
}
