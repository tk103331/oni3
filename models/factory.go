package models

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/tk103331/oni3/config"
)

// Factory is used to create ChatModel for different providers
type Factory struct {
	cfg *config.Config
}

// NewFactory creates a new Factory
func NewFactory(cfg *config.Config) *Factory {
	return &Factory{cfg: cfg}
}

// CreateChatModel creates corresponding ChatModel based on model name
func (f *Factory) CreateChatModel(ctx context.Context, modelName string) (model.ToolCallingChatModel, error) {
	// Get model configuration
	modelCfg, ok := f.cfg.Models[modelName]
	if !ok {
		return nil, fmt.Errorf("model configuration does not exist: %s", modelName)
	}

	// Get provider configuration
	providerCfg, ok := f.cfg.Providers[modelCfg.Provider]
	if !ok {
		return nil, fmt.Errorf("provider configuration does not exist: %s", modelCfg.Provider)
	}

	// Use the same default and strict type validation as config and discovery,
	// including when callers construct Config directly rather than loading YAML.
	kind := providerCfg.EffectiveType()
	if err := config.ValidateProviderType(kind); err != nil {
		return nil, err
	}
	switch kind {
	case config.ProviderTypeOpenAI:
		return f.createOpenAIModel(ctx, &modelCfg, &providerCfg)
	case config.ProviderTypeAnthropic:
		return f.createAnthropicModel(ctx, &modelCfg, &providerCfg)
	case config.ProviderTypeGemini:
		return f.createGeminiModel(ctx, &modelCfg, &providerCfg)
	case config.ProviderTypeQwen:
		return f.createQwenModel(ctx, &modelCfg, &providerCfg)
	case config.ProviderTypeQianfan:
		return f.createQianfanModel(ctx, &modelCfg, &providerCfg)
	case config.ProviderTypeArk:
		return f.createArkModel(ctx, &modelCfg, &providerCfg)
	case config.ProviderTypeDeepSeek:
		return f.createDeepSeekModel(ctx, &modelCfg, &providerCfg)
	case config.ProviderTypeOllama:
		return f.createOllamaModel(ctx, &modelCfg, &providerCfg)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerCfg.Type)
	}
}
