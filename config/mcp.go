package config

import (
	"fmt"
	"regexp"
)

// MCP transport types use exact lowercase identifiers with snake_case for
// multiple words. No aliases, case folding, whitespace trimming, or default apply.
const (
	MCPTypeStdio          = "stdio"
	MCPTypeSSE            = "sse"
	MCPTypeStreamableHTTP = "streamable_http"
)

// MCPToolNameMaxLength bounds runtime names including the server__ prefix.
const MCPToolNameMaxLength = 64

var canonicalMCPName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidateMCPServerName reserves room for "__" and at least one remote name
// character. Names are used verbatim; case and punctuation are never normalized.
func ValidateMCPServerName(name string) error {
	if !canonicalMCPName.MatchString(name) || len(name) > MCPToolNameMaxLength-3 {
		return fmt.Errorf("MCP server name %q must start with a lowercase letter, contain only lowercase letters, digits, or underscores, and be at most %d characters", name, MCPToolNameMaxLength-3)
	}
	return nil
}

// ValidateMCPToolName checks the actual runtime name after converting the remote
// name to a local identifier and adding the server namespace.
func ValidateMCPToolName(name string) error {
	if !canonicalMCPName.MatchString(name) || len(name) > MCPToolNameMaxLength {
		return fmt.Errorf("MCP tool name %q must start with a lowercase letter, contain only lowercase letters, digits, or underscores, and be at most %d characters", name, MCPToolNameMaxLength)
	}
	return nil
}

// ValidateMCPType accepts only the canonical MCP transport type constants.
func ValidateMCPType(kind string) error {
	switch kind {
	case MCPTypeStdio, MCPTypeSSE, MCPTypeStreamableHTTP:
		return nil
	}
	return fmt.Errorf("unsupported MCP server type %q", kind)
}
