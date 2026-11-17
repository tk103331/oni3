package config

import (
	"strings"
	"testing"
)

func TestLoadConfigA2AAgents(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })

	cfg, err := LoadConfig(providerConfigFile(t, `a2a_agents:
  remote_researcher:
    card_url: https://example.com/.well-known/agent-card.json
    description: Remote research agent
  remote_env:
    card_url: http://localhost:8080
    token_env: REMOTE_A2A_TOKEN
`))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	researcher := cfg.A2AAgents["remote_researcher"]
	if researcher.CardURL != "https://example.com/.well-known/agent-card.json" ||
		researcher.Description != "Remote research agent" {
		t.Fatalf("remote_researcher = %+v", researcher)
	}
	if cfg.A2AAgents["remote_env"].TokenEnv != "REMOTE_A2A_TOKEN" {
		t.Fatalf("remote_env = %+v", cfg.A2AAgents["remote_env"])
	}
	if globalConfig != cfg {
		t.Fatal("valid configuration was not published")
	}
}

func TestLoadConfigRejectsInvalidA2AAgents(t *testing.T) {
	previous := globalConfig
	t.Cleanup(func() { globalConfig = previous })
	sentinel := &Config{}
	globalConfig = sentinel

	for _, tc := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "missing card url",
			source: "a2a_agents:\n  remote:\n    description: no url\n",
			want:   "card_url is required",
		},
		{
			name:   "relative card url",
			source: "a2a_agents:\n  remote:\n    card_url: /.well-known/agent-card.json\n",
			want:   "must be an http(s) URL",
		},
		{
			name:   "unsupported scheme",
			source: "a2a_agents:\n  remote:\n    card_url: ftp://example.com/card.json\n",
			want:   "must be an http(s) URL",
		},
		{
			name:   "empty name",
			source: "a2a_agents:\n  \"\":\n    card_url: https://example.com/card.json\n",
			want:   "name must not be empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(providerConfigFile(t, tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if globalConfig != sentinel {
				t.Fatal("invalid configuration replaced the published one")
			}
		})
	}
}
