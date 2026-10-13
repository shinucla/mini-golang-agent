package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/shinucla/mini-golang-agent/internal/config"
)

func TestGatewayAddress(t *testing.T) {
	cases := []struct {
		in, name, base string
		ok             bool
	}{
		{"192.168.1.20:4000", "gateway-192-168-1-20-4000", "http://192.168.1.20:4000/v1", true},
		{" 10.0.0.5:8080/ ", "gateway-10-0-0-5-8080", "http://10.0.0.5:8080/v1", true},
		{"https://llm.example.com/api/v1/", "gateway-llm-example-com", "https://llm.example.com/api/v1", true},
		{"localhost:11434", "gateway-localhost-11434", "http://localhost:11434/v1", true},
		{"", "", "", false},
		{"http://", "", "", false},
	}
	for _, c := range cases {
		name, base, ok := gatewayAddress(c.in)
		if name != c.name || base != c.base || ok != c.ok {
			t.Errorf("gatewayAddress(%q) = %q, %q, %v; want %q, %q, %v", c.in, name, base, ok, c.name, c.base, c.ok)
		}
	}
}

func TestPickerAddsGatewayWithKey(t *testing.T) {
	srv := keyServer(t, "gw-key")
	app := newKeyApp(t, config.ProviderConfig{Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", Local: true})
	view := func() string { return ansi.Strip(app.View()) }
	if !strings.Contains(view(), "a add a gateway") {
		t.Fatalf("the hint names the a key:\n%s", view())
	}

	typeText(app, "a")
	press(app, tea.KeyEnter)
	if app.picker.stage != stageGateway || !strings.Contains(view(), "Enter an address") {
		t.Fatalf("an empty address is rejected:\n%s", view())
	}
	address := strings.TrimPrefix(srv.URL, "http://")
	typeText(app, address)
	press(app, tea.KeyEnter)
	if app.picker.stage != stageKey || !strings.Contains(view(), "Connect the gateway at "+srv.URL+"/v1") {
		t.Fatalf("the key screen follows the address:\n%s", view())
	}

	typeText(app, "bad")
	press(app, tea.KeyEnter)
	if app.picker.stage != stageKey || !strings.Contains(app.picker.keyErr, "rejected") {
		t.Fatalf("a bad key is rejected: %q", app.picker.keyErr)
	}
	for range "bad" {
		app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeText(app, "gw-key")
	press(app, tea.KeyEnter)
	name := "gateway-" + strings.NewReplacer(".", "-", ":", "-").Replace(address)
	if app.picker.stage != stageModels || len(app.picker.models) != 1 || !strings.Contains(view(), "Gateway saved as "+name) {
		t.Fatalf("the models show after a good key: stage=%v err=%q\n%s", app.picker.stage, app.picker.keyErr, view())
	}
	data, _ := os.ReadFile(app.rt.Cfg.Path())
	if !strings.Contains(string(data), `"`+name+`"`) || !strings.Contains(string(data), srv.URL+"/v1") || !strings.Contains(string(data), `"api_key": "gw-key"`) {
		t.Fatalf("the gateway is saved: %s", data)
	}

	press(app, tea.KeyEnter)
	if p, m := app.rt.Current(); p != name || m != "m1" {
		t.Fatalf("current = %s:%s", p, m)
	}
	app.openPicker()
	if !strings.Contains(view(), name) {
		t.Fatalf("the gateway is in the provider list:\n%s", view())
	}
}

func TestPickerAddsGatewayWithoutKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"id":"open-model"}]}`)
	}))
	t.Cleanup(srv.Close)
	app := newKeyApp(t, config.ProviderConfig{Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", Local: true})
	typeText(app, "a")
	typeText(app, srv.URL)
	press(app, tea.KeyEnter)
	press(app, tea.KeyEnter)
	if app.picker.stage != stageModels || len(app.picker.models) != 1 {
		t.Fatalf("an empty key works for an open gateway: stage=%v err=%q", app.picker.stage, app.picker.keyErr)
	}
	name, _, _ := gatewayAddress(srv.URL)
	if pc, _ := app.rt.ProviderConfig(name); !pc.Configured() || pc.APIKey != "" {
		t.Fatalf("the open gateway counts as set up without a key: %+v", pc)
	}
}

func TestPickerReportsUnreachableGateway(t *testing.T) {
	app := newKeyApp(t, config.ProviderConfig{Type: config.TypeOpenAI, BaseURL: "http://127.0.0.1:1", Local: true})
	typeText(app, "a")
	typeText(app, "127.0.0.1:1")
	press(app, tea.KeyEnter)
	typeText(app, "k")
	press(app, tea.KeyEnter)
	if app.picker.stage != stageKey || !strings.Contains(app.picker.keyErr, "Could not reach the gateway") {
		t.Fatalf("an unreachable gateway says so: %q", app.picker.keyErr)
	}
	press(app, tea.KeyEsc)
	if _, ok := app.rt.ProviderConfig("gateway-127-0-0-1-1"); ok {
		t.Fatal("a gateway is saved only after a successful check")
	}
}
