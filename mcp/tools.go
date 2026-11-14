package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpProtocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/tk103331/oni3/config"
)

// namedTool exposes the canonical server__remote_tool name to the model while
// preserving the original tool's metadata and remote invocation name. Never
// mutate the original Info: the MCP adapter uses it to construct tools/call.
type namedTool struct {
	tool.InvokableTool
	info schema.ToolInfo
}

func (t *namedTool) Info(context.Context) (*schema.ToolInfo, error) {
	info := t.info
	return &info, nil
}

var (
	remoteAcronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	remoteWordBoundary    = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	remoteSeparators      = regexp.MustCompile(`[^a-z0-9_]+`)
)

// remoteToolIdentifier adapts external names to the local naming convention.
// The original remote name stays on the wrapped tool for tools/call requests.
func remoteToolIdentifier(name string) (string, error) {
	name = remoteAcronymBoundary.ReplaceAllString(name, `${1}_${2}`)
	name = remoteWordBoundary.ReplaceAllString(name, `${1}_${2}`)
	name = strings.Trim(remoteSeparators.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if name == "" {
		return "", fmt.Errorf("MCP remote tool name must contain an ASCII letter or digit")
	}
	if name[0] < 'a' || name[0] > 'z' {
		name = "tool_" + name
	}
	return name, nil
}

// registerTool runs under the client's initialization lock.
func (c *Client) registerTool(ctx context.Context, serverName string, remote tool.InvokableTool) error {
	info, err := remote.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tool info for server %q: %w", serverName, err)
	}
	if info == nil || info.Name == "" {
		return fmt.Errorf("MCP server %q returned a tool without a name", serverName)
	}
	if err := config.ValidateMCPServerName(serverName); err != nil {
		return err
	}
	localName, err := remoteToolIdentifier(info.Name)
	if err != nil {
		return fmt.Errorf("MCP server %q: %w", serverName, err)
	}
	name := serverName + "__" + localName
	if err := config.ValidateMCPToolName(name); err != nil {
		return err
	}
	if _, exists := c.tools[name]; exists {
		return fmt.Errorf("MCP tool name %q from server %q collides with an existing tool", name, serverName)
	}
	canonical := *info
	canonical.Name = name
	c.tools[name] = &namedTool{InvokableTool: remote, info: canonical}
	c.toolServers[name] = serverName
	return nil
}

// discoverTools discovers tools from MCP servers
func (c *Client) discoverTools(ctx context.Context) error {
	for serverName, mcpClient := range c.clients {
		// Check if client is nil
		if mcpClient == nil {
			return fmt.Errorf("MCP client for server %s is not initialized", serverName)
		}

		// Initialize MCP client connection
		initRequest := mcpProtocol.InitializeRequest{
			Params: mcpProtocol.InitializeParams{
				ProtocolVersion: "2024-11-05",
				ClientInfo: mcpProtocol.Implementation{
					Name:    "oni3",
					Version: "1.0.0",
				},
			},
		}

		_, err := mcpClient.Initialize(ctx, initRequest)
		if err != nil {
			return fmt.Errorf("failed to initialize MCP client for server %s: %w", serverName, err)
		}

		// Use eino-ext's mcp package to get tools
		mcpTools, err := mcp.GetTools(ctx, &mcp.Config{Cli: mcpClient})
		if err != nil {
			return fmt.Errorf("failed to get tools from server %s: %w", serverName, err)
		}

		for _, mcpTool := range mcpTools {
			if invokableTool, ok := mcpTool.(tool.InvokableTool); ok {
				if err := c.registerTool(ctx, serverName, invokableTool); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
