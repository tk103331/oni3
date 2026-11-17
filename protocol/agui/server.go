package agui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tk103331/oni3/runtime"
)

const (
	maxBody          = 4 << 20
	defaultInterrupt = 10 * time.Minute
)

// Server serves an AG-UI endpoint backed by the shared runtime.
type Server struct {
	runner       *runtime.Runner
	defaultAgent string

	mu   sync.Mutex
	runs map[string]*runState
}

// New creates an AG-UI server.
func New(runner *runtime.Runner, defaultAgent string) (*Server, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}
	return &Server{
		runner:       runner,
		defaultAgent: strings.TrimSpace(defaultAgent),
		runs:         make(map[string]*runState),
	}, nil
}

type pendingInterrupt struct {
	id      string
	kind    string
	request runtime.InterruptData
	ch      chan runtime.Response
}

type toolState struct {
	name      string
	arguments string
}

type runState struct {
	key      string
	threadID string
	runID    string
	agent    string

	mu      sync.Mutex
	emitter *Emitter
	parked  bool
	pending *pendingInterrupt
	parkCh  chan struct{}
	done    chan struct{}
	tools   map[string]toolState
	seq     int

	errSent  bool
	errText  string
	finished bool
	cancel   context.CancelFunc
}

func runKey(threadID, runID string) string {
	return threadID + "\x00" + runID
}

// Handler returns the AG-UI HTTP handler. agentOverride is used by
// /agents/{name}/agui routes and otherwise comes from forwardedProps.
func (s *Server) Handler(agentOverride string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		var input RunAgentInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
			return
		}
		if err := ensureSingleJSON(decoder); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		if len(input.Resume) > 0 {
			s.handleResume(w, r, input)
			return
		}
		s.handleRun(w, r, input, agentOverride)
	})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request, input RunAgentInput, agentOverride string) {
	threadID := strings.TrimSpace(input.ThreadID)
	if threadID == "" {
		threadID = "thread-" + randomID()
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = "run-" + randomID()
	}

	agentName := firstString(agentOverride, propString(input.ForwardedProps, "agent"), s.defaultAgent)
	if agentName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent is required"})
		return
	}
	model := propString(input.ForwardedProps, "model")
	if err := s.runner.Validate(agentName, model); err != nil {
		writeRuntimeError(w, err)
		return
	}

	history, prompt, err := History(input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(history) == 0 {
		history = nil
	}

	rs := &runState{
		key:      runKey(threadID, runID),
		threadID: threadID,
		runID:    runID,
		agent:    agentName,
		tools:    make(map[string]toolState),
		parkCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}

	s.mu.Lock()
	if _, exists := s.runs[rs.key]; exists {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this thread and run are already active"})
		return
	}
	s.runs[rs.key] = rs
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	rs.cancel = cancel

	emitter, err := NewEmitter(w, threadID, runID, rs.newID, cancel)
	if err != nil {
		s.removeRun(rs)
		cancel()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rs.mu.Lock()
	rs.emitter = emitter
	rs.mu.Unlock()
	emitter.RunStarted()

	req := runtime.Request{
		Agent:     agentName,
		Prompt:    prompt,
		Model:     model,
		SessionID: threadID,
		History:   history,
	}

	go func() {
		defer close(rs.done)
		defer s.removeRun(rs)
		result, runErr := s.runner.Run(ctx, req, s.emitterFor(rs), rs)
		rs.mu.Lock()
		rs.finished = true
		rs.mu.Unlock()
		if runErr != nil {
			rs.mu.Lock()
			sent := rs.errSent
			em := rs.emitter
			rs.emitter = nil
			if !sent && em != nil {
				rs.errSent = true
			}
			rs.mu.Unlock()
			if !sent && em != nil {
				em.RunError(runErr.Error())
			}
		}
		_ = result
		cancel()
	}()

	s.wait(r.Context(), rs)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request, input RunAgentInput) {
	threadID := strings.TrimSpace(input.ThreadID)
	runID := strings.TrimSpace(input.RunID)
	if threadID == "" || runID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "threadId and runId are required to resume"})
		return
	}

	s.mu.Lock()
	rs := s.runs[runKey(threadID, runID)]
	s.mu.Unlock()
	if rs == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run is not waiting for input"})
		return
	}

	resume := input.Resume[0]
	rs.mu.Lock()
	pending := rs.pending
	if !rs.parked || pending == nil || (resume.InterruptID != "" && resume.InterruptID != pending.id) {
		rs.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "interrupt does not match the pending request"})
		return
	}
	rs.mu.Unlock()

	response := decodeResume(pending.kind, resume)

	emitter, err := NewEmitter(w, threadID, runID, rs.newID, rs.cancelRun)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rs.mu.Lock()
	rs.emitter = emitter
	rs.parked = false
	rs.pending = nil
	rs.parkCh = make(chan struct{})
	pendingTools := make([]struct {
		callID string
		tool   toolState
	}, 0, len(rs.tools))
	for callID, tool := range rs.tools {
		pendingTools = append(pendingTools, struct {
			callID string
			tool   toolState
		}{callID: callID, tool: tool})
	}
	ch := pending.ch
	rs.mu.Unlock()

	emitter.RunStarted()
	for _, tool := range pendingTools {
		emitter.ToolStart(tool.callID, tool.tool.name, tool.tool.arguments, "")
	}
	select {
	case ch <- response:
	default:
		writeJSON(w, http.StatusConflict, map[string]string{"error": "interrupt was already answered"})
		return
	}

	s.wait(r.Context(), rs)
}

func decodeResume(kind string, resume Resume) runtime.Response {
	var object map[string]any
	_ = json.Unmarshal(resume.Payload, &object)
	if kind == "approval" {
		return runtime.Response{
			Approved: boolValue(object, "approved") || boolValue(object, "allow") || boolValue(object, "value"),
			Remember: boolValue(object, "remember"),
		}
	}
	value := stringValue(object, "value")
	if value == "" {
		value = stringValue(object, "answer")
	}
	if value == "" {
		value = stringValue(object, "text")
	}
	var text string
	if len(resume.Payload) > 0 && json.Unmarshal(resume.Payload, &text) == nil {
		value = text
	}
	return runtime.Response{Value: value}
}

func (s *Server) emitterFor(rs *runState) runtime.EmitFunc {
	return func(event runtime.Event) {
		rs.mu.Lock()
		emitter := rs.emitter
		rs.mu.Unlock()
		if emitter == nil {
			return
		}
		s.mapEvent(rs, emitter, event)
	}
}

func (s *Server) mapEvent(rs *runState, emitter *Emitter, event runtime.Event) {
	switch event.Type {
	case runtime.EventContent:
		if data, ok := event.Data.(runtime.TextData); ok {
			emitter.TextContent(data.Text)
		}
	case runtime.EventReasoning:
		if data, ok := event.Data.(runtime.TextData); ok {
			emitter.ReasoningContent(data.Text)
		}
	case runtime.EventToolStart:
		data, ok := event.Data.(runtime.ToolStartData)
		if !ok {
			return
		}
		rs.mu.Lock()
		rs.tools[data.CallID] = toolState{name: data.Name, arguments: data.Arguments}
		rs.mu.Unlock()
		emitter.ToolStart(data.CallID, data.Name, data.Arguments, "")
	case runtime.EventToolEnd:
		data, ok := event.Data.(runtime.ToolEndData)
		if !ok {
			return
		}
		rs.mu.Lock()
		delete(rs.tools, data.CallID)
		rs.mu.Unlock()
		emitter.ToolResult(data.CallID, data.Result)
	case runtime.EventTodos:
		if data, ok := event.Data.(runtime.TodosData); ok {
			emitter.StateSnapshot(map[string]any{"todos": data.Items})
		}
	case runtime.EventError:
		data, _ := event.Data.(runtime.ErrorData)
		rs.mu.Lock()
		rs.errSent = true
		rs.errText = data.Message
		rs.mu.Unlock()
		emitter.RunError(data.Message)
	case runtime.EventRunEnd:
		data, _ := event.Data.(runtime.RunEndData)
		rs.mu.Lock()
		rs.finished = true
		errSent := rs.errSent
		errText := rs.errText
		if data.Stop == "error" && !errSent {
			rs.errSent = true
		}
		rs.mu.Unlock()
		if data.Stop == "error" {
			if !errSent {
				if errText == "" {
					errText = "agent run failed"
				}
				emitter.RunError(errText)
			}
			return
		}
		emitter.RunFinishedSuccess()
	}
}

func (s *Server) wait(ctx context.Context, rs *runState) {
	for {
		rs.mu.Lock()
		if rs.parked {
			rs.mu.Unlock()
			return
		}
		done := rs.done
		park := rs.parkCh
		rs.mu.Unlock()

		select {
		case <-done:
			return
		case <-park:
			continue
		case <-ctx.Done():
			rs.mu.Lock()
			parked := rs.parked
			rs.mu.Unlock()
			if !parked {
				rs.cancelRun()
			}
			return
		}
	}
}

func (s *Server) removeRun(rs *runState) {
	s.mu.Lock()
	if s.runs[rs.key] == rs {
		delete(s.runs, rs.key)
	}
	s.mu.Unlock()
}

func (rs *runState) cancelRun() {
	rs.mu.Lock()
	cancel := rs.cancel
	rs.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (rs *runState) newID(prefix string) string {
	rs.mu.Lock()
	rs.seq++
	seq := rs.seq
	rs.mu.Unlock()
	return fmt.Sprintf("%s-%d", prefix, seq)
}

// Ask parks the run until a resume request answers the question.
func (rs *runState) Ask(ctx context.Context, data runtime.InterruptData) (runtime.Response, error) {
	return rs.park(ctx, "question", "input_required", data, func(interrupt Interrupt) []Interrupt {
		return []Interrupt{interrupt}
	})
}

// Approve parks the run until a resume request allows or denies the tool.
func (rs *runState) Approve(ctx context.Context, data runtime.InterruptData) (runtime.Response, error) {
	return rs.park(ctx, "approval", "tool_call", data, func(interrupt Interrupt) []Interrupt {
		return []Interrupt{interrupt}
	})
}

// Todos publishes the latest task list without interrupting the run.
func (rs *runState) Todos(_ context.Context, data runtime.TodosData) error {
	rs.mu.Lock()
	emitter := rs.emitter
	rs.mu.Unlock()
	if emitter != nil {
		emitter.StateSnapshot(map[string]any{"todos": data.Items})
	}
	return nil
}

func (rs *runState) park(ctx context.Context, kind, reason string, data runtime.InterruptData, wrap func(Interrupt) []Interrupt) (runtime.Response, error) {
	id := data.ID
	if id == "" {
		id = rs.newID("interrupt")
	}
	interrupt := Interrupt{
		ID:     id,
		Reason: reason,
		Metadata: map[string]any{
			"kind": kind,
		},
	}
	if kind == "approval" {
		interrupt.Message = data.Request.Summary
		if interrupt.Message == "" {
			interrupt.Message = "Approve tool " + data.Request.Tool
		}
		interrupt.ToolCallID = data.Request.Tool
		interrupt.Metadata["tool"] = data.Request.Tool
		interrupt.Metadata["detail"] = data.Request.Detail
	} else {
		interrupt.Message = data.Question
		if len(data.Options) > 0 {
			interrupt.Metadata["options"] = data.Options
		}
	}

	ch := make(chan runtime.Response, 1)
	request := data
	request.ID = id

	rs.mu.Lock()
	emitter := rs.emitter
	rs.pending = &pendingInterrupt{id: id, kind: kind, request: request, ch: ch}
	rs.mu.Unlock()

	if emitter != nil {
		emitter.RunFinishedInterrupt(wrap(interrupt))
	}

	rs.mu.Lock()
	rs.emitter = nil
	rs.parked = true
	if rs.parkCh != nil {
		close(rs.parkCh)
	}
	rs.parkCh = make(chan struct{})
	rs.mu.Unlock()

	timer := time.NewTimer(defaultInterrupt)
	defer timer.Stop()
	select {
	case response := <-ch:
		return response, nil
	case <-timer.C:
		return runtime.Response{}, fmt.Errorf("interrupt %s expired", id)
	case <-ctx.Done():
		return runtime.Response{}, ctx.Err()
	}
}

func writeRuntimeError(w http.ResponseWriter, err error) {
	var runErr *runtime.Error
	if errors.As(err, &runErr) {
		writeJSON(w, runErr.Status, map[string]string{"error": runErr.Message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ensureSingleJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}

func randomID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func propString(props map[string]any, key string) string {
	value, _ := props[key].(string)
	return strings.TrimSpace(value)
}

func stringValue(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}

func boolValue(object map[string]any, key string) bool {
	switch value := object[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || strings.EqualFold(value, "allow")
	}
	return false
}
