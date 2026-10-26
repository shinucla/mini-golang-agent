package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/llm"
	"github.com/shinucla/mini-golang-agent/internal/tools"
)

func withPluginParts(app *App) {
	app.rt.Skills = []tools.SkillInfo{{
		Name: "acme:lint", Description: "Lint Go code", Dir: "/plugins/acme/skills/lint",
		Load: func() (string, error) { return "Run the linter.", nil },
	}}
	app.rt.Commands = []agent.UserCommand{{
		Name: "acme:greet", Description: "Say hello", ArgHint: "<name>",
		Load: func() (string, error) { return "Say hello to $ARGUMENTS.", nil },
	}}
}

func lastUserMessage(app *App) string {
	for i := len(app.history) - 1; 0 <= i; i-- {
		if app.history[i].Role == llm.RoleUser {
			return app.history[i].Content
		}
	}
	return ""
}

func TestPluginCommandsAndSkillsRunAsTurns(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", echoProvider{})
	withPluginParts(app)

	typeText(app, "/acme")
	var names []string
	for _, c := range app.suggestions() {
		names = append(names, c.name)
	}
	if !slices.Equal(names, []string{"acme:greet", "acme:lint"}) {
		t.Fatalf("the slash list offers plugin commands and skills: %v", names)
	}
	app.editInput(app.input.Reset)

	typeText(app, "/acme:greet Bob")
	press(app, tea.KeyEnter)
	if got := lastUserMessage(app); got != "Say hello to Bob." {
		t.Fatalf("the command fills in $ARGUMENTS: %q", got)
	}

	typeText(app, "/acme:lint fix main.go")
	press(app, tea.KeyEnter)
	got := lastUserMessage(app)
	if !strings.Contains(got, "Base directory for this skill: /plugins/acme/skills/lint") || !strings.Contains(got, "Run the linter.") || !strings.HasSuffix(got, "ARGUMENTS: fix main.go") {
		t.Fatalf("the skill is sent with its folder and arguments: %q", got)
	}
	if !strings.Contains(app.helpText(), "/acme:greet") {
		t.Fatal("/help lists plugin commands")
	}
}

func TestMainAgentGetsTheSkillTool(t *testing.T) {
	app := newTestApp(t)
	app.rt.RegisterProvider("fake", echoProvider{})
	ag, err := app.rt.MainAgentWith(app.env, "fake", "m")
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(ag.Tools, func(tl tools.Tool) bool { return tl.Name() == "Skill" }) || strings.Contains(ag.System, "# Skills") {
		t.Fatal("without skills there is no Skill tool and no Skills section")
	}
	withPluginParts(app)
	ag, _ = app.rt.MainAgentWith(app.env, "fake", "m")
	if !slices.ContainsFunc(ag.Tools, func(tl tools.Tool) bool { return tl.Name() == "Skill" }) || !strings.Contains(ag.System, "- acme:lint: Lint Go code") {
		t.Fatalf("the agent gets the Skill tool and the skills list:\n%s", ag.System)
	}
	out, err := tools.Skill{Skills: func() []tools.SkillInfo { return app.rt.Skills }}.Run(t.Context(), nil, []byte(`{"skill":"lint"}`))
	if err != nil || !strings.Contains(out, "Run the linter.") {
		t.Fatalf("the short name finds the skill: %q err=%v", out, err)
	}
	if _, err := (tools.Skill{Skills: func() []tools.SkillInfo { return app.rt.Skills }}).Run(t.Context(), nil, []byte(`{"skill":"nope"}`)); err == nil || !strings.Contains(err.Error(), "acme:lint") {
		t.Fatalf("an unknown skill lists the available ones: %v", err)
	}
}
