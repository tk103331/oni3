English | [中文](README_CN.md)

Oni3 is an intelligent AI Agent command-line tool based on the [CloudWeGo Eino](https://github.com/cloudwego/eino) framework. It provides a powerful Agent system with support for multiple tool integrations and various AI model providers, enabling you to easily build and run custom AI Agents.

## Features

- **Intelligent Agent System**: AI Agent based on ReAct pattern with tool calling and reasoning capabilities
- **Interactive TUI**: Terminal UI for real-time conversations with agents, including slash commands, tool approval and session resume
- **Web Mode**: Local browser interface with streaming replies, tool cards, approvals, shared session history and optional token authentication
- **AG-UI and A2A**: Native AG-UI SSE and A2A 1.0 JSON-RPC endpoints, with agent cards, streaming, interrupts and task persistence
- **ACP Agent Mode**: Runs as an [Agent Client Protocol](https://agentclientprotocol.com) agent so editors like Zed can drive Oni3
- **First-Run Onboarding**: Common providers ship as built-in presets; pick one, enter an API key and import its models without editing YAML
- **Rich Tool Ecosystem**: Built-in various tools including search, browser, command line, HTTP requests, etc.
- **Multi-Model Provider Support**: Supports OpenAI, Claude, Gemini, Qwen, DeepSeek, Ollama, Baidu Qianfan, ByteDance Doubao, and more
- **Flexible Configuration System**: Manage agents, tools, models and skills through YAML configuration files
- **Custom Tool Support**: Supports custom HTTP and command-line tools
- **JavaScript Plugins**: Extend tools, lifecycle hooks, themes, agents, models and providers through embedded moejs ES modules
- **MCP Client Integration**: Connects to Model Context Protocol (MCP) servers over SSE, streamable HTTP and STDIO
- **MCP Server Mode**: Exposes `call_agent`, `list_agents` and `list_models` over stdio
- **Langfuse Integration**: Built-in observability with Langfuse for monitoring and tracing

## Installation

### Install

**Go install** (requires Go >= 1.25):

```bash
go install github.com/tk103331/oni3@latest
# If you hit `sumdb ... 404` (checksum DB indexing delay for a new release):
# GONOSUMDB=github.com/tk103331/oni3 go install github.com/tk103331/oni3@latest
```

**npm**:

```bash
npm install -g @tk103331/oni3    # provides the `oni3` command
oni3
```

**Homebrew**:

```bash
brew install tk103331/tap/oni3   # provides the `oni3` command
```

**Scoop** (Windows):

```powershell
scoop bucket add oni3 https://github.com/tk103331/scoop-bucket
scoop install oni3   # provides the `oni3` command
```

**Releasing (maintainers)**:

```bash
git tag v0.1.0 && git push --tags
```

GitHub Actions (`.github/workflows/release.yml`) will:
1. build the `oni3` binary for darwin/linux/windows × amd64/arm64 with goreleaser, pack it as `oni3_<version>_<os>_<arch>.tar.xz` (`.zip` on Windows) (~12–17 MB instead of the ~50 MB stripped binary) and create a Release
2. publish the npm package via OIDC when the repository variable `NPM_TRUSTED_PUBLISHING=true` is set and the npm package has a GitHub Actions trusted publisher (version taken from the tag, no token needed)
3. update the `tk103331/homebrew-tap` formula when the `TAP_GITHUB_TOKEN` secret is configured
4. update the `tk103331/scoop-bucket` manifest when a Scoop token (or the tap token) is available

Preparation before the first release:
- npm automation token -> repo secret `NPM_TOKEN`
- create `tk103331/homebrew-tap`, personal access token (repo scope) -> secret `TAP_GITHUB_TOKEN`

**Build from source**:

```bash
git clone https://github.com/tk103331/oni3.git
cd oni3
go install .             # produces `oni3`
```

For a smaller local binary, build with `-trimpath -ldflags="-s -w"` (~50 MB instead of ~70 MB):

```bash
go build -trimpath -ldflags="-s -w" -o oni3 .
```

## Usage

### 1. Configuration File

On first run Oni3 creates a starter configuration at `~/.oni3/config.yml` (mode `0600`) and opens the interface. Common providers are built in, so the usual setup happens inside the TUI:

1. Start Oni3. With no models configured, the provider picker opens automatically.
2. Choose a provider and enter its API key (masked input).
3. Fetch the provider's model list, then import a single model (and switch to it immediately) or all of them.

The same flow is available later through `/provider`. Built-in presets include OpenAI, Anthropic, Gemini, DeepSeek, Qwen (China/international), Kimi (China/international), Zhipu GLM, MiniMax (China/international), Ark/Doubao, Qianfan, SiliconFlow, OpenRouter, Groq, xAI, Mistral, Together, Fireworks, ModelScope, Ollama, LM Studio and magpie.

Verified API keys and imported models are stored in private state under `~/.oni3/state/` (`0600`); Oni3 never rewrites `config.yml`. Edit the file for custom endpoints, pinned model settings, agents and tools. `config.yml.example` is a complete reference:

```bash
cp config.yml.example ~/.oni3/config.yml
```

The configuration file contains the following main sections:
- `providers`: AI model provider configuration (API keys, base URLs, etc.)
- `models`: Model configuration (temperature, max tokens, etc.)
- `agents`: Agent configuration (system prompts, models and tools; an agent without `model` uses `default_model`, or the first model; `max_steps` caps the ReAct loop and defaults to 1000)
- `tools`: Tool configuration (parameters and settings for custom tools)
- `plugins`: JavaScript modules providing tools, hooks, themes, agents, models and providers
- `mcp_servers`: MCP server configuration. Each server also accepts `timeout` (seconds to connect and list tools, default 60). A server that cannot be reached is skipped for the session instead of disabling the others: `/mcp` marks it `✗ 失败` with its cause, `/status` lists it, and a startup notice in the transcript names it.
- `settings`: Global settings including Langfuse configuration



### 2. Interactive Agent Mode

The TUI supports slash commands (type `/` to see the command palette with argument candidates):

| Command | Description |
|---|---|
| `/help` | List all commands |
| `/clear` | Clear the current conversation |
| `/agent <name>` | Switch agent (agent mode) |
| `/model <name>` | Switch model without restarting |
| `/provider [name]` | Choose a provider, fetch its models and add one (switch immediately) or all |
| `/skill <name> [message]` | Select a configured skill; with a message, run that message as the turn right away (`/skill:coder 修复登录` and `/skill coder 修复登录` are the same command); `/skill off` clears the selection |
| `/resume [id\|#]` | List or resume saved sessions (conversation context is restored) |
| `/theme <name> [light\|dark]` | Switch color theme (every theme ships light and dark variants): `violet` (default), `catppuccin`, `nord`, `gruvbox`, `tokyo-night`, `dracula`, `mono` |
| `/mcp [enable\|disable] [name\|all]` | Open the MCP server management panel, or toggle servers for the current session |
| `/tools` | List available tools |
| `/status` | Show current agent/model/skill/session |

Any command argument can be attached with a colon instead of a space (`/skill:coder`, `/model:gpt4`); the palette keeps the separator you typed.

Switching the model with `/model` is remembered: the choice is stored per agent in `~/.oni3/agent_models.json` and restored on the next start. This only applies to agents whose configured model is the default model (`default_model`, or the first model when it is unset); an agent that pins its own `model:` in `config.yml` always keeps it.

The header is two rows: the ONI3 wordmark beside the active agent and model (`OniThree · qwen-local`), and the abbreviated working directory plus status below it. It updates after `/agent`, `/model` or `/resume`. Narrow terminals drop the wordmark and shorten the label so the status stays visible. The input stays at the bottom with contextual keyboard hints. Selected thinking, tool and error rows use a slim left marker instead of a background fill.

While a reply is streaming, press `Esc` twice to stop it: the first press only arms the hint, the second one within two seconds cancels the run and keeps whatever streamed so far.

While a turn runs, the TUI reports `idle`/`working`/`blocked`/`done`/`error` to the terminal via OSC 7501 (Program Status Protocol). Terminals that support it can show the state in a tab or status bar; others ignore the sequence.

The TUI captures the mouse by default, so clicking a thinking, tool-call or error **header** line expands/folds it right away (the same as `Tab` + `Ctrl+O`); errors start folded to their first line. A click on any other row — message text, an expanded block's body, empty space — automatically releases the capture so the terminal's native text selection and copy work; sending the next message restores the capture. `Ctrl+Y` toggles the capture manually at any time. Scrolling never needs the capture: the TUI enables the terminal's alternate-scroll mode (DECSET 1007), so on supported terminals (iTerm2, Kitty, WezTerm, xterm, ...) the wheel scrolls the transcript without any key press; otherwise use ↑↓/PgUp/PgDn.

Conversations are saved automatically under `~/.oni3/sessions/` as append-only JSONL (one `<id>.jsonl` file per session). Every turn is its own line: user/assistant text, model reasoning and tool calls (arguments, results, timing), so the transcript can be restored exactly and an interrupted turn never rewrites earlier history. Every new run starts a fresh session; `/clear` (or Ctrl+L) also starts a new one, and `/resume <id>` replays a saved conversation into the model context. `/resume` without arguments lists recent sessions; a number picks from that list.

**Tool approval.** In interactive sessions dangerous tool calls (shell commands, custom commands, file writes/edits/deletes/moves, non-GET HTTP requests) pause with a `⚠ 工具审批` panel before running:

| Key | Effect |
|---|---|
| `↑`/`↓` + `Enter` | Choose *allow once* / *allow for this session* / *deny* |
| `Esc` | Deny |

A denial is returned to the model as a result (not an error), so the turn continues and the model can propose a safer plan. "Allow for this session" is remembered per tool until `/clear`, `/resume` or exit. Configure it with `settings.approval`:

```yaml
settings:
  approval:
    mode: dangerous   # off | dangerous (default) | all
    allow: []         # tools that never need confirmation
    always: []        # tools that always do (useful for MCP tools)
```

`run` and `acp` have no UI to ask, so tools run without the prompt there.

Skills follow the [Agent Skills](https://docs.claude.com/en/docs/agents-and-tools/agent-skills) layout: a skill is a directory with a `SKILL.md` document and optional bundled resources (references, scripts, assets).

```
.oni3/skills/                     # project skills (highest priority)
  code-review/
    SKILL.md
    references/checklist.md
~/.oni3/skills/                   # personal skills
  release/SKILL.md
~/.agents/skills/                 # skills shared with other agent tools
  translator.md                   # legacy flat document, still supported
```

On name clashes the first directory wins; file skills override `config.yml` entries. A `SKILL.md` directory wins over a flat `*.md` file with the same name.

`SKILL.md` starts with optional YAML front matter (`allowed-tools` accepts a space/comma separated string or a list):

```markdown
---
name: code-review                  # optional, defaults to the directory name
description: Review code changes for correctness and clarity
license: MIT
allowed-tools: Read Grep
metadata:
  owner: platform
tools: [file_system]               # oni3 extension: extra tools for /skill
---
Start with correctness issues, then clarity. Read `references/checklist.md`
and follow it before reviewing any change.
```

Skills use progressive disclosure:

- the system prompt lists each skill's name, description and `SKILL.md` path, so the model only pays for the summary;
- the model reads `SKILL.md` (and any bundled files) with its file tools when the skill is relevant;
- `/skill <name>` (or `/skill:<name> <message>`) is the explicit shortcut: it inlines the instructions and mounts the skill's extra tools for the session.

Skills can also be declared inline in `config.yml` (selection-only presets, best used with `/skill`):

```yaml
skills:
  translator:
    description: "Translation assistant (EN <-> ZH)"
    system: "You are a professional translator. Output only the translation."
```

Names use lower-case letters, digits and hyphens (max 64 characters); other names still load for backward compatibility but are reported in the log.

Start an interactive session with an agent:

```bash
# No arguments: resume the last used agent, or start the built-in `onthree` agent
oni3 agent
oni3          # bare invocation is equivalent to `oni3 agent`

# The built-in `onthree` agent uses `default_model` (or the first model
# alphabetically) and enables command_line, file_system, http_request, browser_use,
# javascript, subagent, ask_user and todo_list by default. The last used agent is stored in
# ~/.oni3/last_agent

# Start a specific configured agent
oni3 agent --agent search_agent
```

Parameter description:
- `--agent, -a`: Specify the agent name to run (optional; defaults to the last used agent or the built-in `onthree` agent, displayed as OniThree)

### 3. Running an Agent

Use the `run` command to run a specified Agent:

```bash
oni3 run --agent test_agent --prompt "Hello, please help me search for today's weather"
```

Parameter description:
- `--agent, -a`: Specify the Agent name to run (required)
- `--prompt, -p`: Specify the input prompt for the Agent (required)
- `--config`: Specify the configuration file path (optional, defaults to ~/.oni3/config.yml)
- `--cwd, -C`: Run in this working directory (optional, defaults to the current one). Tools resolve relative paths against it, e.g. `oni3 -C ~/project`

The directory can also be a positional argument: `oni3 ~/project`, `oni3 agent ~/project`. Passing both `--cwd` and a positional directory is an error. `run` keeps its flag-only interface.

### 4. ACP Agent Mode (for editors like Zed)

Run Oni3 as an [Agent Client Protocol (ACP)](https://agentclientprotocol.com) agent.
It speaks JSON-RPC 2.0 over stdio, so any ACP client (Zed, or your own tooling) can create sessions, send prompts, and receive streaming responses and tool call events.

```bash
# Expose an agent over ACP
oni3 acp --agent search_agent

# Without --agent the built-in OniThree agent is served (the same default as
# `oni3` / `oni3 agent`); only a configured model is required
oni3 acp
```

In ACP mode `ask_user` and `todo_list` are bridged to the client instead of being disabled: questions arrive as `session/request_permission` dialogs, and task lists appear as ACP plan updates. Dangerous tool calls ask the client for permission in the same way.

Example Zed configuration (`settings.json`):

```json
{
  "agent": {
    "profiles": {
      "oni3": {
        "provider": "acp",
        "name": "oni3",
        "command": "oni3",
        "args": ["acp", "--agent", "search_agent"]
      }
    }
  }
}
```

Supported ACP surface:

- `initialize`, `session/new`, `session/prompt`, `session/cancel`, `session/set_config_option` (protocol v1)
- Session management: `session/list`, `session/load`, `session/resume` and `session/close`. ACP conversations persist to the same `~/.oni3/sessions/*.jsonl` store as the TUI, so text, reasoning and tool calls survive restarts and can be resumed from either side.
- Streaming `session/update` notifications: `agent_message_chunk`, `tool_call`, `tool_call_update`, `plan`, `available_commands_update` and `config_option_update`
- Slash commands advertised to the client: `/clear`, `/model`, `/tools` and `/status`, plus `/skill` when skills are configured. Prompts starting with `/` are answered directly instead of being sent to the model.
- Model and skill selectors through `session/set_config_option`; live changes are pushed back with `config_option_update` and persisted with the session.
- `ask_user` bridged to `session/request_permission`, `todo_list` to ACP plan entries, and dangerous tool calls to the client's permission UI
- Stop reasons: `end_turn` and `cancelled`
- Tools from `config.yml` (built-in tools and MCP servers) are exposed as ACP tool calls with status (`pending`/`in_progress`/`completed`/`failed`) and kind (`search`, `execute`, `fetch`, ...)

Note: logs are written to `~/.oni3/oni3.log` — stdout of the `acp` command is reserved for the protocol.

### 5. Web Mode

Serve the configured agents in a local browser interface:

```bash
oni3 web                                  # http://127.0.0.1:8080
oni3 web --agent search_agent --open      # pick a default agent and open the browser
oni3 web --token my-secret                # require a token
ONI3_WEB_TOKEN=my-secret oni3 web         # token from the environment
oni3 web --addr 0.0.0.0:8080 --token my-secret
```

The interface provides:

- streaming replies, reasoning blocks and tool call cards
- agent and per-turn model selectors docked below the composer
- `ask_user` prompts and dangerous-tool approvals rendered in the page
- a searchable session sidebar backed by the same `~/.oni3/sessions/*.jsonl` files as the TUI and ACP, so a chat can be resumed from any interface
- light and dark themes with a persistent toggle
- built-in API documentation at `/docs`, covering Web API, AG-UI, A2A and MCP

Authentication is optional and disabled by default. Set `--token` (or `ONI3_WEB_TOKEN`) to require it:

- `--open` signs the browser in through a one-time `?token=` URL that is exchanged for a 7-day HttpOnly cookie and removed from the address bar; without `--open`, enter the token on the login page
- API clients can instead send `Authorization: Bearer <token>`, `X-Oni3-Token: <token>` or `?token=<token>`
- unauthenticated API requests return `401`; the page shows a login form, and the 锁定 button clears the cookie

The server listens on `127.0.0.1` by default. Binding it to a non-loopback address without a token exposes the configured shell and file tools to anyone who can reach the port; a warning is printed in that case. Press Ctrl+C to stop the server.

The page uses a small HTTP API: `/api/bootstrap`, `/api/agents`, `/api/models`, `/api/sessions` (optional `q` searches titles, agents, models and message text), `/api/chat` (Server-Sent Events), `/api/respond` and `/api/cancel`. Open `/docs` for the rendered API reference and protocol examples.

### 6. AG-UI, A2A and MCP

`oni3 web` also exposes AG-UI, A2A and an MCP server on the same listener and behind the same token authentication. They run through the shared execution layer, so they use the same agents, tools, approvals, sessions and model selection as the TUI.

```bash
oni3 serve --token my-secret              # same as web, also reachable as `serve`
oni3 web --agui=false                     # disable AG-UI
oni3 web --a2a=false                      # disable A2A
oni3 web --mcp=false                      # disable MCP server endpoints
oni3 serve --web=false --agui=false --a2a=false --mcp  # protocol-only, no browser UI
```

`--web=false` disables the browser interface, Web API, login page and `/docs`; `/api/health` and the enabled protocol endpoints remain available. At least one of `--web`, `--agui`, `--a2a` and `--mcp` must stay enabled. `--open` is ignored when the web interface is disabled.

AG-UI:

- `POST /agui` accepts a `RunAgentInput` (`threadId`, `runId`, `messages`, `forwardedProps`, `resume`) and streams `text/event-stream` frames
- `forwardedProps.agent` selects the agent and `forwardedProps.model` optionally selects a model alias; the default agent is used when omitted
- `threadId` is the Oni3 session ID, so conversations remain resumable from the TUI and ACP
- events include `RUN_STARTED`, `TEXT_MESSAGE_*`, `REASONING_*`, `TOOL_CALL_*`, `STATE_SNAPSHOT`, `RUN_ERROR` and `RUN_FINISHED`
- `ask_user` and tool approvals end the stream with a `RUN_FINISHED` interrupt; send a new request with `resume: [{"interruptId": "...", "status": "resolved", "payload": {...}}]` to continue the parked run
- per-agent convenience endpoint: `POST /agents/{agent}/agui`

A2A 1.0:

- `GET /.well-known/agent-card.json` and `GET /agents/{agent}/.well-known/agent-card.json` publish one card per agent
- `POST /a2a` and `POST /agents/{agent}/a2a` implement JSON-RPC 2.0 methods `SendMessage`, `SendStreamingMessage`, `GetTask`, `ListTasks`, `CancelTask` and `SubscribeToTask` (the 0.3 slash names such as `message/send` are accepted too)
- streaming methods return JSON-RPC responses in `text/event-stream` frames, using `task`, `statusUpdate`, `artifactUpdate` and `message` result wrappers
- message roles use the A2A 1.0 enum values `ROLE_USER` and `ROLE_AGENT`; the 0.3 lowercase `user` and `agent` values are accepted for compatibility and normalized on output
- task states use the A2A 1.0 `TASK_STATE_*` values; `ask_user` and approvals move a task to `TASK_STATE_INPUT_REQUIRED`, and the next `SendMessage` for that task resumes it
- tasks are persisted under `~/.oni3/a2a/tasks/` and can be listed or polled after a restart

AG-UI and A2A do not depend on third-party A2A or AG-UI SDKs; their wire types and transports live in `protocol/agui` and `protocol/a2a`.

MCP Server:

- Oni3 acts as the MCP server here; this is separate from the external `mcp_servers` Oni3 connects to as a client
- `POST /mcp` is the Streamable HTTP endpoint; `GET /mcp` listens for server notifications and `DELETE /mcp` terminates a session
- `GET /mcp/sse` and `POST /mcp/message?sessionId=...` provide the legacy SSE transport
- the endpoint exposes the same three tools as `oni3 mcp`: `call_agent(agent, prompt, model?)`, `list_agents()` and `list_models()`
- use the same `Authorization: Bearer <token>` header as the rest of the web service

Oni3 can also call remote A2A agents as a non-streaming client. Register each remote agent under `a2a_agents`, then expose it to a model through an `a2a` tool:

```yaml
a2a_agents:
  remote_researcher:
    card_url: https://example.com/.well-known/agent-card.json
    description: Remote research agent
    token_env: REMOTE_A2A_TOKEN   # or use `token` for a literal value

tools:
  remote_a2a:
    type: a2a
    config:
      agents: [remote_researcher]  # allowlist; omit to allow every configured agent
      timeout: 300                 # overall timeout in seconds, default 300
      poll_interval_ms: 1000       # task polling interval, default 1000
```

The tool takes `agent` (required), `prompt` (required for a new task), and optional `task_id` and `context_id`. It returns JSON with `agent`, `state`, `task_id`, `context_id`, `answer` or `message`, and `options` when the remote agent asks a question. The first phase is non-streaming: Oni3 discovers the Agent Card, calls `SendMessage`, and polls `GetTask` until the task completes or reaches `TASK_STATE_INPUT_REQUIRED`. Resume an interrupted task by passing the same `task_id` and putting the answer in `prompt`; omit `prompt` to only poll. If the call times out, the result has `state: "timeout"` plus the `task_id`, so a later call can continue polling.

Remote calls are flagged as dangerous, so the default approval policy asks before the prompt leaves the machine. Remote URLs come only from `a2a_agents`; models cannot pass arbitrary URLs, which keeps request targets under configuration control.

### 7. MCP Server Mode

Run Oni3 itself as a Model Context Protocol server. It exposes three tools:

```bash
oni3 mcp
```

While `oni3 web` is running, Oni3 also acts as an MCP server over `POST /mcp` (Streamable HTTP) or `GET /mcp/sse` (legacy SSE). Use `--mcp=false` to disable these server endpoints.

- `call_agent(agent, prompt, model?)` runs any configured agent, including the built-in OniThree agent
- `list_agents()` returns the agent names, descriptions and default model aliases
- `list_models()` returns the model alias IDs and `default_model`

`call_agent` parameters:

- `agent` (required): a name returned by `list_agents`
- `prompt` (required): the task for the agent
- `model` (optional): a model alias for this run; omit it to use the agent's default model

`list_agents()` returns:

```json
{"agents": [{"name": "helper", "description": "Help with small tasks", "default_model": "alpha"}]}
```

`list_models()` returns:

```json
{"default_model": "alpha", "models": ["alpha", "zeta"]}
```

Example `call_agent` arguments:

```json
{"agent": "search_agent", "prompt": "Search for today's weather", "model": "alpha"}
```

Each call runs a fresh, single-turn agent conversation and returns its final answer.

stdout is reserved for the protocol; logs go to `~/.oni3/oni3.log`.

Example client configuration:

```json
{
  "mcpServers": {
    "oni3": {
      "command": "oni3",
      "args": ["mcp"]
    }
  }
}
```

### 8. Configuration Example

Here's a complete configuration example:

```yaml
# Model provider configuration
providers:
  openai:
    type: openai
    base_url: https://api.openai.com/v1
    api_key: sk-xxxxx
  claude:
    type: anthropic
    api_key: sk-ant-xxxxx

# Model configuration
models:
  gpt4:
    provider: openai
    model: gpt-4
    max_tokens: 4096
    temperature: 0.7
  claude_sonnet:
    provider: claude
    model: claude-3-5-sonnet-20241022
    max_tokens: 4096
    temperature: 0.7

# MCP server configuration
mcp_servers:
  # SSE type MCP server
  sse_server:
    type: sse
    url: "http://localhost:3000/sse"  # MCP server URL
    headers:
      "Authorization": "Bearer your-token"  # Optional authentication header
  # STDIO type MCP server
  stdio_server:
    type: stdio
    cmd: "python"                    # Command to execute
    timeout: 120                     # Optional connect timeout in seconds, default 60
    args:
      - "-m"
      - "your_mcp_server"             # MCP server module
    env:
      "PYTHONPATH": "/path/to/server" # Environment variables
      "API_KEY": "your-api-key"

# Agent configuration
agents:
  search_agent:
    description: "Search and summarize information from the web"
    system: "You are a search assistant that can help users search for information"
    model: gpt4
    tools:
      - duckduckgo_search
      - wikipedia_search
    mcp_servers:                        # MCP servers that the Agent can use
      - sse_server
  # Multi-functional assistant example (including custom tools and MCP servers)
  multi_agent:
    system: "You are a multi-functional assistant that can search information, query weather, get system information, etc."
    model: gpt4
    tools:
      - duckduckgo_search
      - weather_api
      - system_info
    mcp_servers:
      - sse_server
      - stdio_server

# Tool configuration
tools:
  duckduckgo_search:
    type: duckduckgo
    config:
      max_results: 10        # Maximum number of search results, default 10
      region: "wt"           # Search region: wt(global), cn(China), us(USA), uk(UK)
      safe_search: "off"     # Safe search: off(disabled), moderate(moderate), strict(strict)
      timeout: 10            # Timeout in seconds, default 10 seconds
  wikipedia_search:
    type: wikipedia
  # Custom HTTP tool example
  weather_api:
    type: custom_http
    description: "API tool for getting weather information"
    config:
      url: "https://api.openweathermap.org/data/2.5/weather?q={{city}}"
      method: "GET"
      headers:
        "Content-Type": "application/json"
    parameters:
      - name: "city"
        type: "string"
        description: "City name"
        required: true
  # Custom command-line tool example
  system_info:
    type: custom_exec
    description: "Get system information"
    config:
      cmd: "uname -a && df -h"
      work_dir: "/tmp"
      timeout: 30
    parameters: []
  # Filesystem tool: edit files below a root directory
  workspace:
    type: file_system
    config:
      root: "."              # Accessible directory, default: current directory
      max_bytes: 204800      # Read budget per call, default 200 KiB
      max_entries: 500       # Max entries returned by list, default 500
      timeout: 30            # Per-call timeout in seconds, default 30

# Global settings
settings:
  # Tool approval for interactive sessions (see above)
  approval:
    mode: dangerous      # off | dangerous | all
    allow: []
    always: []
  langfuse:
    host: https://cloud.langfuse.com
    public_key: pk-xxx
    secret_key: sk-xxx
```

Configuration fields and multiword tool types use `snake_case`; historical spellings are not supported. See the [naming conventions](docs/naming.md) for the complete tool/provider/transport identifiers, reference rules and extension namespaces.

## Supported Tools

Oni3 comes with various built-in tools that can be used in Agents:

### Search Tools
- **DuckDuckGo**: Web search
- **Google Search**: Google search (requires API key)
- **Bing Search**: Bing search (requires API key)
- **Wikipedia**: Wikipedia search

### Browser Tools
- **Browser Use**: Browser automation tool. The browser is started lazily on the first call (it no longer opens one at startup); pass `config.headless: true` to run it without a visible window.

### System Tools
- **Command Line**: Execute system commands
- **Filesystem**: Read and edit files below a root directory (`read`/`write`/`append`/`list`/`stat`/`mkdir`/`delete`/`move`/`grep`); paths escaping the root are rejected
- **HTTP Request**: Send HTTP requests
- **Sequential Thinking**: Sequential thinking tool
- **JavaScript**: Run JavaScript with the embedded moejs engine for calculations and data processing; no Node.js required.

### JavaScript execution

The built-in OniThree agent includes the `javascript` tool. To use it with a configured agent, declare it and include its name in that agent's `tools`:

```yaml
tools:
  javascript:
    type: javascript
    config:
      timeout: 30          # seconds; default 30
      memory_limit_mb: 64  # default 64

agents:
  analyst:
    system: Help the user analyze data.
    tools: [javascript]
```

Tool arguments:

```json
{
  "code": "const sum = input.values.reduce((a, b) => a + b, 0); console.log('done'); return {sum};",
  "input": {"values": [1, 2, 3]}
}
```

The result is `{"result":{"sum":6},"logs":["done"]}`. `code` is an async function body, so `return` and `await` work directly. `input` is an optional JSON object (default `{}`). `console.log/info/warn/error` append entries to `logs`; omitting a return value produces `result: null`.

Each call has an isolated runtime supporting standard JavaScript, promises and timers. Filesystem, network, process and module-loading APIs are unavailable. Code and input each have a 1 MiB limit, and the combined JSON output is limited to 1 MiB. Timeout and memory settings must be nonnegative integers; zero uses the defaults. Syntax/runtime errors, rejected promises and local timeouts are reported as ordinary tool failures; parent cancellation stops execution.

### Agent Tools

When an agent has a tool of type `subagent`, its system prompt automatically lists the configured agents' names and `description` fields, filtered by that tool's `agents` allowlist. Add `description` under each agent to help the model choose a suitable specialist. Temporary agents remain available unless `allow_temp: false` is set.

- **Subagent**: Delegate a self-contained task to another agent and return its answer. Only `prompt` is required: without `agent` or `system` the child is a temporary agent that inherits the parent's model and tools. Pass `agent` to run a configured agent, or `system` (+ optional `model` and `tools`) to give the temporary agent its own instructions. The child has its own conversation, inherits cancellation, tool approvals and the parent's current model (including a `/model` switch; a configured agent with its own `model:` keeps it), and cannot nest deeper than `max_depth`.

```yaml
tools:
  subagent:
    type: subagent
    config:
      agents: []           # allowlist of configured agents, empty = all
      tools: []            # allowlist of tools a temporary agent may use, empty = all
      allow_temp: true     # allow ad-hoc agents built from a system prompt
      max_depth: 1         # nesting limit (prevents runaway recursion)
      timeout: 300         # per-child-run timeout in seconds
```

### Custom Tools
- **Custom HTTP**: Custom HTTP tools
- **Custom Exec**: Custom command execution tools

### JavaScript Plugins

Plugins use the embedded [moejs](https://github.com/MoeForge/moejs) runtime; Node.js is not required. An ES module may export any combination of `tools`, `hooks`, `themes`, `agents`, `models` and `providers` objects.

```yaml
plugins:
  demo:
    path: ./plugins/demo.js  # relative to this YAML file
    timeout: 30             # seconds; default 30
    memory_limit_mb: 64     # default 64
    config:
      greeting: Hello
```

Exports are registered as `<plugin>__<name>`. Add `demo__greet` to an agent's `tools`; select plugin agents, models and themes with `/agent demo__helper`, `/model demo__fast` and `/theme demo__ocean`. Model presets reuse a configured or plugin-provided provider. Lifecycle hooks support `before_agent`, `after_agent`, `before_tool` and `after_tool`.

Plugin `providers` define a chat transport type, `base_url`, `models_url` and optional response field mappings. `/provider` prompts for a missing API key using masked input, fetches the model list with the configured key, and offers a single-model import (switching immediately) or bulk import. Keys validated by a successful model-list request and imported models persist in private local state without rewriting YAML. A configuration with providers and no models can start the TUI for this setup flow; see [providers.js](examples/plugins/providers.js) and [providers.yml](examples/plugins/providers.yml).

Each tool or hook runs in an isolated runtime with timeout, cancellation and a memory budget. Async functions, promises, timers and local relative imports are supported. See the [plugin API guide](docs/plugins.md), [complete JS example](examples/plugins/demo.js) and [runnable configuration](examples/plugins/demo.yml).

### Interactive Tools (agent mode adds these automatically)
- **ask_user**: The model asks the user a question; the TUI shows a question panel with optional choices (↑↓ + Enter) or a free-text answer. The answer is returned to the model as the tool result.
- **todo_list**: The model creates/updates a task list; the TUI shows a `☑ 任务 done/total` panel above the input, and snapshots are appended to the session JSONL so `/resume` restores them. Once every task is completed, the panel folds to a single summary line on your next message; new tasks expand it automatically and clicking it (with mouse capture on) unfolds it again.

Both tools are exposed automatically in agent sessions. To use them in `run`, declare the config entries:

```yaml
tools:
  ask_user:
    type: ask_user
  todo_list:
    type: todo_list
```

Questions and task snapshots are persisted in the session file as `question` / `todo` JSONL events.

## Supported Model Providers

- **Direct SDK types**: `openai`, `anthropic`, `gemini`, `qwen`, `qianfan`, `ark`, `deepseek` and `ollama`
- **OpenAI-compatible presets**: OpenAI, Gemini (compatible endpoint), Kimi, Zhipu GLM, MiniMax, SiliconFlow, OpenRouter, Groq, xAI, Mistral, Together, Fireworks, ModelScope, LM Studio and magpie
- **Local models**: Ollama, LM Studio and other OpenAI-compatible local servers (API key optional)

Any other OpenAI-compatible endpoint can be added under `providers` with `type: openai` and a `base_url`.

## Main Dependencies

- [CloudWeGo Eino](https://github.com/cloudwego/eino) - AI application development framework
- [Cobra](https://github.com/spf13/cobra) - Command-line interface framework
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework for interactive terminal UIs
- Various Eino extension components (models and tools)

## License

[MIT](LICENSE)
