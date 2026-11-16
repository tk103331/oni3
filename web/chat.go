package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/session"
	"github.com/tk103331/oni3/tools"
)

const maxChatBody = 1 << 20

type chatRequest struct {
	Agent     string `json:"agent"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model"`
	SessionID string `json:"session_id"`
}

type cancelRequest struct {
	SessionID string `json:"session_id"`
}

// sseWriter serializes events and flushes each one so the browser receives
// streamed tokens immediately.
type sseWriter struct {
	w      http.ResponseWriter
	flush  http.Flusher
	mu     sync.Mutex
	broken bool
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming is not supported by this connection")
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseWriter{w: w, flush: flusher}, nil
}

func (e *sseWriter) emit(event map[string]any) {
	if e == nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.broken {
		return
	}
	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", data); err != nil {
		e.broken = true
		return
	}
	e.flush.Flush()
}

func (e *sseWriter) send(kind string, payload map[string]any) {
	event := map[string]any{"type": kind}
	for key, value := range payload {
		event[key] = value
	}
	e.emit(event)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxChatBody))
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	req.Agent = strings.TrimSpace(req.Agent)
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Model = strings.TrimSpace(req.Model)
	req.SessionID = strings.TrimSpace(req.SessionID)

	if req.Agent == "" {
		writeError(w, http.StatusBadRequest, "agent is required")
		return
	}
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	agentCfg, ok := s.cfg.Agents[req.Agent]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown agent: "+req.Agent)
		return
	}
	if req.Model != "" {
		if _, ok := s.cfg.Models[req.Model]; !ok {
			writeError(w, http.StatusBadRequest, "unknown model: "+req.Model)
			return
		}
	}

	effectiveModel := req.Model
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(agentCfg.Model)
	}
	if effectiveModel == "" {
		effectiveModel = s.cfg.DefaultModelName()
	}
	if effectiveModel == "" {
		writeError(w, http.StatusBadRequest, "no model is configured; add one before chatting")
		return
	}
	if _, ok := s.cfg.Models[effectiveModel]; !ok {
		writeError(w, http.StatusBadRequest, "model is not configured: "+effectiveModel)
		return
	}

	var sess *session.Session
	if req.SessionID != "" {
		loaded, err := session.Find(session.KindAgent, req.SessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if loaded.Agent != "" && loaded.Agent != req.Agent {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("session %s belongs to agent %s", loaded.ID, loaded.Agent))
			return
		}
		sess = loaded
	} else {
		sess = session.New(session.KindAgent, req.Agent, effectiveModel, "")
	}
	if sess.Model != effectiveModel {
		_ = sess.SetConfig(req.Agent, effectiveModel, sess.Skill)
	}

	ctx, cancel := context.WithCancel(r.Context())
	if !s.startRun(sess.ID, cancel) {
		cancel()
		writeError(w, http.StatusConflict, "this session is already running")
		return
	}
	defer func() {
		s.endRun(sess.ID)
		cancel()
	}()

	stream, err := newSSEWriter(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	stream.send("session", map[string]any{
		"id":      sess.ID,
		"agent":   req.Agent,
		"model":   effectiveModel,
		"workdir": s.workDir,
	})

	factory := agent.NewFactory(s.cfg)
	if req.Model != "" {
		factory = factory.WithModel(req.Model)
	}
	instance, err := factory.CreateAgent(req.Agent)
	if err != nil {
		stream.send("error", map[string]any{"message": err.Error()})
		stream.send("done", map[string]any{"stop": "error"})
		return
	}

	host := &webHost{server: s, stream: stream, store: sess, sessionID: sess.ID}
	ctx = tools.WithHost(ctx, host)

	history := sessionHistory(sess)
	_ = sess.AddUser(req.Prompt)
	var answer strings.Builder
	var thought strings.Builder
	errorReported := false

	err = instance.ChatStream(ctx, history, req.Prompt,
		func(chunk *agent.StreamChunk) {
			if chunk == nil {
				return
			}
			switch chunk.Type {
			case "content":
				if chunk.Content == "" {
					return
				}
				answer.WriteString(chunk.Content)
				stream.send("content", map[string]any{"text": chunk.Content})
			case "reasoning":
				if chunk.Content == "" {
					return
				}
				thought.WriteString(chunk.Content)
				stream.send("reasoning", map[string]any{"text": chunk.Content})
			case "error":
				errorReported = true
				stream.send("error", map[string]any{"message": chunk.Content})
			}
		},
		func(payload interface{}) {
			info, ok := payload.(agent.ToolCallInfo)
			if !ok {
				return
			}
			s.reportTool(stream, sess, info)
		},
	)

	if thought.Len() > 0 {
		_ = sess.AddReasoning(thought.String())
	}
	if text := strings.TrimSpace(answer.String()); text != "" {
		_ = sess.AddAssistant(text)
	}

	switch {
	case err != nil && (errors.Is(err, context.Canceled) || ctx.Err() != nil):
		stream.send("done", map[string]any{"stop": "cancelled"})
	case err != nil:
		if !errorReported {
			stream.send("error", map[string]any{"message": err.Error()})
		}
		stream.send("done", map[string]any{"stop": "error"})
	default:
		stream.send("done", map[string]any{
			"stop":       "end",
			"session_id": sess.ID,
		})
	}
}

// reportTool forwards one tool lifecycle event and records it in the session.
func (s *Server) reportTool(stream *sseWriter, sess *session.Session, info agent.ToolCallInfo) {
	switch info.Type {
	case "start":
		_ = sess.AddToolStart(info.Name, info.Arguments)
		stream.send("tool", map[string]any{
			"phase":     "start",
			"name":      info.Name,
			"call_id":   info.CallID,
			"arguments": info.Arguments,
		})
	case "end":
		failed := agent.IsToolError(info.Result)
		_ = sess.AddToolEnd(info.Name, info.Result, failed)
		stream.send("tool", map[string]any{
			"phase":   "end",
			"name":    info.Name,
			"call_id": info.CallID,
			"result":  info.Result,
			"failed":  failed,
		})
	case "error":
		_ = sess.AddToolEnd(info.Name, info.Error, true)
		stream.send("tool", map[string]any{
			"phase":   "end",
			"name":    info.Name,
			"call_id": info.CallID,
			"result":  info.Error,
			"failed":  true,
		})
	}
}

// sessionHistory replays the persisted user/assistant turns as chat context.
func sessionHistory(sess *session.Session) []*schema.Message {
	if sess == nil {
		return nil
	}
	events := sess.Events()
	out := make([]*schema.Message, 0, len(events))
	for _, ev := range events {
		switch ev.Type {
		case session.EventUser:
			if strings.TrimSpace(ev.Content) != "" {
				out = append(out, schema.UserMessage(ev.Content))
			}
		case session.EventAssistant:
			if strings.TrimSpace(ev.Content) != "" {
				out = append(out, schema.AssistantMessage(ev.Content, nil))
			}
		}
	}
	return out
}

func (s *Server) startRun(id string, cancel context.CancelFunc) bool {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if _, busy := s.runs[id]; busy {
		return false
	}
	s.runs[id] = cancel
	return true
}

func (s *Server) endRun(id string) {
	s.runsMu.Lock()
	delete(s.runs, id)
	s.runsMu.Unlock()
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req cancelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	id := strings.TrimSpace(req.SessionID)
	if id == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	s.runsMu.Lock()
	cancel, ok := s.runs[id]
	s.runsMu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "session is not running")
		return
	}
	cancel()
	w.WriteHeader(http.StatusNoContent)
}
