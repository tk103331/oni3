package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/tk103331/oni3/config"
	mcpserver "github.com/tk103331/oni3/mcp/server"
	"gopkg.in/yaml.v3"
)

func TestMCPCallAgentEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var requestedModels []string
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requestedModels = append(requestedModels, request.Model)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 0,
			"model":   request.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "mock answer from " + request.Model},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer modelServer.Close()

	dir := t.TempDir()
	previous := config.GetConfig()
	if previous == nil {
		previous = &config.Config{}
	}
	data, err := yaml.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	restore := filepath.Join(dir, "restore.yml")
	if err := os.WriteFile(restore, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := config.LoadConfig(restore); err != nil {
			t.Error(err)
		}
	})

	source := "providers:\n" +
		"  mock:\n" +
		"    type: openai\n" +
		"    base_url: " + modelServer.URL + "/v1\n" +
		"    api_key: test-key\n" +
		"models:\n" +
		"  base_model:\n" +
		"    provider: mock\n" +
		"    model: mock-base\n" +
		"  override_model:\n" +
		"    provider: mock\n" +
		"    model: mock-override\n" +
		"agents:\n" +
		"  helper:\n" +
		"    system: You are a test helper.\n" +
		"    model: base_model\n"
	configPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(configPath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	tools, err := mcpTools(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := mcpserver.New(mcpserver.Options{Name: "oni3", Version: "test", Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	mcpClient, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatal(err)
	}
	defer mcpClient.Close()
	if err := mcpClient.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := mcpClient.Initialize(context.Background(), initRequest); err != nil {
		t.Fatal(err)
	}

	list, err := mcpClient.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, item := range list.Tools {
		names = append(names, item.Name)
	}
	if strings.Join(names, ",") != "call_agent,list_agents,list_models" {
		t.Fatalf("tools = %v", names)
	}

	call := func(arguments map[string]any) *mcp.CallToolResult {
		t.Helper()
		request := mcp.CallToolRequest{}
		request.Params.Name = mcpCallAgentToolName
		request.Params.Arguments = arguments
		result, err := mcpClient.CallTool(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	result := call(map[string]any{"agent": "helper", "prompt": "hello"})
	if result.IsError {
		t.Fatalf("default model failed: %+v", result)
	}
	if got := result.Content[0].(mcp.TextContent).Text; got != "mock answer from mock-base" {
		t.Fatalf("default model answer = %q", got)
	}

	result = call(map[string]any{"agent": "helper", "prompt": "hello", "model": "override_model"})
	if result.IsError {
		t.Fatalf("model override failed: %+v", result)
	}
	if got := result.Content[0].(mcp.TextContent).Text; got != "mock answer from mock-override" {
		t.Fatalf("override answer = %q", got)
	}

	agentsRequest := mcp.CallToolRequest{}
	agentsRequest.Params.Name = mcpListAgentsToolName
	agentsRequest.Params.Arguments = map[string]any{}
	agentsResult, err := mcpClient.CallTool(context.Background(), agentsRequest)
	if err != nil {
		t.Fatal(err)
	}
	if got := agentsResult.Content[0].(mcp.TextContent).Text; !strings.Contains(got, `"name":"helper"`) {
		t.Fatalf("list_agents = %q", got)
	}

	modelsRequest := mcp.CallToolRequest{}
	modelsRequest.Params.Name = mcpModelsToolName
	modelsRequest.Params.Arguments = map[string]any{}
	modelsResult, err := mcpClient.CallTool(context.Background(), modelsRequest)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := modelsResult.Content[0].(mcp.TextContent).Text,
		`{"default_model":"base_model","models":["base_model","override_model"]}`; got != want {
		t.Fatalf("list_models = %q, want %q", got, want)
	}

	mu.Lock()
	gotModels := append([]string(nil), requestedModels...)
	mu.Unlock()
	if strings.Join(gotModels, ",") != "mock-base,mock-override" {
		t.Fatalf("requested models = %v", gotModels)
	}
}
