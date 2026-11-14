package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestProviderTypesAreCanonical(t *testing.T) {
	for _, kind := range []string{
		ProviderTypeOpenAI, ProviderTypeAnthropic, ProviderTypeGemini, ProviderTypeQwen,
		ProviderTypeQianfan, ProviderTypeArk, ProviderTypeDeepSeek, ProviderTypeOllama,
	} {
		if err := ValidateProviderType(kind); err != nil {
			t.Fatalf("canonical type %q: %v", kind, err)
		}
		for _, invalid := range []string{strings.ToUpper(kind), " " + kind, kind + " "} {
			if err := ValidateProviderType(invalid); err == nil {
				t.Errorf("accepted noncanonical provider type %q", invalid)
			}
		}
	}
	for _, kind := range []string{"", "claude", "open_ai", "unknown"} {
		if err := ValidateProviderType(kind); err == nil {
			t.Errorf("accepted invalid provider type %q", kind)
		}
	}
	if got := (Provider{}).EffectiveType(); got != ProviderTypeOpenAI {
		t.Fatalf("empty provider type defaults to %q", got)
	}
	if got := (Provider{Type: " OpenAI "}).EffectiveType(); got != " OpenAI " {
		t.Fatalf("EffectiveType silently normalized an invalid type: %q", got)
	}
}

func TestLoadConfigCanonicalProviderTypesAndAliases(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	for _, kind := range []string{"", "openai", "anthropic", "gemini", "qwen", "qianfan", "ark", "deepseek", "ollama"} {
		t.Run(kind, func(t *testing.T) {
			filename := providerConfigFile(t, fmt.Sprintf(`providers:
  My_Cloud:
    type: %q
    model_headers:
      X-Custom-Key: '{{api_key}}'
models:
  My_Model:
    provider: My_Cloud
    model: claude-remote-Original
`, kind))
			cfg, err := LoadConfig(filename)
			if err != nil {
				t.Fatal(err)
			}
			want := kind
			if want == "" {
				want = ProviderTypeOpenAI
			}
			provider, exists := cfg.Providers["My_Cloud"]
			if !exists || provider.Type != want || provider.ModelHeaders["X-Custom-Key"] != "{{api_key}}" {
				t.Fatalf("provider type, headers, or alias changed: %+v", cfg.Providers)
			}
			if model := cfg.Models["My_Model"]; model.Provider != "My_Cloud" || model.Model != "claude-remote-Original" {
				t.Fatalf("model ID or user alias changed: %+v", cfg.Models)
			}
		})
	}
}

func TestLoadConfigRejectsNoncanonicalTypesWithoutPublishing(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	sentinel := &Config{}
	globalConfig = sentinel
	for _, kind := range []string{"claude", "CLAUDE", "OpenAI", " openai", "openai ", "deep_seek"} {
		filename := providerConfigFile(t, fmt.Sprintf("providers:\n  cloud:\n    type: %q\n", kind))
		if _, err := LoadConfig(filename); err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
			t.Errorf("provider type %q: %v", kind, err)
		}
		if globalConfig != sentinel {
			t.Fatal("failed provider validation published configuration")
		}
	}
	for _, kind := range []string{"", "http", "HTTP", "STDIO", "SSE", "streamable-http", "STREAMABLE_HTTP", " sse", "sse "} {
		filename := providerConfigFile(t, fmt.Sprintf("mcp_servers:\n  server:\n    type: %q\n    url: https://example.invalid/mcp\n", kind))
		if _, err := LoadConfig(filename); err == nil || !strings.Contains(err.Error(), "unsupported MCP server type") {
			t.Errorf("MCP type %q: %v", kind, err)
		}
		if globalConfig != sentinel {
			t.Fatal("failed MCP validation published configuration")
		}
	}
}

func TestLoadConfigAcceptsEmptyYAML(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	for _, source := range []string{"", "# Configuration can use all defaults.\n", "---\n"} {
		cfg, err := LoadConfig(providerConfigFile(t, source))
		if err != nil || cfg == nil || globalConfig != cfg {
			t.Fatalf("empty YAML was not published: %+v, %v", cfg, err)
		}
	}
}

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	sentinel := &Config{}
	globalConfig = sentinel
	for _, tc := range []struct{ name, source, field string }{
		{"top level", "providerz: {}\n", "providerz"},
		{"provider", "providers:\n  cloud:\n    models_headers: {}\n", "models_headers"},
		{"provider casing", "providers:\n  cloud:\n    BaseURL: https://example.invalid\n", "BaseURL"},
		{"model", "models:\n  test:\n    maxTokens: 12\n", "maxTokens"},
		{"agent", "agents:\n  helper:\n    modle: test\n", "modle"},
		{"MCP", "mcp_servers:\n  server:\n    type: sse\n    header: {}\n", "header"},
		{"tool parameters", "tools:\n  example:\n    type: custom_http\n    params: []\n", "params"},
		{"parameter fields", "tools:\n  example:\n    type: custom_http\n    parameters:\n      - name: query\n        requiredx: true\n", "requiredx"},
		{"settings", "settings:\n  approvel: {}\n", "approvel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LoadConfig(providerConfigFile(t, tc.source)); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("expected unknown field %q, got %v", tc.field, err)
			}
			if globalConfig != sentinel {
				t.Fatal("invalid YAML replaced published config")
			}
		})
	}
}

func TestLoadConfigKeepsDynamicToolConfig(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	cfg, err := LoadConfig(providerConfigFile(t, `tools:
  My_Alias:
    type: custom_http
    parameters:
      - name: query
        type: string
        description: Search query
        required: true
    config:
      arbitrary_extension_setting:
        nested: true
`))
	if err != nil {
		t.Fatal(err)
	}
	tool := cfg.Tools["My_Alias"]
	if len(tool.Parameters) != 1 || tool.Parameters[0].Name != "query" || !tool.Parameters[0].Required {
		t.Fatalf("canonical parameters not decoded: %+v", tool)
	}
	value := tool.Config["arbitrary_extension_setting"]
	if !value.GetBool("nested") {
		t.Fatal("strict YAML decoder rejected or lost dynamic tool configuration")
	}
}

func TestMCPNamesAndTypesAreCanonical(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	for _, kind := range []string{MCPTypeStdio, MCPTypeSSE, MCPTypeStreamableHTTP} {
		if err := ValidateMCPType(kind); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(providerConfigFile(t, fmt.Sprintf("mcp_servers:\n  server_1:\n    type: %s\n", kind)))
		if err != nil || cfg.MCPServers["server_1"].Type != kind {
			t.Fatalf("canonical MCP type %q: %+v, %v", kind, cfg, err)
		}
	}
	for _, name := range []string{"", "Server", "server-name", " server", "server ", "1server", "服务器", strings.Repeat("a", 62)} {
		if err := ValidateMCPServerName(name); err == nil {
			t.Errorf("invalid MCP server name %q accepted", name)
		}
		filename := providerConfigFile(t, fmt.Sprintf("mcp_servers:\n  %q:\n    type: sse\n", name))
		if _, err := LoadConfig(filename); err == nil {
			t.Errorf("loaded invalid MCP server name %q", name)
		}
	}
	if err := ValidateMCPServerName(strings.Repeat("a", 61)); err != nil {
		t.Fatalf("longest usable server name rejected: %v", err)
	}
	if err := ValidateMCPToolName(strings.Repeat("a", 61) + "__b"); err != nil {
		t.Fatalf("64-character tool name rejected: %v", err)
	}
	if err := ValidateMCPToolName(strings.Repeat("a", 61) + "__bc"); err == nil {
		t.Fatal("overlong tool name accepted")
	}
}

func TestProviderStateUsesEffectiveTypeAndRejectsAliases(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	const source = "providers:\n  cloud:\n    base_url: https://example.invalid/v1\n"
	filename := providerConfigFile(t, source)
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAPIKey("cloud", "saved"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddProviderModels("cloud", []string{"claude-remote-ID"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(source+"    type: openai\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(filename)
	if err != nil || loaded.Providers["cloud"].APIKey != "saved" || loaded.Models["cloud/claude-remote-ID"].Model != "claude-remote-ID" {
		t.Fatalf("empty and explicit OpenAI types have different saved identity: %+v, %v", loaded, err)
	}
	invalid := &Config{SourcePath: filename, Providers: map[string]Provider{"cloud": {Type: "claude"}}}
	if err := invalid.SetProviderAPIKey("cloud", "another"); err == nil {
		t.Fatal("saved credentials for a legacy provider type")
	}
	if _, err := invalid.AddProviderModels("cloud", []string{"another"}); err == nil {
		t.Fatal("saved models for a legacy provider type")
	}
}
