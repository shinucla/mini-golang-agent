package agentdef

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type Scope string

const (
	ScopeBuiltin Scope = "built-in"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type Definition struct {
	Name        string
	Description string
	Model       string
	Tools       []string
	Color       string
	Prompt      string
	Scope       Scope
	Path        string
}

func (d Definition) ModelLabel() string {
	if d.Model == "" {
		return "inherit"
	}
	return d.Model
}

func (d Definition) ToolsLabel() string {
	if len(d.Tools) == 0 {
		return "all tools"
	}
	return strings.Join(d.Tools, ", ")
}

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("name %q must use letters, digits, '-' or '_'", name)
	}
	return nil
}

type Store struct {
	UserDir    string
	ProjectDir string
}

func (s *Store) List() ([]Definition, error) {
	byName := map[string]Definition{}
	for _, d := range builtins() {
		byName[d.Name] = d
	}
	var errs []error
	for _, src := range []struct {
		dir   string
		scope Scope
	}{{s.UserDir, ScopeUser}, {s.ProjectDir, ScopeProject}} {
		defs, err := loadDir(src.dir, src.scope)
		errs = append(errs, err)
		for _, d := range defs {
			byName[d.Name] = d
		}
	}
	defs := make([]Definition, 0, len(byName))
	for _, d := range byName {
		defs = append(defs, d)
	}
	slices.SortFunc(defs, func(a, b Definition) int {
		ab, bb := a.Scope == ScopeBuiltin, b.Scope == ScopeBuiltin
		if ab != bb {
			if ab {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return defs, errors.Join(errs...)
}

func (s *Store) Get(name string) (Definition, bool) {
	defs, _ := s.List()
	for _, d := range defs {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return Definition{}, false
}

func (s *Store) dir(scope Scope) (string, error) {
	switch scope {
	case ScopeUser:
		return s.UserDir, nil
	case ScopeProject:
		return s.ProjectDir, nil
	}
	return "", fmt.Errorf("cannot save to scope %q", scope)
}

func (s *Store) Save(d Definition, previousPath string) (Definition, error) {
	if err := ValidateName(d.Name); err != nil {
		return d, err
	}
	if strings.TrimSpace(d.Description) == "" {
		return d, errors.New("description is required")
	}
	dir, err := s.dir(d.Scope)
	if err != nil {
		return d, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return d, err
	}
	d.Path = filepath.Join(dir, d.Name+".md")
	if err := os.WriteFile(d.Path, Format(d), 0o644); err != nil {
		return d, err
	}
	if previousPath != "" && previousPath != d.Path {
		_ = os.Remove(previousPath)
	}
	return d, nil
}

func (s *Store) Delete(d Definition) error {
	if d.Scope == ScopeBuiltin {
		return errors.New("built-in agents cannot be deleted")
	}
	return os.Remove(d.Path)
}

func (s *Store) Draft(name string, scope Scope) (string, error) {
	d := Definition{Name: name, Description: "Describe when to use this agent", Scope: scope, Prompt: "You are a specialist agent. Describe the job here."}
	saved, err := s.Save(d, "")
	return saved.Path, err
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Model       string `yaml:"model,omitempty"`
	Tools       any    `yaml:"tools,omitempty"`
	Color       string `yaml:"color,omitempty"`
}

func Parse(data []byte) (Definition, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return Definition{}, errors.New("missing frontmatter (the file must start with ---)")
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return Definition{}, errors.New("frontmatter has no closing ---")
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(head), &fm); err != nil {
		return Definition{}, fmt.Errorf("frontmatter: %w", err)
	}
	if fm.Name == "" {
		return Definition{}, errors.New("frontmatter needs a name")
	}
	body = strings.TrimPrefix(body, "\n")
	model := strings.TrimSpace(fm.Model)
	if model == "inherit" {
		model = ""
	}
	return Definition{
		Name:        fm.Name,
		Description: fm.Description,
		Model:       model,
		Tools:       ParseTools(fm.Tools),
		Color:       fm.Color,
		Prompt:      strings.TrimSpace(body),
	}, nil
}

func ParseTools(v any) []string {
	var raw []string
	switch t := v.(type) {
	case string:
		raw = strings.Split(t, ",")
	case []any:
		for _, item := range t {
			raw = append(raw, fmt.Sprint(item))
		}
	}
	var out []string
	for _, r := range raw {
		if r = strings.TrimSpace(r); r != "" && r != "*" {
			out = append(out, r)
		}
	}
	return out
}

func Format(d Definition) []byte {
	fm := frontmatter{Name: d.Name, Description: d.Description, Model: d.Model, Color: d.Color}
	if len(d.Tools) != 0 {
		fm.Tools = strings.Join(d.Tools, ", ")
	}
	head, _ := yaml.Marshal(fm)
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(head)
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(d.Prompt))
	b.WriteString("\n")
	return b.Bytes()
}

func loadDir(dir string, scope Scope) ([]Definition, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var defs []Definition
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		d, err := Parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		d.Scope = scope
		d.Path = path
		defs = append(defs, d)
	}
	return defs, errors.Join(errs...)
}

func builtins() []Definition {
	return []Definition{
		{
			Name:        "general-purpose",
			Description: "General-purpose agent for research, code search, and multi-step tasks. Use it when a search may need several attempts.",
			Scope:       ScopeBuiltin,
			Prompt: "You are a general-purpose sub-agent. Complete the task you get from start to end. " +
				"Search broadly first, then narrow down. Read the files that matter. " +
				"When you finish, reply with one clear, complete report. Include file paths with line numbers for code you mention.",
		},
		{
			Name:        "Explore",
			Description: "Fast read-only agent that finds files, code, and facts in the codebase. It cannot change files.",
			Tools:       []string{"Read", "Glob", "Grep"},
			Scope:       ScopeBuiltin,
			Prompt: "You are a read-only search agent. Find what the task asks for with Glob, Grep, and Read. " +
				"Do not guess. Report the facts you found with file paths and line numbers. Keep the report short.",
		},
		{
			Name:        "Plan",
			Description: "Software architect agent that designs an implementation plan. It reads the code but does not change it.",
			Tools:       []string{"Read", "Glob", "Grep"},
			Scope:       ScopeBuiltin,
			Prompt: "You are a software architect. Read the relevant code, then write a step-by-step implementation plan. " +
				"Name the files to change, the approach, the risks, and how to verify the result. Do not change files.",
		},
	}
}
