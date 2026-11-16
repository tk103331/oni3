// Package acp implements the Agent Client Protocol (ACP) for Oni3 using
// the official Go SDK (github.com/coder/acp-go-sdk).
//
// ACP is an open standard (https://agentclientprotocol.com) that standardizes
// communication between code editors/IDEs (clients) and coding agents.
// Messages are JSON-RPC 2.0 objects, one per line, exchanged over stdio.
package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/coder/acp-go-sdk"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
	"github.com/tk103331/oni3/tools"
	"github.com/tk103331/oni3/version"
)

// Service is the Oni3 ACP agent. It bridges ACP sessions to the
// configured Eino ReAct agent and streams turns to the client.
type Service struct {
	agentName string
	cfg       *config.Config
	conn      *acp.AgentSideConnection

	mu       sync.Mutex
	sessions map[string]*acpSession
	nextSeq  int
}

var _ acp.Agent = (*Service)(nil)

// session holds one ACP conversation context.
type acpSession struct {
	id      string
	cwd     string
	agent   agent.Agent
	history []*schema.Message // prior turns, replayed into every prompt
	store   *sessionStore     // on-disk persistence shared with the TUI
	titled  bool              // first-prompt title already reported

	// model/skill overrides chosen through session/set_config_option; they
	// rebuild the eino agent for the next prompt.
	modelOverride string
	skillOverride string

	running        bool
	toolSeq        int
	lastStartKey   string // consecutive duplicate start events dedupe key
	lastToolCallID acp.ToolCallId
	finalized      bool // whether the current tool call already got a final update

	// toolCalls maps the tool call id of the agent to the id reported to the
	// client, so a result is paired with the call it belongs to.
	toolCalls map[string]acp.ToolCallId
}

// NewService creates an ACP agent bound to the given configured agent name.
func NewService(agentName string, cfg *config.Config) *Service {
	return &Service{
		agentName: agentName,
		cfg:       cfg,
		sessions:  make(map[string]*acpSession),
	}
}

// SetAgentConnection implements acp.AgentConnAware; the SDK calls it right
// after constructing the connection.
func (s *Service) SetAgentConnection(conn *acp.AgentSideConnection) {
	s.conn = conn
}

// Initialize negotiates the protocol version and capabilities.
func (s *Service) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	logger.Info("ACP", fmt.Sprintf("initialize: client=%s requested version=%d",
		clientName(params.ClientInfo), params.ProtocolVersion))

	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentInfo: &acp.Implementation{
			Name:    "oni3",
			Version: version.Version,
		},
		AgentCapabilities: acp.AgentCapabilities{
			// Conversations persist to the oni3 session store and can be
			// listed, loaded and resumed across runs.
			LoadSession:        true,
			PromptCapabilities: acp.PromptCapabilities{},
			McpCapabilities:    acp.McpCapabilities{},
			SessionCapabilities: acp.SessionCapabilities{
				Close:  &acp.SessionCloseCapabilities{},
				List:   &acp.SessionListCapabilities{},
				Resume: &acp.SessionResumeCapabilities{},
			},
		},
		AuthMethods: []acp.AuthMethod{},
	}, nil
}

// NewSession creates a session backed by the configured agent.
func (s *Service) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	einoAgent, err := agent.NewFactory(s.cfg).CreateAgent(s.agentName)
	if err != nil {
		logger.Error("ACP", fmt.Sprintf("failed to create agent: %v", err))
		return acp.NewSessionResponse{}, fmt.Errorf("failed to create agent: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSeq++
	sid := fmt.Sprintf("sess-%d", s.nextSeq)
	s.sessions[sid] = &acpSession{
		id:    sid,
		cwd:   params.Cwd,
		agent: einoAgent,
		store: newSessionStore(s.agentName, s.currentModelName()),
	}
	if len(params.McpServers) > 0 {
		// MCP servers come from config.yml; client-provided ones are ignored.
		names := make([]string, 0, len(params.McpServers))
		for _, server := range params.McpServers {
			names = append(names, mcpServerName(server))
		}
		logger.Warn("ACP", fmt.Sprintf("client-provided MCP servers ignored (using config.yml): %s",
			strings.Join(names, ", ")))
	}

	logger.Info("ACP", fmt.Sprintf("session created: %s (cwd=%s, agent=%s)", sid, params.Cwd, s.agentName))

	// Advertise the slash commands so the client can show its palette.
	s.sendAvailableCommands(ctx, sid)

	return acp.NewSessionResponse{
		SessionId:     acp.SessionId(sid),
		ConfigOptions: s.sessionConfigOptions(s.sessions[sid]),
	}, nil
}

// currentModelName is the model the configured agent resolves to.
func (s *Service) currentModelName() string {
	if ac, ok := s.cfg.Agents[s.agentName]; ok && ac.Model != "" {
		return ac.Model
	}
	return s.cfg.DefaultModelName()
}

// Prompt runs one prompt turn, streaming updates back to the client.
// The SDK cancels the returned context when it receives session/cancel.
func (s *Service) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sid := string(params.SessionId)

	s.mu.Lock()
	session, ok := s.sessions[sid]
	if ok && session.running {
		s.mu.Unlock()
		return acp.PromptResponse{}, fmt.Errorf("prompt already in progress for session %s", sid)
	}
	if !ok {
		s.mu.Unlock()
		return acp.PromptResponse{}, fmt.Errorf("unknown session: %s", sid)
	}
	session.running = true
	session.toolSeq = 0
	session.lastStartKey = ""
	session.lastToolCallID = ""
	session.finalized = false
	session.toolCalls = make(map[string]acp.ToolCallId)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		session.running = false
		s.mu.Unlock()
	}()

	prompt := flattenContentBlocks(params.Prompt)
	if strings.TrimSpace(prompt) == "" {
		return acp.PromptResponse{}, fmt.Errorf("prompt contains no text content")
	}

	// Advertised slash commands come back as ordinary prompt text starting
	// with "/"; answer them directly instead of calling the model.
	if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		reply := s.runSlashCommand(ctx, session, prompt)
		s.sendUpdate(ctx, sid, acp.UpdateUserMessageText(prompt))
		s.sendUpdate(ctx, sid, acp.UpdateAgentMessageText(reply))
		session.store.addUser(prompt)
		session.store.addAssistant(reply)
		s.mu.Lock()
		session.history = append(session.history, schema.UserMessage(prompt), schema.AssistantMessage(reply, nil))
		s.mu.Unlock()
		logger.Info("ACP", fmt.Sprintf("slash command handled: session=%s text=%q", sid, truncate(prompt, 200)))
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	logger.Info("ACP", fmt.Sprintf("prompt received: session=%s text=%q", sid, truncate(prompt, 200)))

	// Echo the user's message back so clients that render the conversation
	// from agent updates show it too.
	s.sendUpdate(ctx, sid, acp.UpdateUserMessageText(prompt))

	// Replay this session's prior turns so the conversation keeps its context.
	s.mu.Lock()
	history := append([]*schema.Message(nil), session.history...)
	s.mu.Unlock()

	// The interactive tools (ask_user, todo_list, approvals) reach the
	// client through the host instead of failing "interactive only".
	hostCtx := tools.WithHost(ctx, &acpHost{svc: s, session: session})

	var answer strings.Builder
	var thought strings.Builder
	chunkCallback := s.makeChunkCallback(ctx, sid)
	err := session.agent.ChatStream(hostCtx, history, prompt,
		func(chunk *agent.StreamChunk) {
			if chunk.Type == "reasoning" && chunk.Content != "" {
				thought.WriteString(chunk.Content)
			}
			if chunk.Type == "content" && chunk.Content != "" {
				answer.WriteString(chunk.Content)
			}
			chunkCallback(chunk)
		},
		s.makeToolCallback(ctx, sid, session),
	)

	if err != nil {
		session.store.flushThought(&thought)
		session.store.addUser(prompt)
		if ctx.Err() != nil {
			logger.Info("ACP", fmt.Sprintf("prompt cancelled: session=%s", sid))
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		logger.Error("ACP", fmt.Sprintf("prompt failed: session=%s error=%v", sid, err))
		session.store.addAssistant("error: " + err.Error())
		s.sendUpdate(ctx, sid, acp.UpdateAgentMessageText(fmt.Sprintf("\n\n❌ Agent error: %v", err)))
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	// Record the completed turn for the next prompt and for /resume.
	session.store.flushThought(&thought)
	session.store.addUser(prompt)
	if text := strings.TrimSpace(answer.String()); text != "" {
		session.store.addAssistant(text)
	}

	s.mu.Lock()
	session.history = append(session.history, schema.UserMessage(prompt))
	if text := strings.TrimSpace(answer.String()); text != "" {
		session.history = append(session.history, schema.AssistantMessage(text, nil))
	}
	s.mu.Unlock()

	// Give the client a human-readable title derived from the first prompt.
	if !session.titled && session.store != nil {
		session.titled = true
		title := firstLine(prompt)
		if title != "" {
			s.sendUpdate(ctx, sid, acp.SessionUpdate{
				SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
					Title: strPtr(fmt.Sprintf("%s · %s", s.agentName, title)),
				},
			})
		}
	}

	logger.Info("ACP", fmt.Sprintf("prompt completed: session=%s", sid))
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// Cancel aborts the running prompt turn. The SDK has already cancelled the
// prompt context before calling this method.
func (s *Service) Cancel(ctx context.Context, params acp.CancelNotification) error {
	logger.Info("ACP", fmt.Sprintf("cancel received: session=%s", string(params.SessionId)))
	return nil
}

// Authenticate is unused: the agent advertises no auth methods.
func (s *Service) Authenticate(ctx context.Context, params acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

// ---- session lifecycle: list / load / close ----

// ListSessions reports the persisted agent conversations so clients can
// offer "resume with oni3" across runs.
func (s *Service) ListSessions(ctx context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	stored := listStoredSessions(50)
	infos := make([]acp.SessionInfo, 0, len(stored))
	for _, st := range stored {
		updated := st.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		infos = append(infos, acp.SessionInfo{
			SessionId: acp.SessionId(st.ID),
			Cwd:       st.Cwd,
			Title:     strPtr(st.Title),
			UpdatedAt: &updated,
		})
	}
	return acp.ListSessionsResponse{Sessions: infos}, nil
}

// LoadSession replays a persisted conversation into a live ACP session. The
// client keeps using the id it passed; the on-disk conversation continues.
func (s *Service) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	store := loadSessionStore(string(params.SessionId))
	if store == nil {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(map[string]any{
			"error": fmt.Sprintf("session not found: %s", string(params.SessionId)),
		})
	}

	// A model recorded in the persisted conversation (a runtime switch)
	// carries over instead of the configured default.
	modelOverride := store.model()
	factory := agent.NewFactory(s.cfg)
	if modelOverride != "" {
		factory = factory.WithModel(modelOverride)
	}
	einoAgent, err := factory.CreateAgent(s.agentName)
	if err != nil {
		return acp.LoadSessionResponse{}, fmt.Errorf("failed to create agent: %w", err)
	}

	history := store.history()
	s.mu.Lock()
	loaded := &acpSession{
		id:            string(params.SessionId),
		cwd:           params.Cwd,
		agent:         einoAgent,
		history:       append([]*schema.Message(nil), history...),
		store:         store,
		titled:        true,
		modelOverride: modelOverride,
	}
	s.sessions[string(params.SessionId)] = loaded
	options := s.sessionConfigOptions(loaded)
	s.mu.Unlock()

	logger.Info("ACP", fmt.Sprintf("session loaded: %s (events=%d, model=%s)", string(params.SessionId), len(history), modelOverride))

	// Advertise the slash commands so the client can show its palette.
	s.sendAvailableCommands(ctx, string(params.SessionId))

	return acp.LoadSessionResponse{ConfigOptions: options}, nil
}

// CloseSession releases the in-memory state; the persisted conversation
// stays on disk for later resume.
func (s *Service) CloseSession(ctx context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	s.mu.Lock()
	delete(s.sessions, string(params.SessionId))
	s.mu.Unlock()
	logger.Info("ACP", fmt.Sprintf("session closed: %s", string(params.SessionId)))
	return acp.CloseSessionResponse{}, nil
}

// ResumeSession continues a stored conversation under a fresh ACP session
// id. Unlike session/load it does not replay the previous messages to the
// client; the history is present as model context and new turns append to
// the same on-disk conversation.
func (s *Service) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	store := loadSessionStore(string(params.SessionId))
	if store == nil {
		return acp.ResumeSessionResponse{}, acp.NewInvalidParams(map[string]any{
			"error": fmt.Sprintf("session not found: %s", string(params.SessionId)),
		})
	}
	// The persisted session keeps its on-disk identity; the ACP session id is
	// a fresh one so the client can treat it like a new session.
	s.mu.Lock()
	s.nextSeq++
	sid := fmt.Sprintf("sess-%d", s.nextSeq)
	loaded := &acpSession{
		id:      sid,
		cwd:     params.Cwd,
		history: append([]*schema.Message(nil), store.history()...),
		store:   store,
		titled:  true,
	}
	// A model recorded in the persisted conversation (a runtime /model or
	// config-option switch) carries over instead of the configured default.
	if model := store.model(); model != "" {
		loaded.modelOverride = model
	}
	s.sessions[sid] = loaded
	options := s.sessionConfigOptions(loaded)
	s.mu.Unlock()

	factory := agent.NewFactory(s.cfg)
	if loaded.modelOverride != "" {
		factory = factory.WithModel(loaded.modelOverride)
	}
	einoAgent, err := factory.CreateAgent(s.agentName)
	if err != nil {
		s.mu.Lock()
		delete(s.sessions, sid)
		s.mu.Unlock()
		return acp.ResumeSessionResponse{}, fmt.Errorf("failed to create agent: %w", err)
	}
	s.mu.Lock()
	loaded.agent = einoAgent
	s.mu.Unlock()

	logger.Info("ACP", fmt.Sprintf("session resumed: %s -> %s (events=%d, model=%s)",
		string(params.SessionId), sid, len(loaded.history), loaded.modelOverride))

	// Advertise the slash commands so the client can show its palette.
	s.sendAvailableCommands(ctx, sid)

	return acp.ResumeSessionResponse{ConfigOptions: options}, nil
}

// ---- unsupported optional methods ----

func (s *Service) Logout(ctx context.Context, params acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound(acp.AgentMethodLogout)
}

func (s *Service) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetMode)
}

// ---- callbacks ----

// makeChunkCallback converts stream chunks into session updates.
// Reasoning is reported as agent_thought_chunk; error chunks are logged but
// not forwarded (the prompt handler emits the final error once).
func (s *Service) makeChunkCallback(ctx context.Context, sid string) func(*agent.StreamChunk) {
	return func(chunk *agent.StreamChunk) {
		switch chunk.Type {
		case "content":
			if chunk.Content == "" {
				return
			}
			s.sendUpdate(ctx, sid, acp.UpdateAgentMessageText(chunk.Content))
		case "reasoning":
			if chunk.Content == "" {
				return
			}
			s.sendUpdate(ctx, sid, acp.UpdateAgentThoughtText(chunk.Content))
		case "error":
			logger.Warn("ACP", fmt.Sprintf("stream error chunk: session=%s %s", sid, chunk.Content))
		}
	}
}

// makeToolCallback converts tool call callbacks into tool_call updates.
// Internal ReAct node events (ChatModel, Tools orchestrator) are filtered out
// so only real tool invocations are reported to the client.
func (s *Service) makeToolCallback(ctx context.Context, sid string, session *acpSession) func(interface{}) {
	return func(data interface{}) {
		info, ok := data.(agent.ToolCallInfo)
		if !ok {
			logger.Debug("ACP", fmt.Sprintf("ignoring tool callback payload: %T", data))
			return
		}
		if isInternalComponent(info.Name) {
			logger.Debug("ACP", fmt.Sprintf("filtered internal component: %s", info.Name))
			return
		}

		switch info.Type {
		case "start":
			s.mu.Lock()
			// The eino callback layer can emit the same tool start event
			// more than once; skip consecutive duplicates.
			startKey := info.Name + "|" + info.Arguments
			if info.CallID != "" {
				startKey = info.CallID
			}
			if startKey == session.lastStartKey {
				s.mu.Unlock()
				return
			}
			session.lastStartKey = startKey
			session.finalized = false
			session.toolSeq++
			toolCallID := acp.ToolCallId(fmt.Sprintf("call-%s-%d", sid, session.toolSeq))
			session.lastToolCallID = toolCallID
			if info.CallID != "" {
				if session.toolCalls == nil {
					session.toolCalls = make(map[string]acp.ToolCallId)
				}
				session.toolCalls[info.CallID] = toolCallID
			}
			s.mu.Unlock()

			opts := []acp.ToolCallStartOpt{
				acp.WithStartKind(toolKind(info.Name)),
				acp.WithStartStatus(acp.ToolCallStatusInProgress),
			}
			if info.Arguments != "" && jsonValid(info.Arguments) {
				opts = append(opts, acp.WithStartRawInput(jsonMap(info.Arguments)))
			}
			logger.Info("ACP", fmt.Sprintf("tool_call start: session=%s id=%s name=%s", sid, toolCallID, info.Name))
			s.sendUpdate(ctx, sid, acp.StartToolCall(toolCallID, buildToolTitle(info.Name, info.Arguments), opts...))
			session.store.addToolStart(info.Name, info.Arguments)

		case "end", "error":
			s.mu.Lock()
			toolCallID := session.lastToolCallID
			// A result with a tool call id belongs to that call, even when
			// other calls ran in parallel.
			paired := false
			if info.CallID != "" {
				if id, ok := session.toolCalls[info.CallID]; ok {
					toolCallID = id
					paired = true
				}
			}
			if toolCallID == "" || (!paired && session.finalized) {
				// No start event seen, or this call already got its final
				// update: the eino callback layer may emit duplicate
				// completion/error events for the same call.
				s.mu.Unlock()
				return
			}
			if paired {
				delete(session.toolCalls, info.CallID)
			} else {
				session.finalized = true
			}
			s.mu.Unlock()

			var update acp.SessionUpdate
			if info.Type == "end" {
				opts := []acp.ToolCallUpdateOpt{
					acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
					acp.WithUpdateContent([]acp.ToolCallContent{
						acp.ToolContent(acp.TextBlock(truncate(info.Result, 8000))),
					}),
				}
				if info.Result != "" && jsonValid(info.Result) {
					opts = append(opts, acp.WithUpdateRawOutput(jsonMap(info.Result)))
				}
				logger.Info("ACP", fmt.Sprintf("tool_call end: session=%s name=%s", sid, info.Name))
				update = acp.UpdateToolCall(toolCallID, opts...)
			} else {
				logger.Warn("ACP", fmt.Sprintf("tool_call error: session=%s name=%s error=%s", sid, info.Name, info.Error))
				update = acp.UpdateToolCall(toolCallID,
					acp.WithUpdateStatus(acp.ToolCallStatusFailed),
					acp.WithUpdateContent([]acp.ToolCallContent{
						acp.ToolContent(acp.TextBlock(truncate("error: "+info.Error, 4000))),
					}),
				)
			}
			s.sendUpdate(ctx, sid, update)
			// Persist the call result so a resumed conversation keeps the
			// tool record too, not just the text turns.
			if info.Type == "end" {
				session.store.addToolEnd(info.Name, info.Result, false)
			} else {
				session.store.addToolEnd(info.Name, info.Error, true)
			}
		}
	}
}

// sendUpdate wraps and sends a session/update notification. Without a
// connection (unit tests) it is a no-op.
func (s *Service) sendUpdate(ctx context.Context, sid string, update acp.SessionUpdate) {
	if s.conn == nil {
		return
	}
	if err := s.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update:    update,
	}); err != nil {
		logger.Warn("ACP", fmt.Sprintf("failed to send session update: %v", err))
	}
}

// ---- helpers ----

// flattenContentBlocks converts prompt content blocks to plain text for the model.
func flattenContentBlocks(blocks []acp.ContentBlock) string {
	var parts []string
	for _, block := range blocks {
		switch {
		case block.Text != nil:
			if block.Text.Text != "" {
				parts = append(parts, block.Text.Text)
			}
		case block.ResourceLink != nil:
			link := block.ResourceLink.Uri
			if block.ResourceLink.Name != "" {
				link = block.ResourceLink.Name + " (" + block.ResourceLink.Uri + ")"
			}
			parts = append(parts, "[Referenced: "+link+"]")
			if block.ResourceLink.Description != nil && *block.ResourceLink.Description != "" {
				parts = append(parts, *block.ResourceLink.Description)
			}
		case block.Resource != nil:
			res := block.Resource.Resource
			if res.TextResourceContents != nil && res.TextResourceContents.Text != "" {
				parts = append(parts, "[Resource "+res.TextResourceContents.Uri+"]\n"+res.TextResourceContents.Text)
			} else if res.BlobResourceContents != nil {
				mime := ""
				if res.BlobResourceContents.MimeType != nil {
					mime = ", " + *res.BlobResourceContents.MimeType
				}
				parts = append(parts, fmt.Sprintf("[Resource %s%s, %d bytes base64]",
					res.BlobResourceContents.Uri, mime, len(res.BlobResourceContents.Blob)))
			}
		case block.Image != nil, block.Audio != nil:
			parts = append(parts, "[Unsupported content omitted]")
		}
	}
	return strings.Join(parts, "\n")
}

// buildToolTitle renders a short human-readable title for a tool call.
func buildToolTitle(name, args string) string {
	if args == "" || args == "{}" {
		return name
	}
	if m := jsonMap(args); m != nil {
		var kv []string
		for k, v := range m {
			kv = append(kv, fmt.Sprintf("%s=%v", k, v))
		}
		if len(kv) > 0 {
			return fmt.Sprintf("%s %s", name, strings.Join(kv, " "))
		}
	}
	return fmt.Sprintf("%s %s", name, truncate(args, 80))
}

// toolKind maps tool names to ACP tool kinds for better client icons.
func toolKind(name string) acp.ToolKind {
	n := strings.ToLower(name)
	switch {
	case containsAny(n, "commandline", "exec", "shell", "command"):
		return acp.ToolKindExecute
	case containsAny(n, "search", "wikipedia", "duckduckgo", "bing", "google"):
		return acp.ToolKindSearch
	case containsAny(n, "http", "browser", "fetch", "request"):
		return acp.ToolKindFetch
	case containsAny(n, "think", "reason", "plan"):
		return acp.ToolKindThink
	case containsAny(n, "read", "file"):
		return acp.ToolKindRead
	case containsAny(n, "edit", "write"):
		return acp.ToolKindEdit
	default:
		return acp.ToolKindOther
	}
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// truncate cuts a string to maxLen with an ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// isInternalComponent reports whether a callback name refers to an internal
// ReAct node that should not surface as a tool call.
func isInternalComponent(name string) bool {
	return name == "ChatModel" || name == "Tools"
}

// jsonValid reports whether s is valid JSON.
func jsonValid(s string) bool {
	return json.Valid([]byte(s))
}

// jsonMap decodes a JSON object into map[string]any, or nil on failure.
func jsonMap(s string) map[string]any {
	if !json.Valid([]byte(s)) {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// clientName renders the client implementation name for logging.
func clientName(info *acp.Implementation) string {
	if info == nil || info.Name == "" {
		return "unknown"
	}
	if info.Version != "" {
		return info.Name + " " + info.Version
	}
	return info.Name
}

// mcpServerName extracts the display name of a client-provided MCP server.
func mcpServerName(server acp.McpServer) string {
	switch {
	case server.Http != nil:
		return server.Http.Name
	case server.Sse != nil:
		return server.Sse.Name
	case server.Acp != nil:
		return server.Acp.Name
	default:
		return "unnamed"
	}
}
