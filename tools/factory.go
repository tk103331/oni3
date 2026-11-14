package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/tools/custom"
)

type toolDefinition struct {
	create     func(string, config.Tool) (tool.InvokableTool, error)
	configKeys string
}

// One catalog defines accepted types, constructors and configuration field names.
// No case folding or historical type aliases are applied.
var toolDefinitions = map[string]toolDefinition{
	AskUserToolType:            {NewAskUserTool, ""},
	BingSearchToolType:         {NewBingSearchTool, "api_key max_results"},
	BrowserUseToolType:         {NewBrowserUseTool, "headless disable_security chrome_path proxy args"},
	CommandLineToolType:        {NewCommandLineTool, "work_dir timeout"},
	CustomExecToolType:         {custom.NewExecTool, "cmd work_dir env timeout"},
	CustomHTTPToolType:         {custom.NewHTTPTool, "url method headers body timeout"},
	DuckDuckGoToolType:         {NewDuckDuckGoTool, "max_results region safe_search timeout"},
	FileSystemToolType:         {NewFileSystemTool, "root max_bytes max_entries timeout"},
	GoogleSearchToolType:       {NewGoogleSearchTool, "api_key search_engine_id base_url max_results language"},
	HTTPRequestToolType:        {NewHTTPRequestTool, "timeout user_agent method headers"},
	JavaScriptToolType:         {NewJavaScriptTool, "timeout memory_limit_mb"},
	PluginToolType:             {loadedPluginTool, ""},
	SequentialThinkingToolType: {NewSequentialThinkingTool, ""},
	SubagentToolType:           {NewSubagentTool, "agents tools allow_temp max_depth timeout"},
	TodoListToolType:           {NewTodoListTool, ""},
	WikipediaToolType:          {NewWikipediaTool, "language max_results doc_max_chars timeout base_url user_agent max_redirects"},
}

var configuredToolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// CreateTool creates a tool whose runtime name is exactly its configuration key.
func CreateTool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	definition, ok := toolDefinitions[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported tool type: %q", cfg.Type)
	}
	if !configuredToolName.MatchString(name) {
		return nil, fmt.Errorf("invalid tool name %q: use lowercase letters, digits and underscores, starting with a letter (maximum 64 characters)", name)
	}
	allowed := make(map[string]bool)
	for _, key := range strings.Fields(definition.configKeys) {
		allowed[key] = true
	}
	var unknown []string
	for key := range cfg.Config {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("tool %q (%s): unknown configuration fields: %s", name, cfg.Type, strings.Join(unknown, ", "))
	}
	if len(cfg.Parameters) > 0 && cfg.Type != CustomExecToolType && cfg.Type != CustomHTTPToolType {
		return nil, fmt.Errorf("tool %q: parameters are only supported by custom_exec and custom_http", name)
	}
	instance, err := definition.create(name, cfg)
	if err != nil {
		return nil, err
	}
	info, err := instance.Info(context.Background())
	if err != nil {
		return nil, fmt.Errorf("tool %q: cannot read metadata: %w", name, err)
	}
	if info == nil || info.Name != name {
		return nil, fmt.Errorf("tool %q: runtime name must match its configuration key", name)
	}
	return instance, nil
}

func loadedPluginTool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	if cfg.PluginTool == nil {
		return nil, fmt.Errorf("plugin tool %q has not been loaded", name)
	}
	return cfg.PluginTool, nil
}

// CreateToolsFromConfig creates all tools from configuration.
func CreateToolsFromConfig(cfg *config.Config) (map[string]tool.InvokableTool, error) {
	instances := make(map[string]tool.InvokableTool)
	names := make([]string, 0, len(cfg.Tools))
	for name := range cfg.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		instance, err := CreateTool(name, cfg.Tools[name])
		if err != nil {
			return nil, fmt.Errorf("failed to create tool %s: %w", name, err)
		}
		instances[name] = instance
	}
	return instances, nil
}
