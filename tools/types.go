package tools

// Tool type identifiers use lowercase snake_case. The default instance name is
// the type identifier; configured instances may use their own explicit names.
const (
	A2AToolType                = "a2a"
	AskUserToolType            = "ask_user"
	BingSearchToolType         = "bing_search"
	BrowserUseToolType         = "browser_use"
	CommandLineToolType        = "command_line"
	CustomExecToolType         = "custom_exec"
	CustomHTTPToolType         = "custom_http"
	DuckDuckGoToolType         = "duckduckgo"
	FileSystemToolType         = "file_system"
	GoogleSearchToolType       = "google_search"
	HTTPRequestToolType        = "http_request"
	JavaScriptToolType         = "javascript"
	PluginToolType             = "plugin"
	SequentialThinkingToolType = "sequential_thinking"
	SubagentToolType           = "subagent"
	TodoListToolType           = "todo_list"
	WikipediaToolType          = "wikipedia"
)

const (
	A2AToolName         = A2AToolType
	AskUserToolName     = AskUserToolType
	BrowserUseToolName  = BrowserUseToolType
	CommandLineToolName = CommandLineToolType
	FileSystemToolName  = FileSystemToolType
	HTTPRequestToolName = HTTPRequestToolType
	JavaScriptToolName  = JavaScriptToolType
	SubagentToolName    = SubagentToolType
	TodoListToolName    = TodoListToolType
)
