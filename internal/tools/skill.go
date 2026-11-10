package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type SkillInfo struct {
	Name        string
	Description string
	ArgHint     string
	Dir         string
	Scope       string
	UserOnly    bool
	ModelOnly   bool
	Load        func() (string, error)
}

func ModelSkills(all []SkillInfo) []SkillInfo {
	var out []SkillInfo
	for _, s := range all {
		if !s.UserOnly {
			out = append(out, s)
		}
	}
	return out
}

type Skill struct {
	Skills func() []SkillInfo
}

func (Skill) Name() string   { return "Skill" }
func (Skill) ReadOnly() bool { return true }

func (Skill) Description() string {
	return "Load a skill: expert instructions for one kind of work. When the user's request matches a skill in the Skills list of the system prompt, call this tool with the skill's name before you start, then follow the instructions it returns."
}

func (Skill) Schema() map[string]any {
	return schema(map[string]any{
		"skill": str("The skill name, as the Skills list gives it (for example plugin:skill-name)"),
		"args":  str("Optional arguments for the skill"),
	}, "skill")
}

func (Skill) Summary(input json.RawMessage) string { return field(input, "skill") }

func (s Skill) Run(_ context.Context, _ *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		Skill string `json:"skill"`
		Args  string `json:"args"`
	}](input)
	if err != nil {
		return "", err
	}
	var all []SkillInfo
	if s.Skills != nil {
		all = ModelSkills(s.Skills())
	}
	info, err := FindSkill(all, in.Skill)
	if err != nil {
		return "", err
	}
	return ExpandSkill(info, in.Args)
}

func FindSkill(all []SkillInfo, name string) (SkillInfo, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	var matches []SkillInfo
	for _, s := range all {
		if s.Name == name {
			return s, nil
		}
		if _, short, ok := strings.Cut(s.Name, ":"); ok && short == name {
			matches = append(matches, s)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	names := make([]string, 0, len(all))
	for _, s := range all {
		names = append(names, s.Name)
	}
	if len(matches) == 0 {
		return SkillInfo{}, fmt.Errorf("unknown skill %q; available skills: %s", name, strings.Join(names, ", "))
	}
	return SkillInfo{}, fmt.Errorf("skill %q exists in several places; use the full name", name)
}

func ExpandSkill(info SkillInfo, args string) (string, error) {
	body, err := info.Load()
	if err != nil {
		return "", fmt.Errorf("load skill %s: %w", info.Name, err)
	}
	body = strings.NewReplacer("${CLAUDE_SKILL_DIR}", info.Dir, "${MGA_SKILL_DIR}", info.Dir).Replace(body)
	return fmt.Sprintf("Base directory for this skill: %s\n\n%s", info.Dir, ExpandArgs(body, args)), nil
}

var positionalArg = regexp.MustCompile(`\$([1-9])`)

func ExpandArgs(body, args string) string {
	args = strings.TrimSpace(args)
	words := strings.Fields(args)
	usedArgs := strings.Contains(body, "$ARGUMENTS") || positionalArg.MatchString(body)
	body = strings.ReplaceAll(body, "$ARGUMENTS", args)
	body = positionalArg.ReplaceAllStringFunc(body, func(m string) string {
		i := int(m[1] - '1')
		if i < len(words) {
			return words[i]
		}
		return ""
	})
	if !usedArgs && args != "" {
		body += "\n\nARGUMENTS: " + args
	}
	return body
}
