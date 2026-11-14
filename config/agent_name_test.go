package config

import "testing"

func TestAgentDisplayName(t *testing.T) {
	cfg := &Config{Agents: map[string]Agent{
		"named":   {Name: "OniThree", System: "s"},
		"unnamed": {System: "s"},
	}}

	if got := cfg.AgentDisplayName("named"); got != "OniThree" {
		t.Fatalf("display name = %q, want OniThree", got)
	}
	// An agent without a name falls back to its configuration key.
	if got := cfg.AgentDisplayName("unnamed"); got != "unnamed" {
		t.Fatalf("display name = %q, want unnamed", got)
	}
	// An unknown key is returned unchanged.
	if got := cfg.AgentDisplayName("missing"); got != "missing" {
		t.Fatalf("display name = %q, want missing", got)
	}
	var nilCfg *Config
	if got := nilCfg.AgentDisplayName("x"); got != "x" {
		t.Fatalf("display name = %q, want x", got)
	}
}
