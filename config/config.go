package config

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tk103331/oni3/plugins"
	"gopkg.in/yaml.v3"
)

// Private global variable to store configuration
var globalConfig *Config

// DefaultMaxSteps is the ReAct loop cap used when an agent does not set
// max_steps. Eino's own default (12) is far too low once tools are involved.
const DefaultMaxSteps = 1000

// Config represents the configuration for Oni3
type Config struct {
	Agents        map[string]Agent          `yaml:"agents,omitempty"`
	Providers     map[string]Provider       `yaml:"providers,omitempty"`
	Models        map[string]Model          `yaml:"models,omitempty"`
	DefaultModel  string                    `yaml:"default_model,omitempty"`
	MCPServers    map[string]MCPServer      `yaml:"mcp_servers,omitempty"`
	A2AAgents     map[string]A2AAgent       `yaml:"a2a_agents,omitempty"`
	Tools         map[string]Tool           `yaml:"tools,omitempty"`
	Skills        map[string]Skill          `yaml:"skills,omitempty"`
	Settings      Settings                  `yaml:"settings,omitempty"`
	Plugins       map[string]plugins.Config `yaml:"plugins,omitempty"`
	PluginThemes  map[string]plugins.Theme  `yaml:"-"`
	LoadedPlugins []*plugins.Plugin         `yaml:"-"`
	SourcePath    string                    `yaml:"-"`

	providerOverrides map[string]yaml.Node
}

// Agent represents AI agent configuration
type Agent struct {
	// Name is the label shown in the interface. It is optional: when it is
	// empty the configuration key is used as the label.
	Name        string   `yaml:"name,omitempty"`
	Description string   `yaml:"description,omitempty"`
	System      string   `yaml:"system"`
	Model       string   `yaml:"model"`
	Tools       []string `yaml:"tools,omitempty"`
	MCPServers  []string `yaml:"mcp_servers,omitempty"`
	// MaxSteps caps the ReAct loop (model + tool rounds). It defaults to
	// DefaultMaxSteps when unset.
	MaxSteps int `yaml:"max_steps,omitempty"`
}

// AgentDisplayName returns the label of the agent with the given
// configuration key. It falls back to the key itself when no name is set.
func (c *Config) AgentDisplayName(key string) string {
	if c == nil {
		return key
	}
	agent, ok := c.Agents[key]
	if !ok {
		return key
	}
	if name := strings.TrimSpace(agent.Name); name != "" {
		return name
	}
	return key
}

// Skill represents an optional capability preset that can be switched on at
// runtime (via the /skill command in the TUI). A skill extends the active
// agent's system prompt and can add extra tools.
type Skill struct {
	Description string   `yaml:"description,omitempty"`
	System      string   `yaml:"system,omitempty"`
	Tools       []string `yaml:"tools,omitempty"`
}

// Provider represents AI provider configuration
type Provider struct {
	// Type is an exact canonical identifier (see ProviderType constants).
	// Only the empty string defaults to openai; aliases and case variants fail.
	Type        string `yaml:"type,omitempty"`
	BaseURL     string `yaml:"base_url,omitempty"`
	APIKey      string `yaml:"api_key,omitempty"`
	Description string `yaml:"description,omitempty"`
	// KeyURL points at the page that issues an API key for this provider. It is
	// shown by the API key prompt; built-in presets fill it in.
	KeyURL string `yaml:"key_url,omitempty"`
	// ModelsURL may be absolute or relative to BaseURL. A relative path such as
	// "models" appends to its base path; "/models" replaces the base path.
	ModelsURL      string            `yaml:"models_url,omitempty"`
	ModelsPath     string            `yaml:"models_path,omitempty"`
	ModelIDPath    string            `yaml:"model_id_path,omitempty"`
	ModelNamePath  string            `yaml:"model_name_path,omitempty"`
	ModelHeaders   map[string]string `yaml:"model_headers,omitempty"`
	APIKeyOptional bool              `yaml:"api_key_optional,omitempty"`
}

// Model represents AI model configuration
type Model struct {
	Provider    string  `yaml:"provider"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens,omitempty"`
	Temperature float64 `yaml:"temperature,omitempty"`
	TopP        float64 `yaml:"top_p,omitempty"`
	TopK        int     `yaml:"top_k,omitempty"`
}

// MCPServer represents MCP server configuration
type MCPServer struct {
	// Type must be exactly stdio, sse, or streamable_http; it has no default.
	Type string `yaml:"type"`
	// Description is an optional label shown in the interface (e.g. the /mcp
	// palette). When it is empty a summary of the endpoint is shown instead.
	Description string `yaml:"description,omitempty"`
	// for stdio
	Cmd  string            `yaml:"cmd,omitempty"`
	Args []string          `yaml:"args,omitempty"`
	Env  map[string]string `yaml:"env,omitempty"`
	// for sse & streamable_http
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// A2AAgent describes a remote agent reached over the A2A protocol.
type A2AAgent struct {
	// CardURL points at the remote /.well-known/agent-card.json document.
	CardURL string `yaml:"card_url"`
	// Description is shown to the model when choosing a remote agent.
	Description string `yaml:"description,omitempty"`
	// Token is sent as a Bearer token when non-empty.
	Token string `yaml:"token,omitempty"`
	// TokenEnv names an environment variable holding the Bearer token. It is
	// read when the remote agent is first used.
	TokenEnv string `yaml:"token_env,omitempty"`
}

// Tool represents tool configuration
type Tool struct {
	Type        string           `yaml:"type"`
	Description string           `yaml:"description,omitempty"`
	Config      map[string]Value `yaml:"config,omitempty"`
	Parameters  []ToolParameter  `yaml:"parameters,omitempty"`
	PluginTool  *plugins.Tool    `yaml:"-"`
}

// ToolParameter represents tool parameter configuration.
type ToolParameter struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
}

// Settings global settings
type Settings struct {
	Langfuse *LangfuseConfig `yaml:"langfuse,omitempty"`
	Approval *ApprovalConfig `yaml:"approval,omitempty"`
}

// ApprovalConfig controls which tool calls must be confirmed by the user in
// interactive sessions (agent/chat TUI).
//
// Mode is one of:
//   - off: never ask
//   - dangerous: ask only for calls a tool flags as mutating (default)
//   - all: ask before every tool call
//
// Allow lists tool names that never need approval; Always lists tools that
// always do (useful for MCP tools, which cannot be classified).
type ApprovalConfig struct {
	Mode   string   `yaml:"mode,omitempty"`
	Allow  []string `yaml:"allow,omitempty"`
	Always []string `yaml:"always,omitempty"`
}

// DefaultModelName returns the effective default model: `default_model` when it
// names a configured model, otherwise the alphabetically first configured
// model. It returns "" when no model is configured.
func (c *Config) DefaultModelName() string {
	if c == nil {
		return ""
	}
	if c.DefaultModel != "" {
		if _, ok := c.Models[c.DefaultModel]; ok {
			return c.DefaultModel
		}
	}
	if len(c.Models) == 0 {
		return ""
	}
	names := make([]string, 0, len(c.Models))
	for name := range c.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0]
}

// AgentUsesDefaultModel reports whether the agent follows the effective default
// model (its model is unset or equal to it). Agents that pin their own model in
// the config keep it, so they are not "default model" agents.
func (c *Config) AgentUsesDefaultModel(agentName string) bool {
	if c == nil {
		return false
	}
	ac, ok := c.Agents[agentName]
	if !ok {
		return false
	}
	return ac.Model == "" || ac.Model == c.DefaultModelName()
}

// LoadConfig loads configuration from file and saves to global variable
func LoadConfig(configPath string) (*Config, error) {
	// Check if configuration file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("configuration file does not exist: %s", configPath)
	}

	// Read configuration file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	// Parse YAML
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to parse configuration file: %w", err)
	}

	cfg.SourcePath, err = filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve configuration path: %w", err)
	}
	// Preserve field presence, including explicit false/empty overrides, when
	// merging YAML settings onto a provider exported by a plugin.
	var raw struct {
		Providers map[string]yaml.Node `yaml:"providers"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse provider settings: %w", err)
	}
	cfg.providerOverrides = raw.Providers

	if err := cfg.normalizeProviderTypes(); err != nil {
		return nil, err
	}
	// Common providers are always selectable from /provider. Seeding happens
	// before plugins and provider state load so saved keys and imported models
	// attach to them; an entry in the configuration file always wins.
	cfg.seedBuiltinProviders()
	for name, server := range cfg.MCPServers {
		if err := ValidateMCPServerName(name); err != nil {
			return nil, err
		}
		if err := ValidateMCPType(server.Type); err != nil {
			return nil, fmt.Errorf("MCP server %q: %w", name, err)
		}
	}
	for name, remote := range cfg.A2AAgents {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("A2A agent name must not be empty")
		}
		if strings.TrimSpace(remote.CardURL) == "" {
			return nil, fmt.Errorf("A2A agent %q: card_url is required", name)
		}
		parsed, err := url.Parse(remote.CardURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("A2A agent %q: card_url must be an http(s) URL", name)
		}
	}

	// Merge skills defined as markdown files (user and project directories).
	loadSkillsFromDirs(&cfg)

	if err := cfg.loadPlugins(context.Background(), configPath); err != nil {
		return nil, err
	}

	// Publish only after every plugin has loaded successfully.
	globalConfig = &cfg

	return &cfg, nil
}

// GetConfig gets global configuration
func GetConfig() *Config {
	return globalConfig
}
