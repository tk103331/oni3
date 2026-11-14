[English](README.md) | 中文

Oni3 是一个基于 [CloudWeGo Eino](https://github.com/cloudwego/eino) 框架的智能 AI Agent 命令行工具。它提供了强大的 Agent 系统，支持多种工具集成和多个 AI 模型提供商，让您能够轻松构建和运行自定义的 AI Agent。

## 功能特点

- **智能 Agent 系统**：基于 ReAct 模式的 AI Agent，支持工具调用和推理
- **交互式 TUI**：基于 TUI 的终端界面，可与 Agent 实时对话，支持斜杠命令、工具审批与会话恢复
- **ACP Agent 模式**：可作为 [Agent Client Protocol](https://agentclientprotocol.com) Agent 运行，供 Zed 等编辑器接入
- **丰富的工具生态**：内置多种工具，包括搜索、浏览器、命令行、HTTP 请求等
- **多模型提供商支持**：支持 OpenAI、Claude、Gemini、Qwen、DeepSeek、Ollama、百度千帆、字节跳动豆包等
- **灵活的配置系统**：通过 YAML 配置文件管理 Agent、工具、模型与 skill
- **自定义工具支持**：支持自定义 HTTP 和命令行工具
- **JavaScript 插件**：通过内嵌 moejs 运行 ES module，扩展 tools、hooks、themes、agents、models 和 providers
- **MCP 服务器集成**：支持 Model Context Protocol (MCP) 服务器，包括 SSE 和 STDIO 传输
- **Langfuse 集成**：内置 Langfuse 可观测性支持，用于监控和追踪

## 安装

### 安装方式

**Go install**（需要 Go ≥ 1.25）：

```bash
go install github.com/tk103331/oni3@latest
# 若提示 sumdb ... 404（新版本校验库索引延迟）：
# GONOSUMDB=github.com/tk103331/oni3 go install github.com/tk103331/oni3@latest
```

**npm**：

```bash
npm install -g oni3    # 提供 oni3 命令
oni3
```

**Homebrew**：

```bash
brew install tk103331/tap/oni3   # 提供 oni3 命令
```

**Scoop**（Windows）：

```powershell
scoop bucket add oni3 https://github.com/tk103331/scoop-bucket
scoop install oni3   # 提供 oni3 命令
```

**发布（维护者）**：

```bash
git tag v0.1.0 && git push --tags
```

GitHub Actions（`.github/workflows/release.yml`）会自动完成：
1. goreleaser 构建 darwin/linux/windows × amd64/arm64 的 `oni3` 二进制，打包为 `oni3_<version>_<os>_<arch>.tar.gz`（Windows 为 `.zip`）（约 15 MB，未压缩的 stripped 二进制约 50 MB）并创建 Release
2. 配置了仓库变量 `NPM_TRUSTED_PUBLISHING=true`（且 npm 包已配置 Trusted Publisher）时，通过 OIDC 发布 npm 包（版本号取自 tag，无需任何 token）
3. 配置了 `TAP_GITHUB_TOKEN` secret 时更新 `tk103331/homebrew-tap` 的 formula
4. 有 Scoop token（或复用 tap token）时更新 `tk103331/scoop-bucket` 的 manifest

首次发布前需要准备：
- npm Automation token → 仓库 secret `NPM_TOKEN`
- 创建 `tk103331/homebrew-tap` 仓库，PAT（repo 权限）→ secret `TAP_GITHUB_TOKEN`

**从源码构建**：

```bash
git clone https://github.com/tk103331/oni3.git
cd oni3
go install .             # 生成 oni3
```

本地想要更小的二进制，可加上 `-trimpath -ldflags="-s -w"`（约 50 MB，默认约 70 MB）：

```bash
go build -trimpath -ldflags="-s -w" -o oni3 .
```

## 使用方法

### 1. 配置文件

首先，复制配置文件模板并根据需要进行修改：

```bash
cp config.yml.example config.yml
```

配置文件包含以下主要部分：
- `providers`: AI 模型提供商配置（API 密钥、基础 URL 等）
- `models`: 模型配置（温度、最大 token 数等）
- `agents`: Agent 配置（系统提示、使用的模型和工具；未写 `model` 时使用 `default_model`，未设置则取排序第一个模型；`max_steps` 控制 ReAct 循环上限，默认 1000）
- `tools`: 工具配置（自定义工具的参数和配置）
- `plugins`: JavaScript 插件，提供工具、生命周期 hooks、主题、agent、模型预设与 provider
- `mcp_servers`: MCP 服务器配置
- `settings`: 全局设置，包括 Langfuse 配置

### 2. 交互式 Agent 模式

TUI 内支持斜杠命令（输入 `/` 会显示命令面板与参数候选）：

| 命令 | 说明 |
|---|---|
| `/help` | 显示所有命令 |
| `/clear` | 清空当前对话 |
| `/agent <name>` | 切换 agent（agent 模式） |
| `/model <name>` | 切换模型（无需重启） |
| `/provider [name]` | 选择 provider 获取模型列表；单个添加并立即切换，或全部添加 |
| `/skill <name>` | 选择配置中的 skill；`/skill off` 取消选择 |
| `/resume [id\|编号]` | 列出或恢复历史会话（会一并恢复对话上下文） |
| `/theme <name> [light\|dark]` | 切换颜色主题（每套主题均含明暗两套配色）：`violet`（默认）、`catppuccin`、`nord`、`gruvbox`、`tokyo-night`、`dracula`、`mono` |
| `/tools` | 列出当前可用工具 |
| `/status` | 显示当前 agent/model/skill/会话 |

用 `/model` 切换的模型会被记住：按 agent 保存在 `~/.oni3/agent_models.json`，下次启动自动沿用。只有「模型为默认模型」的 agent 会记住（`default_model`，未设置时取排序第一个模型）；在 `config.yml` 中为 agent 显式指定了 `model:` 的，始终以配置为准。

顶栏显示当前 `agent · 模型`（如 `Eino / Eino · qwen-local`），`/agent`、`/model`、`/resume` 切换后立即更新；终端过窄时优先保留右侧状态。输入框固定在底部，快捷键提示随当前操作调整。选中的思考、工具和错误行以左侧细线标记，不再铺满背景色。

回复生成过程中按两次 `Esc` 可停止：第一次只显示提示，两秒内再按一次即取消本次回复，已生成的内容会保留。

TUI 默认**不捕获鼠标**，可直接用终端鼠标选择并复制文本；同时自动开启终端「备用屏幕滚轮转发」（DECSET 1007），在支持的终端（iTerm2、Kitty、WezTerm、xterm 等）中滚轮可直接滚动对话，无需按键。若终端不支持 1007，可用 ↑↓/PgUp/PgDn，或按 `Ctrl+Y` 开启鼠标捕获（再按一次关闭）。开启捕获后，点击思考块、工具调用行或错误行即可展开/折叠（等同于 `Tab` + `Ctrl+O`）；错误默认折叠为第一行。

对话会自动保存到 `~/.oni3/sessions/`，采用 append-only 的 JSONL（每个会话一个 `<id>.jsonl` 文件）。每一轮事件各占一行：用户/助手文本、模型思考过程（reasoning）以及工具调用（参数、结果、耗时），因此可以完整还原对话记录，且中断不会改写已有历史。每次启动都是新会话，`/clear`（或 Ctrl+L）同样会开启新会话；`/resume <id>` 会恢复历史对话并把它作为上下文传给模型。不带参数的 `/resume` 列出最近会话，也可以用列表中的编号选择。

**工具审批**：交互会话中，危险的工具调用（shell 命令、自定义命令、文件写入/修改/删除/移动、非 GET 的 HTTP 请求）在执行前会弹出 `⚠ 工具审批` 面板：

| 按键 | 作用 |
|---|---|
| `↑`/`↓` + `Enter` | 选择「允许本次 / 本会话允许 / 拒绝」 |
| `Esc` | 拒绝 |

拒绝会作为工具结果返回给模型（不是错误），回合继续，模型可以改用更安全的方案。「本会话允许」按工具名记忆，直到 `/clear`、`/resume` 或退出。配置项 `settings.approval`：

```yaml
settings:
  approval:
    mode: dangerous   # off | dangerous（默认）| all
    allow: []         # 无需确认的工具名
    always: []        # 总是需要确认的工具名（适合无法分类的 MCP 工具）
```

`run` / `acp` 模式没有可询问的界面，工具直接执行。

`skills` 是 `config.yml` 中的可切换能力预设（扩展 system prompt、可追加工具）：

```yaml
skills:
  translator:
    description: "翻译助手：中英互译"
    system: "你是一名专业翻译，只输出译文。"
  coder:
    description: "编码助手"
    system: "优先给出可运行的代码示例。"
    tools: [shell]   # 可选：追加工具
```

也支持**目录形式**的 skill（无需写进 config.yml），按优先级扫描两个目录：

- `~/.oni3/skills/*.md`（优先）
- `~/.agents/skills/*.md`

同名 skill 以第一个目录为准；目录中的 skill 优先于 config.yml 里的定义。

每个文件是一个 markdown 文档，可用 front matter 声明描述与追加工具：

```markdown
---
description: 代码审查
tools: [shell]
---
你是一位严格的代码审查员，先指出正确性问题……
```



启动与 agent 的交互式会话：

```bash
# 无参数：恢复上次使用的 agent；没有历史时启动内置的 `onthree` agent
oni3 agent
oni3          # 不带子命令等价于 oni3 agent

# 内置 `onthree` agent 使用配置中的 default_model（未设置时取排序第一个模型），
# 默认启用 command_line、file_system、http_request、browser_use、javascript、
# subagent、ask_user、todo_list 工具；上次使用的 agent 记录在 ~/.oni3/last_agent

# 启动指定 agent
oni3 agent --agent search_agent
```

参数说明：
- `--agent, -a`: 指定要运行的 agent 名称（可选，默认上次使用的 agent 或内置 Eino）

### 3. 运行 Agent

使用 `run` 命令运行指定的 Agent：

```bash
oni3 run --agent test_agent --prompt "你好，请帮我搜索一下今天的天气"
```

参数说明：
- `--agent, -a`: 指定要运行的 Agent 名称（必需）
- `--prompt, -p`: 指定 Agent 的输入提示（必需）
- `--config`: 指定配置文件路径（可选，默认为 ~/.oni3/config.yml）
- `--cwd, -C`: 指定工作目录（可选，默认为当前目录）。工具的相对路径以此为基准，如 `eino -C ~/project`

也可以直接作为位置参数：`eino ~/project`、`eino agent ~/project`。同时给出 `--cwd` 和位置参数会报错；`run` 子命令仍只使用参数形式。

### 4. ACP Agent 模式（供 Zed 等编辑器接入）

将 Oni3 作为 [Agent Client Protocol (ACP)](https://agentclientprotocol.com) Agent 运行。
它通过 stdio 使用 JSON-RPC 2.0 通信，任何 ACP 客户端（如 Zed 编辑器）都可以创建会话、发送提示，并接收流式响应和工具调用事件。

```bash
# 以 ACP 方式暴露某个 agent
oni3 acp --agent search_agent

# 不指定 --agent 时使用内置的 `onthree` agent（与 `oni3` / `eino agent` 的默认 agent 相同），
# 只要配置了模型即可直接使用
oni3 acp
```

内置 agent 在 ACP 模式下不启用 `ask_user` / `todo_list`（这两个工具依赖交互界面）。

Zed 配置示例（`settings.json`）：

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

支持的 ACP 能力：

- `initialize`、`session/new`、`session/prompt`、`session/cancel`（协议 v1）
- 流式 `session/update` 通知：`agent_message_chunk`、`tool_call`、`tool_call_update`
- 停止原因：`end_turn` 与 `cancelled`
- `config.yml` 中配置的工具（内置工具与 MCP 服务）会以 ACP 工具调用形式暴露，带状态（`pending`/`in_progress`/`completed`/`failed`）与类型（`search`、`execute`、`fetch` 等）

注意：日志写入 `~/.oni3/oni3.log`——`acp` 命令的 stdout 专用于协议通信。

### 5. 配置示例

以下是一个完整的配置示例：

```yaml
# 模型提供商配置
providers:
  openai:
    type: openai
    base_url: https://api.openai.com/v1
    api_key: sk-xxxxx
  claude:
    type: anthropic
    api_key: sk-ant-xxxxx

# 模型配置
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

# MCP 服务器配置
mcp_servers:
  # SSE 类型的 MCP 服务器
  sse_server:
    type: sse
    url: "http://localhost:3000/sse"  # MCP 服务器 URL
    headers:
      "Authorization": "Bearer your-token"  # 可选的认证头
  # STDIO 类型的 MCP 服务器
  stdio_server:
    type: stdio
    cmd: "python"                    # 要执行的命令
    args:
      - "-m"
      - "your_mcp_server"             # MCP 服务器模块
    env:
      "PYTHONPATH": "/path/to/server" # 环境变量
      "API_KEY": "your-api-key"

# Agent 配置
agents:
  search_agent:
    description: "搜索网络信息并整理摘要"
    system: "你是一个搜索助手，可以帮助用户搜索信息"
    model: gpt4
    tools:
      - duckduckgo_search
      - wikipedia_search
    mcp_servers:                        # Agent 可以使用的 MCP 服务器
      - sse_server
  # 多功能助手示例（包含自定义工具和MCP服务器）
  multi_agent:
    system: "你是一个多功能助手，可以搜索信息、查询天气、获取系统信息等"
    model: gpt4
    tools:
      - duckduckgo_search
      - weather_api
      - system_info
    mcp_servers:
      - sse_server
      - stdio_server

# 工具配置
tools:
  duckduckgo_search:
    type: duckduckgo
    config:
      max_results: 10        # 最大搜索结果数量，默认10
      region: "wt"           # 搜索区域：wt(全球)、cn(中国)、us(美国)、uk(英国)
      safe_search: "off"     # 安全搜索：off(关闭)、moderate(中等)、strict(严格)
      timeout: 10            # 超时时间（秒），默认10秒
  wikipedia_search:
    type: wikipedia
  # 自定义 HTTP 工具示例
  weather_api:
    type: custom_http
    description: "获取天气信息的 API 工具"
    config:
      url: "https://api.openweathermap.org/data/2.5/weather?q={{city}}"
      method: "GET"
      headers:
        "Content-Type": "application/json"
    parameters:
      - name: "city"
        type: "string"
        description: "城市名称"
        required: true
  # 自定义命令行工具示例
  system_info:
    type: custom_exec
    description: "获取系统信息"
    config:
      cmd: "uname -a && df -h"
      work_dir: "/tmp"
      timeout: 30
    parameters: []
  # 文件系统工具：在根目录内编辑文件
  workspace:
    type: file_system
    config:
      root: "."              # 可访问的目录，默认当前目录
      max_bytes: 204800      # 单次读取上限，默认 200 KiB
      max_entries: 500       # list 返回的最大条目数，默认 500
      timeout: 30            # 单次调用超时（秒），默认 30

# 全局设置
settings:
  # 交互会话的工具审批（见上文）
  approval:
    mode: dangerous      # off | dangerous | all
    allow: []
    always: []
  langfuse:
    host: https://cloud.langfuse.com
    public_key: pk-xxx
    secret_key: sk-xxx
```

配置字段和多词工具类型统一使用 `snake_case`，不再支持旧拼写。完整的工具、provider、MCP 类型、引用规则及扩展命名空间见[命名规范](docs/naming.md)。

## 支持的工具

Oni3 内置了多种工具，可以在 Agent 中使用：

### 搜索工具
- **DuckDuckGo**: 网页搜索
- **Google Search**: Google 搜索（需要 API 密钥）
- **Bing Search**: Bing 搜索（需要 API 密钥）
- **Wikipedia**: 维基百科搜索

### 浏览器工具
- **Browser Use**: 浏览器自动化工具。浏览器在第一次调用时才启动（不再随程序启动打开窗口）；`config.headless: true` 可无窗口运行。

### 系统工具
- **Command Line**: 执行系统命令
- **Filesystem**: 在根目录内读写文件（`read`/`write`/`append`/`list`/`stat`/`mkdir`/`delete`/`move`/`grep`）；越出根目录的路径（含符号链接）会被拒绝
- **HTTP Request**: 发送 HTTP 请求
- **Sequential Thinking**: 顺序思考工具
- **JavaScript**: 使用内嵌 moejs 引擎执行 JavaScript，适合计算和数据处理，无需 Node.js。

### JavaScript 脚本执行

内置 `onthree` agent 默认包含 `javascript` 工具。自定义 agent 可以声明工具并在 `tools` 中引用：

```yaml
tools:
  javascript:
    type: javascript
    config:
      timeout: 30          # 秒，默认 30
      memory_limit_mb: 64  # 默认 64 MiB

agents:
  analyst:
    system: 帮助用户分析数据。
    tools: [javascript]
```

工具调用参数：

```json
{
  "code": "const sum = input.values.reduce((a, b) => a + b, 0); console.log('完成'); return {sum};",
  "input": {"values": [1, 2, 3]}
}
```

返回 `{"result":{"sum":6},"logs":["完成"]}`。`code` 按异步函数体执行，可以直接使用 `return` 和 `await`。`input` 是可选 JSON 对象，默认 `{}`。`console.log/info/warn/error` 的输出保存在 `logs`，未返回结果时 `result` 为 `null`。

每次调用使用独立运行时，支持标准 JavaScript、Promise 和定时器，不提供文件系统、网络、进程或模块加载 API。代码和输入各限 1 MiB，合并后的 JSON 输出限 1 MiB。超时和内存配置必须为非负整数，`0` 使用默认值。语法错误、执行异常、Promise 拒绝和工具自身超时会作为普通工具错误返回；父任务取消时终止执行。

### Agent 工具

Agent 配置了 `subagent` 类型的工具后，系统提示词会自动注入可调用 agent 的名称和 `description`，并按该工具的 `agents` 允许列表筛选。在各 agent 下填写 `description`，可帮助模型选择合适的专门 agent。临时创建 agent 的方式仍然可用，除非设置了 `allow_temp: false`。

- **Subagent**: 把一个独立任务交给另一个 agent 执行并返回结果。只传 `prompt` 即可：没有 `agent` 和 `system` 时，子 agent 是继承父 agent 模型与工具的临时 agent。传 `agent` 运行配置中的 agent；传 `system`（可选 `model`、`tools`）为本次调用指定临时 agent 的指令。子 agent 有独立对话，继承取消、工具审批与父 agent 的当前模型（含 `/model` 切换；配置里显式指定了 `model:` 的 agent 以自己的配置为准），嵌套深度受 `max_depth` 限制。

```yaml
tools:
  subagent:
    type: subagent
    config:
      agents: []           # 允许调用的配置 agent，空 = 不限制
      tools: []            # 临时 agent 可使用的工具，空 = 不限制
      allow_temp: true     # 是否允许按 system prompt 临时创建 agent
      max_depth: 1         # 嵌套深度上限（防止递归失控）
      timeout: 300         # 单次子任务超时（秒）
```

### 自定义工具
- **Custom HTTP**: 自定义 HTTP 工具
- **Custom Exec**: 自定义命令执行工具

### JavaScript 插件

插件使用内嵌的 [moejs](https://github.com/MoeForge/moejs) 运行时，无需安装 Node.js。一个 ES module 可以按需导出 `tools`、`hooks`、`themes`、`agents`、`models`、`providers` 六类扩展。

```yaml
plugins:
  demo:
    path: ./plugins/demo.js  # 相对于本 YAML 文件，与 --cwd 无关
    timeout: 30             # 秒，默认 30
    memory_limit_mb: 64     # 默认 64 MiB
    config:
      greeting: 你好
```

扩展名统一为 `<插件名>__<导出名>`，重名时加载失败。把 `demo__greet` 加入 agent 的 `tools` 后即可使用；插件 agent、模型和主题分别通过 `/agent demo__helper`、`/model demo__fast`、`/theme demo__ocean` 选择。`agents`、`models` 的字段与 YAML 配置一致，模型预设复用配置文件或插件提供的 provider。

`providers` 可以声明聊天接口类型、`base_url`、模型列表接口 `models_url`，以及列表和模型 ID 的字段路径。输入 `/provider` 选择提供商后，若未配置 API Key，会显示遮罩输入框；获取列表成功后可以添加单个模型并立即切换，或选择全部添加。支持搜索模型、取消请求和失败重试。验证成功的 Key 和添加的模型保存到权限为 `0600` 的本地状态文件，按配置文件和 provider 地址隔离，不改写原 YAML。只有 provider、尚无模型时也可启动 TUI 完成设置，见 [providers.js](examples/plugins/providers.js) 与 [providers.yml](examples/plugins/providers.yml)。

`hooks` 支持 `before_agent`、`after_agent`、`before_tool`、`after_tool` 四个事件，接收提示、参数、结果或错误的快照；返回值不修改原始请求。每次工具或 hook 调用都使用独立运行时，支持 async/await、Promise、定时器、目录内相对模块导入，并受超时、取消和内存预算限制。当前运行时不提供 Node.js、文件系统、网络或进程 API。

完整示例见 [demo.js](examples/plugins/demo.js)，加载配置见 [demo.yml](examples/plugins/demo.yml)，详细接口见 [插件开发文档](docs/plugins.md)。示例使用已安装 `qwen3` 的本地 Ollama：

```bash
eino --config examples/plugins/demo.yml agent --agent demo__helper
```

### 交互工具（agent 模式自动启用）
- **ask_user**: 模型向用户提问；TUI 显示问题面板，可用 ↑↓ + Enter 选择选项，或直接输入自定义回答，回答会作为工具结果返回给模型。
- **todo_list**: 模型创建/更新任务列表；TUI 在输入框上方显示 `☑ 任务 完成数/总数` 面板，任务快照写入会话 JSONL，`/resume` 后可恢复。任务全部完成后，下一次发送消息时面板折叠为一行摘要；有新任务时自动展开，开启鼠标捕获（Ctrl+Y）后点击摘要行也可再次展开。

这两个工具在 agent 会话中自动可用。如需在 `run` 中使用，请在配置里声明：

```yaml
tools:
  ask_user:
    type: ask_user
  todo_list:
    type: todo_list
```

提问与任务快照都会以 `question` / `todo` 事件写入会话 JSONL。

## 支持的模型提供商

- **OpenAI**: GPT-3.5, GPT-4 系列
- **Anthropic Claude**: Claude 3 系列
- **Google Gemini**: Gemini Pro 系列
- **阿里云通义千问**: Qwen 系列
- **DeepSeek**: DeepSeek 系列
- **字节跳动豆包**: 通过 Ark API
- **百度千帆**: 文心一言等
- **Ollama**: 本地模型部署

## 主要依赖

- [CloudWeGo Eino](https://github.com/cloudwego/eino) - AI 应用开发框架
- [Cobra](https://github.com/spf13/cobra) - 命令行界面框架
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - 交互式终端 TUI 框架
- 各种 Eino 扩展组件（模型和工具）

## 许可证

[MIT](LICENSE)