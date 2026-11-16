package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tk103331/oni3/session"
	"github.com/tk103331/oni3/tools"
	"github.com/tk103331/oni3/tools/approval"
)

// hostResponse is one answer to a pending ask_user or approval prompt.
type hostResponse struct {
	Value    string
	Approved bool
	Remember bool
}

// webHost bridges interactive tools to the browser. Tool execution blocks
// until the page posts to /api/respond, so ask_user and approvals behave like
// they do in the TUI instead of silently continuing.
type webHost struct {
	server    *Server
	stream    *sseWriter
	store     *session.Session
	sessionID string
}

func (h *webHost) AskUser(ctx context.Context, question string, options []string) (string, error) {
	id := newToken("question")
	ch := h.server.registerPending(id)
	defer h.server.unregisterPending(id)

	h.stream.send("question", map[string]any{
		"id":       id,
		"question": question,
		"options":  options,
	})

	select {
	case resp := <-ch:
		answer := strings.TrimSpace(resp.Value)
		_ = h.store.AddQuestion(question, answer)
		return answer, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *webHost) ApproveTool(ctx context.Context, req approval.Request) (bool, error) {
	if h.server.isApproved(h.sessionID, req.Tool) {
		return true, nil
	}

	id := newToken("approval")
	ch := h.server.registerPending(id)
	defer h.server.unregisterPending(id)

	h.stream.send("approval", map[string]any{
		"id":      id,
		"tool":    req.Tool,
		"summary": req.Summary,
		"detail":  req.Detail,
	})

	select {
	case resp := <-ch:
		if resp.Approved && resp.Remember {
			h.server.rememberApproval(h.sessionID, req.Tool)
		}
		return resp.Approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (h *webHost) UpdateTodos(ctx context.Context, items []tools.TodoItem) error {
	persisted := make([]session.TodoItem, 0, len(items))
	payload := make([]map[string]string, 0, len(items))
	for _, item := range items {
		persisted = append(persisted, session.TodoItem{Content: item.Content, Status: item.Status})
		payload = append(payload, map[string]string{"content": item.Content, "status": item.Status})
	}
	_ = h.store.AddTodos(persisted)
	h.stream.send("todos", map[string]any{"items": payload})
	return nil
}

func (s *Server) registerPending(id string) chan hostResponse {
	ch := make(chan hostResponse, 1)
	s.pendingMu.Lock()
	s.pending[id] = ch
	s.pendingMu.Unlock()
	return ch
}

func (s *Server) unregisterPending(id string) {
	s.pendingMu.Lock()
	delete(s.pending, id)
	s.pendingMu.Unlock()
}

type respondRequest struct {
	ID       string `json:"id"`
	Value    string `json:"value"`
	Approved bool   `json:"approved"`
	Remember bool   `json:"remember"`
}

func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request) {
	var req respondRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	s.pendingMu.Lock()
	ch, ok := s.pending[id]
	s.pendingMu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "interaction is no longer pending")
		return
	}

	select {
	case ch <- hostResponse{Value: req.Value, Approved: req.Approved, Remember: req.Remember}:
	default:
		writeError(w, http.StatusConflict, "interaction was already answered")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) isApproved(sessionID, toolName string) bool {
	s.approvedMu.Lock()
	defer s.approvedMu.Unlock()
	return s.approved[sessionID][toolName]
}

func (s *Server) rememberApproval(sessionID, toolName string) {
	s.approvedMu.Lock()
	defer s.approvedMu.Unlock()
	if s.approved[sessionID] == nil {
		s.approved[sessionID] = make(map[string]bool)
	}
	s.approved[sessionID][toolName] = true
}
