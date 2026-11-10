package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shinucla/mini-golang-agent/internal/agentdef"
	"github.com/shinucla/mini-golang-agent/internal/config"
)

const maxMemoryFile = 40000

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

func extendSection(home string) string {
	return fmt.Sprintf(`# Extend mga
When the user asks for a command, a slash command, a skill, or a rule inside mga, create the mga file below. Do not write a shell script or a program unless the user asks for one.
- Slash command: %[1]s/commands/<name>.md for all projects, or .mga/commands/<name>.md for this project. The file is a prompt that mga sends to you when the user types /<name>. $ARGUMENTS becomes the text after the command, and $1 to $9 become single words. Optional frontmatter: description, argument-hint.
- Skill: %[1]s/skills/<name>/SKILL.md, or .mga/skills/<name>/SKILL.md for this project. Frontmatter needs name and description; the description says when to use the skill. The body holds the instructions. Put helper scripts in the same folder.
- Rule: add it to %[1]s/MGA.md or %[1]s/rules/<topic>.md for all projects, or to MGA.md or .mga/rules/<topic>.md for this project.
- New commands and skills load when mga starts again. Tell the user to restart mga.
- mga data: sessions are JSON files in %[1]s/sessions/. The shell command "mga sessions --all --json" lists them, and "mga sessions show <id> --json" prints one.`, home)
}

func MainPrompt(cwd, provider, model string) string {
	parts := []string{mainInstructions, extendSection(config.Home()), environment(cwd, provider, model)}
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
	for _, f := range InstructionFiles(cwd) {
		header := "Contents of " + f.Path
		if 0 < len(f.Paths) {
			header += " (apply these rules only to files that match: " + strings.Join(f.Paths, ", ") + ")"
		}
		sections = append(sections, header+":\n\n"+f.Text)
	}
	return strings.Join(sections, "\n\n")
}
