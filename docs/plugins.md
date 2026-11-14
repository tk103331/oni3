# JavaScript plugins

Oni3 embeds [moejs](https://github.com/MoeForge/moejs), a pure-Go JavaScript runtime. Plugins are ES modules and require no Node.js installation. Building Oni3 now requires Go 1.25 or newer.

One module may export any combination of `tools`, `hooks`, `themes`, `agents`, `models` and `providers`. Each export is an object whose keys name the extensions. Omit unused exports. The complete example is in [`examples/plugins/demo.js`](../examples/plugins/demo.js) with [`demo.yml`](../examples/plugins/demo.yml).

## Loading a plugin

```yaml
plugins:
  demo:
    path: ./plugins/demo.js
    timeout: 30
    memory_limit_mb: 64
    config:
      greeting: Hello

agents:
  assistant:
    system: Help the user with the available tools.
    tools: [demo__greet]
```

`path` is required and resolves relative to the YAML file, independently of `--cwd`. `timeout` is in seconds (default 30); `memory_limit_mb` sets the JS runtime allocation budget (default 64 MiB). Both must be nonnegative; zero selects the default. `config` is an optional JSON-compatible object passed to tools and hooks.

Extensions are named `<plugin>__<export-key>`, for example `demo__greet`, `demo__helper`, `demo__fast` and `demo__ocean`. Names start with a lowercase letter and use lowercase letters, numbers and underscores, with a maximum of 64 characters including the namespace. Registration fails on collisions, invalid definitions or failed module initialization; it never silently overwrites an existing extension. Theme names are case-insensitive. Plugins load in alphabetical order. Restart the CLI to reload changed modules.

Plugin tools must be listed in an agent's `tools` or a skill's additional tools. Loading a plugin does not automatically add its tools to every agent.

## Tools

```js
export const tools = {
  greet: {
    description: "Greet someone",
    parameters: {
      type: "object",
      properties: { name: { type: "string" } },
      required: ["name"],
    },
    execute(args, context) {
      return `${context.config.greeting}, ${args.name}!`;
    },
  },
};
```

`description` and an `execute` function are required. `parameters` is an object JSON Schema; nested properties, arrays and required fields are preserved for the model. Omitting it declares an object with no listed parameters. As with other tools, the plugin is responsible for validating argument values during execution.

The model's JSON object becomes `args`. The second argument is `{ plugin, tool, config }`: the configured plugin namespace, local tool name and an isolated copy of configuration. Strings return as plain text; JSON-compatible values return as JSON. Throwing or rejecting a promise reports a tool failure through the standard agent error handling. A plugin's own timeout is recoverable; cancellation of the parent turn still cancels execution.

`execute` may be `async` and use promises and timers:

```js
async execute(args) {
  await new Promise(resolve => setTimeout(resolve, 10));
  return { result: args.value };
}
```

## Hooks

```js
export const hooks = {
  before_agent(event, context) {
    console.info("Starting", event.agent, context.plugin);
  },
  after_agent(event) {
    console.info("Finished", event.agent, event.error ?? "ok");
  },
  before_tool(event) {
    console.info("Calling", event.tool, event.call_id);
  },
  after_tool(event) {
    console.info("Result", event.tool, event.result);
  },
};
```

Supported events:

| Event | Available information |
| --- | --- |
| `before_agent` | `agent`, `model`, `prompt` |
| `after_agent` | `agent`, `model`, `prompt`, `result`, optional `error` |
| `before_tool` | `agent`, `model`, `tool`, optional `call_id`, `arguments` (JSON text) |
| `after_tool` | Same tool fields plus `result` and optional `error` |

Every event includes `type`. Hooks receive snapshots; return values are ignored and cannot rewrite prompts, tool arguments or results. Hooks may be async. They run in plugin-name order; the first failure stops dispatch and is reported. A failing `before_agent` prevents the turn, and a failing `before_tool` prevents the tool call. After-hook failures preserve the original execution error when one exists. Tool hooks bracket the invocation including approval results. Agent hooks cover `run`, TUI, ACP and subagents, including streaming and nonstreaming paths. No further hooks run once the parent context has been canceled.

Every hook gets a fresh runtime, so a module variable cannot share state between before/after events or tool calls. Use event fields for correlation.

## Themes

```js
export const themes = {
  ocean: {
    description: "Ocean blue",
    light: {
      primary: "#0369A1", accent: "#0E7490", success: "#047857",
      warning: "#B45309", error: "#B91C1C", user: "#4338CA", muted: "#64748B",
    },
    dark: {
      primary: "#7DD3FC", accent: "#67E8F9", success: "#6EE7B7",
      warning: "#FCD34D", error: "#FCA5A5", user: "#A5B4FC", muted: "#94A3B8",
    },
  },
};
```

Both variants must provide all seven colors as `#RRGGBB`. The theme appears in `/theme` completion and is applied with `/theme demo__ocean`, `/theme demo__ocean light` or `/theme demo__ocean dark`. Markdown rendering uses the same palette.

## Agents and models

```js
export const models = {
  fast: { provider: "local", model: "qwen3", temperature: 0.3 },
};

export const agents = {
  helper: {
    description: "Greet people and analyze text",
    system: "Complete the user's task with the available tools.",
    model: "demo__fast",
    tools: ["demo__greet"],
    max_steps: 1000,
  },
};
```

These exports define presets using the same fields as YAML. Agents support `description`, `system`, `model`, `tools`, `mcp_servers` and `max_steps`; models support `provider`, `model`, `max_tokens`, `temperature`, `top_p` and `top_k`. Unknown configuration fields fail loading.

Models reference a provider configured in YAML or registered by a plugin, which holds the provider type, endpoint and credentials. This extension registers model presets; JavaScript does not implement a new model transport or streaming protocol. Reference names explicitly, including prefixes for other plugin extensions. References may cross plugins. An omitted agent model follows the normal default model selection.

Agents appear in `/agent` and the configured-subagent catalog. Models appear in `/model`. For example:

```bash
# The sample uses Ollama with qwen3 installed.
eino --config examples/plugins/demo.yml agent --agent demo__helper
```

Then select `/theme demo__ocean dark` or `/model demo__fast` in the TUI.

## Providers and model discovery

```js
export const providers = {
  cloud: {
    description: "My OpenAI-compatible service",
    type: "openai",
    base_url: "https://api.example.com/v1",
    models_url: "models",
    models_path: "data",
    model_id_path: "id",
    model_name_path: "name",
  },
};
```

A provider selects an existing chat transport with `type` and supplies a `base_url` and model-list endpoint. The registered name is `<plugin>__<key>`, such as `catalog__cloud`. Credentials and endpoints can be customized in YAML without editing the plugin:

```yaml
providers:
  catalog__cloud:
    api_key: your-api-key
    # base_url: https://your-service.example/v1
```

`models_url` may be absolute, a path relative to `base_url` (`models` retains a `/v1` prefix), or an origin-root path (`/api/tags`). `models_path` selects the array in the JSON response; `model_id_path` and optional `model_name_path` select fields within each item. Paths use dot notation. Common model list formats are recognized when mapping fields are omitted.

Requests use the configured API key with the selected transport's authentication convention. For a custom list API, `model_headers` overrides/adds headers and supports `{{api_key}}` substitutions:

```js
model_headers: { "X-API-Key": "{{api_key}}" }
```

`api_key_optional: true` skips the missing-key prompt for services such as local Ollama. Provider definitions and presets load without making model-list requests. Network requests happen only when the user chooses a provider.

In the TUI:

1. Enter `/provider` and choose a provider, or enter `/provider catalog__cloud` directly.
2. If no API key is available, enter it in the masked input. It is never appended to chat or session history.
3. The CLI fetches the remote model list asynchronously with that key. Esc cancels an in-flight request. Failed requests can be retried or supplied with a replacement key.
4. Select one model to add it **and switch immediately**, or choose **全部添加** to import every model while retaining the current selection. Type to filter model IDs/names; the bulk action is shown when the filter is empty.

Discovery currently reads one response page; bulk import adds all models returned in that list.

New model entries are named `<provider>/<remote-model-id>`. Existing matching models are reused, preserving their names and settings. Imports never overwrite a different model. Successfully validated entered keys and imported models are saved in a private state overlay (file mode `0600`) scoped to the configuration file and provider endpoint. Explicit YAML API keys take precedence on restart. The original YAML and JS modules are not rewritten.

A provider-only configuration can start the TUI before any models have been added. See [`providers.js`](../examples/plugins/providers.js) and [`providers.yml`](../examples/plugins/providers.yml):

```bash
eino --config examples/plugins/providers.yml
```

Then use `/provider` to select `catalog__cloud` or `catalog__local` and import models.

## Runtime APIs and limits

- Standard JavaScript, promises, `setTimeout`/`clearTimeout`, `setInterval`/`clearInterval` and the event loop provided by moejs.
- `console.log`, `console.info`, `console.warn` and `console.error` write through the CLI logger.
- Static relative ES-module imports are resolved inside the entry module's directory. Include filenames and extensions, such as `./helpers/text.js`. Absolute paths, escaping paths and bare package imports are rejected. Modules are compiled once; tool calls do not reread files.
- Filesystem, process, network, Node.js built-ins, npm packages and dynamic module loading are not installed in the runtime.
- Each module source is limited to 1 MiB and a plugin graph to 128 modules. Arguments, decoded manifests and returned JSON/text have a 1 MiB budget. Values must be JSON-compatible.
- Each invocation uses an independent runtime. Its deadline covers module initialization, execution, timers and result conversion; the JS memory budget covers runtime allocations. Unsettled promises without pending work report an error. Pending timers keep the invocation alive until completion or timeout, so clear unused intervals.
