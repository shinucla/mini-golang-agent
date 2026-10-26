package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shinucla/mini-golang-agent/internal/agentdef"
	"github.com/shinucla/mini-golang-agent/internal/mcp"
)

type Skill struct {
	Name        string
	Description string
	Dir         string
	File        string
}

type Command struct {
	Name        string
	Description string
	ArgHint     string
	File        string
}

type Info struct {
	ID       string
	Path     string
	Version  string
	Enabled  bool
	Skills   []string
	Commands []string
	Agents   []string
	MCP      []string
	Hooks    bool
	Err      error
}

type Loaded struct {
	Plugins  []Info
	Skills   []Skill
	Commands []Command
	Agents   []agentdef.Definition
	MCP      map[string]mcp.ServerConfig
}

func LoadEnabled() (Loaded, error) {
	return load(true)
}

func LoadAll() (Loaded, error) {
	return load(false)
}

func load(enabledOnly bool) (Loaded, error) {
	out := Loaded{MCP: map[string]mcp.ServerConfig{}}
	installed, err := InstalledPlugins()
	if err != nil {
		return out, err
	}
	markets, _ := Marketplaces()
	for _, id := range slices.Sorted(maps.Keys(installed)) {
		rec := installed[id]
		if enabledOnly && !rec.Enabled {
			continue
		}
		info := Info{ID: id, Path: rec.Path, Version: rec.Version, Enabled: rec.Enabled}
		p, err := loadPlugin(id, rec.Path, entryFor(markets, id))
		info.Err = err
		for _, s := range p.Skills {
			info.Skills = append(info.Skills, s.Name)
		}
		for _, c := range p.Commands {
			info.Commands = append(info.Commands, c.Name)
		}
		for _, a := range p.Agents {
			info.Agents = append(info.Agents, a.Name)
		}
		for _, name := range slices.Sorted(maps.Keys(p.MCP)) {
			info.MCP = append(info.MCP, name)
			key := name
			if _, taken := out.MCP[key]; taken {
				pluginName, _ := SplitID(id)
				key = safeName(pluginName + "-" + name)
			}
			if rec.Enabled {
				out.MCP[key] = p.MCP[name]
			}
		}
		info.Hooks = p.hooks
		out.Plugins = append(out.Plugins, info)
		if rec.Enabled {
			out.Skills = append(out.Skills, p.Skills...)
			out.Commands = append(out.Commands, p.Commands...)
			out.Agents = append(out.Agents, p.Agents...)
		}
	}
	return out, nil
}

func entryFor(markets map[string]Marketplace, id string) *Entry {
	name, market := SplitID(id)
	mk, ok := markets[market]
	if !ok {
		return nil
	}
	m, err := readManifest(mk.Dir)
	if err != nil {
		return nil
	}
	for _, e := range m.Plugins {
		if e.Name == name {
			return &e
		}
	}
	return nil
}

type pluginParts struct {
	Skills   []Skill
	Commands []Command
	Agents   []agentdef.Definition
	MCP      map[string]mcp.ServerConfig
	hooks    bool
}

func loadPlugin(id, root string, entry *Entry) (pluginParts, error) {
	parts := pluginParts{MCP: map[string]mcp.ServerConfig{}}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return parts, fmt.Errorf("plugin folder %s is missing; run: mga plugin update %s", root, id)
	}
	prefix, _ := SplitID(id)
	pj := readPluginJSON(root)
	var errs []error

	skillPaths := []string{"skills"}
	commandPaths := []string{"commands"}
	agentPaths := []string{"agents"}
	if entry != nil {
		skillPaths = append(skillPaths, entry.Skills...)
		commandPaths = append(commandPaths, paths(entry.Commands)...)
		agentPaths = append(agentPaths, paths(entry.Agents)...)
	}
	skillPaths = append(skillPaths, paths(pj.Skills)...)
	commandPaths = append(commandPaths, paths(pj.Commands)...)
	agentPaths = append(agentPaths, paths(pj.Agents)...)

	seen := map[string]bool{}
	for _, dir := range skillDirs(root, skillPaths) {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		file := filepath.Join(dir, "SKILL.md")
		data, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fm, _ := frontmatter(data)
		name := fm.Name
		if name == "" {
			name = filepath.Base(dir)
		}
		parts.Skills = append(parts.Skills, Skill{Name: prefix + ":" + name, Description: fm.Description, Dir: dir, File: file})
	}

	seen = map[string]bool{}
	for _, file := range markdownFiles(root, commandPaths) {
		if seen[file] {
			continue
		}
		seen[file] = true
		data, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fm, _ := frontmatter(data)
		name := strings.TrimSuffix(filepath.Base(file), ".md")
		parts.Commands = append(parts.Commands, Command{Name: prefix + ":" + name, Description: fm.Description, ArgHint: fm.ArgumentHint, File: file})
	}

	seen = map[string]bool{}
	for _, file := range markdownFiles(root, agentPaths) {
		if seen[file] {
			continue
		}
		seen[file] = true
		data, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		d, err := agentdef.Parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", file, err))
			continue
		}
		d.Name = prefix + ":" + d.Name
		d.Scope = agentdef.ScopePlugin
		d.Path = file
		parts.Agents = append(parts.Agents, d)
	}

	servers, err := mcpServers(root, pj.MCPServers)
	if err != nil {
		errs = append(errs, err)
	}
	for name, cfg := range servers {
		parts.MCP[name] = withRoot(cfg, root)
	}

	_, hookErr := os.Stat(filepath.Join(root, "hooks", "hooks.json"))
	parts.hooks = hookErr == nil || 0 < len(pj.Hooks)
	return parts, errors.Join(errs...)
}

func paths(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func inside(root, rel string) (string, bool) {
	path := filepath.Join(root, filepath.Clean(rel))
	r, err := filepath.Rel(root, path)
	return path, err == nil && !strings.HasPrefix(r, "..")
}

func skillDirs(root string, rels []string) []string {
	var out []string
	for _, rel := range rels {
		dir, ok := inside(root, rel)
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
			out = append(out, dir)
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			sub := filepath.Join(dir, e.Name())
			if _, err := os.Stat(filepath.Join(sub, "SKILL.md")); e.IsDir() && err == nil {
				out = append(out, sub)
			}
		}
	}
	return out
}

func markdownFiles(root string, rels []string) []string {
	var out []string
	for _, rel := range rels {
		path, ok := inside(root, rel)
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if strings.HasSuffix(path, ".md") {
				out = append(out, path)
			}
			continue
		}
		entries, _ := os.ReadDir(path)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && !strings.EqualFold(e.Name(), "README.md") {
				out = append(out, filepath.Join(path, e.Name()))
			}
		}
	}
	return out
}

func mcpServers(root string, raw json.RawMessage) (map[string]mcp.ServerConfig, error) {
	out := map[string]mcp.ServerConfig{}
	fromFile, err := mcp.ReadServers(filepath.Join(root, ".mcp.json"))
	if err != nil {
		return out, err
	}
	maps.Copy(out, fromFile)
	if len(raw) == 0 {
		return out, nil
	}
	var inline map[string]mcp.ServerConfig
	if json.Unmarshal(raw, &inline) == nil {
		maps.Copy(out, inline)
		return out, nil
	}
	for _, rel := range paths(raw) {
		path, ok := inside(root, rel)
		if !ok {
			continue
		}
		servers, err := mcp.ReadServers(path)
		if err != nil {
			return out, err
		}
		maps.Copy(out, servers)
	}
	return out, nil
}

func withRoot(cfg mcp.ServerConfig, root string) mcp.ServerConfig {
	r := strings.NewReplacer("${CLAUDE_PLUGIN_ROOT}", root, "${MGA_PLUGIN_ROOT}", root)
	cfg.Command = r.Replace(cfg.Command)
	cfg.URL = r.Replace(cfg.URL)
	args := make([]string, len(cfg.Args))
	for i, a := range cfg.Args {
		args[i] = r.Replace(a)
	}
	cfg.Args = args
	if cfg.Env != nil {
		env := map[string]string{}
		for k, v := range cfg.Env {
			env[k] = r.Replace(v)
		}
		cfg.Env = env
	}
	if cfg.Command != "" && !filepath.IsAbs(cfg.Command) && strings.Contains(cfg.Command, "/") {
		cfg.Command = filepath.Join(root, cfg.Command)
	}
	return cfg
}

type front struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	ArgumentHint string `yaml:"argument-hint"`
}

func frontmatter(data []byte) (front, string) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return front{}, text
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return front{}, text
	}
	body = strings.TrimPrefix(body, "\n")
	var fm front
	if yaml.Unmarshal([]byte(head), &fm) != nil {
		fm = front{}
		for _, line := range strings.Split(head, "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok || strings.HasPrefix(line, " ") {
				continue
			}
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			switch strings.TrimSpace(key) {
			case "name":
				fm.Name = value
			case "description":
				fm.Description = value
			case "argument-hint":
				fm.ArgumentHint = value
			}
		}
	}
	return fm, strings.TrimLeft(body, "\n")
}

func Body(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	_, body := frontmatter(data)
	return strings.TrimSpace(body), nil
}
