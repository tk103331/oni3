package models

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qianfan"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/tk103331/oni3/config"
	"google.golang.org/genai"
)

// createOpenAIModel creates OpenAI model
func (f *Factory) createOpenAIModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &openai.ChatModelConfig{
		Model:   modelCfg.Model,
		BaseURL: providerCfg.BaseURL,
		APIKey:  providerCfg.APIKey,
	}
	cfg.HTTPClient = userAgentClient()

	if modelCfg.MaxTokens > 0 {
		cfg.MaxTokens = &modelCfg.MaxTokens
	}
	if modelCfg.Temperature > 0 {
		temp := float32(modelCfg.Temperature)
		cfg.Temperature = &temp
	}
	if modelCfg.TopP > 0 {
		topP := float32(modelCfg.TopP)
		cfg.TopP = &topP
	}

	return openai.NewChatModel(ctx, cfg)
}

// createAnthropicModel creates an Anthropic model using Eino's claude SDK.
func (f *Factory) createAnthropicModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &claude.Config{
		Model:   modelCfg.Model,
		BaseURL: &(providerCfg.BaseURL),
		APIKey:  providerCfg.APIKey,
	}
	cfg.HTTPClient = userAgentClient()
	if modelCfg.MaxTokens > 0 {
		cfg.MaxTokens = modelCfg.MaxTokens
	}
	if modelCfg.Temperature > 0 {
		temp := float32(modelCfg.Temperature)
		cfg.Temperature = &temp
	}
	if modelCfg.TopP > 0 {
		topP := float32(modelCfg.TopP)
		cfg.TopP = &topP
	}

	return claude.NewChatModel(ctx, cfg)
}

// createGeminiModel creates Gemini model
func (f *Factory) createGeminiModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &gemini.Config{
		Model: modelCfg.Model,
	}

	// The Gemini client is required. It is created here so the configured
	// User-Agent (if any) is applied to every request.
	client, err := f.createGeminiClient(ctx, providerCfg)
	if err != nil {
		return nil, err
	}
	cfg.Client = client

	if modelCfg.MaxTokens > 0 {
		cfg.MaxTokens = &modelCfg.MaxTokens
	}
	if modelCfg.Temperature > 0 {
		temp := float32(modelCfg.Temperature)
		cfg.Temperature = &temp
	}
	if modelCfg.TopP > 0 {
		topP := float32(modelCfg.TopP)
		cfg.TopP = &topP
	}

	return gemini.NewChatModel(ctx, cfg)
}

// createGeminiClient builds the Google GenAI client for a Gemini provider. The
// provider API key is used when set; otherwise the client falls back to the
// environment (GOOGLE_API_KEY / GEMINI_API_KEY).
func (f *Factory) createGeminiClient(ctx context.Context, providerCfg *config.Provider) (*genai.Client, error) {
	clientCfg := &genai.ClientConfig{
		Backend:    genai.BackendGeminiAPI,
		APIKey:     providerCfg.APIKey,
		HTTPClient: userAgentClient(),
	}
	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		// The SDK error embeds the whole client config, including the API key.
		return nil, fmt.Errorf("cannot create gemini client (check the provider API key)")
	}
	return client, nil
}

// createQwenModel creates Qwen model
func (f *Factory) createQwenModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &qwen.ChatModelConfig{
		Model:   modelCfg.Model,
		BaseURL: providerCfg.BaseURL,
		APIKey:  providerCfg.APIKey,
	}
	cfg.HTTPClient = userAgentClient()

	if modelCfg.MaxTokens > 0 {
		cfg.MaxTokens = &modelCfg.MaxTokens
	}
	if modelCfg.Temperature > 0 {
		temp := float32(modelCfg.Temperature)
		cfg.Temperature = &temp
	}
	if modelCfg.TopP > 0 {
		topP := float32(modelCfg.TopP)
		cfg.TopP = &topP
	}

	return qwen.NewChatModel(ctx, cfg)
}

// createQianfanModel creates Qianfan model
func (f *Factory) createQianfanModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &qianfan.ChatModelConfig{
		Model: modelCfg.Model,
	}

	// Qianfan configuration may need adjustment based on actual API
	// Basic configuration provided here, may need specific configuration based on requirements

	return qianfan.NewChatModel(ctx, cfg)
}

// createArkModel creates Ark model
func (f *Factory) createArkModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &ark.ChatModelConfig{
		Model:   modelCfg.Model,
		BaseURL: providerCfg.BaseURL,
		APIKey:  providerCfg.APIKey,
	}
	cfg.HTTPClient = userAgentClient()

	if modelCfg.MaxTokens > 0 {
		cfg.MaxTokens = &modelCfg.MaxTokens
	}
	if modelCfg.Temperature > 0 {
		temp := float32(modelCfg.Temperature)
		cfg.Temperature = &temp
	}
	if modelCfg.TopP > 0 {
		topP := float32(modelCfg.TopP)
		cfg.TopP = &topP
	}

	return ark.NewChatModel(ctx, cfg)
}

// createDeepSeekModel creates DeepSeek model
func (f *Factory) createDeepSeekModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &deepseek.ChatModelConfig{
		Model:   modelCfg.Model,
		BaseURL: providerCfg.BaseURL,
		APIKey:  providerCfg.APIKey,
	}
	cfg.HTTPClient = userAgentClient()

	if modelCfg.MaxTokens > 0 {
		cfg.MaxTokens = modelCfg.MaxTokens
	}
	if modelCfg.Temperature > 0 {
		temp := float32(modelCfg.Temperature)
		cfg.Temperature = temp
	}
	if modelCfg.TopP > 0 {
		topP := float32(modelCfg.TopP)
		cfg.TopP = topP
	}

	return deepseek.NewChatModel(ctx, cfg)
}

// createOllamaModel creates Ollama model
func (f *Factory) createOllamaModel(ctx context.Context, modelCfg *config.Model, providerCfg *config.Provider) (model.ToolCallingChatModel, error) {
	cfg := &ollama.ChatModelConfig{
		Model:   modelCfg.Model,
		BaseURL: providerCfg.BaseURL,
	}
	cfg.HTTPClient = userAgentClient()

	// Ollama configuration is set through Options field
	// Simplified handling here, may need to configure Options based on specific requirements

	return ollama.NewChatModel(ctx, cfg)
}
