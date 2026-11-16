// Package mcpserver exposes Oni3 agent tools through the Model Context
// Protocol. It is the server counterpart of the mcp package, which connects
// Oni3 to external MCP servers.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tk103331/oni3/agent"
)

// Options configures one MCP server instance.
type Options struct {
	Name         string
	Version      string
	Instructions string
	Tools        []tool.InvokableTool
}

// New builds an MCP server that lists and invokes the supplied Oni3 tools.
func New(opts Options) (*server.MCPServer, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return nil, fmt.Errorf("MCP server name is required")
	}
	if strings.TrimSpace(opts.Version) == "" {
		return nil, fmt.Errorf("MCP server version is required")
	}

	options := []server.ServerOption{server.WithToolCapabilities(false)}
	if strings.TrimSpace(opts.Instructions) != "" {
		options = append(options, server.WithInstructions(opts.Instructions))
	}
	srv := server.NewMCPServer(opts.Name, opts.Version, options...)

	for _, instance := range opts.Tools {
		if err := addTool(srv, instance); err != nil {
			return nil, err
		}
	}
	return srv, nil
}

// addTool registers one Oni3 tool with its JSON Schema and handler.
func addTool(srv *server.MCPServer, instance tool.InvokableTool) error {
	if instance == nil {
		return fmt.Errorf("cannot expose a nil tool")
	}
	info, err := instance.Info(context.Background())
	if err != nil {
		return fmt.Errorf("cannot read tool metadata: %w", err)
	}
	if info == nil || strings.TrimSpace(info.Name) == "" {
		return fmt.Errorf("tool must have a nonempty runtime name")
	}

	description := strings.TrimSpace(info.Desc)
	if description == "" {
		description = "Oni3 tool: " + info.Name
	}
	inputSchema, err := toolInputSchema(info)
	if err != nil {
		return fmt.Errorf("tool %q: %w", info.Name, err)
	}

	srv.AddTool(
		mcp.NewToolWithRawSchema(info.Name, description, inputSchema),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			arguments := []byte("{}")
			if values := request.GetArguments(); values != nil {
				var err error
				arguments, err = json.Marshal(values)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("invalid tool arguments: %v", err)), nil
				}
			}
			result, err := instance.InvokableRun(ctx, string(arguments))
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return mcp.NewToolResultError(err.Error()), nil
			}
			if agent.IsToolError(result) {
				return mcp.NewToolResultError(strings.TrimSpace(
					strings.TrimPrefix(strings.TrimSpace(result), agent.ToolErrorPrefix))), nil
			}
			return mcp.NewToolResultText(result), nil
		},
	)
	return nil
}

// toolInputSchema converts an Eino tool schema to the raw JSON Schema that MCP
// clients expect. Tools without parameters still get an object schema.
func toolInputSchema(info *schema.ToolInfo) (json.RawMessage, error) {
	empty := json.RawMessage(`{"type":"object","properties":{}}`)
	if info == nil || info.ParamsOneOf == nil {
		return empty, nil
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("cannot build input schema: %w", err)
	}
	if js == nil {
		return empty, nil
	}
	js.Version = ""
	data, err := json.Marshal(js)
	if err != nil {
		return nil, fmt.Errorf("cannot encode input schema: %w", err)
	}
	return data, nil
}
