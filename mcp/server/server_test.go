package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type stubTool struct {
	info   *schema.ToolInfo
	result string
	err    error
	args   string
}

func (s *stubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return s.info, nil
}

func (s *stubTool) InvokableRun(_ context.Context, arguments string, _ ...tool.Option) (string, error) {
	s.args = arguments
	return s.result, s.err
}

func echoTool() *stubTool {
	return &stubTool{
		info: &schema.ToolInfo{
			Name: "echo",
			Desc: "Echo the input text",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"text": {Type: schema.String, Desc: "Text to echo", Required: true},
			}),
		},
		result: "hello",
	}
}

func newClient(t *testing.T, srv *server.MCPServer) *client.Client {
	t.Helper()
	mcpClient, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mcpClient.Close() })
	if err := mcpClient.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := mcpClient.Initialize(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	return mcpClient
}

func TestServerListsAndCallsTool(t *testing.T) {
	echo := echoTool()
	srv, err := New(Options{Name: "oni3", Version: "test", Tools: []tool.InvokableTool{echo}})
	if err != nil {
		t.Fatal(err)
	}
	mcpClient := newClient(t, srv)

	list, err := mcpClient.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", list.Tools)
	}
	if list.Tools[0].Description != "Echo the input text" {
		t.Fatalf("description = %q", list.Tools[0].Description)
	}
	if list.Tools[0].InputSchema.Type != "object" {
		t.Fatalf("input schema type = %q", list.Tools[0].InputSchema.Type)
	}
	if got := list.Tools[0].InputSchema.Required; len(got) != 1 || got[0] != "text" {
		t.Fatalf("required = %v", got)
	}

	request := mcp.CallToolRequest{}
	request.Params.Name = "echo"
	request.Params.Arguments = map[string]any{"text": "hello"}
	result, err := mcpClient.CallTool(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	if got := result.Content[0].(mcp.TextContent).Text; got != "hello" {
		t.Fatalf("result = %q", got)
	}
	if echo.args != `{"text":"hello"}` {
		t.Fatalf("arguments = %q", echo.args)
	}
}

func TestServerMarksToolFailures(t *testing.T) {
	failing := &stubTool{
		info: &schema.ToolInfo{Name: "failing", Desc: "Always fails"},
		err:  errors.New("boom"),
	}
	recovered := &stubTool{
		info:   &schema.ToolInfo{Name: "recovered", Desc: "Returns a recoverable failure"},
		result: "tool execution failed: nope",
	}
	srv, err := New(Options{
		Name: "oni3", Version: "test",
		Tools: []tool.InvokableTool{failing, recovered},
	})
	if err != nil {
		t.Fatal(err)
	}
	mcpClient := newClient(t, srv)

	for name, want := range map[string]string{"failing": "boom", "recovered": "nope"} {
		request := mcp.CallToolRequest{}
		request.Params.Name = name
		result, err := mcpClient.CallTool(context.Background(), request)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !result.IsError {
			t.Fatalf("%s: failure was not marked as an error: %+v", name, result)
		}
		if got := result.Content[0].(mcp.TextContent).Text; got != want {
			t.Fatalf("%s: result = %q, want %q", name, got, want)
		}
	}
}

func TestToolInputSchema(t *testing.T) {
	raw, err := toolInputSchema(echoTool().info)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Type       string                    `json:"type"`
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Type != "object" || parsed.Properties["text"]["description"] != "Text to echo" {
		t.Fatalf("schema = %s", raw)
	}
	if len(parsed.Required) != 1 || parsed.Required[0] != "text" {
		t.Fatalf("required = %v", parsed.Required)
	}
	if strings.Contains(string(raw), "$schema") {
		t.Fatalf("schema keeps the $schema field: %s", raw)
	}

	empty, err := toolInputSchema(&schema.ToolInfo{Name: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != `{"type":"object","properties":{}}` {
		t.Fatalf("empty schema = %s", empty)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	if _, err := New(Options{Version: "test"}); err == nil {
		t.Fatal("empty server name accepted")
	}
	if _, err := New(Options{Name: "oni3"}); err == nil {
		t.Fatal("empty server version accepted")
	}
	if _, err := New(Options{Name: "oni3", Version: "test", Tools: []tool.InvokableTool{nil}}); err == nil {
		t.Fatal("nil tool accepted")
	}
	if _, err := New(Options{
		Name: "oni3", Version: "test",
		Tools: []tool.InvokableTool{&stubTool{}},
	}); err == nil {
		t.Fatal("tool without metadata accepted")
	}
}
