package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	TypeOpenAI = "openai"
	TypeGemini = "gemini"
)

type ProviderConfig struct {
	Type          string            `json:"type,omitempty"`
	BaseURL       string            `json:"base_url,omitempty"`
	APIKey        string            `json:"api_key,omitempty"`
	APIKeyEnv     string            `json:"api_key_env,omitempty"`
	DefaultModel  string            `json:"default_model,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	SendReasoning bool              `json:"send_reasoning,omitempty"`
	Local         bool              `json:"local,omitempty"`
	KeyURL        string            `json:"key_url,omitempty"`
	ContextWindow int               `json:"context_window,omitempty"`
}

func (p ProviderConfig) Key() string {
	if p.APIKey != "" {
		return p.APIKey
	}
	for _, name := range strings.Split(p.APIKeyEnv, ",") {
		if v := os.Getenv(strings.TrimSpace(name)); v != "" {
			return v
		}
	}
	return ""
}

func (p ProviderConfig) Configured() bool {
	return p.Local || p.Key() != ""
}

func (p ProviderConfig) KeyEnvName() string {
	return strings.TrimSpace(strings.Split(p.APIKeyEnv, ",")[0])
}

type Config struct {
	DefaultProvider string                    `json:"default_provider,omitempty"`
	DefaultModel    string                    `json:"default_model,omitempty"`
	PermissionMode  string                    `json:"permission_mode,omitempty"`
	AllowedTools    []string                  `json:"allowed_tools,omitempty"`
	Providers       map[string]ProviderConfig `json:"providers,omitempty"`

	path string
}

var preferredOrder = []string{"openai", "gemini", "deepseek", "openrouter", "groq", "mistral", "xai"}

func Home() string {
	if dir := os.Getenv("MGA_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mga"
	}
	return filepath.Join(home, ".mga")
}

func SessionsDir() string { return filepath.Join(Home(), "sessions") }

func UserAgentsDir() string { return filepath.Join(Home(), "agents") }

func ProjectAgentsDir(cwd string) string { return filepath.Join(cwd, ".mga", "agents") }

func Load() (*Config, error) {
	cfg := &Config{path: filepath.Join(Home(), "config.json")}
	data, err := os.ReadFile(cfg.path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", cfg.path, err)
	}
	return cfg, nil
}

func (c *Config) Path() string { return c.path }

func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(data, '\n'), 0o600)
}

func (c *Config) AllProviders() map[string]ProviderConfig {
	all := builtinProviders()
	for name, user := range c.Providers {
		all[name] = merge(all[name], user)
	}
	return all
}

func (c *Config) SetProviderKey(name, key string) {
	if c.Providers == nil {
		c.Providers = map[string]ProviderConfig{}
	}
	pc := c.Providers[name]
	pc.APIKey = key
	c.Providers[name] = pc
}

func (c *Config) ProviderNames() []string {
	all := c.AllProviders()
	names := slices.Collect(maps.Keys(all))
	slices.SortFunc(names, func(a, b string) int {
		ra, rb := rank(a), rank(b)
		if ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	return names
}

func (c *Config) Provider(name string) (ProviderConfig, bool) {
	p, ok := c.AllProviders()[name]
	return p, ok
}

func (c *Config) ResolveDefault() (string, string) {
	all := c.AllProviders()
	if p, ok := all[c.DefaultProvider]; ok {
		model := c.DefaultModel
		if model == "" {
			model = p.DefaultModel
		}
		return c.DefaultProvider, model
	}
	for _, name := range preferredOrder {
		if p, ok := all[name]; ok && p.Key() != "" {
			return name, p.DefaultModel
		}
	}
	return "ollama", all["ollama"].DefaultModel
}

func (c *Config) ParseModelRef(ref, currentProvider string) (string, string, error) {
	all := c.AllProviders()
	if name, model, ok := strings.Cut(ref, ":"); ok {
		if _, known := all[name]; known {
			return name, model, nil
		}
	}
	if _, known := all[ref]; known {
		return ref, all[ref].DefaultModel, nil
	}
	if currentProvider == "" {
		return "", "", fmt.Errorf("unknown provider in %q; use provider:model", ref)
	}
	return currentProvider, ref, nil
}

func rank(name string) int {
	if i := slices.Index(preferredOrder, name); 0 <= i {
		return i
	}
	switch name {
	case "ollama":
		return len(preferredOrder)
	case "lmstudio":
		return len(preferredOrder) + 1
	}
	return len(preferredOrder) + 2
}

func merge(base, user ProviderConfig) ProviderConfig {
	if user.Type != "" {
		base.Type = user.Type
	}
	if user.BaseURL != "" {
		base.BaseURL = user.BaseURL
	}
	if user.APIKey != "" {
		base.APIKey = user.APIKey
	}
	if user.APIKeyEnv != "" {
		base.APIKeyEnv = user.APIKeyEnv
	}
	if user.DefaultModel != "" {
		base.DefaultModel = user.DefaultModel
	}
	if 0 < user.ContextWindow {
		base.ContextWindow = user.ContextWindow
	}
	if user.KeyURL != "" {
		base.KeyURL = user.KeyURL
	}
	if user.Headers != nil {
		base.Headers = user.Headers
	}
	base.SendReasoning = base.SendReasoning || user.SendReasoning
	base.Local = base.Local || user.Local
	if base.Type == "" {
		base.Type = TypeOpenAI
	}
	return base
}

func builtinProviders() map[string]ProviderConfig {
	return map[string]ProviderConfig{
		"openai": {
			Type: TypeOpenAI, BaseURL: "https://api.openai.com/v1",
			APIKeyEnv: "OPENAI_API_KEY", DefaultModel: "gpt-5",
			KeyURL: "https://platform.openai.com/api-keys",
		},
		"gemini": {
			Type: TypeGemini, BaseURL: "https://generativelanguage.googleapis.com/v1beta",
			APIKeyEnv: "GEMINI_API_KEY,GOOGLE_API_KEY", DefaultModel: "gemini-2.5-flash",
			KeyURL: "https://aistudio.google.com/apikey",
		},
		"deepseek": {
			Type: TypeOpenAI, BaseURL: "https://api.deepseek.com",
			APIKeyEnv: "DEEPSEEK_API_KEY", DefaultModel: "deepseek-chat", SendReasoning: true,
			KeyURL: "https://platform.deepseek.com/api_keys",
		},
		"ollama": {
			Type: TypeOpenAI, BaseURL: ollamaHost() + "/v1", Local: true,
		},
		"lmstudio": {
			Type: TypeOpenAI, BaseURL: "http://localhost:1234/v1", Local: true,
		},
		"openrouter": {
			Type: TypeOpenAI, BaseURL: "https://openrouter.ai/api/v1",
			APIKeyEnv: "OPENROUTER_API_KEY", DefaultModel: "openai/gpt-5",
			KeyURL: "https://openrouter.ai/keys",
		},
		"groq": {
			Type: TypeOpenAI, BaseURL: "https://api.groq.com/openai/v1",
			APIKeyEnv: "GROQ_API_KEY", DefaultModel: "llama-3.3-70b-versatile",
			KeyURL: "https://console.groq.com/keys",
		},
		"mistral": {
			Type: TypeOpenAI, BaseURL: "https://api.mistral.ai/v1",
			APIKeyEnv: "MISTRAL_API_KEY", DefaultModel: "mistral-large-latest",
			KeyURL: "https://console.mistral.ai/api-keys",
		},
		"xai": {
			Type: TypeOpenAI, BaseURL: "https://api.x.ai/v1",
			APIKeyEnv: "XAI_API_KEY", DefaultModel: "grok-4",
			KeyURL: "https://console.x.ai",
		},
	}
}

func ollamaHost() string {
	host := strings.TrimRight(os.Getenv("OLLAMA_HOST"), "/")
	if host == "" {
		return "http://localhost:11434"
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return strings.Replace(host, "0.0.0.0", "localhost", 1)
}
