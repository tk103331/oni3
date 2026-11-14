package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Canonical provider types are exact lowercase identifiers. Aliases, case
// variants, and surrounding whitespace are rejected. Supported types are openai,
// anthropic, gemini, qwen, qianfan, ark, deepseek, and ollama. User-defined
// provider names and remote model IDs are independent of these identifiers.
const (
	ProviderTypeOpenAI    = "openai"
	ProviderTypeAnthropic = "anthropic"
	ProviderTypeGemini    = "gemini"
	ProviderTypeQwen      = "qwen"
	ProviderTypeQianfan   = "qianfan"
	ProviderTypeArk       = "ark"
	ProviderTypeDeepSeek  = "deepseek"
	ProviderTypeOllama    = "ollama"
)

// EffectiveType applies the OpenAI default only to an empty Type. It preserves
// all nonempty values so validation can reject noncanonical spellings.
func (p Provider) EffectiveType() string {
	if p.Type == "" {
		return ProviderTypeOpenAI
	}
	return p.Type
}

// ValidateProviderType accepts only the canonical provider type constants.
// Call with Provider.EffectiveType() when the empty OpenAI default applies.
func ValidateProviderType(kind string) error {
	switch kind {
	case ProviderTypeOpenAI, ProviderTypeAnthropic, ProviderTypeGemini, ProviderTypeQwen,
		ProviderTypeQianfan, ProviderTypeArk, ProviderTypeDeepSeek, ProviderTypeOllama:
		return nil
	}
	return fmt.Errorf("unsupported provider type %q", kind)
}

func (c *Config) normalizeProviderTypes() error {
	for name, provider := range c.Providers {
		provider.Type = provider.EffectiveType()
		if err := ValidateProviderType(provider.Type); err != nil {
			return fmt.Errorf("provider %q: %w", name, err)
		}
		c.Providers[name] = provider
	}
	return nil
}

func (c *Config) mergeProviderOverride(name string, provider *Provider, override Provider) error {
	var data []byte
	if node, ok := c.providerOverrides[name]; ok {
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("provider override must be an object")
		}
		var err error
		data, err = yaml.Marshal(&node)
		if err != nil {
			return err
		}
	} else {
		var err error
		data, err = yaml.Marshal(override)
		if err != nil {
			return err
		}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(provider)
}
