package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		io.WriteString(w, "data: "+e+"\n\n")
	}
}

func TestOpenAIStreamsTextAndToolCalls(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(&body)
		sse(w,
			`{"choices":[{"delta":{"reasoning_content":"think"}}]}`,
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"Read","arguments":"{\"file"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"_path\":\"a\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"Glob","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	p := &OpenAI{name: "t", baseURL: srv.URL, apiKey: "k", sendReasoning: true}
	var streamed strings.Builder
	resp, err := p.Chat(context.Background(), Request{
		Model:  "m",
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
			{Role: RoleAssistant, Reasoning: "r", ToolCalls: []ToolCall{{ID: "x", Name: "Bash"}}},
			{Role: RoleTool, ToolCallID: "x", Content: "out"},
		},
		Tools: []ToolSpec{{Name: "Read", Parameters: map[string]any{"type": "object"}}},
	}, func(d Delta) { streamed.WriteString(d.Text) })
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Content != "Hello" || streamed.String() != "Hello" || resp.Message.Reasoning != "think" {
		t.Fatalf("content=%q streamed=%q reasoning=%q", resp.Message.Content, streamed.String(), resp.Message.Reasoning)
	}
	calls := resp.Message.ToolCalls
	if len(calls) != 2 || calls[0].Arguments != `{"file_path":"a"}` || calls[1].Name != "Glob" {
		t.Fatalf("tool calls = %+v", calls)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages = %v", msgs)
	}
	assistant := msgs[2].(map[string]any)
	if assistant["reasoning_content"] != "r" {
		t.Fatalf("reasoning_content not sent back: %v", assistant)
	}
	call := assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["arguments"] != "{}" {
		t.Fatalf("empty arguments must become {}: %v", call)
	}
}

func TestOpenAIRetriesThenReportsAPIError(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad model"}}`)
	}))
	defer srv.Close()
	p := &OpenAI{baseURL: srv.URL}
	_, err := p.Chat(context.Background(), Request{Model: "m"}, func(Delta) {})
	if err == nil || !strings.Contains(err.Error(), "bad model") || hits != 1 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
}

func TestGeminiStreamAndContents(t *testing.T) {
	var body struct {
		Contents          []gemContent `json:"contents"`
		SystemInstruction gemContent   `json:"systemInstruction"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/gemini-3-pro:streamGenerateContent") || r.Header.Get("x-goog-api-key") != "k" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		json.NewDecoder(r.Body).Decode(&body)
		sse(w,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"plan","thought":true}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"Read","args":{"file_path":"x"}},"thoughtSignature":"sig"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3,"thoughtsTokenCount":2}}`,
		)
	}))
	defer srv.Close()

	g := &Gemini{baseURL: srv.URL, apiKey: "k"}
	resp, err := g.Chat(context.Background(), Request{
		Model:  "gemini-3-pro",
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Content: "q"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "Bash", Arguments: `{"command":"ls"}`}, {ID: "b", Name: "Glob", Arguments: `{}`}}},
			{Role: RoleTool, ToolCallID: "a", ToolName: "Bash", Content: "out"},
			{Role: RoleTool, ToolCallID: "b", ToolName: "Glob", Content: "boom", IsError: true},
		},
	}, func(Delta) {})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Content != "Hi" || resp.Message.Reasoning != "plan" {
		t.Fatalf("message = %+v", resp.Message)
	}
	if c := resp.Message.ToolCalls; len(c) != 1 || c[0].Signature != "sig" || c[0].Arguments != `{"file_path":"x"}` {
		t.Fatalf("tool calls = %+v", c)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	if len(body.Contents) != 3 {
		t.Fatalf("want user/model/user contents, got %+v", body.Contents)
	}
	model := body.Contents[1]
	if model.Role != "model" || model.Parts[0].ThoughtSignature != skipSignatureValidation || model.Parts[1].ThoughtSignature != "" {
		t.Fatalf("model content = %+v", model)
	}
	results := body.Contents[2].Parts
	if len(results) != 2 || results[1].FunctionResponse.Response["error"] != "boom" {
		t.Fatalf("function responses = %+v", results)
	}
	if body.SystemInstruction.Parts[0].Text != "sys" {
		t.Fatalf("system = %+v", body.SystemInstruction)
	}
}

func TestGeminiListModelsPagesAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			io.WriteString(w, `{"models":[{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["generateContent"]},{"name":"models/embedding-001","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"p2"}`)
			return
		}
		io.WriteString(w, `{"models":[{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"],"inputTokenLimit":1048576}]}`)
	}))
	defer srv.Close()
	models, err := (&Gemini{baseURL: srv.URL}).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "gemini-2.5-flash" || models[0].ContextWindow != 1048576 {
		t.Fatalf("models = %+v", models)
	}
}

func TestOpenAIListModelsReadsContextSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"id":"openai/gpt-5","context_length":400000},{"id":"llama","context_window":131072},{"id":"plain"}]}`)
	}))
	defer srv.Close()
	models, err := (&OpenAI{baseURL: srv.URL}).ListModels(context.Background())
	if err != nil || len(models) != 3 || models[0].ContextWindow != 131072 || models[1].ContextWindow != 400000 || models[2].ContextWindow != 0 {
		t.Fatalf("models = %+v err=%v", models, err)
	}
}

func TestKnownContextWindow(t *testing.T) {
	cases := map[string]int{
		"gpt-5":                 400000,
		"gpt-5-mini":            400000,
		"openai/gpt-4o-mini":    128000,
		"models/gemini-2.5-pro": 1048576,
		"deepseek-reasoner":     128000,
		"qwen3:8b":              0,
	}
	for model, want := range cases {
		if got := KnownContextWindow(model); got != want {
			t.Errorf("KnownContextWindow(%q) = %d, want %d", model, got, want)
		}
	}
}
