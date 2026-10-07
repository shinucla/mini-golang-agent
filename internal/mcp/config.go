package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kzhuang/mini-golang-agent/internal/config"
)

const (
	ScopeUser    = "user"
	ScopeProject = "project"

	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

var validServerName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type ServerConfig struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c ServerConfig) Transport() string {
	switch strings.ToLower(c.Type) {
	case "http", "streamable-http", "streamablehttp":
		return TransportHTTP
	case "", "stdio":
		if c.Command == "" && c.URL != "" {
			return TransportHTTP
		}
		return TransportStdio
	}
	return strings.ToLower(c.Type)
}

func (c ServerConfig) Target() string {
	if c.Transport() == TransportHTTP {
		return c.URL
	}
	return strings.TrimSpace(c.Command + " " + strings.Join(c.Args, " "))
}

func (c ServerConfig) expanded() ServerConfig {
	out := ServerConfig{Type: c.Type, Command: expand(c.Command), URL: expand(c.URL)}
	for _, a := range c.Args {
		out.Args = append(out.Args, expand(a))
	}
	if c.Env != nil {
		out.Env = map[string]string{}
		for k, v := range c.Env {
			out.Env[k] = expand(v)
		}
	}
	if c.Headers != nil {
		out.Headers = map[string]string{}
		for k, v := range c.Headers {
			out.Headers[k] = expand(v)
		}
	}
	return out
}

func expand(s string) string {
	return os.Expand(s, func(key string) string {
		name, fallback, hasFallback := strings.Cut(key, ":-")
		if v := os.Getenv(name); v != "" {
			return v
		}
		if hasFallback {
			return fallback
		}
		return ""
	})
}

func ValidateName(name string) error {
	if !validServerName.MatchString(name) {
		return fmt.Errorf("server name %q must use letters, digits, '-' or '_'", name)
	}
	return nil
}

func UserConfigPath() string { return filepath.Join(config.Home(), "mcp.json") }

func ProjectConfigPath(cwd string) string { return filepath.Join(cwd, ".mcp.json") }

func ConfigPath(scope, cwd string) (string, error) {
	switch scope {
	case ScopeUser:
		return UserConfigPath(), nil
	case ScopeProject:
		return ProjectConfigPath(cwd), nil
	}
	return "", fmt.Errorf("unknown scope %q (use user or project)", scope)
}

func ReadServers(path string) (map[string]ServerConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		MCPServers map[string]ServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return file.MCPServers, nil
}

func WriteServer(path, name string, cfg *ServerConfig) error {
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	servers := map[string]ServerConfig{}
	if existing, ok := raw["mcpServers"]; ok {
		if err := json.Unmarshal(existing, &servers); err != nil {
			return fmt.Errorf("parse mcpServers in %s: %w", path, err)
		}
	}
	if cfg == nil {
		if _, ok := servers[name]; !ok {
			return fmt.Errorf("no MCP server %q in %s", name, path)
		}
		delete(servers, name)
	} else {
		servers[name] = *cfg
	}
	encoded, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	raw["mcpServers"] = encoded
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if path == UserConfigPath() {
		mode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), mode)
}
