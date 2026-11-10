package plugin

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shinucla/mini-golang-agent/internal/config"
)

const (
	ScopePlugin  = "plugin"
	ScopeUser    = "user"
	ScopeProject = "project"
)

type Standalone struct {
	Skills   []Skill
	Commands []Command
}

type base struct {
	dir   string
	scope string
}

func standaloneBases(cwd string) []base {
	out := []base{{config.Home(), ScopeUser}, {config.ClaudeHome(), ScopeUser}}
	for _, dir := range config.FolderChain(cwd) {
		out = append(out, base{filepath.Join(dir, ".mga"), ScopeProject}, base{filepath.Join(dir, ".claude"), ScopeProject})
	}
	return out
}

func LoadStandalone(cwd string) (Standalone, error) {
	var out Standalone
	var errs []error
	seenDir := map[string]bool{}
	skillNames := map[string]bool{}
	commandNames := map[string]bool{}
	for _, b := range standaloneBases(cwd) {
		if seenDir[b.dir] {
			continue
		}
		seenDir[b.dir] = true
		for _, dir := range skillDirs(b.dir, []string{"skills"}) {
			s, err := readSkill(dir, "")
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if skillNames[s.Name] {
				continue
			}
			skillNames[s.Name] = true
			s.Scope = b.scope
			out.Skills = append(out.Skills, s)
		}
		for _, file := range commandFiles(filepath.Join(b.dir, "commands")) {
			c, err := readCommand(file, "")
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if commandNames[c.Name] || skillNames[c.Name] {
				continue
			}
			commandNames[c.Name] = true
			c.Scope = b.scope
			out.Commands = append(out.Commands, c)
		}
	}
	return out, errors.Join(errs...)
}

func commandFiles(root string) []string {
	var out []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") && !strings.EqualFold(d.Name(), "README.md") {
			out = append(out, path)
		}
		return nil
	})
	slices.Sort(out)
	return out
}
