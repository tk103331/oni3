package agent

import (
	"fmt"

	"github.com/tk103331/oni3/config"
)

// Factory is used to create Agent instances
type Factory struct {
	cfg *config.Config
	// modelOverride pins the model of every agent this factory creates (an
	// empty string keeps each agent's configured model).
	modelOverride string
	// skill extends the created agent's system prompt and tool list.
	skill string
}

// NewFactory creates a new Factory
func NewFactory(cfg *config.Config) *Factory {
	return &Factory{cfg: cfg}
}

// WithModel pins the model used by agents created afterwards. The ACP
// service uses it to honor session/set_config_option model switches.
func (f *Factory) WithModel(model string) *Factory {
	f.modelOverride = model
	return f
}

// WithSkill activates a configured skill for created agents.
func (f *Factory) WithSkill(skill string) *Factory {
	f.skill = skill
	return f
}

// CreateAgent creates Agent based on name
func (f *Factory) CreateAgent(name string) (Agent, error) {
	// Get Agent configuration
	agentCfg, ok := f.cfg.Agents[name]
	if !ok {
		return nil, fmt.Errorf("Agent configuration does not exist: %s", name)
	}

	if f.modelOverride != "" {
		agentCfg.Model = f.modelOverride
	}
	if f.skill != "" {
		if sk, ok := f.cfg.Skills[f.skill]; ok {
			ApplySkill(&agentCfg, sk)
		}
	}

	// Create ReactAgent
	agent := NewReactAgent(name, &agentCfg)
	return agent, nil
}
