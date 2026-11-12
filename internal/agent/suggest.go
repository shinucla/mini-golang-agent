package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shinucla/mini-golang-agent/internal/llm"
	"github.com/shinucla/mini-golang-agent/internal/tools"
)

const (
	suggestTimeout      = 20 * time.Second
	suggestMessages     = 6
	maxSuggestUserText  = 1000
	maxSuggestReplyText = 3000
	maxSuggestion       = 150
)

const SuggestSystemPrompt = "You predict the next message that a user will type to a coding agent."

const suggestInstructions = `Predict the next message that the user will most likely type. Suggest a message only when you are confident the user will send it, or something very close to it. In all other cases, answer NONE. NONE is a good answer, and it is the usual one.

Suggest a message only in these cases:
- The agent asked a question or offered options. Answer it, and pick the option that the agent recommended. Example: "Go with option 2".
- The agent proposed one clear next step, or asked for permission to do it. Example: "yes, add the test".
- The agent finished a task, and one next step is clearly the most likely. Examples: "run the tests", "commit and push".

Answer NONE in these cases:
- The agent only answered a question or explained something.
- Several next steps are equally likely, or the next step depends on facts that only the user knows.
- The agent asked for information that only the user has, such as a name, a path, or a secret.
- The agent hit an error that the user must look at.

Write as the user, in the user's style and language. Use at most 12 words. Do not repeat a request that the agent already did.
Answer with the message text only, or NONE.`

func (r *Runtime) SuggestsPrompts() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Cfg.PromptSuggestions == nil || *r.Cfg.PromptSuggestions
}

func (r *Runtime) SetPromptSuggestions(on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Cfg.PromptSuggestions = &on
	return r.Cfg.Save()
}

func (r *Runtime) SuggestNextPrompt(ctx context.Context, providerName, model string, history []llm.Message) (string, error) {
	transcript := suggestTranscript(history)
	if transcript == "" {
		return "", nil
	}
	p, err := r.Provider(providerName)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, suggestTimeout)
	defer cancel()
	resp, err := p.Chat(ctx, llm.Request{
		Model:    model,
		System:   SuggestSystemPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "Here is the end of a conversation between a user and a coding agent.\n\n<conversation>\n" + transcript + "\n</conversation>\n\n" + suggestInstructions}},
	}, func(llm.Delta) {})
	if err != nil {
		return "", err
	}
	return CleanSuggestion(resp.Message.Content), nil
}

func suggestTranscript(history []llm.Message) string {
	var turns []string
	for i := len(history) - 1; 0 <= i && len(turns) < suggestMessages; i-- {
		m := history[i]
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		switch m.Role {
		case llm.RoleUser:
			turns = append(turns, "User: "+tools.Truncate(text, maxSuggestUserText))
		case llm.RoleAssistant:
			turns = append(turns, "Agent: "+tail(text, maxSuggestReplyText))
		}
	}
	if len(turns) == 0 || !strings.HasPrefix(turns[0], "Agent: ") {
		return ""
	}
	var b strings.Builder
	for i := len(turns) - 1; 0 <= i; i-- {
		fmt.Fprintf(&b, "%s\n\n", turns[i])
	}
	return strings.TrimSpace(b.String())
}

func tail(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return "…" + string(runes[len(runes)-limit:])
}

func CleanSuggestion(text string) string {
	var line string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	for _, prefix := range []string{"User:", "user:", "Message:", ">"} {
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	line = strings.Trim(line, "\"'`*“”‘’ ")
	if strings.EqualFold(strings.TrimRight(line, ".!"), "none") {
		return ""
	}
	return tools.OneLine(line, maxSuggestion)
}
