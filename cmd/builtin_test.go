package cmd

import (
	"strings"
	"testing"

	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/state"
	"github.com/tk103331/oni3/tools"
)

func TestEnsureBuiltinAgentUsesDefaultModel(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{
			"alpha": {Provider: "p", Model: "m"},
			"zeta":  {Provider: "p", Model: "m"},
		},
		DefaultModel: "zeta",
	}

	name, err := ensureBuiltinAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if name != builtinAgentName {
		t.Fatalf("name = %q", name)
	}
	agent, ok := cfg.Agents[name]
	if !ok {
		t.Fatal("builtin agent not registered")
	}
	if agent.Model != "zeta" {
		t.Fatalf("model = %q, want default_model zeta", agent.Model)
	}
	if agent.System == "" {
		t.Fatal("builtin agent system prompt missing")
	}
	// The built-in agent carries its display name, so the interface shows
	// "OniThree" instead of the configuration key.
	if got := cfg.AgentDisplayName(name); got != builtinAgentDisplayName {
		t.Fatalf("display name = %q, want %q", got, builtinAgentDisplayName)
	}
	if !strings.Contains(agent.System, builtinAgentDisplayName) {
		t.Fatalf("system prompt does not mention %q: %q", builtinAgentDisplayName, agent.System)
	}

	// The built-in agent ships with a default tool set, and every tool it
	// references has a config entry.
	want := []string{
		"command_line", "file_system", "http_request", "browser_use", tools.JavaScriptToolName,
		tools.SubagentToolName, tools.AskUserToolName, tools.TodoListToolName,
	}
	if len(agent.Tools) != len(want) {
		t.Fatalf("tools = %v, want %v", agent.Tools, want)
	}
	for i, name := range want {
		if agent.Tools[i] != name {
			t.Fatalf("tools[%d] = %q, want %q", i, agent.Tools[i], name)
		}
		if _, ok := cfg.Tools[name]; !ok {
			t.Fatalf("default tool config missing: %s", name)
		}
	}
}

func TestEnsureBuiltinAgentKeepsCustomToolConfig(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{"m": {Provider: "p", Model: "m"}},
		Tools: map[string]config.Tool{
			"command_line": {Type: "command_line", Description: "my shell"},
		},
	}

	if _, err := ensureBuiltinAgent(cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Tools["command_line"].Description; got != "my shell" {
		t.Fatalf("custom tool config was overwritten: %q", got)
	}
	if _, ok := cfg.Tools["file_system"]; !ok {
		t.Fatal("missing default file_system tool")
	}

	// A user-defined agent with the built-in name is left untouched.
	cfg2 := &config.Config{
		Models: map[string]config.Model{"m": {Provider: "p", Model: "m"}},
		Agents: map[string]config.Agent{"onthree": {Model: "m", Tools: []string{"custom"}}},
	}
	if _, err := ensureBuiltinAgent(cfg2); err != nil {
		t.Fatal(err)
	}
	if got := cfg2.Agents["onthree"].Tools; len(got) != 1 || got[0] != "custom" {
		t.Fatalf("user-defined OniThree agent changed: %v", got)
	}
}

func TestEnsureBuiltinAgentFallbackModel(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{
			"beta":  {Provider: "p", Model: "m"},
			"alpha": {Provider: "p", Model: "m"},
		},
	}
	if _, err := ensureBuiltinAgent(cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Agents[builtinAgentName].Model; got != "alpha" {
		t.Fatalf("fallback model = %q, want alphabetically first", got)
	}

	// No models at all is a helpful error.
	empty := &config.Config{}
	if _, err := ensureBuiltinAgent(empty); err == nil {
		t.Fatal("expected error without models")
	}
}

func TestEnsureBuiltinAgentAllowsProviderOnboarding(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{"cloud": {Type: "openai", BaseURL: "https://example.com/v1"}}}
	name, err := ensureBuiltinAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if name != builtinAgentName || cfg.Agents[name].Model != "" {
		t.Fatalf("provider onboarding agent: %q %+v", name, cfg.Agents[name])
	}
}

func TestDropAgentTools(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{"m": {Provider: "p", Model: "m"}},
	}
	name, err := ensureBuiltinAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}

	dropAgentTools(cfg, name, tools.AskUserToolName, tools.TodoListToolName)

	want := []string{"command_line", "file_system", "http_request", "browser_use", tools.JavaScriptToolName, tools.SubagentToolName}
	got := cfg.Agents[name].Tools
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	for i, tool := range want {
		if got[i] != tool {
			t.Fatalf("tools[%d] = %q, want %q", i, got[i], tool)
		}
	}
	// Tool config entries stay, other agents are untouched.
	if _, ok := cfg.Tools[tools.AskUserToolName]; !ok {
		t.Fatal("tool config entry was removed")
	}
	dropAgentTools(cfg, "missing", tools.AskUserToolName)
	dropAgentTools(nil, name, tools.AskUserToolName)
}

func TestResolveDefaultAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_ = state.ClearLastAgent()

	cfg := &config.Config{
		Agents: map[string]config.Agent{},
		Models: map[string]config.Model{"m1": {Provider: "p", Model: "m"}},
	}

	// No previous agent -> built-in.
	name, err := resolveDefaultAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if name != builtinAgentName {
		t.Fatalf("default = %q, want builtinAgentName", name)
	}

	// A previous agent that still exists wins.
	cfg.Agents["worker"] = config.Agent{Model: "m1"}
	if err := state.SetLastAgent("worker"); err != nil {
		t.Fatal(err)
	}
	name, err = resolveDefaultAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if name != "worker" {
		t.Fatalf("default = %q, want worker", name)
	}

	// A stale previous agent falls back to built-in.
	if err := state.SetLastAgent("gone"); err != nil {
		t.Fatal(err)
	}
	name, err = resolveDefaultAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if name != builtinAgentName {
		t.Fatalf("default = %q, want builtinAgentName for stale last agent", name)
	}
}
