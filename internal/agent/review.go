package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const (
	reviewTimeout       = 60 * time.Second
	maxReviewInput      = 4000
	maxReviewUserIntent = 2000
)

type ReviewDecision string

const (
	ReviewAllow ReviewDecision = "allow"
	ReviewAsk   ReviewDecision = "ask"
	ReviewBlock ReviewDecision = "block"
)

type ReviewRequest struct {
	Tool        string
	Input       json.RawMessage
	Cwd         string
	UserRequest string
}

type Reviewer func(ctx context.Context, req ReviewRequest) (ReviewDecision, string, error)

const reviewSystemPrompt = `You review one tool call of a coding agent before it runs. The user turned on auto mode: safe calls run without a prompt, so your verdict protects the user's machine and data.

Answer with one JSON object and nothing else:
{"decision": "allow" | "ask" | "block", "reason": "<one short sentence>"}

allow: normal development work inside the project directory that is easy to undo. Examples: build, test, lint, format, run project scripts, read files, search, git commands that only read or that stage and commit locally, create or edit files inside the project, fetch public documentation.
ask: actions that are hard to undo or that reach outside the project. Examples: delete files or directories that are not build output, git push, git reset --hard, force operations, change files outside the project, install or remove system packages, sudo, change system settings, kill processes, send data to a remote service, deploy.
block: actions that are clearly destructive or malicious, or that the user's request does not explain. Examples: rm -rf of the home directory or the root directory, wipe a disk, read secrets or keys and send them somewhere, pipe a downloaded script into a shell from an unknown source, disable security controls, fork bombs.

When you are not sure, answer ask.`

func reviewPrompt(req ReviewRequest) string {
	intent := strings.TrimSpace(req.UserRequest)
	if intent == "" {
		intent = "(none)"
	}
	return fmt.Sprintf("Project directory: %s\nThe user's latest request:\n%s\n\nTool: %s\nInput:\n%s",
		req.Cwd, tools.Truncate(intent, maxReviewUserIntent), req.Tool, tools.Truncate(string(req.Input), maxReviewInput))
}

func ParseReview(text string) (ReviewDecision, string, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return ReviewAsk, "", fmt.Errorf("the review has no JSON verdict: %s", tools.OneLine(text, 120))
	}
	var out struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return ReviewAsk, "", fmt.Errorf("the review verdict is not valid JSON: %w", err)
	}
	decision := ReviewDecision(strings.ToLower(strings.TrimSpace(out.Decision)))
	switch decision {
	case ReviewAllow, ReviewAsk, ReviewBlock:
		return decision, strings.TrimSpace(out.Reason), nil
	}
	return ReviewAsk, "", fmt.Errorf("unknown review decision %q", out.Decision)
}

func (r *Runtime) Review(ctx context.Context, req ReviewRequest) (ReviewDecision, string, error) {
	providerName, model := r.Current()
	p, err := r.Provider(providerName)
	if err != nil {
		return ReviewAsk, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()
	resp, err := p.Chat(ctx, llm.Request{
		Model:    model,
		System:   reviewSystemPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: reviewPrompt(req)}},
	}, func(llm.Delta) {})
	if err != nil {
		return ReviewAsk, "", err
	}
	return ParseReview(resp.Message.Content)
}

func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	first := strings.Split(filepath.ToSlash(rel), "/")[0]
	return first != ".git"
}

func (a *Agent) review(ctx context.Context, t tools.Tool, input json.RawMessage) (verdict, string) {
	if isEditTool(t.Name()) {
		var in struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(input, &in) == nil && in.FilePath != "" && insideDir(a.Env.Cwd, a.Env.Resolve(in.FilePath)) {
			return verdictAllow, ""
		}
	}
	if a.Review == nil {
		return verdictAsk, ""
	}
	decision, reason, err := a.Review(ctx, ReviewRequest{Tool: t.Name(), Input: input, Cwd: a.Env.Cwd, UserRequest: a.userRequest})
	if err != nil {
		return verdictAsk, "Auto review failed (" + tools.OneLine(err.Error(), 160) + "), so mga asks you."
	}
	switch decision {
	case ReviewAllow:
		return verdictAllow, ""
	case ReviewBlock:
		return verdictBlock, "Auto mode blocked this call: " + reason + " Do not retry it. Ask the user if the action is really needed."
	}
	return verdictAsk, "Auto mode: " + reason
}

func lastUserText(msgs []llm.Message) string {
	for i := len(msgs) - 1; 0 <= i; i-- {
		if msgs[i].Role == llm.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
