package llm

import "strings"

var knownContextWindows = []struct {
	prefix string
	tokens int
}{
	{"gpt-5", 400000},
	{"gpt-4.1", 1047576},
	{"gpt-4o", 128000},
	{"gpt-4-turbo", 128000},
	{"o1", 200000},
	{"o3", 200000},
	{"o4-mini", 200000},
	{"gemini-3", 1048576},
	{"gemini-2.5", 1048576},
	{"gemini-2.0", 1048576},
	{"gemini-1.5-pro", 2097152},
	{"gemini-1.5-flash", 1048576},
	{"deepseek-chat", 128000},
	{"deepseek-reasoner", 128000},
	{"grok-4", 256000},
	{"grok-3", 131072},
	{"llama-3.3", 131072},
	{"llama-3.1", 131072},
	{"mistral-large", 131072},
}

func KnownContextWindow(model string) int {
	id := strings.ToLower(model)
	if i := strings.LastIndex(id, "/"); 0 <= i {
		id = id[i+1:]
	}
	longest, tokens := 0, 0
	for _, k := range knownContextWindows {
		if strings.HasPrefix(id, k.prefix) && longest < len(k.prefix) {
			longest, tokens = len(k.prefix), k.tokens
		}
	}
	return tokens
}
