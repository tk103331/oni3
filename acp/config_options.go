package acp

import (
	"context"
	"fmt"
	"sort"

	"github.com/coder/acp-go-sdk"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
)

// Session config options exposed to ACP clients: the model selector mirrors
// the TUI /model command and the skill selector mirrors /skill. Both apply
// per session and rebuild the eino agent for the next prompt.
const (
	configOptionModel = "model"
	configOptionSkill = "skill"
	// optionOff is the "no override" value of the skill selector.
	optionOff = "off"
)

// sessionConfigOptions renders the current options for one session.
func (s *Service) sessionConfigOptions(session *acpSession) []acp.SessionConfigOption {
	modelOptions := make(acp.SessionConfigSelectOptionsUngrouped, 0, len(s.cfg.Models))
	for _, name := range sortedModelNames(s.cfg) {
		modelOptions = append(modelOptions, acp.SessionConfigSelectOption{
			Name:  name,
			Value: acp.SessionConfigValueId(name),
		})
	}
	currentModel := session.modelOverride
	if currentModel == "" {
		currentModel = s.currentModelName()
	}

	skillOptions := make(acp.SessionConfigSelectOptionsUngrouped, 0, len(s.cfg.Skills)+1)
	skillOptions = append(skillOptions, acp.SessionConfigSelectOption{
		Name:        optionOff,
		Value:       acp.SessionConfigValueId(optionOff),
		Description: strPtr("不启用任何 skill"),
	})
	for _, name := range sortedKeys(s.cfg.Skills) {
		skill := s.cfg.Skills[name]
		opt := acp.SessionConfigSelectOption{
			Name:  name,
			Value: acp.SessionConfigValueId(name),
		}
		if skill.Description != "" {
			opt.Description = strPtr(skill.Description)
		}
		skillOptions = append(skillOptions, opt)
	}
	currentSkill := session.skillOverride
	if currentSkill == "" {
		currentSkill = optionOff
	}

	return []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{
			Type:         "select",
			Id:           acp.SessionConfigId(configOptionModel),
			Name:         "模型",
			CurrentValue: acp.SessionConfigValueId(currentModel),
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &modelOptions},
		}},
		{Select: &acp.SessionConfigOptionSelect{
			Type:         "select",
			Id:           acp.SessionConfigId(configOptionSkill),
			Name:         "技能",
			CurrentValue: acp.SessionConfigValueId(currentSkill),
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &skillOptions},
		}},
	}
}

// SetSessionConfigOption applies a model or skill switch and rebuilds the
// agent of that session, mirroring the TUI /model and /skill commands.
func (s *Service) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	var configID, value, sid string
	switch {
	case params.ValueId != nil:
		configID = string(params.ValueId.ConfigId)
		value = string(params.ValueId.Value)
		sid = string(params.ValueId.SessionId)
	case params.Boolean != nil:
		// oni3 has no boolean session options yet.
		return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{
			"error": fmt.Sprintf("unknown config option: %s", string(params.Boolean.ConfigId)),
		})
	default:
		return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{
			"error": "missing config option payload",
		})
	}

	s.mu.Lock()
	session, ok := s.sessions[sid]
	if !ok {
		s.mu.Unlock()
		return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{
			"error": fmt.Sprintf("unknown session: %s", sid),
		})
	}
	s.mu.Unlock()

	switch configID {
	case configOptionModel:
		if _, exists := s.cfg.Models[value]; !exists {
			return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{
				"error": fmt.Sprintf("模型不存在: %s", value),
			})
		}
		if err := s.rebuildSessionAgent(session, value, session.skillOverride); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("switch model failed: %w", err)
		}
		session.modelOverride = value
		session.store.setModel(value)
		logger.Info("ACP", fmt.Sprintf("model switched: session=%s model=%s", sid, value))
	case configOptionSkill:
		if value != optionOff {
			if _, exists := s.cfg.Skills[value]; !exists {
				return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{
					"error": fmt.Sprintf("skill 不存在: %s", value),
				})
			}
		}
		skill := ""
		if value != optionOff {
			skill = value
		}
		if err := s.rebuildSessionAgent(session, session.modelOverride, skill); err != nil {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("switch skill failed: %w", err)
		}
		session.skillOverride = skill
		session.store.setSkill(skill)
		logger.Info("ACP", fmt.Sprintf("skill switched: session=%s skill=%s", sid, value))
	default:
		return acp.SetSessionConfigOptionResponse{}, acp.NewInvalidParams(map[string]any{
			"error": fmt.Sprintf("unknown config option: %s", configID),
		})
	}

	// Acknowledge with the full option state so the client UI stays in sync.
	s.mu.Lock()
	out := s.sessionConfigOptions(session)
	s.mu.Unlock()
	return acp.SetSessionConfigOptionResponse{ConfigOptions: out}, nil
}

// rebuildSessionAgent recreates the eino agent with the given overrides.
// It swaps the agent only on success, so a failed rebuild keeps the old one.
func (s *Service) rebuildSessionAgent(session *acpSession, model, skill string) error {
	factory := agent.NewFactory(s.cfg)
	if model != "" {
		factory = factory.WithModel(model)
	}
	if skill != "" {
		factory = factory.WithSkill(skill)
	}
	newAgent, err := factory.CreateAgent(s.agentName)
	if err != nil {
		return err
	}
	s.mu.Lock()
	session.agent = newAgent
	s.mu.Unlock()
	return nil
}

// sortedModelNames lists the configured models alphabetically.
func sortedModelNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Models))
	for name := range cfg.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sortedKeys lists the keys of a map alphabetically.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
