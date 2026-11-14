package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPluginProviderCanonicalTypesAndExplicitEmptyOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	for _, tc := range []struct {
		name, source, want string
		override           map[string]any
	}{
		{name: "default", source: `export const providers={cloud:{}}`, want: ProviderTypeOpenAI},
		{name: "anthropic", source: `export const providers={cloud:{type:"anthropic"}}`, want: ProviderTypeAnthropic},
		{name: "missing override type", source: `export const providers={cloud:{type:"anthropic",api_key_optional:true}}`, override: map[string]any{"api_key_optional": false}, want: ProviderTypeAnthropic},
		{name: "explicit empty type", source: `export const providers={cloud:{type:"anthropic"}}`, override: map[string]any{"type": ""}, want: ProviderTypeOpenAI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var extra map[string]any
			if tc.override != nil {
				extra = map[string]any{"providers": map[string]any{"demo__cloud": tc.override}}
			}
			cfg, err := LoadConfig(pluginConfigFile(t, tc.source, extra))
			if err != nil {
				t.Fatal(err)
			}
			if provider := cfg.Providers["demo__cloud"]; provider.Type != tc.want || provider.APIKeyOptional {
				t.Fatalf("provider default or override presence lost: %+v", provider)
			}
		})
	}
}

func TestPluginProvidersMergeBeforeModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	filename := pluginConfigFile(t, `
export const providers={cloud:{base_url:"https://example.invalid/v1",description:"Cloud",api_key_optional:true,models_url:"catalog",model_headers:{"X-Key":"{{api_key}}"}}};
export const models={fast:{provider:"demo__cloud",model:"remote"}};
export const agents={helper:{model:"demo__fast"}};`, map[string]any{
		"providers": map[string]any{"demo__cloud": map[string]any{"api_key": "configured", "api_key_optional": false, "description": ""}},
	})
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Providers["demo__cloud"]
	if p.Type != "openai" || p.BaseURL != "https://example.invalid/v1" || p.APIKey != "configured" || p.APIKeyOptional || p.Description != "" || p.ModelsURL != "catalog" || p.ModelHeaders["X-Key"] != "{{api_key}}" {
		t.Fatalf("provider merge: %+v", p)
	}
	if cfg.Models["demo__fast"].Provider != "demo__cloud" {
		t.Fatal("model cannot reference plugin provider")
	}
}

func TestPluginProviderInvalidDefinitions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		source, want string
		extra        map[string]any
	}{
		{source: `export const providers={cloud:null}`, want: "definition must be an object"},
		{source: `export const providers={cloud:{type:"unknown"}}`, want: "unsupported provider type"},
		{source: `export const providers={cloud:{type:"claude"}}`, want: "unsupported provider type"},
		{source: `export const providers={cloud:{type:"Anthropic"}}`, want: "unsupported provider type"},
		{source: `export const providers={cloud:{type:" openai"}}`, want: "unsupported provider type"},
		{source: `export const providers={cloud:{type:"openai "}}`, want: "unsupported provider type"},
		{source: `export const providers={cloud:{models_headers:{}}}`, want: "models_headers"},
		{source: `export const providers={cloud:{base_ur:"typo"}}`, want: "base_ur"},
		{source: `export const providers={cloud:{type:"openai"}}`, want: "models_ur", extra: map[string]any{"providers": map[string]any{"demo__cloud": map[string]any{"models_ur": "typo"}}}},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if _, err := LoadConfig(pluginConfigFile(t, tc.source, tc.extra)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPluginProviderStateRestoredBeforeAgentValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	filename := pluginConfigFile(t, `export const providers={cloud:{base_url:"https://example.invalid/v1"}}`, nil)
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAPIKey("demo__cloud", "saved"); err != nil {
		t.Fatal(err)
	}
	names, err := cfg.AddProviderModels("demo__cloud", []string{"remote/id"})
	if err != nil || !reflect.DeepEqual(names, []string{"demo__cloud/remote/id"}) {
		t.Fatalf("import: %v, %v", names, err)
	}
	// Add an agent that references only an imported model; validation must wait
	// until the overlay has restored that model.
	path := filepath.Join(filepath.Dir(filename), "plugin.js")
	if err := os.WriteFile(path, []byte(`export const providers={cloud:{base_url:"https://example.invalid/v1"}}; export const agents={helper:{model:"demo__cloud/remote/id"}};`), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(filename)
	if err != nil || loaded.Agents["demo__helper"].Model != "demo__cloud/remote/id" || loaded.Providers["demo__cloud"].APIKey != "saved" {
		t.Fatalf("restore before validation: %+v, %v", loaded, err)
	}
}

func TestPluginProviderNamespaceCollision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	filename := pluginConfigFile(t, `export const providers={cloud__remote:{}}`, nil)
	// The delimiter itself is allowed in plugin and export names; reject the
	// otherwise ambiguous a__b + c versus a + b__c namespaces.
	data := `plugins:
  demo:
    path: plugin.js
  demo__cloud:
    path: second.js
`
	if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(filepath.Dir(filename), "second.js")
	if err := os.WriteFile(second, []byte(`export const providers={remote:{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filename); err == nil || !strings.Contains(err.Error(), "already exported") {
		t.Fatalf("expected collision error, got %v", err)
	}
}
