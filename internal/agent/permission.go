package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type Mode string

const (
	ModeDefault     Mode = "default"
	ModeAcceptEdits Mode = "acceptEdits"
	ModePlan        Mode = "plan"
	ModeAuto        Mode = "auto"
	ModeBypass      Mode = "bypassPermissions"
)

var Modes = []Mode{ModeDefault, ModeAcceptEdits, ModePlan, ModeAuto, ModeBypass}

var autoSafeRules = []string{
	"Bash(ls:*)", "Bash(pwd)", "Bash(cat:*)", "Bash(head:*)", "Bash(wc:*)", "Bash(which:*)",
	"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git branch)",
	"Bash(go build:*)", "Bash(go test:*)", "Bash(go vet:*)", "Bash(gofmt -l:*)", "Bash(go version)",
	"Bash(make build)", "Bash(make test)", "Bash(make check)", "Bash(make lint)",
	"Bash(npm test)", "Bash(npm run test:*)", "Bash(npm run lint:*)", "Bash(npm run build:*)",
	"Bash(pytest:*)", "Bash(cargo build:*)", "Bash(cargo test:*)", "Bash(cargo check:*)",
}

func ParseMode(s string) (Mode, error) {
	for _, m := range Modes {
		if strings.EqualFold(string(m), s) {
			return m, nil
		}
	}
	switch strings.ToLower(s) {
	case "", "ask":
		return ModeDefault, nil
	case "bypass", "yolo":
		return ModeBypass, nil
	case "edits", "accept":
		return ModeAcceptEdits, nil
	}
	return ModeDefault, fmt.Errorf("unknown permission mode %q (use default, acceptEdits, plan, auto, or bypassPermissions)", s)
}

func (m Mode) Next() Mode {
	i := slices.Index(Modes, m)
	return Modes[(i+1)%len(Modes)]
}

func (m Mode) Label() string {
	switch m {
	case ModeAcceptEdits:
		return "⏵⏵ accept edits on"
	case ModePlan:
		return "⏸ plan mode on"
	case ModeAuto:
		return "⏵⏵ auto mode on"
	case ModeBypass:
		return "⏵⏵ bypass permissions on"
	}
	return "⏵ default mode"
}

func (m Mode) Persistent() bool {
	return m != ModeBypass
}

type Decision int

const (
	Allow Decision = iota
	AllowAlways
	Deny
)

type ApprovalRequest struct {
	Agent       string
	Tool        string
	Summary     string
	Input       json.RawMessage
	AlwaysLabel string
	Reason      string
}

type Approver func(ctx context.Context, req ApprovalRequest) Decision

type Permissions struct {
	mu    sync.Mutex
	mode  Mode
	rules []string
}

func NewPermissions(mode Mode, rules []string) *Permissions {
	return &Permissions{mode: mode, rules: slices.Clone(rules)}
}

func (p *Permissions) Mode() Mode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mode
}

func (p *Permissions) SetMode(m Mode) {
	p.mu.Lock()
	p.mode = m
	p.mu.Unlock()
}

func (p *Permissions) Rules() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.rules)
}

type verdict int

const (
	verdictAllow verdict = iota
	verdictAsk
	verdictBlock
	verdictReview
)

func (p *Permissions) check(t tools.Tool, input json.RawMessage) (verdict, string) {
	if t.ReadOnly() {
		return verdictAllow, ""
	}
	mode := p.Mode()
	switch mode {
	case ModePlan:
		return verdictBlock, "Plan mode is on, so only read-only tools can run. Present your plan to the user and wait for approval."
	case ModeBypass:
		return verdictAllow, ""
	case ModeAcceptEdits:
		if isEditTool(t.Name()) {
			return verdictAllow, ""
		}
	}
	subject := ruleSubject(t.Name(), input)
	if matchesAny(p.Rules(), t.Name(), subject) {
		return verdictAllow, ""
	}
	if mode != ModeAuto {
		return verdictAsk, ""
	}
	if matchesAny(autoSafeRules, t.Name(), subject) {
		return verdictAllow, ""
	}
	return verdictReview, ""
}

func matchesAny(rules []string, tool, subject string) bool {
	for _, rule := range rules {
		if RuleMatches(rule, tool, subject) {
			return true
		}
	}
	return false
}

func (p *Permissions) remember(t tools.Tool, input json.RawMessage) {
	if isEditTool(t.Name()) && p.Mode() == ModeDefault {
		p.SetMode(ModeAcceptEdits)
		return
	}
	rule := suggestRule(t.Name(), input)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Contains(p.rules, rule) {
		p.rules = append(p.rules, rule)
	}
}

func AlwaysLabel(tool string, input json.RawMessage) string {
	if isEditTool(tool) {
		return "Yes, allow all edits this session"
	}
	switch tool {
	case "Bash":
		pattern, wildcard := bashRule(bashCommand(input))
		if wildcard {
			return fmt.Sprintf("Yes, and don't ask again for `%s` commands this session", pattern)
		}
		return "Yes, and don't ask again for this exact command this session"
	case "WebFetch":
		return fmt.Sprintf("Yes, and don't ask again for %s this session", tools.Host(urlOf(input)))
	}
	return fmt.Sprintf("Yes, and don't ask again for %s this session", tool)
}

func isEditTool(name string) bool { return name == "Edit" || name == "Write" }

func suggestRule(tool string, input json.RawMessage) string {
	switch tool {
	case "Bash":
		pattern, wildcard := bashRule(bashCommand(input))
		if wildcard {
			return fmt.Sprintf("Bash(%s:*)", pattern)
		}
		return fmt.Sprintf("Bash(%s)", pattern)
	case "WebFetch":
		return fmt.Sprintf("WebFetch(domain:%s)", tools.Host(urlOf(input)))
	}
	return tool
}

func ruleSubject(tool string, input json.RawMessage) string {
	switch tool {
	case "Bash":
		return strings.TrimSpace(bashCommand(input))
	case "WebFetch":
		return "domain:" + tools.Host(urlOf(input))
	}
	return ""
}

func RuleMatches(rule, tool, subject string) bool {
	name, pattern, hasPattern := strings.Cut(rule, "(")
	if strings.TrimSpace(name) != tool {
		return false
	}
	if !hasPattern {
		return true
	}
	pattern = strings.TrimSuffix(pattern, ")")
	prefix, isPrefix := strings.CutSuffix(pattern, ":*")
	if !isPrefix {
		return subject == pattern
	}
	if tool == "Bash" && hasShellOperators(subject) {
		return false
	}
	return subject == prefix || strings.HasPrefix(subject, prefix+" ")
}

func hasShellOperators(cmd string) bool {
	for _, op := range []string{"&&", "||", ";", "|", "`", "$(", ">", "<", "\n", "&"} {
		if strings.Contains(cmd, op) {
			return true
		}
	}
	return false
}

var subcommandTools = []string{
	"git", "go", "npm", "pnpm", "yarn", "cargo", "docker", "kubectl", "gh", "pip", "brew", "make", "terraform", "helm",
}

func bashRule(cmd string) (string, bool) {
	cmd = strings.TrimSpace(cmd)
	fields := strings.Fields(cmd)
	switch {
	case len(fields) == 0, hasShellOperators(cmd):
		return cmd, false
	case !slices.Contains(subcommandTools, fields[0]):
		return fields[0], true
	case len(fields) == 1, strings.HasPrefix(fields[1], "-"):
		return cmd, false
	}
	return fields[0] + " " + fields[1], true
}

func bashCommand(input json.RawMessage) string {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &in)
	return in.Command
}

func urlOf(input json.RawMessage) string {
	var in struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(input, &in)
	return in.URL
}
