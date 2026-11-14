package agent

import (
	"testing"

	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/state"
)

// /model choices are remembered for agents that follow the default model.
func TestRememberedModelAppliedToDefaultModelAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{
		DefaultModel: "base",
		Models: map[string]config.Model{
			"base":  {},
			"other": {},
		},
		Agents: map[string]config.Agent{
			"OniThree": {Model: "base"},
			"pinned":   {Model: "other"},
		},
	}

	if got := rememberedModel(cfg, "OniThree"); got != "" {
		t.Fatalf("no remembered model expected, got %q", got)
	}

	rememberModel(cfg, "OniThree", "other")
	if got := rememberedModel(cfg, "OniThree"); got != "other" {
		t.Fatalf("remembered model = %q, want other", got)
	}

	// Switching back to the configured default forgets the override.
	rememberModel(cfg, "OniThree", "base")
	if got := state.RememberedModel("OniThree"); got != "" {
		t.Fatalf("override should be cleared, got %q", got)
	}

	// An agent with its own model pinned in the config keeps it.
	rememberModel(cfg, "pinned", "base")
	if got := state.RememberedModel("pinned"); got != "" {
		t.Fatalf("pinned agent override should be ignored, got %q", got)
	}
	if got := rememberedModel(cfg, "pinned"); got != "" {
		t.Fatalf("pinned agent must keep its config model, got %q", got)
	}
}

// A remembered model that no longer exists in the config is ignored.
func TestRememberedModelIgnoredWhenStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{
		DefaultModel: "base",
		Models:       map[string]config.Model{"base": {}},
		Agents:       map[string]config.Agent{"OniThree": {Model: "base"}},
	}
	if err := state.SetRememberedModel("OniThree", "removed"); err != nil {
		t.Fatal(err)
	}
	if got := rememberedModel(cfg, "OniThree"); got != "" {
		t.Fatalf("stale model must be ignored, got %q", got)
	}
}
