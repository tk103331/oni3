package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
)

// Manager MCP manager, responsible for managing all MCP clients and tools
type Manager struct {
	mu     sync.RWMutex
	client *Client
	config *config.Config

	// done is closed once initialization finished, so front-ends that start
	// while the manager is still connecting can report the outcome afterwards
	// instead of waiting on the network inside their first frame.
	done     chan struct{}
	doneOnce sync.Once
}

// NewManager creates a new MCP manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		client: NewClient(cfg),
		config: cfg,
		done:   make(chan struct{}),
	}
}

// Done is closed when initialization finished, successfully or not.
func (m *Manager) Done() <-chan struct{} {
	return m.done
}

func (m *Manager) markDone() {
	m.doneOnce.Do(func() { close(m.done) })
}

// Initialize initializes the MCP manager
func (m *Manager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.markDone()

	// Initialize client. Unreachable servers are recorded per server and do
	// not abort the servers that work; see Client.Initialize.
	if err := m.client.Initialize(ctx); err != nil {
		return NewMCPError("manager_init", "", "", fmt.Errorf("MCP client initialization failed: %w", err))
	}

	return nil
}

// GetToolsForAgent gets MCP tools for specified agent
func (m *Manager) GetToolsForAgent(agentName string) ([]tool.InvokableTool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if manager is initialized
	if m.client == nil {
		return nil, NewMCPError("get_tools", "", "", ErrMCPNotInitialized)
	}

	// Get agent's MCP server list
	serverNames, err := GetAgentMCPServers(m.config, agentName)
	if err != nil {
		return nil, NewMCPError("get_tools", "", "", err)
	}

	// If agent has no MCP servers configured, return empty list
	if len(serverNames) == 0 {
		return []tool.InvokableTool{}, nil
	}

	// Get tools from specified servers
	mcpTools := m.client.GetToolsForServers(serverNames)

	// Convert to tool list
	tools := make([]tool.InvokableTool, 0, len(mcpTools))
	for _, mcpTool := range mcpTools {
		tools = append(tools, mcpTool)
	}

	return tools, nil
}

// GetAllTools gets all MCP tools
func (m *Manager) GetAllTools() map[string]tool.InvokableTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.client.GetTools()
}

// ToolCountsByServer returns the number of discovered tools per server. It
// reports an empty map when the manager has not been initialized.
func (m *Manager) ToolCountsByServer() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.client == nil {
		return map[string]int{}
	}
	return m.client.ToolCountsByServer()
}

// ServerErrors returns the servers that could not be initialized and why. An
// empty map means every configured server is usable this session.
func (m *Manager) ServerErrors() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.client == nil {
		return map[string]error{}
	}
	return m.client.ServerErrors()
}

// Close closes the MCP manager
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.client.Close()
}

// Global MCP manager instance
var (
	globalManager *Manager
	managerMu     sync.RWMutex
)

// InitializeGlobalManager initializes the global MCP manager
func InitializeGlobalManager(ctx context.Context, cfg *config.Config) error {
	managerMu.Lock()
	defer managerMu.Unlock()

	if globalManager != nil {
		// If already exists, close it first
		globalManager.Close()
	}

	globalManager = NewManager(cfg)
	return globalManager.Initialize(ctx)
}

// GetGlobalManager gets the global MCP manager
func GetGlobalManager() *Manager {
	managerMu.RLock()
	defer managerMu.RUnlock()
	return globalManager
}

// WaitGlobalManager waits until the global manager exists and its background
// initialization finished, then returns it. It returns nil when ctx ends
// first, which lets a front-end proceed with built-in tools only.
//
// Callers surface the result through their own interface: while a TUI owns the
// terminal, writing the outcome to stderr would paint over the frame.
func WaitGlobalManager(ctx context.Context) *Manager {
	for {
		managerMu.RLock()
		manager := globalManager
		managerMu.RUnlock()

		if manager != nil {
			select {
			case <-manager.Done():
				return manager
			case <-ctx.Done():
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// CloseGlobalManager closes the global MCP manager
func CloseGlobalManager() error {
	managerMu.Lock()
	defer managerMu.Unlock()

	if globalManager != nil {
		err := globalManager.Close()
		globalManager = nil
		return err
	}
	return nil
}
