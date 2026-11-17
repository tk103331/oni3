// Package web serves Oni3 agents to a browser over a small HTTP API.
//
// The API mirrors the MCP tools (agents, models) and streams a single agent
// turn as Server-Sent Events. Conversations are stored with the same session
// files the TUI and ACP use, so a chat started in the browser can be resumed
// from the CLI and vice versa.
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/server"
	"github.com/tk103331/oni3/config"
	mcpserver "github.com/tk103331/oni3/mcp/server"
	a2aprotocol "github.com/tk103331/oni3/protocol/a2a"
	aguiprotocol "github.com/tk103331/oni3/protocol/agui"
	"github.com/tk103331/oni3/runtime"
	"github.com/tk103331/oni3/session"
)

//go:embed static/*
var staticFiles embed.FS

// Options configures a web server.
type Options struct {
	// DefaultAgent is pre-selected in the interface. An empty value picks the
	// first agent by name.
	DefaultAgent string
	// WorkDir is shown in the interface and is the directory tools resolve
	// relative paths against (the process working directory).
	WorkDir string
	// Version is reported by /api/bootstrap.
	Version string
	// Token enables bearer-token authentication when non-empty.
	Token string
	// DisableWeb hides the browser interface, web API and login/docs pages.
	// Protocol endpoints and /api/health remain available.
	DisableWeb bool
	// EnableAGUI mounts the AG-UI SSE endpoint.
	EnableAGUI bool
	// EnableA2A mounts the A2A JSON-RPC and agent-card endpoints.
	EnableA2A bool
	// EnableMCPServer makes Oni3 itself an MCP server over Streamable HTTP and
	// legacy SSE. It does not manage external MCP servers.
	EnableMCPServer bool
}

// Server exposes configured agents over HTTP.
type Server struct {
	cfg          *config.Config
	defaultAgent string
	workDir      string
	version      string
	tokenHash    string
	authCookie   string
	webEnabled   bool
	mux          *http.ServeMux

	pendingMu sync.Mutex
	pending   map[string]chan hostResponse

	runsMu sync.Mutex
	runs   map[string]context.CancelFunc

	approvedMu sync.Mutex
	approved   map[string]map[string]bool

	agui    *aguiprotocol.Server
	a2a     *a2aprotocol.Server
	mcpHTTP *mcpgo.StreamableHTTPServer
	mcpSSE  *mcpgo.SSEServer
}

// New creates a web server for cfg.
func New(cfg *config.Config, opts Options) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("global configuration not initialized")
	}

	agentName := strings.TrimSpace(opts.DefaultAgent)
	if agentName == "" {
		names := sortedAgentNames(cfg)
		if len(names) > 0 {
			agentName = names[0]
		}
	}
	if agentName != "" {
		if _, ok := cfg.Agents[agentName]; !ok {
			return nil, fmt.Errorf("Agent configuration does not exist: %s", agentName)
		}
	}

	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	s := &Server{
		cfg:          cfg,
		defaultAgent: agentName,
		workDir:      workDir,
		version:      opts.Version,
		tokenHash:    "",
		authCookie:   "",
		webEnabled:   !opts.DisableWeb,
		mux:          http.NewServeMux(),
		pending:      make(map[string]chan hostResponse),
		runs:         make(map[string]context.CancelFunc),
		approved:     make(map[string]map[string]bool),
	}
	if token := strings.TrimSpace(opts.Token); token != "" {
		s.tokenHash = hashToken(token)
		s.authCookie = tokenCookieValue(token)
	}
	if opts.DisableWeb && !opts.EnableAGUI && !opts.EnableA2A && !opts.EnableMCPServer {
		return nil, fmt.Errorf("no web interface or protocol endpoints enabled")
	}

	if opts.EnableAGUI || opts.EnableA2A {
		runner, err := runtime.New(cfg)
		if err != nil {
			return nil, err
		}
		if opts.EnableAGUI {
			handler, err := aguiprotocol.New(runner, agentName)
			if err != nil {
				return nil, err
			}
			s.agui = handler
		}
		if opts.EnableA2A {
			handler, err := a2aprotocol.New(cfg, runner, agentName, opts.Version, s.tokenHash != "")
			if err != nil {
				return nil, err
			}
			s.a2a = handler
		}
	}
	if opts.EnableMCPServer {
		exposed, err := mcpserver.AgentTools(cfg)
		if err != nil {
			return nil, err
		}
		mcpSrv, err := mcpserver.New(mcpserver.Options{
			Name:    "oni3",
			Version: opts.Version,
			Instructions: fmt.Sprintf(
				"Use list_agents to discover agents, call_agent to run one, and "+
					"list_models to discover model aliases. Relative paths resolve against %s.", workDir),
			Tools: exposed,
		})
		if err != nil {
			return nil, err
		}
		s.mcpHTTP = mcpgo.NewStreamableHTTPServer(mcpSrv, mcpgo.WithEndpointPath("/mcp"))
		s.mcpSSE = mcpgo.NewSSEServer(
			mcpSrv,
			mcpgo.WithStaticBasePath("/mcp"),
			mcpgo.WithSSEEndpoint("/sse"),
			mcpgo.WithMessageEndpoint("/message"),
			mcpgo.WithAppendQueryToMessageEndpoint(),
			mcpgo.WithUseFullURLForMessageEndpoint(false),
		)
	}
	s.routes()
	return s, nil
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The API can run shell commands through tools. Reject cross-origin
		// browser requests so another site cannot drive a loopback server.
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin requests are not allowed")
				return
			}
		}
		if s.tokenHash != "" {
			if s.webEnabled {
				// The login endpoint itself must stay reachable without a token.
				if r.Method == http.MethodPost && r.URL.Path == "/api/login" {
					s.mux.ServeHTTP(w, r)
					return
				}
				// Exchange a token supplied in the URL for an HttpOnly cookie and
				// remove it from the address bar on the redirect.
				if queryToken := strings.TrimSpace(r.URL.Query().Get(authQueryParam)); queryToken != "" {
					if !s.validToken(queryToken) {
						writeError(w, http.StatusUnauthorized, "invalid token")
						return
					}
					if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/login") {
						s.setAuthCookie(w, r)
						if r.URL.Path == "/login" {
							http.Redirect(w, r, "/", http.StatusFound)
							return
						}
						target := *r.URL
						query := target.Query()
						query.Del(authQueryParam)
						target.RawQuery = query.Encode()
						http.Redirect(w, r, target.String(), http.StatusFound)
						return
					}
				}
			}
			if !s.authorized(r) {
				if s.webEnabled && r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/login") {
					s.serveLogin(w)
					return
				}
				w.Header().Set("WWW-Authenticate", `Bearer realm="oni3"`)
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
		}
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if s.webEnabled {
		s.mux.HandleFunc("GET /api/bootstrap", s.handleBootstrap)
		s.mux.HandleFunc("GET /api/agents", s.handleAgents)
		s.mux.HandleFunc("GET /api/models", s.handleModels)
		s.mux.HandleFunc("GET /api/sessions", s.handleSessions)
		s.mux.HandleFunc("GET /api/sessions/{id}", s.handleSession)
		s.mux.HandleFunc("POST /api/chat", s.handleChat)
		s.mux.HandleFunc("POST /api/respond", s.handleRespond)
		s.mux.HandleFunc("POST /api/cancel", s.handleCancel)
		s.mux.HandleFunc("GET /login", s.handleLoginPage)
		s.mux.HandleFunc("GET /docs", s.handleDocs)
		s.mux.HandleFunc("GET /api/auth", s.handleAuth)
		s.mux.HandleFunc("POST /api/login", s.handleLogin)
		s.mux.HandleFunc("POST /api/logout", s.handleLogout)
		s.mux.HandleFunc("GET /", s.handleIndex)
	}
	if s.agui != nil {
		s.mux.Handle("POST /agui", s.agui.Handler(""))
		s.mux.HandleFunc("POST /agents/{agent}/agui", func(w http.ResponseWriter, r *http.Request) {
			s.agui.Handler(r.PathValue("agent")).ServeHTTP(w, r)
		})
	}
	if s.a2a != nil {
		s.mux.Handle("POST /a2a", s.a2a.Handler(""))
		s.mux.HandleFunc("POST /agents/{agent}/a2a", func(w http.ResponseWriter, r *http.Request) {
			s.a2a.Handler(r.PathValue("agent")).ServeHTTP(w, r)
		})
		s.mux.Handle("GET /.well-known/agent-card.json", s.a2a.CardHandler(""))
		s.mux.HandleFunc("GET /agents/{agent}/.well-known/agent-card.json", func(w http.ResponseWriter, r *http.Request) {
			s.a2a.CardHandler(r.PathValue("agent")).ServeHTTP(w, r)
		})
	}
	if s.mcpHTTP != nil {
		s.mux.Handle("POST /mcp", s.mcpHTTP)
		s.mux.Handle("GET /mcp", s.mcpHTTP)
		s.mux.Handle("DELETE /mcp", s.mcpHTTP)
	}
	if s.mcpSSE != nil {
		s.mux.Handle("GET /mcp/sse", s.mcpSSE.SSEHandler())
		s.mux.Handle("POST /mcp/message", s.mcpSSE.MessageHandler())
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "web interface unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(data)
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/docs.html")
	if err != nil {
		http.Error(w, "API documentation unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(data)
}

func (s *Server) serveLogin(w http.ResponseWriter) {
	data, err := staticFiles.ReadFile("static/login.html")
	if err != nil {
		http.Error(w, "login page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(data)
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.tokenHash == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if s.authorized(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.serveLogin(w)
}

// AgentInfo describes one selectable agent.
type AgentInfo struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	DefaultModel string `json:"default_model"`
}

func (s *Server) agentInfos() []AgentInfo {
	names := sortedAgentNames(s.cfg)
	out := make([]AgentInfo, 0, len(names))
	for _, name := range names {
		ac := s.cfg.Agents[name]
		description := strings.TrimSpace(ac.Description)
		if description == "" {
			description = s.cfg.AgentDisplayName(name)
		}
		model := strings.TrimSpace(ac.Model)
		if model == "" {
			model = s.cfg.DefaultModelName()
		}
		out = append(out, AgentInfo{Name: name, Description: description, DefaultModel: model})
	}
	return out
}

// ModelList reports model aliases only, matching list_models over MCP.
type ModelList struct {
	DefaultModel string   `json:"default_model"`
	Models       []string `json:"models"`
}

func (s *Server) modelList() ModelList {
	names := make([]string, 0, len(s.cfg.Models))
	for name := range s.cfg.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return ModelList{DefaultModel: s.cfg.DefaultModelName(), Models: names}
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	models := s.modelList()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       s.version,
		"workdir":       s.workDir,
		"default_agent": s.defaultAgent,
		"auth_enabled":  s.tokenHash != "",
		"agents":        s.agentInfos(),
		"default_model": models.DefaultModel,
		"models":        models.Models,
	})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"agents": s.agentInfos()})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.modelList())
}

// SessionSummary is one row of the conversation sidebar.
type SessionSummary struct {
	ID      string    `json:"id"`
	Agent   string    `json:"agent"`
	Model   string    `json:"model"`
	Title   string    `json:"title"`
	Turns   int       `json:"turns"`
	Updated time.Time `json:"updated"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	listLimit := limit
	if query != "" {
		// Search all stored sessions, then cap the matches below.
		listLimit = 0
	}

	stored, err := session.List(session.KindAgent, listLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summaries := make([]SessionSummary, 0, len(stored))
	for _, sess := range stored {
		summary := summarizeSession(sess)
		if query != "" && !sessionMatchesQuery(sess, summary, query) {
			continue
		}
		summaries = append(summaries, summary)
		if len(summaries) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": summaries})
}

func sessionMatchesQuery(sess *session.Session, summary SessionSummary, query string) bool {
	if strings.Contains(strings.ToLower(summary.Title), query) ||
		strings.Contains(strings.ToLower(summary.Agent), query) ||
		strings.Contains(strings.ToLower(summary.Model), query) ||
		strings.Contains(strings.ToLower(summary.ID), query) {
		return true
	}
	for _, event := range sess.Events() {
		if strings.Contains(strings.ToLower(event.Content), query) {
			return true
		}
	}
	return false
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	sess, err := session.Find(session.KindAgent, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": summarizeSession(sess),
		"events":  sess.Events(),
	})
}

func summarizeSession(sess *session.Session) SessionSummary {
	turns := 0
	for _, ev := range sess.Events() {
		if ev.Type == session.EventUser {
			turns++
		}
	}
	return SessionSummary{
		ID:      sess.ID,
		Agent:   sess.Agent,
		Model:   sess.Model,
		Title:   sessionTitle(sess),
		Turns:   turns,
		Updated: sess.Updated,
	}
}

// sessionTitle uses the first user message, falling back to the agent name.
func sessionTitle(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	for _, ev := range sess.Events() {
		if ev.Type == session.EventUser && strings.TrimSpace(ev.Content) != "" {
			return firstLine(ev.Content)
		}
	}
	if title := firstLine(sess.LastUserMessage()); title != "" {
		return title
	}
	return sess.Agent
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	runes := []rune(text)
	if len(runes) > 60 {
		return string(runes[:57]) + "..."
	}
	return text
}

func sortedAgentNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// newToken creates a short random identifier for pending interactions.
func newToken(prefix string) string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf[:])
}
