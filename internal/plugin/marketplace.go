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

type PluginSource struct {
	Path string
	Kind string `json:"source"`
	Repo string `json:"repo"`
	URL  string `json:"url"`
	Sub  string `json:"path"`
	Ref  string `json:"ref"`
}

func (s *PluginSource) UnmarshalJSON(data []byte) error {
	var path string
	if json.Unmarshal(data, &path) == nil {
		s.Path = path
		return nil
	}
	type plain PluginSource
	return json.Unmarshal(data, (*plain)(s))
}

type Entry struct {
	Name           string          `json:"name"`
	Source         PluginSource    `json:"source"`
	Description    string          `json:"description"`
	Version        string          `json:"version"`
	Skills         []string        `json:"skills"`
	Commands       json.RawMessage `json:"commands"`
	Agents         json.RawMessage `json:"agents"`
	MCPServers     json.RawMessage `json:"mcpServers"`
	DefaultEnabled *bool           `json:"defaultEnabled"`
}

type Manifest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Plugins     []Entry `json:"plugins"`
}

func readManifest(dir string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, fmt.Errorf("%s has no .claude-plugin/marketplace.json", dir)
		}
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("marketplace.json: %w", err)
	}
	if m.Name == "" {
		return m, errors.New("marketplace.json has no name")
	}
	return m, nil
}

func ReadMarketplace(name string) (Marketplace, Manifest, error) {
	all, err := Marketplaces()
	if err != nil {
		return Marketplace{}, Manifest{}, err
	}
	mk, ok := all[name]
	if !ok {
		return Marketplace{}, Manifest{}, fmt.Errorf("unknown marketplace %q; add it with: mga plugin marketplace add <git url>", name)
	}
	m, err := readManifest(mk.Dir)
	return mk, m, err
}

func AddMarketplace(source string) (Marketplace, error) {
	source = strings.TrimSpace(source)
	mk := Marketplace{Source: source, Updated: time.Now()}
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		dir, err := filepath.Abs(source)
		if err != nil {
			return mk, err
		}
		m, err := readManifest(dir)
		if err != nil {
			return mk, err
		}
		mk.Name, mk.Dir = m.Name, dir
		return mk, saveMarketplace(mk)
	}
	url, ok := gitURL(source)
	if !ok {
		return mk, fmt.Errorf("%q is not a git URL, owner/repo, or folder", source)
	}
	if err := os.MkdirAll(Root(), 0o755); err != nil {
		return mk, err
	}
	staging, err := os.MkdirTemp(Root(), "clone-")
	if err != nil {
		return mk, err
	}
	defer os.RemoveAll(staging)
	repo := filepath.Join(staging, "repo")
	if _, err := git("clone", "--depth", "1", url, repo); err != nil {
		return mk, err
	}
	m, err := readManifest(repo)
	if err != nil {
		return mk, err
	}
	all, err := Marketplaces()
	if err != nil {
		return mk, err
	}
	if old, ok := all[m.Name]; ok && old.URL != url {
		return mk, fmt.Errorf("a marketplace named %q already exists from %s; remove it first", m.Name, old.Source)
	}
	dir := filepath.Join(Root(), "marketplaces", safeName(m.Name))
	if err := os.RemoveAll(dir); err != nil {
		return mk, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return mk, err
	}
	if err := os.Rename(repo, dir); err != nil {
		return mk, err
	}
	mk.Name, mk.URL, mk.Dir = m.Name, url, dir
	return mk, saveMarketplace(mk)
}

func saveMarketplace(mk Marketplace) error {
	all, err := Marketplaces()
	if err != nil {
		return err
	}
	all[mk.Name] = mk
	return writeJSON(marketplacesFile(), all)
}

func UpdateMarketplace(name string) error {
	all, err := Marketplaces()
	if err != nil {
		return err
	}
	mk, ok := all[name]
	if !ok {
		return fmt.Errorf("unknown marketplace %q", name)
	}
	if mk.URL != "" {
		if _, err := git("-C", mk.Dir, "pull", "--ff-only"); err != nil {
			return err
		}
	}
	if _, err := readManifest(mk.Dir); err != nil {
		return err
	}
	mk.Updated = time.Now()
	all[name] = mk
	return writeJSON(marketplacesFile(), all)
}

func RemoveMarketplace(name string) ([]string, error) {
	all, err := Marketplaces()
	if err != nil {
		return nil, err
	}
	mk, ok := all[name]
	if !ok {
		return nil, fmt.Errorf("unknown marketplace %q", name)
	}
	installed, err := InstalledPlugins()
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, id := range slices.Sorted(maps.Keys(installed)) {
		if _, market := SplitID(id); market == name {
			if err := Uninstall(id); err != nil {
				return removed, err
			}
			removed = append(removed, id)
		}
	}
	if mk.URL != "" {
		if err := os.RemoveAll(mk.Dir); err != nil {
			return removed, err
		}
	}
	delete(all, name)
	return removed, writeJSON(marketplacesFile(), all)
}

func SplitID(id string) (string, string) {
	name, market, _ := strings.Cut(id, "@")
	return name, market
}
