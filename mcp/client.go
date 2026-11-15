package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/tk103331/oni3/config"
)

// Client MCP client structure
type Client struct {
	mu          sync.RWMutex
	clients     map[string]*client.Client
	tools       map[string]tool.InvokableTool
	toolServers map[string]string // canonical tool key -> exact owning server
	config      *config.Config
}

// NewClient creates a new MCP client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		clients:     make(map[string]*client.Client),
		tools:       make(map[string]tool.InvokableTool),
		toolServers: make(map[string]string),
		config:      cfg,
	}
}

// Initialize initializes the MCP client
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate configuration
	if err := ValidateConfig(c.config); err != nil {
		return NewMCPError("initialize", "", "", fmt.Errorf("configuration validation failed: %w", err))
	}

	// Create clients for each configured MCP server
	for serverName, serverConfig := range c.config.MCPServers {
		client, err := c.createMCPClient(ctx, serverName, serverConfig)
		if err != nil {
			return NewMCPError("initialize", serverName, "", fmt.Errorf("failed to create MCP client: %w", err))
		}

		c.clients[serverName] = client
	}

	// Discover and register all tools
	if err := c.discoverTools(ctx); err != nil {
		return NewMCPError("initialize", "", "", fmt.Errorf("failed to discover MCP tools: %w", err))
	}

	return nil
}

// GetTools gets all available MCP tools
func (c *Client) GetTools() map[string]tool.InvokableTool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tools := make(map[string]tool.InvokableTool)
	for name, tool := range c.tools {
		tools[name] = tool
	}
	return tools
}

// GetToolsForServers gets tools from specified MCP servers
func (c *Client) GetToolsForServers(serverNames []string) map[string]tool.InvokableTool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	requested := make(map[string]bool, len(serverNames))
	for _, serverName := range serverNames {
		requested[serverName] = true
	}
	tools := make(map[string]tool.InvokableTool)
	for name, t := range c.tools {
		if owner, ok := c.toolServers[name]; ok && requested[owner] {
			tools[name] = t
		}
	}
	return tools
}

// ToolCountsByServer returns the number of discovered tools per server.
func (c *Client) ToolCountsByServer() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	counts := make(map[string]int)
	for _, owner := range c.toolServers {
		counts[owner]++
	}
	return counts
}

// Close closes all MCP client connections
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error
	for name, client := range c.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close MCP client %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors occurred while closing MCP clients: %v", errs)
	}

	return nil
}
