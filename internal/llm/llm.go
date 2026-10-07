package llm

import (
	"context"
	"fmt"

	"github.com/kzhuang/mini-golang-agent/internal/config"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Signature string `json:"signature,omitempty"`
}

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	Reasoning  string     `json:"reasoning,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	IsError    bool       `json:"is_error,omitempty"`
}

type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []ToolSpec
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Response struct {
	Message    Message
	Usage      Usage
	StopReason string
}

type Delta struct {
	Text      string
	Reasoning string
}

type Model struct {
	ID            string
	Description   string
	ContextWindow int
}

type Provider interface {
	Name() string
	ListModels(ctx context.Context) ([]Model, error)
	Chat(ctx context.Context, req Request, onDelta func(Delta)) (*Response, error)
}

func New(name string, pc config.ProviderConfig) (Provider, error) {
	if !pc.Configured() {
		return nil, fmt.Errorf("provider %q has no API key: use /model in mga to add one, or set %s", name, pc.KeyEnvName())
	}
	switch pc.Type {
	case config.TypeGemini:
		return &Gemini{name: name, baseURL: pc.BaseURL, apiKey: pc.Key(), headers: pc.Headers}, nil
	case config.TypeOpenAI, "":
		return &OpenAI{name: name, baseURL: pc.BaseURL, apiKey: pc.Key(), headers: pc.Headers, sendReasoning: pc.SendReasoning}, nil
	}
	return nil, fmt.Errorf("provider %q has unknown type %q", name, pc.Type)
}
