package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/agentdef"
	"github.com/kzhuang/mini-golang-agent/internal/config"
)

const maxMemoryFile = 40000

var MemoryFiles = []string{"MGA.md", "AGENTS.md", "CLAUDE.md", ".mga/MGA.md"}

const mainInstructions = `You are mga, an interactive command-line coding agent. You help the user with software engineering tasks: fix bugs, add features, refactor code, explain code, and run commands.

# Tone and style
- Be concise and direct. A terminal shows your output as GitHub-flavored Markdown.
- Answer first. Do not add a preamble or a recap.
- Refer to code as path:line so the user can find it.

# How to work
- Search and read the code before you change it. Use Glob and Grep to find files and Read to read them.
- Use Edit for changes to existing files. Read a file before you Edit or Write it.
- Match the style and conventions of the code around your change.
- Verify your work. Run the build, the tests, or the linter when the project has them.
- Use TodoWrite to plan and track work that has three or more steps.
- Use the Task tool to give broad searches or independent sub-tasks to sub-agents. Start independent sub-agents in parallel.
- Do not commit, push, or delete data unless the user asks for it.

# Tools
- Call several independent read-only tools in one response when you can.
- If the user denies a tool call, do not try it again the same way. Ask the user what to do.`

func environment(cwd, provider, model string) string {
	_, gitErr := os.Stat(filepath.Join(cwd, ".git"))
	env := fmt.Sprintf("# Environment\nWorking directory: %s\nIs a git repository: %t\nPlatform: %s/%s\nToday's date: %s",
		cwd, gitErr == nil, runtime.GOOS, runtime.GOARCH, time.Now().Format("2006-01-02"))
	if model != "" {
		env += fmt.Sprintf("\nModel: %s:%s", provider, model)
	}
	return env
}

func MainPrompt(cwd, provider, model string) string {
	parts := []string{mainInstructions, environment(cwd, provider, model)}
	if mem := memory(cwd); mem != "" {
		parts = append(parts, "# Project and user instructions\nFollow these instructions. They override the defaults above.\n\n"+mem)
	}
	return strings.Join(parts, "\n\n")
}

func SubAgentPrompt(def agentdef.Definition, cwd string) string {
	parts := []string{
		def.Prompt,
		"You run as a sub-agent of mga. Nobody reads your intermediate messages. Your final message is your report, so make it complete and self-contained. Use absolute file paths.",
		environment(cwd, "", ""),
	}
	if mem := memory(cwd); mem != "" {
		parts = append(parts, "# Project and user instructions\n"+mem)
	}
	return strings.Join(parts, "\n\n")
}

func memory(cwd string) string {
	var sections []string
	paths := []string{filepath.Join(config.Home(), "MGA.md")}
	for _, name := range MemoryFiles {
		paths = append(paths, filepath.Join(cwd, name))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		text := string(data)
		if maxMemoryFile < len(text) {
			text = text[:maxMemoryFile]
		}
		sections = append(sections, fmt.Sprintf("Contents of %s:\n\n%s", path, strings.TrimSpace(text)))
	}
	return strings.Join(sections, "\n\n")
}
