package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinucla/mini-golang-agent/internal/config"
)

type Marketplace struct {
	Name    string    `json:"name"`
	Source  string    `json:"source"`
	URL     string    `json:"url,omitempty"`
	Dir     string    `json:"dir"`
	Updated time.Time `json:"updated"`
}

type Installed struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Enabled     bool      `json:"enabled"`
	Version     string    `json:"version,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func Root() string { return filepath.Join(config.Home(), "plugins") }

func marketplacesFile() string { return filepath.Join(Root(), "marketplaces.json") }
func installedFile() string    { return filepath.Join(Root(), "installed.json") }

func readJSON[T any](path string) (map[string]T, error) {
	out := map[string]T{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	if err := errors.Join(werr, tmp.Close()); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func Marketplaces() (map[string]Marketplace, error) { return readJSON[Marketplace](marketplacesFile()) }

func InstalledPlugins() (map[string]Installed, error) { return readJSON[Installed](installedFile()) }

func git(args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", errors.New("git is not installed; install git to add marketplaces and plugins")
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
	}
	return text, nil
}

func headCommit(dir string) string {
	out, err := git("-C", dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func gitURL(source string) (string, bool) {
	switch {
	case strings.HasPrefix(source, "https://"), strings.HasPrefix(source, "http://"),
		strings.HasPrefix(source, "git@"), strings.HasPrefix(source, "ssh://"), strings.HasPrefix(source, "file://"),
		strings.HasSuffix(source, ".git"):
		return source, true
	case strings.Count(source, "/") == 1 && !strings.HasPrefix(source, ".") && !strings.HasPrefix(source, "/"):
		return "https://github.com/" + source + ".git", true
	}
	return "", false
}

func safeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, name)
}
