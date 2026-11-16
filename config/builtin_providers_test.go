package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinProvidersAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, preset := range BuiltinProviders() {
		if preset.Name == "" || preset.BaseURL == "" {
			t.Fatalf("incomplete preset: %+v", preset)
		}
		if seen[preset.Name] {
			t.Fatalf("duplicate preset name %q", preset.Name)
		}
		seen[preset.Name] = true
		if err := ValidateProviderType(preset.Type); err != nil {
			t.Fatalf("preset %q: %v", preset.Name, err)
		}
		if preset.APIKeyOptional == (preset.KeyURL != "") {
			// Local servers and key-issuing vendors are the two groups.
			t.Fatalf("preset %q mixes optional keys and a key URL: %+v", preset.Name, preset)
		}
	}
	// The catalog covers the vendors the onboarding flow advertises.
	for _, want := range []string{"openai", "anthropic", "gemini", "deepseek", "qwen", "kimi", "zhipu", "minimax", "magpie", "ollama"} {
		if !seen[want] {
			t.Fatalf("missing builtin provider %q", want)
		}
	}
}

func TestSeedBuiltinProvidersKeepsConfiguredProvider(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"deepseek": {Type: ProviderTypeOpenAI, BaseURL: "https://custom.example/v1", APIKey: "mine"},
	}}
	cfg.seedBuiltinProviders()
	if got := cfg.Providers["deepseek"]; got.BaseURL != "https://custom.example/v1" || got.APIKey != "mine" {
		t.Fatalf("configured provider was overwritten: %+v", got)
	}
	if _, ok := cfg.Providers["openai"]; !ok {
		t.Fatal("missing seeded provider")
	}
	// Seeding twice is a no-op.
	cfg.seedBuiltinProviders()
	if got := cfg.Providers["openai"]; got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("seeded provider changed: %+v", got)
	}
}

func TestBuiltinProviderKeySurvivesRestart(t *testing.T) {
	filename := providerConfigFile(t, "# only comments\n")
	if _, err := LoadConfig(filename); err != nil {
		t.Fatal(err)
	}
	// A key entered for a built-in provider persists like one for a provider
	// written in the file: the seeded entry gives it a stable identity.
	cfg := GetConfig()
	if _, ok := cfg.Providers["openai"]; !ok {
		t.Fatal("built-in provider missing")
	}
	if err := cfg.SetProviderAPIKey("openai", "saved-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddProviderModels("openai", []string{"gpt-4o-mini"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Providers["openai"].APIKey != "saved-key" {
		t.Fatal("built-in provider key was not restored")
	}
	if _, ok := reloaded.Models["openai/gpt-4o-mini"]; !ok {
		t.Fatalf("imported model missing: %v", reloaded.Models)
	}
}

func TestBuiltinProviderKeyURLIsOptionalToConfigure(t *testing.T) {
	filename := providerConfigFile(t, "providers:\n  mine:\n    type: openai\n    base_url: https://example.invalid/v1\n    key_url: https://example.invalid/keys\n")
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["mine"].KeyURL != "https://example.invalid/keys" {
		t.Fatalf("key_url not parsed: %+v", cfg.Providers["mine"])
	}
}

func TestStarterConfigLoadsWithBuiltinProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if _, err := EnsureConfigFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/provider") {
		t.Fatal("starter configuration does not mention /provider")
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Providers["openai"]; !ok {
		t.Fatal("built-in providers missing from a first-run configuration")
	}
}
