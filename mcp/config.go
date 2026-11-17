package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tk103331/oni3/config"
)

// ValidateConfig validates a complete MCP configuration, including every
// server's own settings. It answers "is this configuration correct?".
//
// Runtime initialization deliberately does not use it: Client.Initialize
// validates per server so one broken or unreachable entry cannot block the
// servers that work. Use ValidateConfig for diagnostics and configuration
// editing, not as a gate in front of connecting.
func ValidateConfig(cfg *config.Config) error {
	if cfg == nil {
		return NewMCPError("validate", "", "", ErrInvalidConfig)
	}

	// Validate MCP server configurations
	for serverName, serverConfig := range cfg.MCPServers {
		if err := validateServerConfig(serverName, serverConfig); err != nil {
			return err
		}
	}

	// Validate agent's MCP server references
	return validateConfigReferences(cfg)
}

// validateConfigReferences checks the part of the configuration that is not
// tied to a single server. Per-server problems are detected when that server
// is created, so one broken entry cannot block the others.
func validateConfigReferences(cfg *config.Config) error {
	if cfg == nil {
		return NewMCPError("validate", "", "", ErrInvalidConfig)
	}

	for agentName, agentConfig := range cfg.Agents {
		for _, serverName := range agentConfig.MCPServers {
			if _, exists := cfg.MCPServers[serverName]; !exists {
				return NewMCPError("validate", serverName, "",
					fmt.Errorf("agent %s references non-existent MCP server: %s", agentName, serverName))
			}
		}
	}

	return nil
}

// validateServerConfig validates a single MCP server configuration
func validateServerConfig(serverName string, serverConfig config.MCPServer) error {
	if err := config.ValidateMCPServerName(serverName); err != nil {
		return NewMCPError("validate", serverName, "", err)
	}

	if err := config.ValidateMCPType(serverConfig.Type); err != nil {
		return NewMCPError("validate", serverName, "", err)
	}
	if serverConfig.Timeout < 0 {
		return NewMCPError("validate", serverName, "",
			fmt.Errorf("timeout must not be negative: %d", serverConfig.Timeout))
	}
	switch serverConfig.Type {
	case config.MCPTypeStdio:
		if serverConfig.Cmd == "" || serverConfig.URL != "" {
			return NewMCPError("validate", serverName, "",
				fmt.Errorf("stdio MCP server must specify cmd and cannot specify url"))
		}
	case config.MCPTypeSSE, config.MCPTypeStreamableHTTP:
		if serverConfig.URL == "" || serverConfig.Cmd != "" {
			return NewMCPError("validate", serverName, "",
				fmt.Errorf("%s MCP server must specify url and cannot specify cmd", serverConfig.Type))
		}
	}

	// Validate command path
	if serverConfig.Cmd != "" {
		if err := validateCommand(serverName, serverConfig.Cmd); err != nil {
			return err
		}
	}

	// Validate URL format
	if serverConfig.URL != "" {
		if err := validateURL(serverName, serverConfig.URL); err != nil {
			return err
		}
	}

	return nil
}

// validateCommand validates command path
func validateCommand(serverName, command string) error {
	// Parse command (may contain parameters)
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return NewMCPError("validate", serverName, "",
			fmt.Errorf("command cannot be empty"))
	}

	cmdPath := parts[0]

	// If relative path, check if exists
	if !filepath.IsAbs(cmdPath) {
		// Check if command exists in PATH
		if _, err := os.Stat(cmdPath); err != nil {
			// If not in current directory, try to find in PATH
			if _, pathErr := exec.LookPath(cmdPath); pathErr != nil {
				return NewMCPError("validate", serverName, "",
					fmt.Errorf("command does not exist: %s", cmdPath))
			}
		}
	} else {
		// Absolute path, directly check if file exists
		if _, err := os.Stat(cmdPath); err != nil {
			return NewMCPError("validate", serverName, "",
				fmt.Errorf("command file does not exist: %s", cmdPath))
		}
	}

	return nil
}

// validateURL validates URL format
func validateURL(serverName, url string) error {
	// Simple URL format validation
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://") {
		return NewMCPError("validate", serverName, "",
			fmt.Errorf("invalid URL format: %s, must start with http://, https://, ws:// or wss://", url))
	}

	return nil
}

// GetServerConfig gets configuration for specified server
func GetServerConfig(cfg *config.Config, serverName string) (*config.MCPServer, error) {
	if cfg == nil {
		return nil, NewMCPError("get_config", serverName, "", ErrInvalidConfig)
	}

	serverConfig, exists := cfg.MCPServers[serverName]
	if !exists {
		return nil, NewMCPError("get_config", serverName, "", ErrServerNotFound)
	}

	return &serverConfig, nil
}

// GetAgentMCPServers gets MCP server list for specified agent
func GetAgentMCPServers(cfg *config.Config, agentName string) ([]string, error) {
	if cfg == nil {
		return nil, NewMCPError("get_agent_servers", "", "", ErrInvalidConfig)
	}

	agentConfig, exists := cfg.Agents[agentName]
	if !exists {
		return nil, NewMCPError("get_agent_servers", "", "",
			fmt.Errorf("agent configuration does not exist: %s", agentName))
	}

	return agentConfig.MCPServers, nil
}
