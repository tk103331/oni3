package config

// BuiltinProvider is a provider preset that ships with Oni3. Presets are only
// added to the in-memory configuration when the user's file does not define a
// provider with the same name, so the configuration file always wins.
type BuiltinProvider struct {
	Name           string // configuration key, also the /provider value
	Type           string // canonical provider type
	BaseURL        string
	Description    string
	KeyURL         string // where to create an API key
	APIKeyOptional bool   // local servers need no key
}

// BuiltinProviders returns the provider presets shown on the first run and in
// the /provider palette. The catalog mirrors the vendors commonly used with
// Oni3; OpenAI-compatible endpoints use the openai type so a single client can
// talk to them.
func BuiltinProviders() []BuiltinProvider {
	return []BuiltinProvider{
		{
			Name: "openai", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.openai.com/v1", Description: "OpenAI 官方",
			KeyURL: "https://platform.openai.com/api-keys",
		},
		{
			Name: "anthropic", Type: ProviderTypeAnthropic,
			BaseURL: "https://api.anthropic.com", Description: "Anthropic Claude 官方",
			KeyURL: "https://console.anthropic.com/settings/keys",
		},
		{
			Name: "gemini", Type: ProviderTypeOpenAI,
			BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Description: "Google Gemini（OpenAI 兼容）",
			KeyURL: "https://aistudio.google.com/apikey",
		},
		{
			Name: "deepseek", Type: ProviderTypeDeepSeek,
			BaseURL: "https://api.deepseek.com/v1", Description: "DeepSeek 官方",
			KeyURL: "https://platform.deepseek.com/api_keys",
		},
		{
			Name: "qwen", Type: ProviderTypeQwen,
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Description: "阿里云百炼 · 通义千问",
			KeyURL: "https://bailian.console.aliyun.com/?tab=model#/api-key",
		},
		{
			Name: "qwen-intl", Type: ProviderTypeQwen,
			BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", Description: "阿里云百炼国际站",
			KeyURL: "https://modelstudio.console.alibabacloud.com/?tab=playground#/api-key",
		},
		{
			Name: "kimi", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.moonshot.cn/v1", Description: "月之暗面 Kimi",
			KeyURL: "https://platform.moonshot.cn/console/api-keys",
		},
		{
			Name: "kimi-intl", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.moonshot.ai/v1", Description: "Kimi 国际站",
			KeyURL: "https://platform.moonshot.ai/console/api-keys",
		},
		{
			Name: "zhipu", Type: ProviderTypeOpenAI,
			BaseURL: "https://open.bigmodel.cn/api/paas/v4", Description: "智谱 GLM",
			KeyURL: "https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys",
		},
		{
			Name: "minimax", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.minimaxi.com/v1", Description: "MiniMax（中国站）",
			KeyURL: "https://platform.minimaxi.com/user-center/basic-information/interface-key",
		},
		{
			Name: "minimax-intl", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.minimax.io/v1", Description: "MiniMax（国际站）",
			KeyURL: "https://platform.minimax.io/user-center/basic-information/interface-key",
		},
		{
			Name: "ark", Type: ProviderTypeArk,
			BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Description: "火山方舟 · 豆包",
			KeyURL: "https://console.volcengine.com/ark",
		},
		{
			Name: "qianfan", Type: ProviderTypeQianfan,
			BaseURL: "https://qianfan.baidubce.com/v2", Description: "百度千帆",
			KeyURL: "https://console.bce.baidu.com/qianfan",
		},
		{
			Name: "siliconflow", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.siliconflow.cn/v1", Description: "SiliconFlow 硅基流动",
			KeyURL: "https://cloud.siliconflow.cn/account/ak",
		},
		{
			Name: "openrouter", Type: ProviderTypeOpenAI,
			BaseURL: "https://openrouter.ai/api/v1", Description: "OpenRouter 聚合网关",
			KeyURL: "https://openrouter.ai/keys",
		},
		{
			Name: "groq", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.groq.com/openai/v1", Description: "Groq 高速推理",
			KeyURL: "https://console.groq.com/keys",
		},
		{
			Name: "xai", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.x.ai/v1", Description: "xAI Grok",
			KeyURL: "https://console.x.ai",
		},
		{
			Name: "mistral", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.mistral.ai/v1", Description: "Mistral AI",
			KeyURL: "https://console.mistral.ai/api-keys",
		},
		{
			Name: "together", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.together.xyz/v1", Description: "Together AI",
			KeyURL: "https://api.together.ai/settings/api-keys",
		},
		{
			Name: "fireworks", Type: ProviderTypeOpenAI,
			BaseURL: "https://api.fireworks.ai/inference/v1", Description: "Fireworks AI",
			KeyURL: "https://app.fireworks.ai/settings/users/api-keys",
		},
		{
			Name: "modelscope", Type: ProviderTypeOpenAI,
			BaseURL: "https://api-inference.modelscope.cn/v1", Description: "ModelScope 魔搭",
			KeyURL: "https://modelscope.cn/my/myaccesstoken",
		},
		{
			Name: "magpie", Type: ProviderTypeOpenAI,
			BaseURL: "http://127.0.0.1:3425/v1", Description: "magpie 本地网关",
			APIKeyOptional: true,
		},
		{
			Name: "ollama", Type: ProviderTypeOllama,
			BaseURL: "http://localhost:11434", Description: "Ollama 本地模型",
			APIKeyOptional: true,
		},
		{
			Name: "lmstudio", Type: ProviderTypeOpenAI,
			BaseURL: "http://localhost:1234/v1", Description: "LM Studio 本地模型",
			APIKeyOptional: true,
		},
	}
}

// seedBuiltinProviders adds the presets the configuration does not define yet.
// They are memory-only: Oni3 never rewrites the user's configuration file, and
// an entry with the same name in the file always wins.
func (c *Config) seedBuiltinProviders() {
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	for _, preset := range BuiltinProviders() {
		if _, exists := c.Providers[preset.Name]; exists {
			continue
		}
		c.Providers[preset.Name] = Provider{
			Type:           preset.Type,
			BaseURL:        preset.BaseURL,
			Description:    preset.Description,
			KeyURL:         preset.KeyURL,
			APIKeyOptional: preset.APIKeyOptional,
		}
	}
}
