# mga — project description for future maintainers

This file gives a new maintainer (human or LLM) the full context of the project: the goal, the decisions, the design, the implementation, and the next steps. Read it before you change code. `README.md` is the user guide. This file is the developer guide.

Last update: 2026-10-07.

---

## 1. Goal

`mga` ("mini Go agent") is a terminal coding agent in Go. It copies the main behavior of the Claude Code CLI:

- an interactive full-screen chat in the terminal,
- an agent loop that calls tools (shell, file read/write/edit, search, web fetch, to-do list),
- permission prompts before risky tool calls,
- sub-agents that the main agent starts, in the foreground or the background,
- a manager for agent definitions and for running sub-agents,
- sessions that you can list, open, rename, and delete,
- five permission modes, including an auto mode with a model-based safety review,
- a two-line status bar (model, tokens, context left, mode) and the session name on the input box,
- MCP servers (stdio and Streamable HTTP) whose tools the model can call, configured like Claude Code.

The difference from Claude Code: `mga` works with many LLM providers, not one.

### 1.1 Decisions from the project owner

The owner answered these questions at the start. Do not change them without asking.

| Question | Decision |
|---|---|
| Meaning of "agent list view and management" | **Both**: (a) agent definitions (named agents with a prompt, model, and tools) with create/edit/delete, and (b) a live view of running sub-agents with status, log, and stop. |
| Terminal UI | **Bubble Tea TUI** (charmbracelet), not a plain line REPL. |
| Providers | **OpenAI, Gemini, DeepSeek, Ollama, plus any OpenAI-compatible endpoint.** Anthropic is **not** included. |
| Git | Repo: `git@github.com:shinucla/mini-golang-agent.git`, branch `main` (since 2026-10-07; the project started as plain files). The owner works on it from more than one computer. Commit and push only when the owner asks. `bin/` is ignored: each machine builds its own binary with `make build` or `make install`. |

### 1.2 Workspace rules that apply to this code

The project lives in `/Users/kevinz/project`, a multi-repo workspace. Its `CLAUDE.md` sets rules that apply here too:

- **No code comments.** The default is zero comments. Use clear names, small functions, and named constants. A comment is allowed only when it passes a strict four-question test (a "why" that the code cannot show, outside knowledge, and it stays true). The current code has **zero** comments. Keep it that way.
- **Never use `>` or `>=`** in comparisons. Flip the operands and use `<` or `<=`. Example: `0 < len(x)`, not `len(x) > 0`. The whole code base follows this rule.
- **No negative names** (`isNotX`, `cantBeY`). Use positive names.
- **Prose (docs, chat, PR text) follows ASD-STE100 Simplified Technical English**: short sentences, active voice, simple tenses, one idea per sentence.
- Keep each shell command simple, without `&&` chains, when you work in this workspace with an agent tool.

Check the two code rules with:

```sh
grep -rnE ' >=? ' --include='*.go' .    # must print nothing
grep -rnE '^\s*//' --include='*.go' .   # must print nothing
```

---

## 2. Status

| Area | Status |
|---|---|
| Build, `go vet`, `gofmt` | Clean |
| Unit tests (`go test -race ./...`) | All pass: llm, tools, agent, agentdef, tui |
| Built binary in `-p` mode against a fake OpenAI-compatible server | Works: tool call, deny path, allow-rule path, auto mode with an allow review, model list, provider list, agent list, session save |
| Interactive TUI in a pseudo-terminal | Works: banner, streaming, tool spinner, tool result, slash completion, `/agents`, home view (← → Ctrl+R), key screen, mode cycle and mode saved across restarts, two-line status bar, session name on the input box, Esc, double Ctrl+C exit with status 0 |
| MCP against a real server (2026-10-07) | Works with the official `@modelcontextprotocol/server-filesystem` (via `npx`): `mga mcp add`, `list` (connected, 14 tools), `get` (read-only flags honored), a `-p` turn where the model called `mcp__fs__read_text_file` and got the file text, and the project-approval flow in the TUI (needs approval → `a` → connected, approval saved). The model was a fake OpenAI-compatible server. |
| Live calls to real providers | **Only one:** the `/model` key screen sent a fake key to the real OpenAI API and showed the real 401 rejection. No successful chat with a real provider yet. The build machine has no Ollama, no LM Studio, and no API keys. |
| Build for Linux | A linux/amd64 build with Go 1.24.2 passed vet and tests on the build machine. The owner builds on a Linux machine too; a stale-file problem there is gotcha 15. |

Behaviors that follow the provider docs but are **not verified live**:

- DeepSeek: mga sends `reasoning_content` back on assistant messages in the tool loop (`send_reasoning: true`).
- Gemini 3: mga sends a placeholder thought signature (`skip_thought_signature_validator`) on a function call that has no real signature, for example a call from another provider earlier in the same session.
- Ollama tool calls over its OpenAI-compatible `/v1` endpoint, with `stream_options.include_usage`.
- The context sizes in the built-in table (`internal/llm/context.go`).
- Auto-mode verdicts from real models (the tests use scripted verdicts).

---

## 3. Layout

```
mini-golang-agent/
├── cmd/mga/main.go          CLI entry: flags, subcommands, print mode (-p), session pick, TUI start
├── internal/config/         ~/.mga/config.json, built-in providers, model references
├── internal/llm/            provider interface; OpenAI-compatible and native Gemini clients; HTTP + SSE; context sizes
├── internal/tools/          the tools, their JSON schemas, and the per-agent tool environment
├── internal/agent/          agent loop, permissions, auto-mode review, runtime (providers + sub-agents), task registry, prompts, sessions
├── internal/agentdef/       agent definition files (Markdown + YAML frontmatter), built-in agents
├── internal/mcp/            MCP client: config files, JSON-RPC, stdio and HTTP transports, server manager, tool wrapper
├── internal/tui/            Bubble Tea UI: chat, approvals, status bar, model picker + key screen, agents manager, form, home view
├── .gitignore               bin/ and .claude/worktrees/
├── Makefile                 build, run, dev, test, test-race, vet, fmt, lint, check, tidy, install, uninstall, clean
├── README.md                user guide
└── description.md           this file
```

Module path: `github.com/kzhuang/mini-golang-agent`. Go version: **1.24.2**, the highest version that any dependency needs (`charmbracelet/x/ansi`, `x/cellbuf`). Do not raise it without a reason. Write the `go` line with three parts (`1.24.2`, not `1.26`): an older Go then downloads a toolchain that exists. A two-part `go 1.26` made `go1.26 for linux/amd64: toolchain not available` on a Linux machine. Do not use APIs newer than Go 1.24, for example `sync.WaitGroup.Go` (Go 1.25).

Dependencies (see `go.mod`):

| Dependency | Version | Use |
|---|---|---|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | TUI runtime (v1 API, **not** v2 `charm.land/...`) |
| `github.com/charmbracelet/bubbles` | v1.0.0 | `textarea`, `textinput`, `spinner`, `key` (same API as v0.2x) |
| `github.com/charmbracelet/lipgloss` | v1.1.1 pseudo-version | styles (pulled by glamour) |
| `github.com/charmbracelet/glamour` | v1.0.0 | Markdown rendering of assistant text |
| `github.com/charmbracelet/x/ansi` | v0.11.6 | `ansi.Truncate` for width-safe clipping |
| `github.com/bmatcuk/doublestar/v4` | v4.10.2 | `**` globs in Glob and Grep |
| `gopkg.in/yaml.v3` | v3.0.1 | agent definition frontmatter |

The LLM clients use only `net/http`. There is no provider SDK.

---

## 4. Architecture

### 4.1 Layers

```
cmd/mga  ──►  tui  ──►  agent (Runtime, Agent, Permissions, TaskRegistry, Session)
                │            │
                │            ├──►  llm (Provider: OpenAI-compatible, Gemini)
                │            ├──►  tools (Tool, Env)
                │            └──►  agentdef (Store, Definition)
                └──────────────►  config
```

Rules of the dependency graph:

- `config` imports nothing from the project.
- `llm` imports `config` only (for `llm.New`).
- `tools` imports nothing from the project. It knows sub-agents only through `Env.Spawn` (a function) and `Task.Agents` (a function).
- `agent` imports `llm`, `tools`, `agentdef`, `config`. It sees MCP only through the `ToolSource` interface (`Tools()`, `Instructions()`), so it does not import `mcp`.
- `mcp` imports `config` and `tools`.
- `tui` imports everything above. Nothing imports `tui` except `cmd/mga`.

### 4.2 One user turn (interactive)

1. The user types in the textarea and presses Enter. `App.submit` trims the text. A leading `/` that is a single word without another `/` means a slash command (`runCommand`). Otherwise it is a prompt.
2. If a turn already runs, the text goes into `App.queue`. Otherwise `App.startTurn` runs.
3. `startTurn` asks `Runtime.MainAgent(env)` for a fresh `Agent` (current provider and model, system prompt, all tools, `StopOnDeny: true`). It appends the user message to `App.history`. It starts a goroutine (as a `tea.Cmd`) that calls `Agent.Run(ctx, historyCopy, uiObserver)`.
4. `Agent.Run` loops: `Provider.Chat` (streams deltas to the observer) → append the assistant message → if there are tool calls, run them → append one tool message per call → repeat. It stops when an assistant message has no tool calls, on error, on cancel, or after `MaxSteps`.
5. The `uiObserver` sends each event to the Bubble Tea program with `p.Send`: `deltaMsg`, `assistantMsg`, `toolStartMsg`, `toolResultMsg`, `usageMsg`. The UI shows streaming text and running tools in the live area, and prints finished blocks to the terminal scrollback.
6. When `Run` returns, the cmd returns `turnDoneMsg{msgs, err}`. `finishTurn` replaces `App.history` with `msgs`, prints an error line if needed, saves the session, and starts the next queued message or background notification (`nextTurn`).

### 4.3 Print mode (`mga -p`)

`runPrint` in `cmd/mga/main.go`:

- reads extra prompt text from stdin when stdin is not a terminal,
- sets `Runtime.BackgroundAllowed = false` (all sub-agents run in the foreground),
- sets an approver that **denies** every call that needs approval and prints why to stderr,
- sets `StopOnDeny = false`, so the model sees the denial and continues,
- prints only the last assistant text of this run to stdout, and tool activity to stderr with `--verbose`,
- saves the session.

Print mode also starts when stdin is not a terminal and no `-p` is given.

---

## 5. Package details

### 5.1 `internal/config`

- `ProviderConfig.KeyURL` is the page where a user gets a key. `Config.SetProviderKey` sets `api_key` in the user's entry; call it through `Runtime.SetProviderKey`, which locks, saves, and drops the cached client. `Runtime.SaveDefault` and `Runtime.ProviderConfig` also lock. Use these from the UI, because background agents read the config at the same time.
- `Config` holds the user's file content only: `default_provider`, `default_model`, `permission_mode`, `auto_mode_ask`, `allowed_tools`, `providers`. `Save()` writes only these user fields, with file mode `0600`.
- `ProviderConfig` fields: `type`, `base_url`, `api_key`, `api_key_env`, `default_model`, `headers`, `send_reasoning`, `local`, `key_url`, `context_window`. `KeyEnvName()` returns the first name in `api_key_env`.
- `AllProviders()` merges the built-in providers with the user's entries. A user field that is not empty (or a `context_window` above 0) replaces the built-in field. `send_reasoning` and `local` are OR-ed. An empty `type` becomes `openai`.
- The UI writes the config at runtime in three places: a new API key, the default model, and the permission mode. All three go through `Runtime` methods that hold the runtime lock (see 5.4).
- Built-in providers: `openai`, `gemini`, `deepseek`, `ollama`, `lmstudio`, `openrouter`, `groq`, `mistral`, `xai`. See `builtinProviders()` for base URLs, key env vars, and default models.
- `ProviderConfig.Key()` returns `api_key` first. Then it tries each name in `api_key_env`, which is a comma-separated list (Gemini uses `GEMINI_API_KEY,GOOGLE_API_KEY`).
- `Configured()` is true when `local` is set or a key exists.
- `ResolveDefault()` picks the provider and model at startup. It uses the configured default if one exists. Otherwise it uses the first provider in `preferredOrder` that has a key. Otherwise it uses `ollama`.
- `ParseModelRef(ref, currentProvider)` accepts `provider:model`, a bare provider name (uses that provider's default model), or a bare model id (uses the current provider). Note: Ollama model ids contain `:` (`qwen3:8b`). `ollama:qwen3:8b` works because only the first `:` splits, and only when the left part is a known provider name.
- `Home()` is `$MGA_HOME` or `~/.mga`. `SessionsDir()`, `UserAgentsDir()`, and `ProjectAgentsDir(cwd)` derive from it.
- `OLLAMA_HOST` changes the Ollama base URL. A value without a scheme gets `http://`, and `0.0.0.0` becomes `localhost`.

### 5.2 `internal/llm`

Neutral types (`llm.go`):

- `Message{Role, Content, Reasoning, ToolCalls, ToolCallID, ToolName, IsError}`. Roles: `user`, `assistant`, `tool`. A tool result is its own message with `Role: tool`.
- `ToolCall{ID, Name, Arguments (raw JSON string), Signature (Gemini thought signature)}`.
- `Request{Model, System, Messages, Tools []ToolSpec}`. There are no temperature or max-token fields on purpose. Newer OpenAI models reject `max_tokens`.
- `Provider` interface: `Name()`, `ListModels(ctx)`, `Chat(ctx, req, onDelta func(Delta)) (*Response, error)`. `Chat` streams through `onDelta` and returns the full assistant message and usage.
- `llm.New(name, ProviderConfig)` returns the client for the type. It returns an error when the provider has no key and is not local. The error tells the user to use `/model` or to set the key variable.
- `Model{ID, Description, ContextWindow}`. `ContextWindow` is 0 when the provider does not report it.
- `KnownContextWindow(model)` (`context.go`) looks up a context size in a prefix table of common models (GPT-5/4.1/4o, o1/o3/o4-mini, Gemini 1.5–3, DeepSeek chat/reasoner, Grok 3/4, Llama 3.1/3.3, Mistral Large). It matches the longest prefix of the text after the last `/`, so `openai/gpt-4o-mini` and `models/gemini-2.5-pro` work. It returns 0 for an unknown model. The values come from provider docs as known at build time and are not checked live.

HTTP and SSE (`http.go`):

- `postStream` sends JSON and returns the response for streaming. It retries up to 4 times on network errors and on status 408, 409, 429, 500, 502, 503, 504, 529. The wait is exponential (1s, 2s, 4s), or the `Retry-After` header value when it is 60 seconds or less. It does not retry after the stream starts.
- `readAPIError` extracts `error.message` from a JSON body when one exists.
- `readSSE` joins `data:` lines into one event and dispatches on each blank line. The scanner buffer is 32 MB. `errStopStream` ends the stream cleanly (OpenAI `[DONE]`).

OpenAI-compatible client (`openai.go`) — used for OpenAI, DeepSeek, Ollama, LM Studio, OpenRouter, Groq, Mistral, xAI, and custom endpoints:

- `POST {base}/chat/completions` with `stream: true` and `stream_options.include_usage: true`.
- The system prompt is the first message, with role `system`.
- An assistant message with no text and no tool calls is skipped. Empty tool arguments become `{}`.
- `reasoning_content` goes back only when `sendReasoning` is true (DeepSeek).
- Stream parse: `delta.content` → text. `delta.reasoning_content` (DeepSeek) and `delta.reasoning` (Ollama, OpenRouter) → reasoning. `delta.tool_calls` accumulate by `index`. A new `id` at an index that already has a different id starts a new call, because some servers reuse index 0. A missing id becomes `call_N`.
- `ListModels`: `GET {base}/models`, sorted by id. It reads the context size from `context_length` (OpenRouter), `context_window` (Groq), or `max_context_length` (Mistral), whichever is largest.

Gemini client (`gemini.go`) — native API, not the OpenAI-compatible layer:

- `POST {base}/models/{model}:streamGenerateContent?alt=sse` with header `x-goog-api-key`.
- `systemInstruction` carries the system prompt. Tools go in `tools[0].functionDeclarations` with `parameters` (the same JSON schema; keep schemas simple: no `additionalProperties`, no `$schema`).
- `geminiContents` maps roles: `user` → `user`; `assistant` → `model` (text part plus `functionCall` parts); `tool` → `user` with a `functionResponse` part whose response is `{"output": ...}` or `{"error": ...}`. Neighbor contents with the same role merge, so parallel tool results become one `user` content.
- Thought signatures: a function call from Gemini keeps its `thoughtSignature` in `ToolCall.Signature` and sends it back. When a call has no signature, the model is not `gemini-1*` or `gemini-2*`, and the call is the first in its message, mga sends the placeholder `skip_thought_signature_validator`. Gemini 3 rejects a function call without a signature.
- Stream parse: parts with `thought: true` → reasoning. Other text → text. `functionCall` → tool call. `promptFeedback.blockReason` → error.
- `ListModels`: pages through `GET {base}/models?pageSize=1000`, keeps models that support `generateContent`, removes the `models/` prefix, and sets `ContextWindow` from `inputTokenLimit`.

### 5.3 `internal/tools`

`Tool` interface: `Name`, `Description`, `Schema` (JSON schema map), `ReadOnly`, `Summary(input)` (one line for the UI), `Run(ctx, env, input) (string, error)`.

A tool returns `(output, error)`. The agent loop turns an error into a tool message with `IsError: true` and the text `output + "Error: " + err`. So a tool can return partial output together with an error (Bash does this for a non-zero exit code).

`Env` (one per agent):

- `Cwd` — all relative paths resolve against it. `~/` expands.
- `readFiles` — the time each path was read or written. `checkFresh` refuses Write and Edit on an existing file that this env did not read, or that changed after the read. A new file needs no read.
- `todos` and `OnTodos` — TodoWrite state and a UI callback.
- `Spawn` — set only on the main agent's env (by `Runtime.MainAgent`). Sub-agent envs have `Spawn == nil`, so a sub-agent cannot start another sub-agent.

The tools:

| Tool | ReadOnly | Notes |
|---|---|---|
| `Bash` | no | `bash -c` (falls back to `sh`) in `Cwd`. No stdin. Timeout 2 min by default, 10 min maximum. Own process group; cancel kills the whole group. Output (stdout + stderr) is capped at 30 000 bytes. A non-zero exit returns `exit code N` as the error. Unix only. |
| `Read` | yes | `cat -n` format, 2000 lines by default, `offset`/`limit`, lines cut at 2000 chars. Reports empty, binary (NUL in the first 8000 bytes), and directories. Marks the file as read. |
| `Write` | no | Needs a fresh read for an existing file. Creates parent dirs. Keeps the file mode. |
| `Edit` | no | Exact string replace. Must be unique unless `replace_all`. An empty `old_string` on a missing file creates the file. Needs a fresh read. |
| `Glob` | yes | doublestar pattern, newest first, 200 results maximum. Skips `.git`, `node_modules`, `.hg`, `.svn`, `.idea`, `.venv`, `__pycache__`. |
| `Grep` | yes | Pure Go (RE2), no ripgrep. Modes: `files_with_matches` (default), `content` (`path:line:text`, with context lines marked `-`), `count`. Skips the dirs above and hidden dirs, binaries, and files over 5 MB. `glob` filters by base name, or by relative path when the pattern has a `/`. 500 output lines by default. |
| `WebFetch` | no | GET with a 30 s timeout. 5 MB read limit. HTML → text with regexes. Output capped at 100 000 chars. |
| `TodoWrite` | yes | Replaces the whole list. Statuses: `pending`, `in_progress`, `completed`. |
| `Task` | yes | Starts a sub-agent through `Env.Spawn`. Its description lists the available agent types live, through the `Agents` function. Inputs: `description`, `prompt`, `subagent_type`, `run_in_background`. |

`Task` is marked read-only on purpose: the sub-agent's own tool calls go through permissions.

`tools.All(agents)` returns all tools in a fixed order. `Select(all, names)` keeps the named tools (an empty list keeps all). `Without(all, "Task")` removes one tool.

Helpers: `Truncate(s, n)` cuts long tool output and notes how much it cut. `OneLine(s, n)` collapses white space and cuts at `n` bytes, but only on a UTF-8 character boundary, so Chinese text and emoji stay whole.

### 5.4 `internal/agent`

**`agent.go` — the loop.** `Agent{Name, Provider, Model, System, Tools, Env, Perms, Approve, Review, StopOnDeny, MaxSteps}` plus the private `userRequest` (the latest user message, set at the start of `Run` for the auto-mode review). `MaxSteps` is 200 for the main agent and 150 for a sub-agent.

- `Run` never returns a broken history. Every assistant message with tool calls is followed by exactly one tool message per call, in the same order. This matters because OpenAI rejects a history with a tool call that has no result. On cancel or deny, the remaining calls get a result like `Not run: ...`.
- When **all** calls in one response are read-only, they run in parallel (goroutines with a `sync.WaitGroup`). Otherwise they run one by one. This lets the model start several sub-agents at the same time.
- Tool output for the model is capped at 60 000 chars.
- `ErrDenied` stops the main turn when the user denies a call (`StopOnDeny`), like Claude Code. The user then types what to do instead.
- `LastAssistantText(msgs)` returns the last non-empty assistant text.

**`permission.go` — permissions.**

- Modes: `default`, `acceptEdits`, `plan`, `auto`, `bypassPermissions`. `Mode.Next()` cycles in that order (shift+tab). `ParseMode` also accepts `ask`, `edits`, `accept`, `bypass`, `yolo`.
- `Mode.Label()`: "⏵ default mode", "⏵⏵ accept edits on", "⏸ plan mode on", "⏵⏵ auto mode on", "⏵⏵ bypass permissions on". `Mode.Persistent()` is false only for bypass.
- `check(tool, input)` returns allow, ask, block, or review:
  - a read-only tool → allow;
  - plan mode → block, with a message that tells the model to present a plan;
  - bypass → allow;
  - acceptEdits and the tool is Edit or Write → allow;
  - a rule matches → allow;
  - auto mode and the subject matches `autoSafeRules` (a fixed list of read-only and build/test commands, with the same no-shell-operator rule) → allow;
  - auto mode otherwise → review;
  - otherwise → ask.
- Rule syntax: `Tool` (all uses), `Tool(exact)`, `Tool(prefix:*)`. The subject is the trimmed command for Bash, and `domain:<host>` for WebFetch. A Bash prefix rule never matches a command that contains `&&`, `||`, `;`, `|`, backtick, `$(`, `>`, `<`, newline, or `&`.
- "Yes, don't ask again" (`AllowAlways`) → `remember`: in default mode, Edit and Write switch the session to `acceptEdits`; in other modes they add the tool-name rule, so auto mode stays on. Bash adds the rule from `bashRule`: for a tool with subcommands (`subcommandTools`: git, go, npm, pnpm, yarn, cargo, docker, kubectl, gh, pip, brew, make, terraform, helm) it is `Bash(<tool> <subcommand>:*)`, so allowing `git push` does not allow `git reset --hard`; for any other command it is `Bash(<first word>:*)`; for a chained command, a flag in the second position, or a bare subcommand tool it is the exact command `Bash(<command>)`, because a prefix rule never matches a chained command. The approval label says which ("`git push` commands" or "this exact command"). WebFetch adds `WebFetch(domain:<host>)`. Other tools add the tool name. These rules live in memory for the session only. `allowed_tools` in the config file and `--allowed-tools` load rules at startup.
- The `Approver` function blocks the agent goroutine until the user answers or the context ends. `ApprovalRequest{Agent, Tool, Summary, Input, AlwaysLabel, Reason}`; `Reason` carries the auto-mode reason to the approval box.

**`review.go` — auto mode review.** When `check` returns review, `Agent.review` decides. An Edit or Write whose resolved `file_path` is inside `Env.Cwd` (and not under `.git/`) is allowed without a model call. Otherwise it calls `Agent.Review` (set to `Runtime.Review` for the main agent and sub-agents) with the tool, the raw input, the cwd, and the latest user message (`Agent.userRequest`, set at the start of `Run`). `Runtime.Review` sends `reviewSystemPrompt` plus one user message to the **current** provider and model, with no tools and a 60 s timeout. `ParseReview` takes the text between the first `{` and the last `}`, so code fences and extra words are fine. allow → run. block → an error tool result "Auto mode blocked this call: <reason> …" with no prompt. ask → depends on `Agent.AskPolicy`, set from the config key `auto_mode_ask` (`ParseAskPolicy`; main checks the value at startup): `allow` (the owner's chosen default, also for an empty value) runs the call and puts "(Auto mode allowed a flagged call: <reason>)" at the start of its tool result; `prompt` shows the normal approval prompt with `ApprovalRequest.Reason` = "Auto mode: <reason>"; `block` acts like a block verdict. Any review error → prompt, whatever the policy, because an unreviewed call must not run silently. In `-p` mode a prompt means deny.

**`runtime.go` — the runtime.** One `Runtime` per process. It holds the config, cwd, agent store, permissions, task registry, approver, base context, a provider cache, and the **current provider and model** (`SetCurrent` / `Current`, mutex-guarded).

- `Provider(name)` creates and caches a client. `RegisterProvider(name, p)` injects one (tests use it).
- Locked config writes for the UI: `SetProviderKey(name, key)` (saves the key and drops the cached client), `SaveDefault(provider, model)`, and `SetMode(mode)` (sets the mode and saves `permission_mode` unless the mode is not `Persistent`). `ProviderConfig(name)` is the locked read. `resolveModel` also reads the config under the lock.
- Context sizes: `RememberModels(provider, models)` caches the sizes from a model list. `ContextWindow(provider, model)` returns, in order: the provider's `context_window`, the cached size, `llm.KnownContextWindow(model)`, or 0.
- `Review(ctx, ReviewRequest)` is the auto-mode reviewer (see `review.go`).
- `MainAgent(env)` builds the main agent for the current model and sets `env.Spawn = r.Spawn`.
- `Spawn(ctx, SpawnRequest)`:
  1. finds the definition (default `general-purpose`),
  2. resolves the model: an empty model or `inherit` uses the current one; otherwise `ParseModelRef`,
  3. builds an `Agent` with the definition's tools minus `Task`, a new `Env`, and the shared `Perms` and `Approve`,
  4. registers a `Task` in the registry,
  5. **foreground**: runs under the caller's context and returns the sub-agent's last text as the tool result;
  6. **background** (only when `BackgroundAllowed`): runs under `BaseCtx` (so Esc on the main turn does not stop it) and returns at once with a "started" message. `NotifyMain` is true unless the user started it by hand from the UI (`SpawnRequest.Manual`).

**`tasks.go` — the sub-agent registry.** `TaskRegistry` holds `Task` records: id (`a1`, `a2`, …), agent, model, description, prompt, background flag, `NotifyMain`, status (`running`, `completed`, `failed`, `stopped`), times, a log of up to 1000 lines, result, error, tool count, and tokens.

- `Snapshot()` and `Get()` return copies, so the UI never reads a record under change.
- `Stop(id)` marks the task and cancels its context. `finish` then records `stopped`.
- `OnChange` and `OnFinish` are callbacks for the UI. They run **outside** the registry lock.
- `taskObserver` writes the log: assistant text lines, `→ Tool(summary)` on start, and `  ⎿ first line` on the result.

**`prompt.go` — system prompts.** `MainPrompt(cwd, provider, model)` = fixed instructions + environment (cwd, git repo yes/no, platform, date, model) + memory files. `SubAgentPrompt(def, cwd)` = the definition's prompt + a sub-agent note + environment + memory files. The sub-agent note contains the text "You run as a sub-agent"; a test uses this text to tell sub-agent requests apart. Memory files, in order: `~/.mga/MGA.md`, then `MGA.md`, `AGENTS.md`, `CLAUDE.md`, `.mga/MGA.md` in the cwd. Each is capped at 40 000 chars.

**`session.go` — sessions.** One JSON file per session in `~/.mga/sessions/<id>.json`. The id format is `YYYYMMDD-HHMMSS-<6 hex>`. The file holds cwd, provider, model, title, times, all messages, `usage` (session input and output tokens), and `context_tokens` (input + output of the last request). The TUI writes both in `saveSession` after every turn, and `newApp` and `loadSession` restore them, so the status bar shows the same tokens and context percentage after a resume. Print mode adds its usage too. mga stores these numbers and does not compute them again, because it has no tokenizer for the providers. Sessions saved before 2026-10-07 have no usage and start at 0. The title is the session name, and `title_source` (`TitleSource`) says where it came from: `text` (`Save` sets it from the first user message, 80 bytes via `OneLine`, when the title is empty), `model`, or `user` (`Rename`, the `r` key in the home view). `SetTitle(dir, title, source)` writes without a change to `Updated`. **Model titles:** `App.startTurn` checks `needsTitle` (no earlier user message, not a `<task-notification>`, and no user title); then it adds `titleCmd` to the turn's commands. That runs `Runtime.SessionTitle` (`title.go`): one request to the current model with `titleSystemPrompt` and the first request (4,000 characters at most), no tools, a 30 s timeout. `CleanTitle` keeps the first non-empty line, removes a `Title:` or `#` prefix, quotes, `*`, and an ending period, and keeps at most 8 words. `App.applyTitle` saves it as `model` on the current session, or on the saved file when the user switched sessions; it never replaces a `user` title. A failed title call changes nothing, so the `text` title stays. Print mode does not ask for a model title. `Save` writes a temp file, then renames it. An empty session is not saved. `ListSessions(dir, cwd)` filters by cwd and sorts newest first. `Rename(dir, title)` writes the new title without a change to `Updated`, so a rename does not reorder the list; an empty session keeps the name in memory until its first save. `DeleteSession(dir, id)` removes the file.

### 5.5 `internal/agentdef`

- A definition file is Markdown with YAML frontmatter: `name`, `description`, `model` (`inherit` is stored as empty), `tools` (comma string or YAML list; `*` means all), `color` (stored but not used yet). The body is the system prompt.
- Scopes: `built-in`, `user` (`~/.mga/agents/`), `project` (`<cwd>/.mga/agents/`). A project definition replaces a user or built-in definition with the same name.
- `List()` returns built-ins first, then the others sorted by name (case-insensitive). It returns the definitions it could load together with a joined error for bad files.
- `Save(def, previousPath)` checks the name (`^[A-Za-z0-9][A-Za-z0-9_-]*$`) and the description, writes `<dir>/<name>.md`, and removes `previousPath` when the name or scope changed. `Delete` refuses built-ins. `Draft` writes a template (used by `mga agents new`).
- Built-ins: `general-purpose` (all tools except Task), `Explore` (Read, Glob, Grep), `Plan` (Read, Glob, Grep).

### 5.6 `internal/tui`

Bubble Tea v1 in **inline mode** (no alt screen), like Claude Code. Finished output goes to the normal terminal scrollback with `tea.Println`. The program's `View()` shows only the live part at the bottom: streaming text, running tools, a status line, and the input box or an overlay.

Files:

| File | Content |
|---|---|
| `app.go` | `App` model, `Run`, `Init`, `Update`, key handling, turn start and finish, background notifications, history recall, `View`, live view, `setMode`, two-line `statusLine` and `statusRow`, approval box |
| `messages.go` | all `tea.Msg` types and `uiObserver` (agent events → `p.Send`) |
| `commands.go` | slash commands, help, status, `/compact`, `/init` prompt, `setModel` |
| `picker.go` | `/model` overlay: provider list → key screen (masked input, check, save) → live model list with filter; also `closeOverlay` and `openURL` |
| `agents.go` | `/agents` and `/tasks` overlay: Library tab and Running tab, detail, delete confirm, run prompt, task log, `$EDITOR` |
| `form.go` | create/edit form for an agent definition |
| `home.go` | Home view (← on an empty input, or `/resume`): sessions of this directory plus sub-agents of this run; open (→), rename (r), new (n), delete (d), stop agent (x), agent log; also `newSession` and `loadSession` |
| `render.go` | styles, glamour Markdown, tool result blocks, diffs, to-dos, clip, list window helper, approval preview, `titledBox` (input box with the session name) |

`App` is a pointer model (`*App` implements `tea.Model`). `Update` always returns the same pointer.

Overlays: `App.view` is `viewChat`, `viewModels`, `viewAgents`, or `viewHome`. A pending approval has priority over every view, for keys and for rendering.

Slash commands: `/help`, `/model [ref]`, `/agents`, `/tasks`, `/mode [mode]`, `/permissions`, `/clear`, `/compact [focus]`, `/resume`, `/init`, `/status`, `/exit` (plus aliases: `/models`, `/bashes`, `/new`, `/continue`, `/sessions`, `/quit`, `/q`, `/?`). `/resume` opens the home view.

Keys in chat: ← on an empty input opens the home view; Enter sends (queues while busy); Ctrl+J, Alt+Enter, or `\` + Enter for a new line; Esc interrupts or clears; Shift+Tab cycles the mode; Up/Down recall earlier inputs; Tab completes a slash command; Ctrl+C interrupts, clears, or (twice in 2 s) quits; Ctrl+D on empty input quits.

Behavior details:

- **Mode changes.** Shift+Tab and `/mode` call `App.setMode` → `Runtime.SetMode`. That saves `permission_mode` to the config file, so the next start uses the same mode. Bypass is never saved, so the file keeps the last other mode. A `--permission-mode` flag overrides the saved mode for one run.
- **Input box.** `titledBox` draws it. When `App.session.Title` is set, the top border is built by hand as `╭──── <name> ─╮` above a body rendered with `styleBox.BorderTop(false)`. The name sits at the right corner, and the top line has the same width as the box. A long name is shortened with `…` by display width. An unnamed session keeps the plain border.
- **Status bar.** `statusLine` always returns two lines. It is modeled on the owner's Claude Code status line script (`model | tokens | % ctx remaining`, without cost).
  - Line 1: `<model> (<provider>) | <session tokens> tokens | <N>% ctx remaining | <n> agent(s) running`. Session tokens are `App.usage` input + output. The percentage is `remainingPercent(App.contextTokens, window)`, where `contextTokens` is the last request's input + output and `window` is `Runtime.ContextWindow`. The percentage is hidden when the window is 0 (unknown).
  - Line 2: the mode label (dim in default mode) and "(shift+tab to cycle)" on the left; the notice or a "← sessions · /help" hint on the right. On a narrow terminal the "(shift+tab to cycle)" text is dropped first.
  - `statusRow` and `clip` keep each line at most `width - 1` columns, so the terminal never wraps it and the bar height never changes.
- **Approval box.** It shows `ApprovalRequest.Reason` (the auto-mode reason) under the summary, and a diff preview for Edit and Write.
- **`/model`.** It sets the current model and saves it as the default with `Runtime.SaveDefault`. The picker calls `Runtime.RememberModels` after each model list, so the status bar knows the context size of listed models. That cache lives in memory only, so `learnContextWindow` refills it: at startup (`Init`) and after a session opens from the home view, when the current model has no known size, it fetches the provider's model list once in the background; a model with a known size (config, cache, or the built-in table) causes no request.
- **`/compact`.** It sends a text transcript (not structured messages, so it works on every provider) with a summary prompt. Then it replaces the history with two messages: the summary (user) and an acknowledgment (assistant).
- **Background agents.** Their results arrive as a `<task-notification>` user message. mga sends it automatically when the main agent is idle.
- **Screen on session change.** `newSession` (`/clear`, `n` in the home view, deleting the current session) and `loadSession` (opening a session) call `resetScreen`: it drops queued output and sets `clearScreen`, and the next `flush` sends `tea.ClearScreen` and prints `\x1b[3J` (clear scrollback) in front of the queued text, in one `tea.Sequence`. A new session then shows the start banner. An opened session shows the banner, an "Opened session" note, and `replay`.
- **Replay.** `replay` prints the last `replayLimit` (200) messages after a "… N earlier messages" note: user lines, assistant Markdown, and each tool result as a full tool block (`formatToolBlock`, with `toolSummary` from the tool call's arguments). `<task-notification>` messages replay as a note. At startup (`-c`, `--resume`) the replay waits for the first `tea.WindowSizeMsg` (`replayPending`), so it uses the real terminal width.
- **Markdown padding.** Glamour pads lines with spaces wrapped in color codes. `trimStyledSpaces` removes them, so printed lines do not wrap when the terminal gets narrower.
- **MCP in the UI.** `tui.Run` takes the `*mcp.Manager`, sets `OnChange` to `go p.Send(mcpChangedMsg{})`, and starts `ConnectAll` in a goroutine, so startup does not wait for servers. `/mcp` opens `mcpView` (`mcpview.go`): the server list, a detail page (target, server name and version, error, stderr tail, instructions, tools), `a` approve, `r` reconnect. `reportMCP` prints one chat note when a server fails or needs approval, and when it connects after one of those; `Init` calls it once, so a project that has only unapproved servers still gets its note. `displayToolName` shows `mcp__s__t` as `s - t (MCP)` in tool blocks and approval boxes. `/status` shows the connected count.

### 5.7 `internal/mcp`

- **Config (`config.go`).** `ServerConfig{type, command, args, env, url, headers}`, the same keys as Claude Code's `.mcp.json`. `Transport()` is `stdio` for a command, `http` for `type: http` (also `streamable-http`) or a URL without a command; anything else, such as `sse`, fails at connect with "not supported". Files: `ProjectConfigPath(cwd)` = `<cwd>/.mcp.json`, `UserConfigPath()` = `~/.mga/mcp.json` (written with mode 0600, because it can hold tokens). `WriteServer` keeps other top-level keys. `expand` resolves `${VAR}` and `${VAR:-default}` (and `$VAR`) at connect time, not when the file is read.
- **Client (`client.go`).** JSON-RPC 2.0 with numeric ids and a pending map. `initialize` sends protocol version `2025-06-18` with empty client capabilities, then `notifications/initialized`. `listTools` follows `nextCursor` (100 pages at most). `callTool` sends `{name, arguments}`. A cancelled context sends `notifications/cancelled`. Server requests get answers: `ping` → `{}`, anything else → error -32601 (mga advertises no client capabilities, so servers should not ask for sampling or roots). `notifications/tools/list_changed` re-lists the tools. When the transport closes, every pending call fails with the close reason.
- **Transports (`transport.go`).** stdio: the command runs in the project directory with the parent environment plus `env`, in its own process group; one JSON message per line; stderr goes to a 4 KB tail buffer that error messages and `/mcp` show; `close` closes stdin, waits 2 s, then kills the group. HTTP: every message is a POST with `Accept: application/json, text/event-stream`; the answer is JSON, an SSE stream (read to its end inside `send`), or 202 for notifications. mga keeps `Mcp-Session-Id` from any response and sends it and `MCP-Protocol-Version` (from `initialize`) on later requests; `close` sends DELETE with the session id. No GET stream, so the server cannot push messages outside a request.
- **Tools (`tool.go`).** `Tool` implements `tools.Tool`. `ExposedName` = `mcp__<server>__<tool>` with characters outside `[A-Za-z0-9_-]` replaced by `_`; names over 64 characters end in a short SHA-1 suffix. `ReadOnly` comes from `annotations.readOnlyHint`. `Description` is "[MCP server s] …", at most 1,000 characters. `normalizeSchema` drops `$schema` and adds `type: object` and empty `properties` when missing (a nil schema is fine). `FormatResult` joins text parts, describes images, audio, and resources in one line, and falls back to `structuredContent`. `isError: true` becomes a tool error. A tool whose server is not connected returns "not connected; check /mcp".
- **Manager (`manager.go`).** `Load(cwd, approved)` reads both files; a project server replaces a user server with the same name, and a project server not in `approved` starts as `needs approval`. `ConnectAll` connects the others in parallel with a 30 s limit for start, `initialize`, and the tool list; a failure keeps the error plus the stderr tail. `Approve` calls `PersistApproval` (the runtime saves `approved_mcp_servers[cwd]` in the config), then connects. `Reconnect` closes the old client first. `fail` ignores a client that is no longer the server's current client, which prevents an old client's close from marking a new connection as failed. `Tools`, `Instructions`, `Snapshot`, and `Count` read under the manager lock. `Close` stops every server.
- **Wiring.** `cmd/mga` loads the manager at startup and sets `rt.MCP`. The TUI connects in the background. `-p` mode calls `connectForPrint` first: it waits for the servers and prints one stderr line for each failed or unapproved server. `Runtime.MainAgent` appends the MCP tools and adds "# MCP server instructions" to the system prompt. `Spawn` selects from built-in tools plus MCP tools, so an agent definition can list single MCP tools, and an agent with no list gets all of them (still minus `Task`). `RuleMatches` treats a bare `mcp__<server>` rule as "all tools of that server". `llm.geminiParameters` removes JSON Schema keys that Gemini rejects (`$schema`, `additionalProperties`, `$ref`, `const`, `examples`, and others), turns `type: [x, "null"]` into `type: x, nullable: true`, and leaves out the parameters of a tool with no properties, because Gemini rejects an empty object schema.
- **CLI (`cmd/mga/mcp.go`).** `mga mcp list | get <name> | add | remove | approve <name>`. `add` takes `-s user|project` (default user), `-t stdio|http` (default http for an `http(s)://` target), repeatable `-e KEY=VALUE` and `-H "Name: value"`, then `<name>`, an optional `--`, and the command and its arguments or the URL. A server added with `-s project` is approved at once. `remove` searches the project file, then the user file, unless `-s` is given.

---

## 6. Gotchas and invariants (read before you change the TUI or the loop)

1. **Never call `p.Send` synchronously from inside `Update`.** The program's message channel has no buffer, so this deadlocks. The registry callbacks (`OnChange`, `OnFinish`) can run inside `Update` (for example `Spawn` from the agents view, or `Stop`), so `attach` wraps them in `go p.Send(...)`. The `uiObserver` and `OnTodos` calls run on agent goroutines, so they send synchronously. That keeps the event order (deltas before the assistant message before tool results).
2. **Print order.** `tea.Println` commands from different `Update` calls run in separate goroutines and can arrive out of order. `App.emit` only queues text. `App.flush` sends one `tea.Sequence(tea.Println(all queued), printDoneMsg)` at a time. The next flush waits for `printDoneMsg`. Always print through `emit`. Never return `tea.Println` directly.
3. **The `turnDoneMsg` comes last.** The turn goroutine returns `turnDoneMsg` only after `Run` returns. All observer sends happen before that, so the final message arrives after all events of the turn.
4. **Approval lifetime.** Each approval carries the agent's context. `activeApproval` drops approvals whose context ended (the turn was interrupted). The approver goroutine then returns `Deny` by itself.
5. **Background-color query at startup.** `bubbletea`'s own package `init()` calls `lipgloss.HasDarkBackground()`. That sends an OSC 11 query to the terminal and waits up to 5 seconds for an answer. Real terminals answer at once. A pseudo-terminal in a test must answer `\x1b]11;rgb:0000/0000/0000\x07\x1b[1;1R`, or the program appears to hang. `tui.Run` reads the cached result, applies `MGA_THEME=light|dark` when set, and calls `lipgloss.SetHasDarkBackground`.
6. **Glamour.** `markdown.render` copies glamour's dark or light style and sets the document margin to 0 and the block prefix and suffix to empty. `formatAssistant` can then put `⏺ ` on the first line and two spaces on the others. The renderer is rebuilt when the width changes.
7. **Textarea quirks (bubbles v1.0.0).** mga sets `CharLimit` to 0 (no limit). That is the v1.0.0 default, but older bubbles versions used 400, so keep the explicit value. `MaxHeight` above 0 also limits the **number of lines** a user can type, so mga sets 0 and controls the height itself (1 to 10 rows). The height counts **screen rows after soft wrap**, not typed lines: `wrappedRows` (render.go) copies the textarea's word-wrap rule, and `TestWrappedRowsMatchesTextarea` compares it with `LineInfo().Height` on 2,000 random lines. Change the input only through `App.editInput(edit)`: it raises the height to the maximum, applies the edit, then shrinks the box to fit. The textarea never scrolls back up when it grows, so an edit at the small height would hide the first row. Set `FocusedStyle` before `Focus()`, because `Focus` stores a pointer to the style.
8. **Spinner.** Return `a.spin.Tick` whenever animation can start. `spinner` drops ticks with an old tag, so extra ticks do not speed it up. The tick loop stops when `animating()` is false: not busy, no running sub-agents, and the picker is not loading models or checking a key.
9. **History validity.** Any code that edits `App.history` must keep tool calls and tool results paired. `finishTurn` uses the `msgs` from `Run`, which are always valid.
10. **Provider switch inside one session.** The neutral `Message` type makes this work. Reasoning text is sent back only to DeepSeek. Gemini signatures are sent only to Gemini, with the placeholder rule above.
11. **The tool schemas** must stay in the subset that both OpenAI and Gemini accept: `type`, `properties`, `required`, `description`, `items`, `enum`. Do not add `additionalProperties`, `oneOf`, or `$ref`.
12. **Config writes at runtime.** Background agents read the config while the UI writes it. Write only through the locked `Runtime` methods (`SetProviderKey`, `SaveDefault`, `SetMode`). Do not set `rt.Cfg` fields directly from the UI.
13. **Inputs inside overlays.** Non-key messages (cursor blinks) go to the chat textarea by default. The key screen's `textinput` gets them only because `App.update` forwards them while `picker.stage == stageKey`. Do the same for a new overlay with a focused input.
14. **Headless tests and timers.** A focused `textinput` or `textarea` returns a blink command that returns another blink command forever. Test helpers that run commands (`drain`) must skip `spinner.TickMsg` and `cursor` messages, or the test hangs.
15. **Moving work between machines.** Use git (`git pull`). A plain file copy keeps files that were deleted on the other side: a leftover `internal/tui/resume.go` once broke the Linux build (duplicate `loadSession`). Never commit `bin/`: a binary built on Linux does not run on macOS, and the reverse.
16. **MCP callbacks.** `Client.onClosed` and `onToolsChanged` run in their own goroutines, and the manager's `OnChange` goes through `go p.Send`. Never set `onClosed` after the client starts: its reader goroutine reads it. To ignore an old client, take it out of `server.client` first; `fail` then ignores it.
17. **MCP security.** Never start a project `.mcp.json` server before approval: the file comes from the repository, and its command runs with the user's rights. Approvals are per directory (`approved_mcp_servers` in `~/.mga/config.json`).
18. **The `--` in `mga mcp add`.** Go's flag parser drops `--` only before the first argument. Here it comes after the server name, so `mcpAdd` removes it by hand; without that, `--` became the command.

---

## 7. Install

`make install` is a safe user install. It runs `go test ./...`, builds with `-trimpath -ldflags "-s -w"`, and checks that `bin/mga --help` exits 0. Then it installs to `$(INSTALL_DIR)` (default `$(PREFIX)/bin`, and `PREFIX` defaults to `~/.local`), with no `sudo`. It refuses a symlink or another file type at the target, and it refuses a directory that it cannot write to. It copies an earlier binary to `mga.bak`, writes `mga.tmp.<pid>`, and renames that file over the target in one step. It runs the installed binary, then warns when `INSTALL_DIR` is not on `PATH` or when `command -v mga` finds a different file. `make uninstall` removes only the binary. It keeps `mga.bak` and `~/.mga`.

## 8. Testing

```sh
make check        # gofmt check, go vet, race tests
make test         # quick tests
```

| Test file | What it covers |
|---|---|
| `internal/llm/llm_test.go` | OpenAI stream: text, reasoning, tool-call accumulation across chunks and indexes, usage, system message, `reasoning_content` passback, `{}` for empty args, no retry on 400. Gemini stream: thoughts, text, function call with signature, usage, content merge, placeholder signature on the first call only, error responses, system instruction, model paging and filter. Context sizes from OpenAI-compatible model lists; the `KnownContextWindow` table. |
| `internal/tools/tools_test.go` | Edit needs a read and a unique match, `replace_all`; Read format and offset; Write needs a read to overwrite; Glob and Grep skip `node_modules`, case-insensitive grep, content mode; Bash exit code and timeout; HTML to text; `OneLine` cuts on character boundaries (Chinese text). |
| `internal/agentdef/agentdef_test.go` | Parse/format round trip; save, list, project-over-user override, delete, built-ins, name check. |
| `internal/agent/agent_test.go` | A scripted provider drives the main agent: parallel Task + Glob, Explore gets three tools, the registry records the task; deny stops the turn and keeps the history valid; a background sub-agent finishes and calls `OnFinish` with `NotifyMain`; permission rule table and modes; `ParseReview` cases; the auto-mode check table and cycle; an auto-mode turn where an edit inside the project skips review, a write outside gets ask (approver sees the reason), `echo` gets allow, `rm -rf ~` gets block; `Runtime.Review` with a scripted model, and the fallback to ask on an unclear answer. |
| `internal/tui/app_test.go` | Headless UI: a full turn with history and session save; approval keys; the agents form creates a project definition; the model picker with a custom model id. Home view: ← opens it only on an empty input, sessions of other directories are hidden, → opens an older session with its history, Ctrl+R renames without a reorder, d + y deletes, Esc returns, an agent log opens and ← returns (`selectSession` moves to the top first, because the list is newest first). Key screen: rejected key not saved, good key saved with mode 0600, stale key sends the user back to the key screen. Mode cycle at widths 60, 80, 120: always two status lines, each narrower than the terminal, label on line 2, auto saved, bypass not saved. Status line text (`gpt-5 (fake) | 52.0k tokens | 87% ctx remaining`), unknown size hides the percentage, listed size and `context_window` override, notice and mode on a narrow terminal. `titledBox`: name right-aligned at three widths, even line widths, plain border without a name, long name shortened, name on the app view. The `drain` helper runs nested `tea.BatchMsg` commands and ignores spinner ticks and cursor blinks (gotcha 14). |
| `internal/mcp/mcp_test.go` | The test binary is also a fake stdio MCP server (`TestMain` with `MGA_FAKE_MCP=1`). Stdio: a project server waits for approval and exposes no tools, then connects; two tool pages; `readOnlyHint`; `$schema` removed; a nil schema; text, error, `${VAR:-default}` expansion, a server-to-client ping, cancel with `notifications/cancelled`, a crash with the stderr tail, a dead tool, and reconnect. HTTP: JSON and multi-line SSE answers, `Mcp-Session-Id` and `MCP-Protocol-Version` on later requests, DELETE on close, `structuredContent`, header expansion, and HTTP 401. Config files, names, and `FormatResult`. |
| `internal/agent/mcp_test.go` | A stub `ToolSource`: the main agent sees and runs the MCP tool, the system prompt has the server instructions, the `mcp__docs` rule allows the call, and a sub-agent gets exactly the tools its definition lists. |
| `internal/llm/gemini_schema_test.go` | `geminiParameters` drops unsupported keys, maps a nullable type, and leaves out an empty object schema. |
| `internal/tui/mcp_test.go` | A fake HTTP server plus a broken one plus an unapproved project server: one note each (only once), `/mcp` rows and the detail page, the `/status` line, and the `server - tool (MCP)` display name. |
| `cmd/mga/mcp_test.go` | `mga mcp add` for stdio (with `--` after the name) and http, mode 0600 on the user file, project add approves, invalid name, approve and remove errors. |

Manual end-to-end checks (scripts lived in the job's temporary directory and are not in the repo):

- 2026-10-05: a fake OpenAI-compatible server in Python. The first request returns a Bash tool call (`echo hello-from-tool`); a request with a tool result returns the result as text; a request whose system prompt starts with "You review one tool call" returns an auto-mode verdict. Point a `fake` provider at it in `$MGA_HOME/config.json` with `"local": true`. Then run `mga -p "..." --model fake:fake-small --allowed-tools "Bash(echo:*)" --verbose`, or `--permission-mode auto` without the rule.
- The TUI in a pseudo-terminal through Python `pty.fork`, with an answer to the OSC 11 query (gotcha 5). Arrow keys are `\x1b[D` (←), `\x1b[C` (→), `\x1b[B` (↓); Shift+Tab is `\x1b[Z`; Ctrl+R is `\x12`.
- 2026-10-05: the `/model` key screen against the real OpenAI API with a fake key: real 401, rejection shown, no config file written.
- 2026-10-07: home view (← → Ctrl+R; rename found in the session file), mode saved across a restart, the two-line status bar after a turn, and the session name on the input box of a resumed session.
- 2026-10-07: `make install` in a temporary `INSTALL_DIR`: first install, backup on reinstall, symlink refusal, read-only refusal, uninstall.

---

## 9. Known gaps and limits

- No successful chat with a real provider yet (see section 2).
- No automatic compaction when the context gets full. `/compact` is manual.
- The context size is unknown for most local models (Ollama, LM Studio) until the user sets `context_window` on the provider. The status bar then hides the percentage.
- No cost tracking. Only token counts.
- MCP covers tools only: no resources, no prompts as slash commands, no OAuth sign-in for HTTP servers (use a header with a token), and no old SSE transport. HTTP servers cannot push messages outside a request (no GET stream).
- No hooks, skills, or plugins.
- No `WebSearch` tool (it needs a search API).
- No image input. `Read` reports images as binary.
- Bash runs each command in a new shell in `Cwd`. `cd` does not persist between calls.
- Permission rules from "don't ask again" are not saved to the config file. The permission mode is saved.
- With the default `auto_mode_ask: allow`, calls that the reviewer flags as hard to undo (for example `git push`) run without a prompt; only block verdicts and failed reviews stop or ask. The auto-mode review uses the current model, adds one model call and a few seconds per reviewed call, and does not cache verdicts. A verdict is a model judgment, not a guarantee.
- The status bar layout is fixed. There is no custom status line command like Claude Code's `statusLine`.
- API keys are saved in plain JSON (`0600`), not in the OS keychain.
- Windows is not supported.
- The `color` field of an agent definition is not used in the UI.
- Background sub-agents stop when mga quits (`Tasks.StopAll`).
- `Read` loads the whole file into memory before it slices lines.

---

## 10. Roadmap

### 10.1 `/model` login flow

**Built (2026-10-05):**

- `picker.go` has three stages: `stageProviders` → `stageKey` → `stageModels`.
- Selecting a provider that is not `Configured()` opens the key screen. It shows `ProviderConfig.KeyURL` (Ctrl+O opens it with `open` on macOS or `xdg-open` on Linux), a masked `textinput` (`EchoPassword`, `•`), and a note that a saved key takes priority over the environment variable.
- `e` on the provider list opens the key screen for any provider that is not local, to replace a key.
- Enter runs `checkKey`: it builds a client with the new key and calls `ListModels`. A 401 or 403 shows "rejected", another error shows "could not check", and neither saves anything. On success, `Runtime.SetProviderKey` saves the key (under the runtime lock, then `Cfg.Save()`, and it drops the cached client). The model list then opens with the models from the check, and shows "✔ Key saved to …".
- When the model list load fails with 401 or 403 for a provider that has a key (stale key), the picker moves to the key screen with "rejected the current key".
- A local provider whose server does not answer shows a start hint (`ollama serve`) under the error.
- `llm.New` now tells the user to use `/model` when a key is missing.
- Built-in `KeyURL` values: OpenAI `https://platform.openai.com/api-keys`, Gemini `https://aistudio.google.com/apikey`, DeepSeek `https://platform.deepseek.com/api_keys`, OpenRouter `https://openrouter.ai/keys`, Groq `https://console.groq.com/keys`, Mistral `https://console.mistral.ai/api-keys`, xAI `https://console.x.ai`. A user can set `key_url` for a custom provider.
- Tests: `TestPickerAsksForKeyChecksAndSaves` and `TestPickerSendsRejectedSavedKeyToKeyScreen` in `internal/tui/app_test.go`. A manual pseudo-terminal run against the real OpenAI API showed the key screen, the spinner, and a real 401 rejection, with no config file written.

**Not built yet (from the design review):**

1. When the current provider has a key, open `/model` directly on the model list, with a "← change provider" row.
2. An "auto (recommended)" row at the top of the model list. It means the provider's `default_model`, not routing between models.
3. Remove a saved key from the UI, and edit the base URL of a local provider from the UI.
4. Optional OpenRouter sign-in with OAuth PKCE on its key screen (see 10.2).

### 10.2 OAuth facts (from the design discussion)

- **OpenRouter** has an official OAuth PKCE flow that returns a real API key. Open `https://openrouter.ai/auth?callback_url=<local callback>&code_challenge=<S256 challenge>&code_challenge_method=S256`, receive `code` on the local callback, then `POST https://openrouter.ai/api/v1/auth/keys` with the code and verifier to get `{"key": ...}`. This is a good optional "sign in" on the OpenRouter key screen.
- **Gemini** accepts Google OAuth access tokens (`Authorization: Bearer`). That needs the owner's own Google Cloud OAuth client, or gcloud credentials. It needs a token source with refresh in the Gemini client, in place of the fixed key header.
- **OpenAI** and **DeepSeek** have no OAuth for third-party apps. Use the pasted key.
- Do not reuse the OAuth client IDs of other products (Codex CLI, Gemini CLI).

### 10.3 Later ideas

Keychain storage for keys; auto-compaction near the context limit (the status bar already knows the percentage); persistent Bash working directory; saved permission rules; cost estimates per model; a custom status line command that gets Claude Code-style JSON on stdin; a context size lookup for Ollama (`/api/show`); MCP resources (`@` mentions) and MCP prompts (as slash commands); OAuth for HTTP MCP servers; the legacy SSE MCP transport; `WebSearch`; image input for vision models; agent colors in the UI.

---

## 11. How to work on this project

1. Read this file and `README.md`.
2. Run `make check` before you change code, and after.
3. Follow the rules in section 1.2: no comments, no `>` or `>=`, positive names, STE prose.
4. For a new provider type, implement `llm.Provider`, add the type in `config` and `llm.New`, and add a fake-server test like the ones in `llm_test.go`.
5. For a new tool, implement `tools.Tool`, add it to `tools.All`, keep the schema in the safe subset (gotcha 11), decide `ReadOnly` with care (it controls permissions and parallel runs), and add a `toolBody` case in `tui/render.go` if the default preview is not good.
6. For TUI work, follow gotchas 1 to 8 and 12 to 14. For MCP work, follow gotchas 16 and 17. Add a headless test in `internal/tui/app_test.go`. Check the result in a pseudo-terminal when the layout changes.
7. Update this file when you change a design decision, an invariant, or the roadmap.
