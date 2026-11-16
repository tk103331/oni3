English

Oni3 is an intelligent AI Agent command-line tool based on the [CloudWeGo Eino](https://github.com/cloudwego/eino) framework. It provides a powerful Agent system with support for multiple tool integrations and various AI model providers, enabling you to easily build and run custom AI Agents.

## Features

- **Intelligent Agent System**: AI Agent based on ReAct pattern with tool calling and reasoning capabilities
- **Interactive TUI**: Terminal UI for real-time conversations with agents, including slash commands, tool approval and session resume
- **ACP Agent Mode**: Runs as an [Agent Client Protocol](https://agentclientprotocol.com) agent so editors like Zed can drive Oni3
- **Rich Tool Ecosystem**: Built-in various tools including search, browser, command line, HTTP requests, etc.
- **Multi-Model Provider Support**: Supports OpenAI, Claude, Gemini, Qwen, DeepSeek, Ollama, Baidu Qianfan, ByteDance Doubao, and more
- **Flexible Configuration System**: Manage agents, tools, models and skills through YAML configuration files
- **Custom Tool Support**: Supports custom HTTP and command-line tools
- **JavaScript Plugins**: Extend tools, lifecycle hooks, themes, agents, models and providers through embedded moejs ES modules
- **MCP Server Integration**: Supports Model Context Protocol (MCP) servers with both SSE and STDIO transport
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
1. build the `oni3` binary for darwin/linux/windows × amd64/arm64 with goreleaser, pack it as `oni3_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) (~15 MB instead of the ~50 MB stripped binary) and create a Release
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

First, copy the configuration template and modify it as needed:

```bash
cp config.yml.example config.yml
```

The configuration file contains the following main sections:
- `providers`: AI model provider configuration (API keys, base URLs, etc.)
- `models`: Model configuration (temperature, max tokens, etc.)
- `agents`: Agent configuration (system prompts, models and tools; an agent without `model` uses `default_model`, or the first model; `max_steps` caps the ReAct loop and defaults to 1000)
- `tools`: Tool configuration (parameters and settings for custom tools)
- `plugins`: JavaScript modules providing tools, hooks, themes, agents, models and providers
- `mcp_servers`: MCP server configuration
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
| `/skill <name>` | Select a configured skill; `/skill off` clears the selection |
| `/resume [id\|#]` | List or resume saved sessions (conversation context is restored) |
| `/theme <name> [light\|dark]` | Switch color theme (every theme ships light and dark variants): `violet` (default), `catppuccin`, `nord`, `gruvbox`, `tokyo-night`, `dracula`, `mono` |
| `/tools` | List available tools |
| `/status` | Show current agent/model/skill/session |

Switching the model with `/model` is remembered: the choice is stored per agent in `~/.oni3/agent_models.json` and restored on the next start. This only applies to agents whose configured model is the default model (`default_model`, or the first model when it is unset); an agent that pins its own `model:` in `config.yml` always keeps it.

The header shows the active agent and model (`Eino / Eino · qwen-local`) and updates after `/agent`, `/model` or `/resume`. Narrow terminals keep the status visible by shortening the label. The input stays at the bottom with contextual keyboard hints. Selected thinking, tool and error rows use a slim left marker instead of a background fill.

While a reply is streaming, press `Esc` twice to stop it: the first press only arms the hint, the second one within two seconds cancels the run and keeps whatever streamed so far.

The TUI does **not capture the mouse** by default, so native terminal text selection and copy work. It also enables the terminal's alternate-scroll mode (DECSET 1007), so on supported terminals (iTerm2, Kitty, WezTerm, xterm, ...) the wheel scrolls the transcript without any key press. If 1007 is unsupported, use ↑↓/PgUp/PgDn, or press `Ctrl+Y` to capture the mouse (press again to release). With mouse capture on, clicking a thinking, tool-call or error line expands/folds it (the same as `Tab` + `Ctrl+O`). Errors start folded to their first line.

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

`skills` are switchable capability presets in `config.yml` (extra system prompt, optional extra tools):

```yaml
skills:
  translator:
    description: "Translation assistant (EN <-> ZH)"
    system: "You are a professional translator. Output only the translation."
  coder:
    description: "Coding assistant"
    system: "Prefer runnable code examples."
    tools: [shell]   # optional: extra tools
```

Skills can also live in **directories** (no config.yml needed), scanned in priority order:

- `~/.oni3/skills/*.md` (highest priority)
- `~/.agents/skills/*.md`

On name clashes the first directory wins; directory skills override config.yml entries.

Each file is a markdown document with optional front matter:

```markdown
---
description: Code review
tools: [shell]
---
You are a strict code reviewer. Start with correctness issues...
```

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
- `--agent, -a`: Specify the agent name to run (optional; defaults to the last used agent or the built-in `Eino`)

### 3. Running an Agent

Use the `run` command to run a specified Agent:

```bash
oni3 run --agent test_agent --prompt "Hello, please help me search for today's weather"
```

Parameter description:
- `--agent, -a`: Specify the Agent name to run (required)
- `--prompt, -p`: Specify the input prompt for the Agent (required)
- `--config`: Specify the configuration file path (optional, defaults to ~/.oni3/config.yml)
- `--cwd, -C`: Run in this working directory (optional, defaults to the current one). Tools resolve relative paths against it, e.g. `eino -C ~/project`

The directory can also be a positional argument: `eino ~/project`, `eino agent ~/project`. Passing both `--cwd` and a positional directory is an error. `run` keeps its flag-only interface.

### 4. ACP Agent Mode (for editors like Zed)

Run Oni3 as an [Agent Client Protocol (ACP)](https://agentclientprotocol.com) agent.
It speaks JSON-RPC 2.0 over stdio, so any ACP client (Zed, or your own tooling) can create sessions, send prompts, and receive streaming responses and tool call events.

```bash
# Expose an agent over ACP
oni3 acp --agent search_agent

# Without --agent the built-in Eino agent is served (the same default as
# `oni3` / `eino agent`); only a configured model is required
oni3 acp
```

In ACP mode the built-in agent does not enable `ask_user` / `todo_list`, since both need an interactive UI.

Example Zed configuration (`settings.json`):

```json
{
  "agent": {
    "profiles": {
      "eino": {
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

- `initialize`, `session/new`, `session/prompt`, `session/cancel` (protocol v1)
- Streaming `session/update` notifications: `agent_message_chunk`, `tool_call`, `tool_call_update`
- Stop reasons: `end_turn` and `cancelled`
- Tools from `config.yml` (built-in tools and MCP servers) are exposed as ACP tool calls with status (`pending`/`in_progress`/`completed`/`failed`) and kind (`search`, `execute`, `fetch`, ...)

Note: logs are written to `~/.oni3/oni3.log` — stdout of the `acp` command is reserved for the protocol.

### 5. Configuration Example

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

The built-in Eino agent includes the `javascript` tool. To use it with a configured agent, declare it and include its name in that agent's `tools`:

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

- **OpenAI**: GPT-3.5, GPT-4 series
- **Anthropic Claude**: Claude 3 series
- **Google Gemini**: Gemini Pro series
- **Alibaba Qwen**: Qwen series
- **DeepSeek**: DeepSeek series
- **ByteDance Doubao**: Through Ark API
- **Baidu Qianfan**: ERNIE and others
- **Ollama**: Local model deployment

## Main Dependencies

- [CloudWeGo Eino](https://github.com/cloudwego/eino) - AI application development framework
- [Cobra](https://github.com/spf13/cobra) - Command-line interface framework
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework for interactive terminal UIs
- Various Eino extension components (models and tools)

## License

[MIT](LICENSE)