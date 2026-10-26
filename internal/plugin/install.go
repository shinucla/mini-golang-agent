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
	"time"
)

type Available struct {
	ID          string
	Description string
	Installed   bool
	Enabled     bool
}

func AvailablePlugins() ([]Available, error) {
	all, err := Marketplaces()
	if err != nil {
		return nil, err
	}
	installed, err := InstalledPlugins()
	if err != nil {
		return nil, err
	}
	var out []Available
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(all)) {
		m, err := readManifest(all[name].Dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		for _, e := range m.Plugins {
			id := e.Name + "@" + name
			rec, ok := installed[id]
			out = append(out, Available{ID: id, Description: e.Description, Installed: ok, Enabled: ok && rec.Enabled})
		}
	}
	return out, errors.Join(errs...)
}

func findEntry(id string) (Marketplace, Entry, string, error) {
	name, market := SplitID(id)
	if market != "" {
		mk, m, err := ReadMarketplace(market)
		if err != nil {
			return mk, Entry{}, "", err
		}
		for _, e := range m.Plugins {
			if e.Name == name {
				return mk, e, name + "@" + market, nil
			}
		}
		return mk, Entry{}, "", fmt.Errorf("marketplace %q has no plugin %q", market, name)
	}
	avail, err := AvailablePlugins()
	if err != nil && len(avail) == 0 {
		return Marketplace{}, Entry{}, "", err
	}
	var matches []string
	for _, a := range avail {
		if n, _ := SplitID(a.ID); n == name {
			matches = append(matches, a.ID)
		}
	}
	switch len(matches) {
	case 0:
		return Marketplace{}, Entry{}, "", fmt.Errorf("no marketplace has a plugin %q; see: mga plugin list --available", name)
	case 1:
		return findEntry(matches[0])
	}
	return Marketplace{}, Entry{}, "", fmt.Errorf("plugin %q is in several marketplaces; use one of: %s", name, strings.Join(matches, ", "))
}

func cacheDir(id string) string {
	name, market := SplitID(id)
	return filepath.Join(Root(), "cache", safeName(market), safeName(name))
}

func resolveSource(mk Marketplace, e Entry, id string) (string, string, error) {
	src := e.Source
	if src.Path != "" {
		path := filepath.Join(mk.Dir, filepath.Clean(src.Path))
		if rel, err := filepath.Rel(mk.Dir, path); err != nil || strings.HasPrefix(rel, "..") {
			return "", "", fmt.Errorf("plugin source %q is outside the marketplace", src.Path)
		}
		return path, headCommit(mk.Dir), nil
	}
	var url string
	var ok bool
	switch src.Kind {
	case "github":
		url, ok = gitURL(src.Repo)
	case "git", "url", "git-subdir":
		url, ok = gitURL(src.URL)
	default:
		return "", "", fmt.Errorf("plugin source type %q is not supported", src.Kind)
	}
	if !ok {
		return "", "", fmt.Errorf("plugin source %q is not a git URL", src.URL+src.Repo)
	}
	dir := cacheDir(id)
	if err := os.RemoveAll(dir); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", "", err
	}
	args := []string{"clone", "--depth", "1"}
	if src.Ref != "" {
		args = append(args, "--branch", src.Ref)
	}
	if _, err := git(append(args, url, dir)...); err != nil {
		return "", "", err
	}
	path := dir
	if src.Kind == "git-subdir" && src.Sub != "" {
		path = filepath.Join(dir, filepath.Clean(src.Sub))
	}
	return path, headCommit(dir), nil
}

type pluginJSON struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Skills      json.RawMessage `json:"skills"`
	Commands    json.RawMessage `json:"commands"`
	Agents      json.RawMessage `json:"agents"`
	MCPServers  json.RawMessage `json:"mcpServers"`
	Hooks       json.RawMessage `json:"hooks"`
}

func readPluginJSON(root string) pluginJSON {
	var p pluginJSON
	data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if err == nil {
		_ = json.Unmarshal(data, &p)
	}
	return p
}

func Install(id string) (Installed, error) {
	mk, entry, full, err := findEntry(id)
	if err != nil {
		return Installed{}, err
	}
	path, commit, err := resolveSource(mk, entry, full)
	if err != nil {
		return Installed{}, err
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return Installed{}, fmt.Errorf("plugin folder %s does not exist", path)
	}
	version := readPluginJSON(path).Version
	if version == "" {
		version = entry.Version
	}
	if version == "" && 7 <= len(commit) {
		version = commit[:7]
	}
	all, err := InstalledPlugins()
	if err != nil {
		return Installed{}, err
	}
	now := time.Now()
	rec := Installed{ID: full, Path: path, Enabled: true, Version: version, Commit: commit, InstalledAt: now, UpdatedAt: now}
	if old, ok := all[full]; ok {
		rec.InstalledAt, rec.Enabled = old.InstalledAt, old.Enabled
	}
	all[full] = rec
	return rec, writeJSON(installedFile(), all)
}

func Uninstall(id string) error {
	all, err := InstalledPlugins()
	if err != nil {
		return err
	}
	full, err := installedID(all, id)
	if err != nil {
		return err
	}
	delete(all, full)
	if err := os.RemoveAll(cacheDir(full)); err != nil {
		return err
	}
	return writeJSON(installedFile(), all)
}

func SetEnabled(id string, enabled bool) (string, error) {
	all, err := InstalledPlugins()
	if err != nil {
		return "", err
	}
	full, err := installedID(all, id)
	if err != nil {
		return "", err
	}
	rec := all[full]
	rec.Enabled = enabled
	all[full] = rec
	return full, writeJSON(installedFile(), all)
}

func Update(id string) (Installed, error) {
	all, err := InstalledPlugins()
	if err != nil {
		return Installed{}, err
	}
	full, err := installedID(all, id)
	if err != nil {
		return Installed{}, err
	}
	_, market := SplitID(full)
	if err := UpdateMarketplace(market); err != nil {
		return Installed{}, err
	}
	return Install(full)
}

func installedID(all map[string]Installed, id string) (string, error) {
	if _, ok := all[id]; ok {
		return id, nil
	}
	var matches []string
	for full := range all {
		if name, _ := SplitID(full); name == id {
			matches = append(matches, full)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("plugin %q is not installed", id)
	case 1:
		return matches[0], nil
	}
	slices.Sort(matches)
	return "", fmt.Errorf("plugin %q matches several installs; use one of: %s", id, strings.Join(matches, ", "))
}
