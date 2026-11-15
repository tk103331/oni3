# Naming conventions

These names are the current public interface. Historical spellings are not aliases. Configuration types are exact, lowercase identifiers: case changes, surrounding whitespace and punctuation variants are rejected.

## Go and filenames

Use Go's mixed-case identifiers and standard initialisms: `APIKey`, `BaseURL`, `ModelID`, `HTTPRequest`, `MCPServer` and `JavaScript`. Use lowercase package names and snake_case source filenames, such as `http_request.go`, `file_system.go` and `sequential_thinking.go`. Third-party import paths and SDK identifiers keep their upstream spelling.

Tool constructors follow `New<Capability>Tool`; tool type constants follow `<Capability>ToolType`. Default instance-name constants reference the corresponding type constant. The built-in agent's identifier is `onthree` and its display name is `OniThree`; Oni3 is the product's display name.

## Configuration and tool arguments

YAML fields and application-owned JSON fields use snake_case. Examples include `base_url`, `api_key`, `default_model`, `max_steps`, `work_dir` and `memory_limit_mb`. Unknown YAML fields and unknown built-in tool configuration fields are errors. A plugin's own `config` object, request headers, environment variables, remote API payloads and JavaScript property names may follow their own schemas.

| Setting | Canonical name |
| --- | --- |
| Working directory for command tools | `config.work_dir` |
| Command template for custom execution / executable for MCP stdio | `cmd` |
| Custom tool parameter definitions | `parameters` |
| Search result count | `config.max_results` |
| Search language | `config.language` |
| Wikipedia redirect limit | `config.max_redirects` |
| Provider model-list request headers | `model_headers` |
| Provider model-list endpoint and array path | `models_url`, `models_path` |
| Fields within one discovered model | `model_id_path`, `model_name_path` |

Custom-tool parameters use lowercase snake_case names. Their types are the exact JSON Schema identifiers `string`, `number`, `integer`, `boolean`, `array` and `object`. Task statuses are `pending`, `in_progress` and `completed`; an omitted status defaults to `pending`.

Langfuse settings also use explicit snake_case fields, including `public_key`, `secret_key`, `max_task_queue_size`, `flush_interval`, `user_id` and `session_id`. Its duration fields include units, such as `timeout: 30s` and `flush_interval: 500ms`; built-in tool `timeout` fields are integer seconds.

## Tool types and identities

| Capability | Type / default name |
| --- | --- |
| Ask the user | `ask_user` |
| Bing search | `bing_search` |
| Browser control | `browser_use` |
| Shell commands | `command_line` |
| Custom command | `custom_exec` |
| Custom HTTP endpoint | `custom_http` |
| DuckDuckGo | `duckduckgo` |
| Files | `file_system` |
| Google search | `google_search` |
| HTTP requests | `http_request` |
| JavaScript execution | `javascript` |
| Loaded plugin tool | `plugin` |
| Sequential thinking | `sequential_thinking` |
| Delegate to an agent | `subagent` |
| Task list | `todo_list` |
| Wikipedia | `wikipedia` |

A configured instance may use an explicit name such as `workspace` or `weather_api`. Configured tool names start with a lowercase ASCII letter, contain lowercase letters, digits or underscores, and have at most 64 characters. The configuration key, `Info().Name`, name exposed to the model, tool events and approval policy use the same identity. Defining a second tool with the same runtime name is an error.

TUI tool rows, `/tools` and approval headings use PascalCase display labels, for example `command_line` → `CommandLine`, `http_request` → `HTTPRequest` and `javascript` → `JavaScript`. Namespaces remain visible: `demo__greet` displays as `Demo · Greet`. This formatting does not change configuration keys, runtime names, approval matching or saved session tool identities.

An interactive tool's behavior depends on its implementation, rather than a special instance-name string. For example, an `ask_user` instance named `clarify` still behaves as an interactive question tool.

## Providers, models and references

The supported provider types are `openai`, `anthropic`, `gemini`, `qwen`, `qianfan`, `ark`, `deepseek` and `ollama`. An omitted type defaults to `openai` across plugin loading, chat model creation, discovery and saved provider identity. Provider types select supported API implementations; provider configuration keys are user-defined references.

A model has two separate identities:

- Its configuration key is the local alias used by `default_model`, `agents.<name>.model` and `/model`.
- Its `model` field is the exact remote model ID sent to the provider.

Do not lowercase, replace punctuation in, or otherwise rewrite remote model IDs. A discovered model's generated local alias is `<provider>/<remote-model-id>`. Existing local aliases and their settings are reused when they already describe that provider and remote model. User-defined agent, model, provider and skill references are matched exactly; lowercase snake_case is recommended for new aliases.

## Plugins and MCP

Plugin namespaces and local export keys start with a lowercase ASCII letter and contain lowercase letters, digits and underscores. Plugin extensions use `<plugin>__<export-key>`, with a maximum full length of 64 characters. Theme lookup is case-insensitive, while the declared name uses the same lowercase naming convention.

MCP transport types are exactly `stdio`, `sse` and `streamable_http`. Fields such as `url`, `headers`, `cmd`, `args`, `env` and the optional display-only `description` belong directly under the server definition:

```yaml
mcp_servers:
  repository:
    type: streamable_http
    url: http://localhost:3000/mcp
```

MCP tools use `<server>__<local-tool>` for both their registry key and model-visible name. External tool names are converted to lowercase snake_case locally: `ReadFile` becomes `read_file`, and `get-weather` becomes `get_weather`. A name starting with a digit receives a `tool_` prefix. The complete name has a 64-character limit. Names that contain no ASCII letters or digits are rejected, and collisions after conversion are errors. The wrapper retains the exact original remote name when sending requests to the MCP server. Server selection uses recorded ownership, not string-prefix matching. Plugin and MCP tools share the agent's runtime-name collision check.

The `oni3` executable is the single entry point to the program. CLI flags follow their CLI spelling, such as `--config` and `--cwd`; they are not YAML field aliases.

The per-user data directory is `~/.oni3`. It holds `config.yml`, `skills/`, `sessions/`, `state/` and the log file `oni3.log`. An agent may set a display name with the optional `name` field; when it is absent the configuration key is used as the label.
