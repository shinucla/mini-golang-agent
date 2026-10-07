package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const (
	maxTitleWords   = 8
	titleTimeout    = 30 * time.Second
	maxTitleRequest = 4000
)

const titleSystemPrompt = "You name chat sessions of a coding agent."

func titlePrompt(request string) string {
	return "Write a title for a session that starts with the request below. " +
		"Capture the main idea, not the wording. Use at most 8 words. " +
		"Use plain words, no quotes, and no period at the end. Answer with the title only.\n\nRequest:\n" +
		tools.Truncate(request, maxTitleRequest)
}

func (r *Runtime) SessionTitle(ctx context.Context, request string) (string, error) {
	providerName, model := r.Current()
	p, err := r.Provider(providerName)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, titleTimeout)
	defer cancel()
	resp, err := p.Chat(ctx, llm.Request{
		Model:    model,
		System:   titleSystemPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: titlePrompt(request)}},
	}, func(llm.Delta) {})
	if err != nil {
		return "", err
	}
	return CleanTitle(resp.Message.Content)
}

func CleanTitle(text string) (string, error) {
	var line string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	for _, prefix := range []string{"Title:", "title:", "#"} {
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	line = strings.Trim(line, "\"'`*“”‘’ ")
	line = strings.TrimRight(line, ".!。 ")
	words := strings.Fields(line)
	if len(words) == 0 {
		return "", errors.New("the model returned no title")
	}
	if maxTitleWords < len(words) {
		words = words[:maxTitleWords]
	}
	return tools.OneLine(strings.Join(words, " "), 80), nil
}
