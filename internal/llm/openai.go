package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type OpenAI struct {
	name          string
	baseURL       string
	apiKey        string
	headers       map[string]string
	sendReasoning bool
}

type oaFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type oaToolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Function oaFunction `json:"function"`
}

type oaMessage struct {
	Role             string       `json:"role"`
	Content          string       `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCalls        []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type oaChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int        `json:"index"`
				ID       string     `json:"id"`
				Function oaFunction `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAI) Name() string { return p.name }

func (p *OpenAI) authHeaders() map[string]string {
	h := map[string]string{}
	for k, v := range p.headers {
		h[k] = v
	}
	if p.apiKey != "" {
		h["Authorization"] = "Bearer " + p.apiKey
	}
	return h
}

func (p *OpenAI) url(path string) string {
	return strings.TrimRight(p.baseURL, "/") + path
}

func (p *OpenAI) ListModels(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []struct {
			ID               string `json:"id"`
			OwnedBy          string `json:"owned_by"`
			ContextLength    int    `json:"context_length"`
			ContextWindow    int    `json:"context_window"`
			MaxContextLength int    `json:"max_context_length"`
		} `json:"data"`
	}
	if err := getJSON(ctx, p.url("/models"), p.authHeaders(), &out); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(out.Data))
	for _, m := range out.Data {
		window := max(m.ContextLength, m.ContextWindow, m.MaxContextLength)
		models = append(models, Model{ID: m.ID, Description: m.OwnedBy, ContextWindow: window})
	}
	slices.SortFunc(models, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	return models, nil
}

func (p *OpenAI) Chat(ctx context.Context, req Request, onDelta func(Delta)) (*Response, error) {
	body := map[string]any{
		"model":          req.Model,
		"messages":       p.messages(req),
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(req.Tools) != 0 {
		body["tools"] = openAITools(req.Tools)
	}
	resp, err := postStream(ctx, p.url("/chat/completions"), p.authHeaders(), body)
	if err != nil {
		return nil, err
	}

	out := &Response{Message: Message{Role: RoleAssistant}}
	var text, reasoning strings.Builder
	byIndex := map[int]*ToolCall{}
	var calls []*ToolCall

	err = streamSSE(resp, func(data []byte) error {
		if string(data) == "[DONE]" {
			return errStopStream
		}
		var chunk oaChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("stream error: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			out.Usage = Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		for _, choice := range chunk.Choices {
			d := choice.Delta
			thinking := d.ReasoningContent + d.Reasoning
			if thinking != "" {
				reasoning.WriteString(thinking)
				onDelta(Delta{Reasoning: thinking})
			}
			if d.Content != "" {
				text.WriteString(d.Content)
				onDelta(Delta{Text: d.Content})
			}
			for _, tc := range d.ToolCalls {
				cur, ok := byIndex[tc.Index]
				if !ok || (tc.ID != "" && cur.ID != "" && tc.ID != cur.ID) {
					cur = &ToolCall{ID: tc.ID}
					byIndex[tc.Index] = cur
					calls = append(calls, cur)
				}
				if cur.ID == "" {
					cur.ID = tc.ID
				}
				cur.Name += tc.Function.Name
				cur.Arguments += tc.Function.Arguments
			}
			if choice.FinishReason != "" {
				out.StopReason = choice.FinishReason
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out.Message.Content = text.String()
	out.Message.Reasoning = reasoning.String()
	for i, c := range calls {
		if c.ID == "" {
			c.ID = fmt.Sprintf("call_%d", i)
		}
		out.Message.ToolCalls = append(out.Message.ToolCalls, *c)
	}
	return out, nil
}

func (p *OpenAI) messages(req Request) []oaMessage {
	var msgs []oaMessage
	if req.System != "" {
		msgs = append(msgs, oaMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			msgs = append(msgs, oaMessage{Role: "user", Content: m.Content})
		case RoleTool:
			msgs = append(msgs, oaMessage{Role: "tool", Content: m.Content, ToolCallID: m.ToolCallID})
		case RoleAssistant:
			if m.Content == "" && len(m.ToolCalls) == 0 {
				continue
			}
			om := oaMessage{Role: "assistant", Content: m.Content}
			if p.sendReasoning {
				om.ReasoningContent = m.Reasoning
			}
			for _, c := range m.ToolCalls {
				args := c.Arguments
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				om.ToolCalls = append(om.ToolCalls, oaToolCall{
					ID: c.ID, Type: "function", Function: oaFunction{Name: c.Name, Arguments: args},
				})
			}
			msgs = append(msgs, om)
		}
	}
	return msgs
}

func openAITools(specs []ToolSpec) []oaTool {
	tools := make([]oaTool, len(specs))
	for i, s := range specs {
		tools[i].Type = "function"
		tools[i].Function.Name = s.Name
		tools[i].Function.Description = s.Description
		tools[i].Function.Parameters = s.Parameters
	}
	return tools
}
