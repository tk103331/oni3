package a2a

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/runtime"
)

const maxBody = 4 << 20

// Server implements the A2A JSON-RPC binding and agent-card endpoints.
type Server struct {
	cfg          *config.Config
	runner       *runtime.Runner
	defaultAgent string
	version      string
	tokenEnabled bool
	store        *Store

	mu   sync.Mutex
	runs map[string]*taskRun
}

// New creates an A2A server using the default task store.
func New(cfg *config.Config, runner *runtime.Runner, defaultAgent, version string, tokenEnabled bool) (*Server, error) {
	return NewWithStore(cfg, runner, defaultAgent, version, tokenEnabled, NewStore(""))
}

// NewWithStore creates an A2A server with a specific task store.
func NewWithStore(cfg *config.Config, runner *runtime.Runner, defaultAgent, version string, tokenEnabled bool, store *Store) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration is required")
	}
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}
	if store == nil {
		store = NewStore("")
	}
	defaultAgent = strings.TrimSpace(defaultAgent)
	if defaultAgent != "" {
		if _, ok := cfg.Agents[defaultAgent]; !ok {
			return nil, fmt.Errorf("agent configuration does not exist: %s", defaultAgent)
		}
	}
	return &Server{
		cfg:          cfg,
		runner:       runner,
		defaultAgent: defaultAgent,
		version:      strings.TrimSpace(version),
		tokenEnabled: tokenEnabled,
		store:        store,
		runs:         make(map[string]*taskRun),
	}, nil
}

// Handler returns the JSON-RPC endpoint for one agent. An empty override uses
// the default agent.
func (s *Server) Handler(agentOverride string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		agentName := strings.TrimSpace(agentOverride)
		if agentName == "" {
			agentName = s.defaultAgent
		}
		if _, ok := s.cfg.Agents[agentName]; !ok {
			writeJSON(w, http.StatusNotFound, failure(nil, rpcError(CodeInvalidParams, "unknown agent: "+agentName)))
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, failure(nil, rpcError(CodeParseError, "failed to read request body")))
			return
		}
		request, rpcErr := parseRequest(body)
		if rpcErr != nil {
			writeJSON(w, http.StatusOK, failure(request.ID, rpcErr))
			return
		}

		switch CanonicalMethod(request.Method) {
		case MethodSendMessage:
			_, task, sendErr := s.send(agentName, request.Params)
			if sendErr != nil {
				writeJSON(w, http.StatusOK, failure(request.ID, sendErr))
				return
			}
			writeJSON(w, http.StatusOK, success(request.ID, task))
		case MethodSendStreamingMessage:
			run, _, sendErr := s.send(agentName, request.Params)
			if sendErr != nil {
				writeJSON(w, http.StatusOK, failure(request.ID, sendErr))
				return
			}
			s.stream(w, r, request.ID, run)
		case MethodGetTask:
			task, getErr := s.getTask(agentName, request.Params)
			if getErr != nil {
				writeJSON(w, http.StatusOK, failure(request.ID, getErr))
				return
			}
			writeJSON(w, http.StatusOK, success(request.ID, task))
		case MethodListTasks:
			result, listErr := s.listTasks(agentName, request.Params)
			if listErr != nil {
				writeJSON(w, http.StatusOK, failure(request.ID, listErr))
				return
			}
			writeJSON(w, http.StatusOK, success(request.ID, result))
		case MethodCancelTask:
			task, cancelErr := s.cancelTask(agentName, request.Params)
			if cancelErr != nil {
				writeJSON(w, http.StatusOK, failure(request.ID, cancelErr))
				return
			}
			writeJSON(w, http.StatusOK, success(request.ID, task))
		case MethodSubscribeToTask:
			s.subscribeTask(w, r, request.ID, agentName, request.Params)
		default:
			writeJSON(w, http.StatusOK, failure(request.ID, rpcError(CodeMethodNotFound, "method not found")))
		}
	})
}

func (s *Server) send(agentName string, raw json.RawMessage) (*taskRun, *Task, *RPCError) {
	var params SendMessageRequest
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, nil, rpcError(CodeInvalidParams, "invalid message parameters: "+err.Error())
	}
	message := params.Message
	if message == nil {
		return nil, nil, rpcError(CodeInvalidParams, "message is required")
	}
	if !IsUserRole(message.Role) {
		return nil, nil, rpcError(CodeInvalidParams, "message role must be ROLE_USER")
	}
	message.Role = RoleUser
	if message.MessageID == "" {
		message.MessageID = "msg-" + randomID()
	}
	text := strings.TrimSpace(ensurePartData(message.Parts))

	if message.TaskID != "" {
		task, ok := s.store.Get(message.TaskID)
		if !ok {
			return nil, nil, rpcError(CodeInvalidParams, "task not found: "+message.TaskID)
		}
		if owner := taskAgent(task); owner != "" && owner != agentName {
			return nil, nil, rpcError(CodeInvalidParams, "task belongs to another agent")
		}
		run := s.runFor(task.ID)
		if run != nil {
			if resumedTask, resumed := s.resume(run, *message); resumed {
				return run, resumedTask, nil
			}
		}
		if task.Status.State == StateInputRequired {
			return nil, nil, rpcError(CodeInvalidParams, "task is waiting for input but the request expired")
		}
		if !terminalState(task.Status.State) {
			return nil, nil, rpcError(CodeInvalidParams, "task is already running")
		}
		// A message for a finished task starts a new task in the same context.
		message.ContextID = task.ContextID
		message.TaskID = ""
	}

	contextID := strings.TrimSpace(message.ContextID)
	if contextID == "" {
		contextID = "ctx-" + randomID()
	}
	if !validContextID(contextID) {
		return nil, nil, rpcError(CodeInvalidParams, "invalid contextId")
	}
	if text == "" {
		return nil, nil, rpcError(CodeInvalidParams, "message must contain text or data")
	}

	model := messageModel(message, params)
	if err := s.runner.Validate(agentName, model); err != nil {
		return nil, nil, rpcError(CodeInvalidParams, err.Error())
	}

	message.ContextID = contextID
	message.Kind = "message"
	task := &Task{
		Kind:      "task",
		ID:        "task-" + randomID(),
		ContextID: contextID,
		Status:    TaskStatus{State: StateSubmitted, Timestamp: nowTimestamp()},
		History:   []Message{*message},
		Metadata: map[string]any{
			"agent": agentName,
		},
	}
	if model != "" {
		task.Metadata["model"] = model
	}
	run := newTaskRun(task, agentName, model, s.store)
	if err := s.store.Save(task); err != nil {
		return nil, nil, rpcError(CodeInternalError, "failed to store task: "+err.Error())
	}
	s.registerRun(run)
	go s.execute(run, text)
	return run, run.snapshot(), nil
}

func (s *Server) resume(run *taskRun, message Message) (*Task, bool) {
	run.mu.Lock()
	pending := run.pending
	if pending == nil {
		run.mu.Unlock()
		return nil, false
	}
	text := ensurePartData(message.Parts)
	input := resumeInput{text: text, metadata: message.Metadata, parts: message.Parts}
	run.task.History = append(run.task.History, message)
	run.task.Status = TaskStatus{State: StateWorking, Timestamp: nowTimestamp()}
	snapshot := cloneTask(run.task)
	run.seq++
	item := streamItem{
		seq: run.seq,
		response: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			Kind:      "status-update",
			TaskID:    run.taskID,
			ContextID: run.contextID,
			Status:    run.task.Status,
		}},
	}
	run.history = append(run.history, item)
	subs := run.copySubsLocked()
	ch := pending.ch
	run.mu.Unlock()

	run.persist(snapshot)
	run.broadcast(subs, item)
	select {
	case ch <- input:
		return snapshot, true
	default:
		return nil, false
	}
}

func (s *Server) getTask(agentName string, raw json.RawMessage) (*Task, *RPCError) {
	var params GetTaskRequest
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, rpcError(CodeInvalidParams, "invalid task parameters: "+err.Error())
	}
	id := strings.TrimSpace(params.ID)
	if id == "" {
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		id, _ = wire["taskId"].(string)
	}
	if id == "" {
		return nil, rpcError(CodeInvalidParams, "id is required")
	}
	task, ok := s.store.Get(id)
	if !ok {
		return nil, rpcError(CodeInvalidParams, "task not found: "+id)
	}
	if owner := taskAgent(task); owner != "" && owner != agentName {
		return nil, rpcError(CodeInvalidParams, "task belongs to another agent")
	}
	if params.HistoryLength != nil && *params.HistoryLength >= 0 && *params.HistoryLength < len(task.History) {
		task.History = task.History[len(task.History)-*params.HistoryLength:]
	}
	return task, nil
}

func (s *Server) listTasks(agentName string, raw json.RawMessage) (*ListTasksResponse, *RPCError) {
	var params ListTasksRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, rpcError(CodeInvalidParams, "invalid list parameters: "+err.Error())
		}
	}
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := 0
	if params.PageToken != "" {
		parsed, err := strconv.Atoi(params.PageToken)
		if err != nil || parsed < 0 {
			return nil, rpcError(CodeInvalidParams, "invalid pageToken")
		}
		offset = parsed
	}
	tasks := s.store.List(params.ContextID, normalizeState(params.Status))
	filtered := tasks[:0]
	for _, task := range tasks {
		if owner := taskAgent(task); owner == "" || owner == agentName {
			filtered = append(filtered, task)
		}
	}
	tasks = filtered
	total := len(tasks)
	if offset > total {
		offset = total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	next := ""
	if end < total {
		next = strconv.Itoa(end)
	}
	return &ListTasksResponse{
		Tasks:         tasks[offset:end],
		TotalSize:     total,
		PageSize:      pageSize,
		NextPageToken: next,
	}, nil
}

func (s *Server) cancelTask(agentName string, raw json.RawMessage) (*Task, *RPCError) {
	var params CancelTaskRequest
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, rpcError(CodeInvalidParams, "invalid cancel parameters: "+err.Error())
	}
	id := strings.TrimSpace(params.ID)
	if id == "" {
		return nil, rpcError(CodeInvalidParams, "id is required")
	}
	task, ok := s.store.Get(id)
	if !ok {
		return nil, rpcError(CodeInvalidParams, "task not found: "+id)
	}
	if owner := taskAgent(task); owner != "" && owner != agentName {
		return nil, rpcError(CodeInvalidParams, "task belongs to another agent")
	}
	if terminalState(task.Status.State) {
		return task, nil
	}
	run := s.runFor(id)
	if run == nil {
		task.Status = TaskStatus{State: StateCanceled, Timestamp: nowTimestamp()}
		_ = s.store.Save(task)
		return task, nil
	}
	run.mu.Lock()
	cancel := run.cancel
	run.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	run.setStateFinal(StateCanceled, nil, true)
	return run.snapshot(), nil
}

func (s *Server) subscribeTask(w http.ResponseWriter, r *http.Request, id json.RawMessage, agentName string, raw json.RawMessage) {
	var params SubscribeToTaskRequest
	if err := json.Unmarshal(raw, &params); err != nil {
		writeJSON(w, http.StatusOK, failure(id, rpcError(CodeInvalidParams, "invalid subscribe parameters: "+err.Error())))
		return
	}
	taskID := strings.TrimSpace(params.ID)
	if taskID == "" {
		writeJSON(w, http.StatusOK, failure(id, rpcError(CodeInvalidParams, "id is required")))
		return
	}
	if run := s.runFor(taskID); run != nil {
		if run.agent != "" && run.agent != agentName {
			writeJSON(w, http.StatusOK, failure(id, rpcError(CodeInvalidParams, "task belongs to another agent")))
			return
		}
		s.stream(w, r, id, run)
		return
	}
	task, ok := s.store.Get(taskID)
	if !ok {
		writeJSON(w, http.StatusOK, failure(id, rpcError(CodeInvalidParams, "task not found: "+taskID)))
		return
	}
	if owner := taskAgent(task); owner != "" && owner != agentName {
		writeJSON(w, http.StatusOK, failure(id, rpcError(CodeInvalidParams, "task belongs to another agent")))
		return
	}
	stream, err := newSSE(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, failure(id, rpcError(CodeInternalError, err.Error())))
		return
	}
	_ = stream.write(success(id, StreamResponse{Task: task}))
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request, id json.RawMessage, run *taskRun) {
	history, ch, subID := run.subscribe()
	defer run.unsubscribe(subID)

	stream, err := newSSE(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, failure(id, rpcError(CodeInternalError, err.Error())))
		return
	}
	if snapshot := run.snapshot(); snapshot != nil {
		if !stream.write(success(id, StreamResponse{Task: snapshot})) {
			return
		}
	}
	for i, item := range history {
		if !stream.write(success(id, item.response)) {
			return
		}
		if item.final && i == len(history)-1 {
			return
		}
	}
	for {
		select {
		case item := <-ch:
			if !stream.write(success(id, item.response)) {
				return
			}
			if item.final {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

type sseWriter struct {
	w      http.ResponseWriter
	flush  http.Flusher
	broken bool
}

func newSSE(w http.ResponseWriter) (*sseWriter, error) {
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

func (s *sseWriter) write(value any) bool {
	if s == nil || s.broken {
		return false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		s.broken = true
		return false
	}
	s.flush.Flush()
	return true
}

func (s *Server) registerRun(run *taskRun) {
	s.mu.Lock()
	s.runs[run.taskID] = run
	s.mu.Unlock()
}

func (s *Server) removeRun(run *taskRun) {
	s.mu.Lock()
	if s.runs[run.taskID] == run {
		delete(s.runs, run.taskID)
	}
	s.mu.Unlock()
}

func (s *Server) runFor(taskID string) *taskRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[taskID]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func messageModel(message *Message, params SendMessageRequest) string {
	if message != nil && message.Metadata != nil {
		if model, ok := message.Metadata["model"].(string); ok {
			return strings.TrimSpace(model)
		}
	}
	if params.Configuration != nil {
		if model, ok := params.Configuration["model"].(string); ok {
			return strings.TrimSpace(model)
		}
	}
	return ""
}

func terminalState(state string) bool {
	switch state {
	case StateCompleted, StateCanceled, StateFailed, StateRejected:
		return true
	}
	return false
}

func normalizeState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "submitted":
		return StateSubmitted
	case "working":
		return StateWorking
	case "input-required":
		return StateInputRequired
	case "completed":
		return StateCompleted
	case "canceled":
		return StateCanceled
	case "failed":
		return StateFailed
	case "rejected":
		return StateRejected
	case "auth-required":
		return StateAuthRequired
	default:
		return state
	}
}

func taskAgent(task *Task) string {
	if task == nil || task.Metadata == nil {
		return ""
	}
	agent, _ := task.Metadata["agent"].(string)
	return agent
}

func validContextID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '-':
		default:
			return false
		}
	}
	return true
}
