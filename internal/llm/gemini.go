package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const skipSignatureValidation = "skip_thought_signature_validator"

type Gemini struct {
	name    string
	baseURL string
	apiKey  string
	headers map[string]string
}

type gemFunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type gemFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type gemPart struct {
	Text             string               `json:"text,omitempty"`
	Thought          bool                 `json:"thought,omitempty"`
	ThoughtSignature string               `json:"thoughtSignature,omitempty"`
	FunctionCall     *gemFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *gemFunctionResponse `json:"functionResponse,omitempty"`
}

type gemContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []gemPart `json:"parts"`
}

type gemDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type gemChunk struct {
	Candidates []struct {
		Content      gemContent `json:"content"`
		FinishReason string     `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *Gemini) Name() string { return g.name }

func (g *Gemini) authHeaders() map[string]string {
	h := map[string]string{"x-goog-api-key": g.apiKey}
	for k, v := range g.headers {
		h[k] = v
	}
	return h
}

func (g *Gemini) ListModels(ctx context.Context) ([]Model, error) {
	var models []Model
	pageToken := ""
	for {
		u := strings.TrimRight(g.baseURL, "/") + "/models?pageSize=1000"
		if pageToken != "" {
			u += "&pageToken=" + url.QueryEscape(pageToken)
		}
		var out struct {
			Models []struct {
				Name                       string   `json:"name"`
				DisplayName                string   `json:"displayName"`
				InputTokenLimit            int      `json:"inputTokenLimit"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := getJSON(ctx, u, g.authHeaders(), &out); err != nil {
			return nil, err
		}
		for _, m := range out.Models {
			if !slices.Contains(m.SupportedGenerationMethods, "generateContent") {
				continue
			}
			models = append(models, Model{
				ID:            strings.TrimPrefix(m.Name, "models/"),
				Description:   m.DisplayName,
				ContextWindow: m.InputTokenLimit,
			})
		}
		if out.NextPageToken == "" {
			break
		}
		pageToken = out.NextPageToken
	}
	slices.SortFunc(models, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	return models, nil
}

func (g *Gemini) Chat(ctx context.Context, req Request, onDelta func(Delta)) (*Response, error) {
	body := map[string]any{"contents": geminiContents(req)}
	if req.System != "" {
		body["systemInstruction"] = gemContent{Parts: []gemPart{{Text: req.System}}}
	}
	if len(req.Tools) != 0 {
		decls := make([]gemDeclaration, len(req.Tools))
		for i, t := range req.Tools {
			decls[i] = gemDeclaration{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
		}
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}
	model := strings.TrimPrefix(req.Model, "models/")
	u := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", strings.TrimRight(g.baseURL, "/"), url.PathEscape(model))
	resp, err := postStream(ctx, u, g.authHeaders(), body)
	if err != nil {
		return nil, err
	}

	out := &Response{Message: Message{Role: RoleAssistant}}
	var text, reasoning strings.Builder
	err = streamSSE(resp, func(data []byte) error {
		var chunk gemChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("stream error: %s", chunk.Error.Message)
		}
		if chunk.PromptFeedback != nil && chunk.PromptFeedback.BlockReason != "" {
			return fmt.Errorf("prompt blocked: %s", chunk.PromptFeedback.BlockReason)
		}
		if u := chunk.UsageMetadata; u != nil {
			out.Usage = Usage{InputTokens: u.PromptTokenCount, OutputTokens: u.CandidatesTokenCount + u.ThoughtsTokenCount}
		}
		for _, cand := range chunk.Candidates {
			for _, part := range cand.Content.Parts {
				switch {
				case part.FunctionCall != nil:
					args, err := json.Marshal(part.FunctionCall.Args)
					if err != nil || part.FunctionCall.Args == nil {
						args = []byte("{}")
					}
					id := part.FunctionCall.ID
					if id == "" {
						id = fmt.Sprintf("call_%d", len(out.Message.ToolCalls))
					}
					out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{
						ID: id, Name: part.FunctionCall.Name, Arguments: string(args), Signature: part.ThoughtSignature,
					})
				case part.Thought:
					reasoning.WriteString(part.Text)
					onDelta(Delta{Reasoning: part.Text})
				case part.Text != "":
					text.WriteString(part.Text)
					onDelta(Delta{Text: part.Text})
				}
			}
			if cand.FinishReason != "" {
				out.StopReason = cand.FinishReason
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Message.Content = text.String()
	out.Message.Reasoning = reasoning.String()
	return out, nil
}

func geminiContents(req Request) []gemContent {
	var contents []gemContent
	add := func(role string, parts ...gemPart) {
		if len(parts) == 0 {
			return
		}
		if n := len(contents); n != 0 && contents[n-1].Role == role {
			contents[n-1].Parts = append(contents[n-1].Parts, parts...)
			return
		}
		contents = append(contents, gemContent{Role: role, Parts: parts})
	}
	needsSignature := requiresSignature(req.Model)
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if m.Content != "" {
				add("user", gemPart{Text: m.Content})
			}
		case RoleAssistant:
			var parts []gemPart
			if m.Content != "" {
				parts = append(parts, gemPart{Text: m.Content})
			}
			for i, c := range m.ToolCalls {
				args := map[string]any{}
				_ = json.Unmarshal([]byte(c.Arguments), &args)
				part := gemPart{FunctionCall: &gemFunctionCall{Name: c.Name, Args: args}, ThoughtSignature: c.Signature}
				if part.ThoughtSignature == "" && needsSignature && i == 0 {
					part.ThoughtSignature = skipSignatureValidation
				}
				parts = append(parts, part)
			}
			add("model", parts...)
		case RoleTool:
			key := "output"
			if m.IsError {
				key = "error"
			}
			add("user", gemPart{FunctionResponse: &gemFunctionResponse{
				Name: m.ToolName, Response: map[string]any{key: m.Content},
			}})
		}
	}
	return contents
}

func requiresSignature(model string) bool {
	model = strings.TrimPrefix(model, "models/")
	return !strings.HasPrefix(model, "gemini-1") && !strings.HasPrefix(model, "gemini-2")
}
