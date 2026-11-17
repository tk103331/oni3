package a2a

import (
	"net/http"
	"strings"
)

// CardHandler serves the agent card for agentOverride, or the default agent.
func (s *Server) CardHandler(agentOverride string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		agentName := strings.TrimSpace(agentOverride)
		if agentName == "" {
			agentName = s.defaultAgent
		}
		if _, ok := s.cfg.Agents[agentName]; !ok {
			http.NotFound(w, r)
			return
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded == "http" || forwarded == "https" {
			scheme = forwarded
		}
		host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
		if host == "" {
			host = r.Host
		}
		path := "/a2a"
		if agentName != s.defaultAgent || r.URL.Path != "/.well-known/agent-card.json" {
			path = "/agents/" + agentName + "/a2a"
		}
		endpoint := scheme + "://" + host + path
		card := s.buildCard(agentName, endpoint)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		writeJSON(w, http.StatusOK, card)
	})
}

func (s *Server) buildCard(agentName, endpoint string) AgentCard {
	agentCfg := s.cfg.Agents[agentName]
	name := s.cfg.AgentDisplayName(agentName)
	version := s.version
	if version == "" {
		version = "dev"
	}
	description := strings.TrimSpace(agentCfg.Description)
	if description == "" {
		description = "Oni3 agent " + name
	}

	card := AgentCard{
		ProtocolVersion:    ProtocolVersion,
		Name:               name,
		Description:        description,
		URL:                endpoint,
		PreferredTransport: "JSONRPC",
		SupportedInterfaces: []AgentInterface{{
			URL:             endpoint,
			ProtocolBinding: "JSONRPC",
			ProtocolVersion: ProtocolVersion,
		}},
		Provider: &AgentProvider{Organization: "Oni3"},
		Version:  version,
		Capabilities: AgentCapabilities{
			Streaming:         true,
			PushNotifications: false,
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []AgentSkill{{
			ID:          agentName,
			Name:        name,
			Description: description,
			Tags:        []string{"agent", "oni3"},
			InputModes:  []string{"text/plain"},
			OutputModes: []string{"text/plain"},
		}},
	}
	if s.tokenEnabled {
		card.SecuritySchemes = map[string]SecurityScheme{
			"bearer": {
				Type:         "http",
				Scheme:       "bearer",
				BearerFormat: "opaque",
				Description:  "Oni3 access token",
			},
		}
		card.Security = []map[string][]string{{"bearer": {}}}
		card.SecurityRequirements = card.Security
	}
	return card
}
