package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk103331/oni3/plugins"
	"gopkg.in/yaml.v3"
)

func pluginConfigFile(t *testing.T, source string, extra map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.js"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if extra == nil {
		extra = make(map[string]any)
	}
	extra["plugins"] = map[string]any{"demo": map[string]any{"path": "./plugin.js", "config": map[string]any{"greeting": "你好"}}}
	data, err := yaml.Marshal(extra)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestPluginConfigRelativePathsAndAllExtensions(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	// This path is outside the process working directory. No chdir is needed.
	cfg, err := LoadConfig(filepath.Join("..", "examples", "plugins", "demo.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LoadedPlugins) != 1 || len(cfg.PluginThemes) != 1 {
		t.Fatalf("extensions missing: %+v", cfg)
	}
	model := cfg.Models["demo__fast"]
	if model.Provider != "local" || model.Model != "qwen3" || model.Temperature != 0.3 {
		t.Fatalf("model: %+v", model)
	}
	agent := cfg.Agents["demo__helper"]
	if agent.Description == "" || agent.Model != "demo__fast" || len(agent.Tools) != 2 {
		t.Fatalf("agent: %+v", agent)
	}
	if cfg.DefaultModelName() != "demo__fast" {
		t.Fatalf("default model: %s", cfg.DefaultModelName())
	}
	tool := cfg.Tools["demo__greet"]
	if tool.Type != "plugin" || tool.PluginTool == nil {
		t.Fatalf("tool: %+v", tool)
	}
	result, err := tool.PluginTool.InvokableRun(context.Background(), `{"name":"世界"}`)
	if err != nil || result != "你好, 世界!" {
		t.Fatalf("result: %q %v", result, err)
	}
	if !cfg.LoadedPlugins[0].HasHook("after_tool") {
		t.Fatal("hooks not registered")
	}
}

func TestPluginFailuresPreservePublishedConfig(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	sentinel := &Config{}
	globalConfig = sentinel
	for _, tc := range []struct {
		source, want string
		extra        map[string]any
	}{
		{source: `export const tools={x:{description:"X",execute(){}}}`, want: "already exists", extra: map[string]any{"tools": map[string]any{"demo__x": map[string]any{"type": "file_system"}}}},
		{source: `export const agents={x:{system:"X"}}`, want: "already exists", extra: map[string]any{"agents": map[string]any{"demo__x": map[string]any{"system": "existing"}}}},
		{source: `export const models={x:{provider:"local",model:"test"}}`, want: "already exists", extra: map[string]any{"models": map[string]any{"demo__x": map[string]any{"model": "existing"}}}},
		{source: `export const models={x:{provider:"missing",model:"test"}}`, want: "configured provider"},
		{source: `export const agents={x:{system:"X",modle:"typo"}}`, want: "modle"},
		{source: `export const models={x:{provider:"local",model:"test",generate:"unsupported"}}`, want: "generate"},
		{source: `export const agents={x:null}`, want: "definition must be an object"},
		{source: `export const hooks={before_angent(){}}`, want: "unknown hook event"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			_, err := LoadConfig(pluginConfigFile(t, tc.source, tc.extra))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
			if globalConfig != sentinel {
				t.Fatal("failed load replaced global config")
			}
		})
	}
}

func TestPluginHookOrderingAndError(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "hook.js")
	if err := os.WriteFile(filename, []byte(`export const hooks={before_agent(event,context){throw new Error(context.plugin+":"+event.agent)}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Plugins: map[string]plugins.Config{"z": {Path: filename}, "a": {Path: filename}}}
	if err := cfg.loadPlugins(context.Background(), filepath.Join(dir, "demo.yml")); err != nil {
		t.Fatal(err)
	}
	if cfg.LoadedPlugins[0].Name() != "a" || cfg.LoadedPlugins[1].Name() != "z" {
		t.Fatal("non-deterministic plugin order")
	}
	err := cfg.EmitPluginEvent(context.Background(), plugins.Event{Type: "before_agent", Agent: "helper"})
	if err == nil || !strings.Contains(err.Error(), "a:helper") {
		t.Fatalf("hook error: %v", err)
	}
}
