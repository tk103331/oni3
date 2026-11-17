package mcp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	mcpProtocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tk103331/oni3/config"
)

// newLookupServer serves one tool over Streamable HTTP, standing in for a
// reachable external MCP server.
func newLookupServer(t *testing.T) *httptest.Server {
	t.Helper()
	remote := server.NewMCPServer("test", "1.0.0", server.WithToolCapabilities(false))
	remote.AddTool(mcpProtocol.NewTool("lookup", mcpProtocol.WithDescription("Remote lookup")),
		func(context.Context, mcpProtocol.CallToolRequest) (*mcpProtocol.CallToolResult, error) {
			return mcpProtocol.NewToolResultText("remote result"), nil
		})
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(remote))
	t.Cleanup(httpServer.Close)
	return httpServer
}

// TestOneBrokenServerKeepsTheOthersUsable pins the startup contract: an
// unreachable server is recorded per server instead of discarding every other
// server's tools.
func TestOneBrokenServerKeepsTheOthersUsable(t *testing.T) {
	httpServer := newLookupServer(t)
	cfg := &config.Config{MCPServers: map[string]config.MCPServer{
		"broken":  {Type: config.MCPTypeStreamableHTTP, URL: "https://127.0.0.1:1/mcp"},
		"working": {Type: config.MCPTypeStreamableHTTP, URL: httpServer.URL + "/mcp"},
	}}

	client := NewClient(cfg)
	defer client.Close()
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("a broken server aborted the reachable ones: %v", err)
	}

	tools := client.GetTools()
	if len(tools) != 1 || tools["working__lookup"] == nil {
		t.Fatalf("tools of the reachable server are missing: %+v", tools)
	}

	failures := client.ServerErrors()
	if len(failures) != 1 || failures["broken"] == nil {
		t.Fatalf("broken server not reported: %+v", failures)
	}
	if summary := ShortError(failures["broken"]); summary == "" || strings.Contains(summary, "\n") {
		t.Fatalf("short error is not a single line: %q", summary)
	}
}

// TestAllServersBrokenReportsFailure keeps a fully broken configuration
// visible: nothing is usable, so the caller gets an error.
func TestAllServersBrokenReportsFailure(t *testing.T) {
	cfg := &config.Config{MCPServers: map[string]config.MCPServer{
		"broken": {Type: config.MCPTypeStreamableHTTP, URL: "https://127.0.0.1:1/mcp"},
	}}

	client := NewClient(cfg)
	defer client.Close()
	if err := client.Initialize(context.Background()); err == nil {
		t.Fatal("a configuration without any usable server initialized silently")
	}
	if len(client.GetTools()) != 0 {
		t.Fatalf("failed servers registered tools: %+v", client.GetTools())
	}
	if len(client.ServerErrors()) != 1 {
		t.Fatalf("failure not recorded: %+v", client.ServerErrors())
	}
}
