# mga — mini Go agent

`mga` is a terminal coding agent in Go, modeled on the Claude Code CLI. It works with OpenAI, Gemini, DeepSeek, Ollama, LM Studio, and any OpenAI-compatible endpoint. It has a Bubble Tea UI, tools, permission prompts, sub-agents, and an agents manager.

## Install

```sh
make install                # then run `mga` from any directory
```

`make install` runs the tests, builds a stripped binary, checks that it starts, and copies it to `~/.local/bin/mga` without `sudo`. It backs up an earlier `mga` to `mga.bak` and replaces the file in one step. It refuses a target that is a symlink or a directory you cannot write to. It warns you when the directory is not on your `PATH`, or when another `mga` comes first on the `PATH`.

Other Make targets:

```sh
make build                                # bin/mga
make run                                  # build, then start the UI
make run ARGS="--model ollama:qwen3"      # pass flags
make dev ARGS="-p 'list the TODOs'"       # go run, no build step
make check                                # gofmt, vet, and race tests
make install                              # test, build, install to ~/.local/bin/mga
make install PREFIX=/opt/tools            # install to /opt/tools/bin/mga
make uninstall                            # remove it (keeps ~/.mga)
make help                                 # all targets
```

Go 1.24.2 or later is necessary. With an older Go, the `go` command downloads Go 1.24.2 when `GOTOOLCHAIN=auto` (the default). The tool supports macOS and Linux. Bash uses Unix process groups, so Windows is not supported.

## Providers and models

| Provider | Type | Key | Notes |
|---|---|---|---|
| `openai` | OpenAI | `OPENAI_API_KEY` | default model `gpt-5` |
| `gemini` | native Gemini API | `GEMINI_API_KEY` or `GOOGLE_API_KEY` | streams thoughts; keeps thought signatures for tool calls |
| `deepseek` | OpenAI-compatible | `DEEPSEEK_API_KEY` | sends `reasoning_content` back during tool loops |
| `ollama` | OpenAI-compatible | none | `OLLAMA_HOST` overrides `http://localhost:11434` |
| `lmstudio` | OpenAI-compatible | none | `http://localhost:1234/v1` |
| `openrouter`, `groq`, `mistral`, `xai` | OpenAI-compatible | `<NAME>_API_KEY` | |

The model lists come live from each provider's `/models` endpoint. Use `/model` in the UI, or `mga models`. You can also type any model id that the list does not show.

To add a key, type `/model` and select the provider. If the provider has no key, mga shows the page where you get one (Ctrl+O opens it) and a masked input. mga checks the key with the provider before it saves the key in `~/.mga/config.json`. Press `e` on a provider to replace its key.

To add an endpoint, put it in `~/.mga/config.json`. Set `MGA_HOME` to use a different directory.

```json
{
  "default_provider": "ollama",
  "default_model": "qwen3-coder:30b",
  "permission_mode": "default",
  "auto_mode_ask": "allow",
  "allowed_tools": ["Bash(go test:*)", "Bash(git status:*)", "WebFetch(domain:go.dev)"],
  "providers": {
    "vllm": { "type": "openai", "base_url": "http://gpu-box:8000/v1", "local": true },
    "together": { "type": "openai", "base_url": "https://api.together.xyz/v1", "api_key_env": "TOGETHER_API_KEY" }
  }
}
```

The fields of an entry are `type` (`openai` or `gemini`), `base_url`, `api_key` or `api_key_env`, `default_model`, `headers`, `send_reasoning`, `local`, `key_url` (the page that `/model` shows when the key is missing), and `context_window` (the context size in tokens for every model of this provider, for example the `num_ctx` of your Ollama server). Set `local` when the provider needs no key.

## Usage

```sh
mga                                   # interactive UI
mga "explain the build"               # interactive UI with a first prompt
mga -c                                # continue the last session in this directory
mga --resume 20261005-101500-ab12cd   # resume a session by id
mga --model gemini:gemini-2.5-pro     # pick provider and model
mga -p "list the TODOs" --allowed-tools "Bash(grep:*)"   # one prompt, print the answer
git diff | mga -p "review this diff"  # piped stdin is added to the prompt
mga providers                         # provider status
mga models [provider...]              # live model lists
mga agents | agents show NAME | agents new NAME [--user] | agents delete NAME
```

### Status bar

The status bar under the input box has two lines:

```
  gpt-5 (openai) | 52.0k tokens | 87% ctx remaining
  ⏵⏵ auto mode on (shift+tab to cycle)                    ← sessions · /help
```

- Line 1: the model and provider, the tokens used in this session (input plus output), and the part of the context window that is still free after the last request. mga gets the context size from `context_window` in the config, then from the provider's model list (Gemini, OpenRouter, Groq, Mistral), then from a built-in table of common models. When the size is not known, the percentage is not shown.
- Line 2: the permission mode, and on the right a hint or a short notice.

### Keys

| Key | Action |
|---|---|
| `enter` | Send. While the agent works, the message goes into a queue. |
| `ctrl+j`, `alt+enter`, `\` + `enter` | New line |
| `esc` | Interrupt the agent, or clear the input |
| `shift+tab` | Cycle the permission mode: default → accept edits → plan → auto → bypass. mga saves the mode in `~/.mga/config.json`, so the next start uses it. Bypass is never saved. |
| `←` (empty input) | Open the session and agent list |
| `up` / `down` | Earlier inputs |
| `tab` | Complete a slash command |
| `ctrl+c` twice | Quit |

### Sessions and agents list

Press `←` when the input is empty. The list shows the sessions of this directory (● marks the current one) and the sub-agents of this run.

| Key | Action |
|---|---|
| `↑` / `↓` | Select |
| `→` / `enter` | Open the session, or the agent's live log (`←` goes back) |
| `r` | Rename the session |
| `n` / `ctrl+n` | Start a new session |
| `d` | Delete the session (asks first) |
| `x` | Stop the agent |
| `esc` | Back to the current session |

`/resume` opens the same list. `/resume <session id>` opens that session at once; if no session has that id, mga opens the list and says so in red.

mga names each new session for you. After your first message, the current model writes a title of at most 8 words that captures the idea, and the title shows on the input box and in the list. Until the title arrives, or if the model cannot make one, the session uses the start of your first message. A name you set with `r` is never replaced.

### Slash commands

`/help`, `/model [provider:model]`, `/agents`, `/tasks`, `/mcp`, `/mode [mode]`, `/permissions`, `/clear`, `/compact [focus]`, `/resume`, `/init`, `/status`, `/exit`.

## Tools

`Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`, `WebFetch`, `TodoWrite`, and `Task`.

- `Edit` and `Write` refuse a file that the agent did not read first, or that changed after the read.
- Read-only tools run in parallel when the model asks for several in one response.
- `Bash`, `Write`, `Edit`, and `WebFetch` ask for approval. The approval prompt offers "don't ask again". For Bash, that rule covers the first word of the command. For edits, it switches the session to accept-edits mode.
- A prefix rule such as `Bash(go test:*)` never matches a command that contains `&&`, `;`, `|`, redirection, or command substitution.
- Plan mode blocks every tool that is not read-only.
- Auto mode runs read-only tools, edits inside the project, your allow rules, and a short list of safe commands (`go test`, `git status`, `ls`, and similar) at once. The current model reviews every other call before it runs. It answers allow (the call runs), ask, or block (the call does not run and the agent is told why). What happens on ask depends on `"auto_mode_ask"` in `~/.mga/config.json`: `allow` (the default) runs the call and adds a note to its result, `prompt` asks you, and `block` stops it. If the review itself fails, mga always asks you. Each review is one extra model call, and the verdict is a model judgment, not a guarantee.

## Agents

Agent definitions are Markdown files with YAML frontmatter. They live in `.mga/agents/` (project scope) and `~/.mga/agents/` (user scope). A project agent replaces a user agent of the same name. Three built-in agents exist: `general-purpose`, `Explore`, and `Plan`.

```markdown
---
name: code-reviewer
description: Reviews a diff for bugs and risky changes. Use after code changes.
model: deepseek:deepseek-reasoner   # inherit (default), provider:model, or a model id
tools: Read, Grep, Glob             # empty = all tools except Task
---

You are a strict code reviewer. ...
```

The main agent starts sub-agents with the `Task` tool. Each sub-agent can run in the foreground or in the background. When a background agent finishes, mga sends its result to the main agent.

`/agents` opens the manager. It has two tabs:

- **Library**: list, view, create (`n`), edit (`e`), open in `$EDITOR` (`o`), delete (`d`), or run an agent in the background with a prompt (`r`).
- **Running**: all sub-agents with their status, time, and tool count. Press `enter` for the live log, `x` to stop an agent, and `c` to clear finished agents. `/tasks` opens this tab directly.

## MCP servers

mga connects to MCP (Model Context Protocol) servers and gives their tools to the model, like Claude Code.

```sh
mga mcp add github -e GITHUB_TOKEN='${GITHUB_TOKEN}' -- npx -y @modelcontextprotocol/server-github
mga mcp add -t http -H "Authorization: Bearer ${TOKEN}" docs https://example.com/mcp
mga mcp add -s project fs -- npx -y @modelcontextprotocol/server-filesystem .
mga mcp list            # connect to each server and show its status and tool count
mga mcp get github      # one server and its tools
mga mcp remove github
mga mcp approve fs      # allow a server from this project's .mcp.json
```

- **Config files.** `.mcp.json` in the project (the same format as Claude Code, so you can share one file) and `~/.mga/mcp.json` for all projects. `-s project` writes the first, and the default `-s user` writes the second. A project server replaces a user server with the same name. `${VAR}` and `${VAR:-default}` are expanded when mga connects.
- **Transports.** stdio (a local command) and Streamable HTTP (a URL with optional headers). The old SSE transport and OAuth sign-in are not supported yet; use a header with a token.
- **Approval.** A server in a project's `.mcp.json` can run any command, so mga starts it only after you approve it for that directory: press `a` in `/mcp`, or run `mga mcp approve <name>`. A server you add with `mga mcp add -s project` counts as approved.
- **Tools.** Each tool is named `mcp__<server>__<tool>`, and the transcript shows it as `server - tool (MCP)`. A tool that the server marks read-only runs without a prompt; other tools follow the permission mode, and auto mode reviews them. The allow rule `mcp__<server>` allows every tool of that server. Sub-agents with no tool list get the MCP tools too; an agent definition can name single MCP tools in `tools:`.
- **`/mcp`** lists the servers with their status and tool count. `enter` shows a server's details, tools, and error output, `a` approves, and `r` reconnects. When a server fails or waits for approval, the chat shows a short note.
- Server instructions go into the system prompt. MCP resources and prompts are not supported yet.

## Project instructions

mga adds these files to the system prompt when they exist: `~/.mga/MGA.md`, then `MGA.md`, `AGENTS.md`, `CLAUDE.md`, and `.mga/MGA.md` in the working directory. `/init` writes an `MGA.md` for the project.

## Sessions

mga saves each conversation to `~/.mga/sessions/<id>.json` after every turn. `/compact` replaces the history with a summary from the model.

## Layout

```
cmd/mga            CLI entry: flags, subcommands, print mode
internal/config    config file, built-in providers, model references
internal/llm       provider interface, OpenAI-compatible and Gemini clients, SSE
internal/tools     the tools and their JSON schemas
internal/agent     agent loop, permissions, sub-agent runtime and registry, prompts, sessions
internal/agentdef  agent definition files
internal/mcp       MCP client: config files, JSON-RPC, stdio and HTTP transports, server manager
internal/tui       Bubble Tea UI: chat, approvals, model picker, agents manager, sessions, MCP view
```

## Tests

```sh
go test -race ./...
```

The tests use fake HTTP servers for the OpenAI and Gemini protocols, and a scripted provider for the agent loop and the UI.
