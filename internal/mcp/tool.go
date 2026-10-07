package mcp

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const (
	ToolPrefix        = "mcp__"
	maxToolNameLength = 64
	maxDescription    = 1000
)

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func ExposedName(server, tool string) string {
	name := ToolPrefix + unsafeNameChars.ReplaceAllString(server, "_") + "__" + unsafeNameChars.ReplaceAllString(tool, "_")
	if len(name) <= maxToolNameLength {
		return name
	}
	sum := sha1.Sum([]byte(name))
	return name[:maxToolNameLength-9] + "_" + hex.EncodeToString(sum[:4])
}

func SplitName(name string) (string, string, bool) {
	rest, ok := strings.CutPrefix(name, ToolPrefix)
	if !ok {
		return "", "", false
	}
	return strings.Cut(rest, "__")
}

type Tool struct {
	server string
	name   string
	info   ToolInfo
	client func() *Client
}

func (t *Tool) Name() string { return t.name }

func (t *Tool) Server() string { return t.server }

func (t *Tool) Info() ToolInfo { return t.info }

func (t *Tool) ReadOnly() bool {
	return t.info.Annotations.ReadOnlyHint != nil && *t.info.Annotations.ReadOnlyHint
}

func (t *Tool) Description() string {
	desc := strings.TrimSpace(t.info.Description)
	if desc == "" {
		desc = t.info.Title
	}
	desc = fmt.Sprintf("[MCP server %s] %s", t.server, desc)
	if maxDescription < len(desc) {
		desc = tools.OneLine(desc, maxDescription)
	}
	return desc
}

func (t *Tool) Schema() map[string]any {
	return normalizeSchema(t.info.InputSchema)
}

func (t *Tool) Summary(input json.RawMessage) string {
	return tools.OneLine(string(input), 100)
}

func (t *Tool) Run(ctx context.Context, _ *tools.Env, input json.RawMessage) (string, error) {
	c := t.client()
	if c == nil {
		return "", fmt.Errorf("MCP server %s is not connected; check /mcp", t.server)
	}
	res, err := c.callTool(ctx, t.info.Name, input)
	if err != nil {
		return "", err
	}
	text := FormatResult(res)
	if res.IsError {
		return text, errors.New("the MCP tool reported an error")
	}
	return text, nil
}

func normalizeSchema(in map[string]any) map[string]any {
	out := map[string]any{}
	if data, err := json.Marshal(in); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	delete(out, "$schema")
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	if _, ok := out["properties"]; !ok {
		out["properties"] = map[string]any{}
	}
	return out
}

func FormatResult(res *CallResult) string {
	var parts []string
	for _, item := range res.Content {
		switch item.Type {
		case "text":
			parts = append(parts, item.Text)
		case "image", "audio":
			parts = append(parts, fmt.Sprintf("[%s: %s, %d bytes of base64]", item.Type, item.MimeType, len(item.Data)))
		case "resource":
			if item.Resource == nil {
				continue
			}
			if item.Resource.Text != "" {
				parts = append(parts, item.Resource.Text)
			} else {
				parts = append(parts, fmt.Sprintf("[resource %s, %s]", item.Resource.URI, item.Resource.MimeType))
			}
		case "resource_link":
			parts = append(parts, fmt.Sprintf("[resource link %s %s]", item.Name, item.URI))
		}
	}
	if len(parts) == 0 && len(res.StructuredContent) != 0 {
		parts = append(parts, string(res.StructuredContent))
	}
	if len(parts) == 0 {
		return "(no output)"
	}
	return strings.Join(parts, "\n")
}
