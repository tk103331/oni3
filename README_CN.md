[English](README.md) | 中文

Oni3 是一个基于 [CloudWeGo Eino](https://github.com/cloudwego/eino) 框架的智能 AI Agent 命令行工具。它提供了强大的 Agent 系统，支持多种工具集成和多个 AI 模型提供商，让您能够轻松构建和运行自定义的 AI Agent。

## 功能特点

- **智能 Agent 系统**：基于 ReAct 模式的 AI Agent，支持工具调用和推理
- **交互式 TUI**：基于 TUI 的终端界面，可与 Agent 实时对话，支持斜杠命令、工具审批与会话恢复
- **Web 模式**：本地浏览器界面，支持流式回复、工具卡片、审批操作、共享会话历史与可选 Token 认证
- **AG-UI 与 A2A**：原生 AG-UI SSE 与 A2A 1.0 JSON-RPC 端点，支持 Agent Card、流式输出、中断恢复与任务持久化
- **ACP Agent 模式**：可作为 [Agent Client Protocol](https://agentclientprotocol.com) Agent 运行，供 Zed 等编辑器接入
- **首次运行引导**：内置常用 provider 预设，在界面中选择、输入 API Key 并导入模型，无需编辑 YAML
- **丰富的工具生态**：内置多种工具，包括搜索、浏览器、命令行、HTTP 请求等
- **多模型提供商支持**：支持 OpenAI、Claude、Gemini、Qwen、DeepSeek、Ollama、百度千帆、字节跳动豆包等
- **灵活的配置系统**：通过 YAML 配置文件管理 Agent、工具、模型与 skill
- **自定义工具支持**：支持自定义 HTTP 和命令行工具
- **JavaScript 插件**：通过内嵌 moejs 运行 ES module，扩展 tools、hooks、themes、agents、models 和 providers
- **MCP 客户端集成**：连接 Model Context Protocol (MCP) 服务器，支持 SSE、streamable HTTP 与 STDIO 传输
- **MCP 服务端模式**：通过 stdio 提供 `call_agent`、`list_agents` 与 `list_models`
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
npm install -g @tk103331/oni3    # 提供 oni3 命令
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
1. goreleaser 构建 darwin/linux/windows × amd64/arm64 的 `oni3` 二进制，打包为 `oni3_<version>_<os>_<arch>.tar.xz`（Windows 为 `.zip`）（约 12–17 MB，未压缩的 stripped 二进制约 50 MB）并创建 Release
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

首次运行时，Oni3 会在 `~/.oni3/config.yml` 创建起始配置（权限 `0600`）并进入界面。常用 provider 已内置，通常的设置都可以在 TUI 中完成：

1. 启动 Oni3。尚未配置模型时会自动打开 provider 选择面板。
2. 选择 provider，输入 API Key（遮罩显示）。
3. 获取模型列表后，可添加单个模型（并立即切换）或全部添加。

之后也可以随时通过 `/provider` 使用同样流程。内置预设包括 OpenAI、Anthropic、Gemini、DeepSeek、通义千问（国内/国际）、Kimi（国内/国际）、智谱 GLM、MiniMax（国内/国际）、火山方舟/豆包、百度千帆、SiliconFlow、OpenRouter、Groq、xAI、Mistral、Together、Fireworks、ModelScope、Ollama、LM Studio 与 magpie。

验证通过的 API Key 和导入的模型保存在 `~/.oni3/state/` 下的私有状态文件（`0600`）；Oni3 不会改写 `config.yml`。仅在需要自定义接口、固定模型参数、agent 或工具时编辑该文件。`config.yml.example` 是完整参考：

```bash
cp config.yml.example ~/.oni3/config.yml
```

配置文件包含以下主要部分：
- `providers`: AI 模型提供商配置（API 密钥、基础 URL 等）
- `models`: 模型配置（温度、最大 token 数等）
- `agents`: Agent 配置（系统提示、使用的模型和工具；未写 `model` 时使用 `default_model`，未设置则取排序第一个模型；`max_steps` 控制 ReAct 循环上限，默认 1000）
- `tools`: 工具配置（自定义工具的参数和配置）
- `plugins`: JavaScript 插件，提供工具、生命周期 hooks、主题、agent、模型预设与 provider
- `mcp_servers`: MCP 服务器配置。每个服务器可设置 `timeout`（连接并列出工具的秒数，默认 60）。某个服务器连不上时只在本次会话中跳过它，不会影响其它服务器：`/mcp` 会把它标为 `✗ 失败` 并显示原因，`/status` 会列出它，启动时还会在对话区给出提示。
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
| `/mcp [enable\|disable] [name\|all]` | 打开 MCP 服务管理面板，或在本次会话中启用/禁用服务 |
| `/tools` | 列出当前可用工具 |
| `/status` | 显示当前 agent/model/skill/会话 |

用 `/model` 切换的模型会被记住：按 agent 保存在 `~/.oni3/agent_models.json`，下次启动自动沿用。只有「模型为默认模型」的 agent 会记住（`default_model`，未设置时取排序第一个模型）；在 `config.yml` 中为 agent 显式指定了 `model:` 的，始终以配置为准。

顶栏为两行：第一行是 ONI3 字标与当前 agent/模型（如 `OniThree · qwen-local`），第二行是缩写后的工作目录与状态。`/agent`、`/model`、`/resume` 切换后立即更新；终端过窄时会隐藏字标并缩短标签，优先保留右侧状态。输入框固定在底部，快捷键提示随当前操作调整。选中的思考、工具和错误行以左侧细线标记，不再铺满背景色。

回复生成过程中按两次 `Esc` 可停止：第一次只显示提示，两秒内再按一次即取消本次回复，已生成的内容会保留。

回复进行中，TUI 会通过 OSC 7501（Program Status Protocol）向终端上报 `idle`/`working`/`blocked`/`done`/`error` 状态；支持的终端可在标签页或状态栏显示，其他终端会忽略该序列。

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
- `--agent, -a`: 指定要运行的 agent 名称（可选，默认上次使用的 agent 或内置 `onthree` agent，显示为 OniThree）

### 3. 运行 Agent

使用 `run` 命令运行指定的 Agent：

```bash
oni3 run --agent test_agent --prompt "你好，请帮我搜索一下今天的天气"
```

参数说明：
- `--agent, -a`: 指定要运行的 Agent 名称（必需）
- `--prompt, -p`: 指定 Agent 的输入提示（必需）
- `--config`: 指定配置文件路径（可选，默认为 ~/.oni3/config.yml）
- `--cwd, -C`: 指定工作目录（可选，默认为当前目录）。工具的相对路径以此为基准，如 `oni3 -C ~/project`

也可以直接作为位置参数：`oni3 ~/project`、`oni3 agent ~/project`。同时给出 `--cwd` 和位置参数会报错；`run` 子命令仍只使用参数形式。

### 4. ACP Agent 模式（供 Zed 等编辑器接入）

将 Oni3 作为 [Agent Client Protocol (ACP)](https://agentclientprotocol.com) Agent 运行。
它通过 stdio 使用 JSON-RPC 2.0 通信，任何 ACP 客户端（如 Zed 编辑器）都可以创建会话、发送提示，并接收流式响应和工具调用事件。

```bash
# 以 ACP 方式暴露某个 agent
oni3 acp --agent search_agent

# 不指定 --agent 时使用内置的 `onthree` agent（与 `oni3` / `oni3 agent` 的默认 agent 相同），
# 只要配置了模型即可直接使用
oni3 acp
```

ACP 模式下 `ask_user` 与 `todo_list` 不再被禁用，而是桥接到客户端：提问以 `session/request_permission` 对话框呈现，任务列表作为 ACP plan 更新显示；危险工具调用也通过同一机制向客户端申请权限。

Zed 配置示例（`settings.json`）：

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

支持的 ACP 能力：

- `initialize`、`session/new`、`session/prompt`、`session/cancel`、`session/set_config_option`（协议 v1）
- 会话管理：`session/list`、`session/load`、`session/resume`、`session/close`。ACP 会话与 TUI 共用 `~/.oni3/sessions/*.jsonl`，文本、思考和工具调用都会持久化，重启后仍可从任意一侧恢复。
- 流式 `session/update` 通知：`agent_message_chunk`、`tool_call`、`tool_call_update`、`plan`、`available_commands_update`、`config_option_update`
- 向客户端公布的斜杠命令：`/clear`、`/model`、`/tools`、`/status`；配置了 skill 时还会公布 `/skill`。以 `/` 开头的 prompt 会直接应答，不发送给模型。
- 通过 `session/set_config_option` 切换模型与 skill；变更通过 `config_option_update` 推送并随会话持久化。
- `ask_user` 桥接为 `session/request_permission`，`todo_list` 桥接为 ACP plan 条目，危险工具调用交由客户端权限界面确认
- 停止原因：`end_turn` 与 `cancelled`
- `config.yml` 中配置的工具（内置工具与 MCP 服务）会以 ACP 工具调用形式暴露，带状态（`pending`/`in_progress`/`completed`/`failed`）与类型（`search`、`execute`、`fetch` 等）

注意：日志写入 `~/.oni3/oni3.log`——`acp` 命令的 stdout 专用于协议通信。

### 5. Web 模式

在本地浏览器中使用已配置的 Agent：

```bash
oni3 web                                  # http://127.0.0.1:8080
oni3 web --agent search_agent --open      # 指定默认 Agent 并自动打开浏览器
oni3 web --token my-secret                # 启用 Token 认证
ONI3_WEB_TOKEN=my-secret oni3 web         # 从环境变量读取 Token
oni3 web --addr 0.0.0.0:8080 --token my-secret
```

界面能力：

- 流式回复、推理过程折叠与工具调用卡片
- Agent 与模型选择器停靠在输入框下方，支持每轮切换模型
- `ask_user` 提问与危险工具审批直接在页面中完成
- 可搜索的侧栏会话列表与 TUI、ACP 共用 `~/.oni3/sessions/*.jsonl`，任意界面都可恢复
- 明暗主题切换，并记住上次选择
- 内置 `/docs` API 文档，覆盖 Web API、AG-UI、A2A 与 MCP

Token 认证可选，默认关闭。通过 `--token`（或 `ONI3_WEB_TOKEN`）启用后：

- `--open` 会通过一次性 `?token=` URL 自动登录，将其换成 7 天有效的 HttpOnly Cookie，并从地址栏移除 Token；未使用 `--open` 时在登录页输入 Token 即可
- API 客户端也可使用 `Authorization: Bearer <token>`、`X-Oni3-Token: <token>` 或 `?token=<token>`
- 未认证的 API 请求返回 `401`；页面显示登录表单，右上角“锁定”按钮会清除 Cookie

服务默认只监听 `127.0.0.1`。若绑定到非回环地址且未设置 Token，已配置的 shell 与文件工具会暴露给任何能访问该端口的人，启动时会打印警告。按 Ctrl+C 停止服务。

页面使用一组小型 HTTP API：`/api/bootstrap`、`/api/agents`、`/api/models`、`/api/sessions`（可选 `q` 参数搜索标题、Agent、模型和消息内容）、`/api/chat`（Server-Sent Events）、`/api/respond` 与 `/api/cancel`。访问 `/docs` 可查看渲染后的 API 参考与协议调用示例。

### 6. AG-UI、A2A 与 MCP

`oni3 web` 会在同一个监听地址上同时提供 AG-UI、A2A 与 MCP Server，并复用同一套 Token 认证。它们都走共享执行层，因此使用的 Agent、工具、审批、会话与模型选择与 TUI 完全一致。

```bash
oni3 serve --token my-secret              # 等价于 web，也可使用 serve 命令
oni3 web --agui=false                     # 关闭 AG-UI
oni3 web --a2a=false                      # 关闭 A2A
oni3 web --mcp=false                      # 关闭 MCP Server 端点
oni3 serve --web=false --agui=false --a2a=false --mcp  # 仅协议模式，不启动网页
```

`--web=false` 会关闭浏览器界面、Web API、登录页与 `/docs`，但保留 `/api/health` 和已启用的协议端点。`--web`、`--agui`、`--a2a`、`--mcp` 至少要保留一个；关闭网页时 `--open` 会被忽略。

AG-UI：

- `POST /agui` 接收 `RunAgentInput`（`threadId`、`runId`、`messages`、`forwardedProps`、`resume`），返回 `text/event-stream`
- `forwardedProps.agent` 选择 Agent，`forwardedProps.model` 可选指定模型别名；省略时使用默认 Agent
- `threadId` 就是 Oni3 会话 ID，因此对话可以在 TUI、ACP 与 AG-UI 之间继续
- 事件包括 `RUN_STARTED`、`TEXT_MESSAGE_*`、`REASONING_*`、`TOOL_CALL_*`、`STATE_SNAPSHOT`、`RUN_ERROR` 与 `RUN_FINISHED`
- `ask_user` 与工具审批会以 `RUN_FINISHED` 的 interrupt 结束当前流；客户端再次请求并携带 `resume: [{"interruptId": "...", "status": "resolved", "payload": {...}}]` 即可恢复被挂起的运行
- 单 Agent 便捷端点：`POST /agents/{agent}/agui`

A2A 1.0：

- `GET /.well-known/agent-card.json` 与 `GET /agents/{agent}/.well-known/agent-card.json` 为每个 Agent 发布一张 Card
- `POST /a2a` 与 `POST /agents/{agent}/a2a` 实现 JSON-RPC 2.0 方法 `SendMessage`、`SendStreamingMessage`、`GetTask`、`ListTasks`、`CancelTask`、`SubscribeToTask`（同时兼容 `message/send` 等 0.3 斜杠方法名）
- 流式方法在 `text/event-stream` 帧中返回 JSON-RPC 响应，result 使用 `task`、`statusUpdate`、`artifactUpdate`、`message` 包装
- 消息角色使用 A2A 1.0 的 `ROLE_USER` 与 `ROLE_AGENT`；同时兼容 0.3 的小写 `user` 与 `agent`，并统一按 1.0 形式输出
- 任务状态使用 A2A 1.0 的 `TASK_STATE_*`；`ask_user` 与审批会把任务置为 `TASK_STATE_INPUT_REQUIRED`，针对该任务再次 `SendMessage` 即可恢复
- 任务持久化在 `~/.oni3/a2a/tasks/`，重启后仍可查询与列出

AG-UI 与 A2A 不依赖第三方 A2A/AG-UI SDK；协议类型与传输实现位于 `protocol/agui` 与 `protocol/a2a`。

MCP Server：

- 这里 Oni3 作为 MCP Server 对外提供工具，与 `config.yml` 中作为客户端连接的外部 `mcp_servers` 是两件事
- `POST /mcp` 是 Streamable HTTP 端点；`GET /mcp` 监听服务端通知，`DELETE /mcp` 终止会话
- `GET /mcp/sse` 与 `POST /mcp/message?sessionId=...` 提供旧版 SSE 传输
- 暴露与 `oni3 mcp` 相同的三个工具：`call_agent(agent, prompt, model?)`、`list_agents()` 与 `list_models()`
- 使用与 Web 服务相同的 `Authorization: Bearer <token>` 认证头

Oni3 也可以作为非流式客户端调用远端 A2A Agent。先在 `a2a_agents` 中登记远端 Agent，再通过 `a2a` 工具暴露给模型：

```yaml
a2a_agents:
  remote_researcher:
    card_url: https://example.com/.well-known/agent-card.json
    description: 远端研究 Agent
    token_env: REMOTE_A2A_TOKEN   # 也可以使用 token 直接填写

tools:
  remote_a2a:
    type: a2a
    config:
      agents: [remote_researcher]  # 白名单；省略时允许全部已配置 Agent
      timeout: 300                 # 整体超时秒数，默认 300
      poll_interval_ms: 1000       # 任务轮询间隔，默认 1000
```

工具参数为 `agent`（必填）、`prompt`（新任务必填），以及可选的 `task_id` 与 `context_id`。返回 JSON 包含 `agent`、`state`、`task_id`、`context_id`、`answer` 或 `message`；远端要求补充信息时还会返回 `options`。第一阶段不做流式：Oni3 先发现 Agent Card，调用 `SendMessage`，再轮询 `GetTask`，直到任务完成或进入 `TASK_STATE_INPUT_REQUIRED`。恢复中断任务时传入同一个 `task_id`，并把回答放进 `prompt`；只轮询时省略 `prompt`。调用超时会返回 `state: "timeout"` 和 `task_id`，后续调用可继续轮询。

远端调用会被标记为危险操作，因此在默认审批策略下，prompt 离开本机前会要求确认。远端地址只能来自 `a2a_agents`，模型不能传入任意 URL，请求目标因此由配置控制。

### 7. MCP 服务端模式

将 Oni3 本身作为 Model Context Protocol 服务器运行，提供三个工具：

```bash
oni3 mcp
```

`oni3 web` 运行时，Oni3 也会作为 MCP Server，通过 `POST /mcp`（Streamable HTTP）或 `GET /mcp/sse`（旧版 SSE）提供同一组工具。使用 `--mcp=false` 可关闭这些 Server 端点。

- `call_agent(agent, prompt, model?)`：运行任意已配置 agent，包括内置 OniThree
- `list_agents()`：返回 agent 名称、描述和默认模型别名
- `list_models()`：返回模型别名 ID 列表和 `default_model`

`call_agent` 参数：

- `agent`（必填）：`list_agents` 返回的 agent 名称
- `prompt`（必填）：交给该 agent 的任务
- `model`（可选）：本次调用使用的模型别名；省略时使用该 agent 的默认模型

`list_agents()` 返回：

```json
{"agents": [{"name": "helper", "description": "帮助处理小任务", "default_model": "alpha"}]}
```

`list_models()` 返回：

```json
{"default_model": "alpha", "models": ["alpha", "zeta"]}
```

`call_agent` 参数示例：

```json
{"agent": "search_agent", "prompt": "搜索今天的天气", "model": "alpha"}
```

每次调用都会开启一个新的单轮 agent 对话，并返回最终回答。

stdout 专用于协议通信，日志写入 `~/.oni3/oni3.log`。

客户端配置示例：

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

### 8. 配置示例

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
    timeout: 120                     # 可选：连接超时秒数，默认 60
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
oni3 --config examples/plugins/demo.yml agent --agent demo__helper
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

- **直接 SDK 类型**：`openai`、`anthropic`、`gemini`、`qwen`、`qianfan`、`ark`、`deepseek` 与 `ollama`
- **OpenAI 兼容预设**：OpenAI、Gemini（兼容接口）、Kimi、智谱 GLM、MiniMax、SiliconFlow、OpenRouter、Groq、xAI、Mistral、Together、Fireworks、ModelScope、LM Studio 与 magpie
- **本地模型**：Ollama、LM Studio 及其他 OpenAI 兼容本地服务（API Key 可选）

其他 OpenAI 兼容接口可在 `providers` 下以 `type: openai` 和 `base_url` 添加。

## 主要依赖

- [CloudWeGo Eino](https://github.com/cloudwego/eino) - AI 应用开发框架
- [Cobra](https://github.com/spf13/cobra) - 命令行界面框架
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - 交互式终端 TUI 框架
- 各种 Eino 扩展组件（模型和工具）

## 许可证

[MIT](LICENSE)
