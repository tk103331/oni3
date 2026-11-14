package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tk103331/oni3/config"
	"gopkg.in/yaml.v3"
)

func TestToolCatalogUsesConfiguredIdentity(t *testing.T) {
	for _, tc := range []struct{ kind, settings string }{
		{AskUserToolType, ""},
		{BingSearchToolType, "config:\n  api_key: test\n"},
		{BrowserUseToolType, ""},
		{CommandLineToolType, ""},
		{CustomExecToolType, "config:\n  cmd: pwd\n"},
		{CustomHTTPToolType, "config:\n  url: https://example.com\n"},
		{DuckDuckGoToolType, ""},
		{FileSystemToolType, ""},
		{GoogleSearchToolType, "config:\n  api_key: test\n  search_engine_id: test\n  max_results: 3\n  language: en\n"},
		{HTTPRequestToolType, ""},
		{JavaScriptToolType, ""},
		{SequentialThinkingToolType, ""},
		{SubagentToolType, ""},
		{TodoListToolType, ""},
		{WikipediaToolType, "config:\n  max_results: 3\n  max_redirects: 2\n"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var cfg config.Tool
			if err := yaml.Unmarshal([]byte("type: "+tc.kind+"\ndescription: Custom description\n"+tc.settings), &cfg); err != nil {
				t.Fatal(err)
			}
			instance, err := CreateTool("custom_"+tc.kind, cfg)
			if err != nil {
				t.Fatal(err)
			}
			info, err := instance.Info(context.Background())
			if err != nil || info.Name != "custom_"+tc.kind || info.Desc != cfg.Description {
				t.Fatalf("configured identity lost: %+v / %v", info, err)
			}
		})
	}
}

func TestToolCatalogRejectsNoncanonicalTypesAndFields(t *testing.T) {
	for _, kind := range []string{"askuser", "todolist", "todo", "commandline", "filesystem", "httprequest", "browseruse", "bingsearch", "googlesearch", "sequentialthinking", "customhttp", "customexec", "SUBAGENT", " subagent "} {
		if _, err := CreateTool("test", config.Tool{Type: kind}); err == nil {
			t.Errorf("accepted noncanonical type %q", kind)
		}
	}
	for _, name := range []string{"CamelCase", "hyphen-name", "name with spaces", "_leading", strings.Repeat("a", 65)} {
		if _, err := CreateTool(name, config.Tool{Type: JavaScriptToolType}); err == nil {
			t.Errorf("accepted noncanonical tool name %q", name)
		}
	}
	for _, tc := range []struct{ kind, field string }{
		{CommandLineToolType, "workdir"},
		{CustomExecToolType, "dir"},
		{GoogleSearchToolType, "num"},
		{GoogleSearchToolType, "lang"},
		{WikipediaToolType, "top_k"},
		{WikipediaToolType, "max_redirect"},
		{JavaScriptToolType, "memoryLimitMB"},
	} {
		var cfg config.Tool
		if err := yaml.Unmarshal([]byte("type: "+tc.kind+"\nconfig:\n  "+tc.field+": 1\n"), &cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := CreateTool("test", cfg); err == nil || !strings.Contains(err.Error(), "unknown configuration fields") {
			t.Errorf("accepted noncanonical %s field %s: %v", tc.kind, tc.field, err)
		}
	}
}

func TestTodoListRejectsStatusAliases(t *testing.T) {
	instance, err := NewTodoListTool(TodoListToolName, config.Tool{})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"done", "doing", "in-progress", "COMPLETED"} {
		_, err := instance.InvokableRun(WithHost(context.Background(), &fakeHost{}), `{"todos":[{"content":"task","status":"`+status+`"}]}`)
		if err == nil || !strings.Contains(err.Error(), "invalid status") {
			t.Errorf("accepted status alias %q: %v", status, err)
		}
	}
}
