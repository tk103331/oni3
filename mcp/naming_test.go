package mcp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpClient "github.com/mark3labs/mcp-go/client"
	mcpProtocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tk103331/oni3/config"
)

func TestDiscoveredToolsExposeCanonicalNamesAndInvokeRemoteNames(t *testing.T) {
	ctx := context.Background()
	c := NewClient(&config.Config{})
	defer c.Close()
	var calledServer, calledRemote, calledQuery string
	for _, name := range []string{"a", "a_b", "a__branch"} {
		remote := server.NewMCPServer("test", "1.0.0", server.WithToolCapabilities(false))
		remote.AddTool(mcpProtocol.NewTool("Lookup", mcpProtocol.WithDescription("Remote lookup"), mcpProtocol.WithString("query")),
			func(ctx context.Context, request mcpProtocol.CallToolRequest) (*mcpProtocol.CallToolResult, error) {
				calledServer = name
				calledRemote = request.Params.Name
				calledQuery = request.GetString("query", "")
				return mcpProtocol.NewToolResultText("remote result"), nil
			})
		client, err := mcpClient.NewInProcessClient(remote)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Start(ctx); err != nil {
			t.Fatal(err)
		}
		c.clients[name] = client
	}
	if err := c.discoverTools(ctx); err != nil {
		t.Fatal(err)
	}
	all := c.GetTools()
	if len(all) != 3 {
		t.Fatalf("tools missing: %+v", all)
	}
	for key, discovered := range all {
		info, err := discovered.Info(ctx)
		if err != nil || info.Name != key || info.Desc != "Remote lookup" || info.ParamsOneOf == nil {
			t.Fatalf("registry and model-visible metadata differ: key=%q info=%+v err=%v", key, info, err)
		}
		remoteInfo, err := discovered.(*namedTool).InvokableTool.Info(ctx)
		if err != nil || remoteInfo.Name != "Lookup" {
			t.Fatalf("remote invocation metadata mutated: %+v, %v", remoteInfo, err)
		}
		info.Name = "modified_by_caller"
		again, _ := discovered.Info(ctx)
		if again.Name != key {
			t.Fatal("caller modified the registered tool name through Info")
		}
	}
	selected := c.GetToolsForServers([]string{"a", "a", "missing"})
	if len(selected) != 1 || selected["a__lookup"] == nil {
		t.Fatalf("prefix-related servers leaked into selection: %+v", selected)
	}
	if _, err := selected["a__lookup"].InvokableRun(ctx, `{"query":"test"}`); err != nil {
		t.Fatal(err)
	}
	if calledServer != "a" || calledRemote != "Lookup" || calledQuery != "test" {
		t.Fatalf("canonical call did not reach original tool: server=%q remote=%q query=%q", calledServer, calledRemote, calledQuery)
	}
	if selected := c.GetToolsForServers([]string{"a_b", "a__branch"}); len(selected) != 2 || selected["a__lookup"] != nil {
		t.Fatalf("exact server ownership lost: %+v", selected)
	}
	delete(all, "a__lookup")
	if c.GetTools()["a__lookup"] == nil {
		t.Fatal("GetTools exposed the mutable registry map")
	}
}

type stubRemoteTool struct {
	info *schema.ToolInfo
	err  error
}

func (t *stubRemoteTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, t.err
}

func (t *stubRemoteTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "remote result", nil
}

func TestExternalToolNamesUseCanonicalLocalIdentifiers(t *testing.T) {
	for _, tc := range []struct{ remote, local string }{
		{"ReadFile", "read_file"},
		{"read-HTTPValue", "read_http_value"},
		{"tools.lookup", "tools_lookup"},
		{"123lookup", "tool_123lookup"},
	} {
		c := NewClient(&config.Config{})
		original := &stubRemoteTool{info: &schema.ToolInfo{Name: tc.remote}}
		if err := c.registerTool(context.Background(), "server", original); err != nil {
			t.Fatal(err)
		}
		registered := c.GetTools()["server__"+tc.local]
		if registered == nil {
			t.Fatalf("missing canonical name for %q", tc.remote)
		}
		info, err := registered.Info(context.Background())
		if err != nil || info.Name != "server__"+tc.local || original.info.Name != tc.remote {
			t.Fatalf("external/local identity mismatch: %+v / %v", info, err)
		}
	}
}

func TestToolNameCollisionsDoNotOverwrite(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ firstServer, firstRemote, secondServer, secondRemote string }{
		{"a", "lookup", "a", "lookup"},
		{"a", "b__c", "a__b", "c"},
		{"a", "read_file", "a", "Read-File"},
	} {
		t.Run(tc.firstServer+"__"+tc.firstRemote, func(t *testing.T) {
			c := NewClient(&config.Config{})
			first := &stubRemoteTool{info: &schema.ToolInfo{Name: tc.firstRemote}}
			if err := c.registerTool(ctx, tc.firstServer, first); err != nil {
				t.Fatal(err)
			}
			local, err := remoteToolIdentifier(tc.firstRemote)
			if err != nil {
				t.Fatal(err)
			}
			key := tc.firstServer + "__" + local
			registered := c.GetTools()[key]
			second := &stubRemoteTool{info: &schema.ToolInfo{Name: tc.secondRemote}}
			if err := c.registerTool(ctx, tc.secondServer, second); err == nil || !strings.Contains(err.Error(), "collides") {
				t.Fatalf("expected duplicate-name error, got %v", err)
			}
			if len(c.GetTools()) != 1 || c.GetTools()[key] != registered || c.toolServers[key] != tc.firstServer {
				t.Fatal("collision overwrote the original tool or owning server")
			}
		})
	}
}

func TestToolRegistrationRejectsUnusableNamesAndMetadata(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, server string
		remote       *stubRemoteTool
	}{
		{"nil info", "server", &stubRemoteTool{}},
		{"info error", "server", &stubRemoteTool{err: errors.New("info failed")}},
		{"empty remote", "server", &stubRemoteTool{info: &schema.ToolInfo{}}},
		{"uppercase server", "Server", &stubRemoteTool{info: &schema.ToolInfo{Name: "lookup"}}},
		{"hyphen server", "my-server", &stubRemoteTool{info: &schema.ToolInfo{Name: "lookup"}}},
		{"punctuation remote", "server", &stubRemoteTool{info: &schema.ToolInfo{Name: "---"}}},
		{"space remote", "server", &stubRemoteTool{info: &schema.ToolInfo{Name: " "}}},
		{"long composite", strings.Repeat("a", 60), &stubRemoteTool{info: &schema.ToolInfo{Name: "abc"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(&config.Config{})
			if err := c.registerTool(ctx, tc.server, tc.remote); err == nil {
				t.Fatal("unusable tool registration succeeded")
			}
			if len(c.tools) != 0 || len(c.toolServers) != 0 {
				t.Fatal("failed registration changed the registry")
			}
		})
	}
	c := NewClient(&config.Config{})
	longest := strings.Repeat("a", 61)
	if err := c.registerTool(ctx, longest, &stubRemoteTool{info: &schema.ToolInfo{Name: "b"}}); err != nil {
		t.Fatalf("valid 64-character composite name rejected: %v", err)
	}
}

func TestTransportValidationAndFactoryRejectTheSameTypes(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"", "STDIO", "SSE", "http", "HTTP", "streamable-http", "STREAMABLE_HTTP", " sse", "sse "} {
		t.Run(kind, func(t *testing.T) {
			serverConfig := config.MCPServer{Type: kind, URL: "https://example.invalid/mcp"}
			cfg := &config.Config{MCPServers: map[string]config.MCPServer{"test": serverConfig}}
			validationErr := ValidateConfig(cfg)
			_, factoryErr := NewClient(cfg).createMCPClient(context.Background(), "test", serverConfig)
			if validationErr == nil || factoryErr == nil || validationErr.Error() != factoryErr.Error() || !strings.Contains(factoryErr.Error(), "unsupported MCP server type") {
				t.Fatalf("validation/factory differ for %q: %v / %v", kind, validationErr, factoryErr)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		server config.MCPServer
	}{
		{"stdio with URL", config.MCPServer{Type: config.MCPTypeStdio, URL: "https://example.invalid/mcp"}},
		{"SSE with command", config.MCPServer{Type: config.MCPTypeSSE, Cmd: executable}},
		{"HTTP with command", config.MCPServer{Type: config.MCPTypeStreamableHTTP, Cmd: executable}},
		{"both", config.MCPServer{Type: config.MCPTypeSSE, Cmd: executable, URL: "https://example.invalid/mcp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validationErr := validateServerConfig("test", tc.server)
			_, factoryErr := NewClient(&config.Config{}).createMCPClient(context.Background(), "test", tc.server)
			if validationErr == nil || factoryErr == nil || validationErr.Error() != factoryErr.Error() {
				t.Fatalf("validation/factory differ: %v / %v", validationErr, factoryErr)
			}
		})
	}
	// Validate stdio without starting a subprocess. HTTP constructors do not
	// connect until the transport starts, so all of these checks remain offline.
	if err := validateServerConfig("test", config.MCPServer{Type: config.MCPTypeStdio, Cmd: executable}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{config.MCPTypeSSE, config.MCPTypeStreamableHTTP} {
		serverConfig := config.MCPServer{Type: kind, URL: "https://example.invalid/mcp"}
		if err := validateServerConfig("test", serverConfig); err != nil {
			t.Fatal(err)
		}
		client, err := NewClient(&config.Config{}).createMCPClient(context.Background(), "test", serverConfig)
		if err != nil || client == nil {
			t.Fatalf("canonical transport %q rejected: %v", kind, err)
		}
		client.Close()
	}
}
