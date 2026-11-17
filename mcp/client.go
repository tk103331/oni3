package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
)

// serverInitTimeout bounds one server's handshake and tool discovery so a
// server that never answers cannot stall startup or the other servers. A
// server can override it with its own `timeout` setting.
const serverInitTimeout = 60 * time.Second

// Client MCP client structure
type Client struct {
	mu           sync.RWMutex
	clients      map[string]*client.Client
	tools        map[string]tool.InvokableTool
	toolServers  map[string]string // canonical tool key -> exact owning server
	serverErrors map[string]error  // server -> why it is unusable this session
	config       *config.Config
}

// NewClient creates a new MCP client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		clients:      make(map[string]*client.Client),
		tools:        make(map[string]tool.InvokableTool),
		toolServers:  make(map[string]string),
		serverErrors: make(map[string]error),
		config:       cfg,
	}
}

// Initialize initializes the MCP client.
//
// Servers are set up one by one. A server that cannot be reached (a dead
// endpoint, a TLS handshake timeout, a missing command) is recorded in
// ServerErrors and skipped, so its failure never removes the tools of the
// servers that do work. An error is returned only when nothing is left to
// use, which keeps a fully broken configuration visible.
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Only configuration problems that are not tied to a single server stay
	// fatal: there is nothing usable to start from them.
	if err := validateConfigReferences(c.config); err != nil {
		return NewMCPError("initialize", "", "", fmt.Errorf("configuration validation failed: %w", err))
	}

	var failures []error
	for _, serverName := range sortedServerNames(c.config.MCPServers) {
		if err := ctx.Err(); err != nil {
			return NewMCPError("initialize", "", "", err)
		}
		serverConfig := c.config.MCPServers[serverName]
		if err := c.initializeServer(ctx, serverName, serverConfig); err != nil {
			c.serverErrors[serverName] = err
			failures = append(failures, err)
			logger.Warn("MCP", fmt.Sprintf("server %q unavailable: %v", serverName, err))
		}
	}

	if len(failures) > 0 && len(c.clients) == 0 {
		return NewMCPError("initialize", "", "", errors.Join(failures...))
	}
	return nil
}

// initializeServer connects one server and registers its tools. It runs under
// Initialize's write lock. The created client is only kept on success, so a
// half-connected server never shows up as usable.
func (c *Client) initializeServer(ctx context.Context, serverName string, serverConfig config.MCPServer) error {
	timeout := serverInitTimeout
	if serverConfig.Timeout > 0 {
		timeout = time.Duration(serverConfig.Timeout) * time.Second
	}
	serverCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	mcpClient, err := c.createMCPClient(serverCtx, serverName, serverConfig)
	if err != nil {
		return NewMCPError("initialize", serverName, "", fmt.Errorf("failed to create MCP client: %w", err))
	}
	if err := c.connectServer(serverCtx, serverName, mcpClient); err != nil {
		mcpClient.Close()
		return NewMCPError("initialize", serverName, "", err)
	}
	c.clients[serverName] = mcpClient
	return nil
}

// sortedServerNames returns the configured server names in a stable order so
// startup logs, notices and the resulting tool list do not shuffle run to run.
func sortedServerNames(servers map[string]config.MCPServer) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ServerErrors returns the servers that could not be initialized, mapped to
// the reason. An empty map means every configured server is usable.
func (c *Client) ServerErrors() map[string]error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	errs := make(map[string]error, len(c.serverErrors))
	for name, err := range c.serverErrors {
		errs[name] = err
	}
	return errs
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
