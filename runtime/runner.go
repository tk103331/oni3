package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/session"
	"github.com/tk103331/oni3/tools"
	"github.com/tk103331/oni3/tools/approval"
)

// Request describes one agent turn.
type Request struct {
	Agent     string
	Prompt    string
	Model     string
	SessionID string
	// History overrides the session history when non-nil. AG-UI clients send
	// the complete message list, while A2A resumes from the stored session.
	History []*schema.Message
}

// Result describes a completed turn.
type Result struct {
	SessionID string
	Answer    string
	Stop      string // end, error, cancelled
}

// Error is a user-facing validation or execution error.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func badRequest(message string) error {
	return &Error{Status: 400, Message: message}
}

// Runner executes agent turns and emits protocol-neutral events.
type Runner struct {
	cfg *config.Config
}

// Validate checks the agent and model names without starting a turn.
func (r *Runner) Validate(agentName, modelName string) error {
	agentName = strings.TrimSpace(agentName)
	modelName = strings.TrimSpace(modelName)
	if agentName == "" {
		return badRequest("agent is required")
	}
	if _, ok := r.cfg.Agents[agentName]; !ok {
		return badRequest("unknown agent: " + agentName)
	}
	if modelName != "" {
		if _, ok := r.cfg.Models[modelName]; !ok {
			return badRequest("unknown model: " + modelName)
		}
	}
	return nil
}

// New creates a runner for cfg.
func New(cfg *config.Config) (*Runner, error) {
	if cfg == nil {
		return nil, fmt.Errorf("global configuration not initialized")
	}
	return &Runner{cfg: cfg}, nil
}

// EmitFunc receives execution events in order.
type EmitFunc func(Event)

// Run executes one turn. It blocks until the agent finishes, is cancelled, or
// an Interactor waits for a client response.
func (r *Runner) Run(ctx context.Context, req Request, emit EmitFunc, interactor Interactor) (Result, error) {
	req.Agent = strings.TrimSpace(req.Agent)
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Model = strings.TrimSpace(req.Model)
	req.SessionID = strings.TrimSpace(req.SessionID)

	if req.Agent == "" {
		return Result{}, badRequest("agent is required")
	}
	if req.Prompt == "" {
		return Result{}, badRequest("prompt is required")
	}
	agentCfg, ok := r.cfg.Agents[req.Agent]
	if !ok {
		return Result{}, badRequest("unknown agent: " + req.Agent)
	}
	if req.Model != "" {
		if _, ok := r.cfg.Models[req.Model]; !ok {
			return Result{}, badRequest("unknown model: " + req.Model)
		}
	}

	defaultModel := req.Model
	if defaultModel == "" {
		defaultModel = strings.TrimSpace(agentCfg.Model)
	}
	if defaultModel == "" {
		defaultModel = r.cfg.DefaultModelName()
	}
	sess, err := r.loadSession(req, defaultModel)
	if err != nil {
		return Result{}, err
	}

	effectiveModel := req.Model
	if effectiveModel == "" && sess.Model != "" {
		effectiveModel = sess.Model
	}
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(agentCfg.Model)
	}
	if effectiveModel == "" {
		effectiveModel = r.cfg.DefaultModelName()
	}
	if effectiveModel == "" {
		return Result{}, badRequest("no model is configured; add one before chatting")
	}
	if _, ok := r.cfg.Models[effectiveModel]; !ok {
		return Result{}, badRequest("model is not configured: " + effectiveModel)
	}
	if sess.Model != effectiveModel {
		_ = sess.SetConfig(req.Agent, effectiveModel, sess.Skill)
	}

	history := req.History
	if history == nil {
		history = sessionHistory(sess)
	}

	runID := newRunID()
	seq := int64(0)
	send := func(kind string, data any) {
		if emit == nil {
			return
		}
		seq++
		emit(Event{
			Seq:      seq,
			ThreadID: sess.ID,
			RunID:    runID,
			Type:     kind,
			Data:     data,
		})
	}

	send(EventRunStart, RunStartData{Agent: req.Agent, Model: effectiveModel, Session: sess.ID})
	_ = sess.AddUser(req.Prompt)

	factory := agent.NewFactory(r.cfg)
	if req.Model != "" {
		factory = factory.WithModel(req.Model)
	}
	instance, err := factory.CreateAgent(req.Agent)
	if err != nil {
		send(EventError, ErrorData{Message: err.Error()})
		send(EventRunEnd, RunEndData{Stop: "error", SessionID: sess.ID})
		return Result{SessionID: sess.ID, Stop: "error"}, err
	}

	if interactor != nil {
		ctx = tools.WithHost(ctx, &hostAdapter{interactor: interactor, store: sess})
	}

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
				send(EventContent, TextData{Text: chunk.Content})
			case "reasoning":
				if chunk.Content == "" {
					return
				}
				thought.WriteString(chunk.Content)
				send(EventReasoning, TextData{Text: chunk.Content})
			case "error":
				errorReported = true
				send(EventError, ErrorData{Message: chunk.Content})
			}
		},
		func(payload interface{}) {
			info, ok := payload.(agent.ToolCallInfo)
			if !ok {
				return
			}
			switch info.Type {
			case "start":
				_ = sess.AddToolStart(info.Name, info.Arguments)
				send(EventToolStart, ToolStartData{
					Name: info.Name, CallID: info.CallID, Arguments: info.Arguments,
				})
			case "end":
				failed := agent.IsToolError(info.Result)
				_ = sess.AddToolEnd(info.Name, info.Result, failed)
				send(EventToolEnd, ToolEndData{
					Name: info.Name, CallID: info.CallID, Result: info.Result, Failed: failed,
				})
			case "error":
				_ = sess.AddToolEnd(info.Name, info.Error, true)
				send(EventToolEnd, ToolEndData{
					Name: info.Name, CallID: info.CallID, Result: info.Error, Failed: true,
				})
			}
		},
	)

	if thought.Len() > 0 {
		_ = sess.AddReasoning(thought.String())
	}
	if text := strings.TrimSpace(answer.String()); text != "" {
		_ = sess.AddAssistant(text)
	}

	result := Result{SessionID: sess.ID, Answer: answer.String()}
	switch {
	case err != nil && (errors.Is(err, context.Canceled) || ctx.Err() != nil):
		result.Stop = "cancelled"
		send(EventRunEnd, RunEndData{Stop: "cancelled", SessionID: sess.ID, Answer: result.Answer})
		return result, nil
	case err != nil:
		result.Stop = "error"
		if !errorReported {
			send(EventError, ErrorData{Message: err.Error()})
		}
		send(EventRunEnd, RunEndData{Stop: "error", SessionID: sess.ID, Answer: result.Answer})
		return result, nil
	default:
		result.Stop = "end"
		send(EventRunEnd, RunEndData{Stop: "end", SessionID: sess.ID, Answer: result.Answer})
		return result, nil
	}
}

func (r *Runner) loadSession(req Request, defaultModel string) (*session.Session, error) {
	if req.SessionID != "" {
		if !validSessionID(req.SessionID) {
			return nil, badRequest("invalid session id: " + req.SessionID)
		}
		if existing, err := session.Load(req.SessionID); err == nil {
			if existing.Agent != "" && existing.Agent != req.Agent {
				return nil, badRequest(fmt.Sprintf("session %s belongs to agent %s", existing.ID, existing.Agent))
			}
			return existing, nil
		}
		sess := session.New(session.KindAgent, req.Agent, defaultModel, "")
		sess.ID = req.SessionID
		return sess, nil
	}
	return session.New(session.KindAgent, req.Agent, defaultModel, ""), nil
}

func validSessionID(id string) bool {
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

func newRunID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "run"
	}
	return "run-" + hex.EncodeToString(buf[:])
}

type hostAdapter struct {
	interactor Interactor
	store      *session.Session
}

func (h *hostAdapter) AskUser(ctx context.Context, question string, options []string) (string, error) {
	resp, err := h.interactor.Ask(ctx, InterruptData{
		Kind:     "question",
		Question: question,
		Options:  options,
	})
	if err != nil {
		return "", err
	}
	answer := strings.TrimSpace(resp.Value)
	_ = h.store.AddQuestion(question, answer)
	return answer, nil
}

func (h *hostAdapter) ApproveTool(ctx context.Context, req approval.Request) (bool, error) {
	resp, err := h.interactor.Approve(ctx, InterruptData{
		Kind:    "approval",
		Request: req,
	})
	if err != nil {
		return false, err
	}
	return resp.Approved, nil
}

func (h *hostAdapter) UpdateTodos(ctx context.Context, items []tools.TodoItem) error {
	persisted := make([]session.TodoItem, 0, len(items))
	for _, item := range items {
		persisted = append(persisted, session.TodoItem{Content: item.Content, Status: item.Status})
	}
	_ = h.store.AddTodos(persisted)
	return h.interactor.Todos(ctx, TodosData{Items: items})
}
