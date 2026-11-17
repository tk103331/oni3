package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
)

// Names of the three MCP tools exposed by Oni3.
const (
	CallAgentToolName  = "call_agent"
	ListAgentsToolName = "list_agents"
	ListModelsToolName = "list_models"
)

// AgentTools builds the standard Oni3 MCP surface used by both `oni3 mcp` and
// the HTTP endpoint embedded in `oni3 web`.
func AgentTools(cfg *config.Config) ([]tool.InvokableTool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("global configuration not initialized")
	}
	return []tool.InvokableTool{
		&callAgentTool{
			cfg: cfg,
			info: &schema.ToolInfo{
				Name: CallAgentToolName,
				Desc: "Run a configured Oni3 agent. Use list_agents to discover agent names " +
					"and list_models to discover model aliases.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"agent": {
						Type:     schema.String,
						Desc:     "Configured agent name, as returned by list_agents.",
						Required: true,
					},
					"prompt": {
						Type:     schema.String,
						Desc:     "The task for the agent.",
						Required: true,
					},
					"model": {
						Type: schema.String,
						Desc: "Optional model alias for this run. Use list_models to discover aliases; " +
							"omit it to use the agent's default model.",
					},
				}),
			},
		},
		&listAgentsTool{
			cfg: cfg,
			info: &schema.ToolInfo{
				Name: ListAgentsToolName,
				Desc: "List configured agent names, descriptions and default model aliases. " +
					"Pass a name as the agent argument of call_agent.",
			},
		},
		&modelListTool{
			cfg: cfg,
			info: &schema.ToolInfo{
				Name: ListModelsToolName,
				Desc: "List configured model aliases. Pass an alias as the optional model " +
					"argument of call_agent; omit it to use the agent's default model.",
			},
		},
	}, nil
}

// callAgentTool runs one configured agent.
type callAgentTool struct {
	cfg  *config.Config
	info *schema.ToolInfo
}

func (t *callAgentTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *callAgentTool) InvokableRun(ctx context.Context, arguments string, _ ...tool.Option) (string, error) {
	var in struct {
		Agent  string `json:"agent"`
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal([]byte(arguments), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	agentName := strings.TrimSpace(in.Agent)
	if agentName == "" {
		return "", fmt.Errorf("agent is required")
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	return RunAgent(ctx, t.cfg, agentName, strings.TrimSpace(in.Model), prompt)
}

// listAgentsTool reports the names accepted by call_agent.
type listAgentsTool struct {
	cfg  *config.Config
	info *schema.ToolInfo
}

func (t *listAgentsTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *listAgentsTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	type agentEntry struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		DefaultModel string `json:"default_model,omitempty"`
	}
	names := sortedAgentNames(t.cfg)
	agents := make([]agentEntry, 0, len(names))
	for _, name := range names {
		agentCfg := t.cfg.Agents[name]
		description := strings.TrimSpace(agentCfg.Description)
		if description == "" {
			description = t.cfg.AgentDisplayName(name)
		}
		model := strings.TrimSpace(agentCfg.Model)
		if model == "" {
			model = t.cfg.DefaultModelName()
		}
		agents = append(agents, agentEntry{Name: name, Description: description, DefaultModel: model})
	}
	payload := struct {
		Agents []agentEntry `json:"agents"`
	}{Agents: agents}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("cannot encode agent list: %w", err)
	}
	return string(data), nil
}

// modelListTool reports the model aliases accepted by call_agent.
type modelListTool struct {
	cfg  *config.Config
	info *schema.ToolInfo
}

func (t *modelListTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *modelListTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	payload := struct {
		DefaultModel string   `json:"default_model"`
		Models       []string `json:"models"`
	}{
		DefaultModel: t.cfg.DefaultModelName(),
		Models:       sortedModelNames(t.cfg),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("cannot encode model list: %w", err)
	}
	return string(data), nil
}

// RunAgent executes one agent turn with an optional model override.
func RunAgent(ctx context.Context, cfg *config.Config, agentName, model, prompt string) (string, error) {
	if _, ok := cfg.Agents[agentName]; !ok {
		return "", fmt.Errorf("agent %q does not exist; available agents: %s",
			agentName, strings.Join(sortedAgentNames(cfg), ", "))
	}
	if model != "" {
		if _, ok := cfg.Models[model]; !ok {
			return "", fmt.Errorf("model %q does not exist; available models: %s",
				model, strings.Join(sortedModelNames(cfg), ", "))
		}
	}

	factory := agent.NewFactory(cfg)
	if model != "" {
		factory = factory.WithModel(model)
	}
	instance, err := factory.CreateAgent(agentName)
	if err != nil {
		return "", err
	}
	output, err := instance.Chat(ctx, nil, prompt)
	if err != nil {
		return "", err
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return "(agent returned no output)", nil
	}
	return output, nil
}

func sortedAgentNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedModelNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Models))
	for name := range cfg.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
