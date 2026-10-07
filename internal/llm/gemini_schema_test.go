package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiParametersCleanMCPSchemas(t *testing.T) {
	in := map[string]any{
		"$schema": "x", "type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"q":    map[string]any{"type": []any{"string", "null"}, "description": "query", "examples": []any{"a"}},
			"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string", "const": "x"}},
			"mode": map[string]any{"anyOf": []any{map[string]any{"type": "string", "$ref": "#/a"}}},
		},
		"required": []any{"q"},
	}
	out := geminiParameters(in)
	data, _ := json.Marshal(out)
	got := string(data)
	for _, banned := range []string{"$schema", "additionalProperties", "examples", "const", "$ref"} {
		if strings.Contains(got, banned) {
			t.Fatalf("%s left in %s", banned, got)
		}
	}
	q := out["properties"].(map[string]any)["q"].(map[string]any)
	if q["type"] != "string" || q["nullable"] != true || out["required"] == nil {
		t.Fatalf("schema = %s", got)
	}
	if geminiParameters(map[string]any{"type": "object", "properties": map[string]any{}}) != nil {
		t.Fatal("an empty object schema must be left out for Gemini")
	}
}
