package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tk103331/oni3/runtime"
)

const (
	streamBuffer   = 256
	interruptLimit = 10 * time.Minute
)

type resumeInput struct {
	text     string
	metadata map[string]any
	parts    []Part
}

type pendingInput struct {
	kind    string
	request runtime.InterruptData
	ch      chan resumeInput
}

type streamItem struct {
	seq      int
	response StreamResponse
	final    bool
}

type taskRun struct {
	taskID    string
	contextID string
	agent     string
	model     string

	mu       sync.Mutex
	task     *Task
	pending  *pendingInput
	subs     map[int]chan streamItem
	nextSub  int
	history  []streamItem
	seq      int
	answer   strings.Builder
	thought  strings.Builder
	errText  string
	cancel   context.CancelFunc
	done     chan struct{}
	terminal bool
	store    *Store
}

func newTaskRun(task *Task, agent, model string, store *Store) *taskRun {
	return &taskRun{
		taskID:    task.ID,
		contextID: task.ContextID,
		agent:     agent,
		model:     model,
		task:      task,
		subs:      make(map[int]chan streamItem),
		done:      make(chan struct{}),
		store:     store,
	}
}

func (tr *taskRun) snapshot() *Task {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return cloneTask(tr.task)
}

func (tr *taskRun) subscribe() ([]streamItem, chan streamItem, int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	history := append([]streamItem(nil), tr.history...)
	tr.nextSub++
	id := tr.nextSub
	ch := make(chan streamItem, streamBuffer)
	tr.subs[id] = ch
	return history, ch, id
}

func (tr *taskRun) unsubscribe(id int) {
	tr.mu.Lock()
	delete(tr.subs, id)
	tr.mu.Unlock()
}

func (tr *taskRun) setState(state string, message *Message) {
	tr.setStateFinal(state, message, false)
}

func (tr *taskRun) setStateFinal(state string, message *Message, final bool) {
	tr.mu.Lock()
	if tr.terminal {
		tr.mu.Unlock()
		return
	}
	tr.task.Status = TaskStatus{
		State:     state,
		Message:   message,
		Timestamp: nowTimestamp(),
	}
	if final {
		tr.terminal = true
	}
	snapshot := cloneTask(tr.task)
	tr.seq++
	item := streamItem{
		seq: tr.seq,
		response: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			Kind:      "status-update",
			TaskID:    tr.taskID,
			ContextID: tr.contextID,
			Status:    tr.task.Status,
			Final:     final,
		}},
		final: final,
	}
	tr.history = append(tr.history, item)
	subs := tr.copySubsLocked()
	tr.mu.Unlock()

	tr.persist(snapshot)
	tr.broadcast(subs, item)
}

func (tr *taskRun) addArtifact(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	artifact := Artifact{
		ArtifactID: "artifact-" + randomID(),
		Name:       "answer",
		Parts:      []Part{TextPart(text)},
	}

	tr.mu.Lock()
	tr.task.Artifacts = append(tr.task.Artifacts, artifact)
	snapshot := cloneTask(tr.task)
	tr.seq++
	item := streamItem{
		seq: tr.seq,
		response: StreamResponse{ArtifactUpdate: &TaskArtifactUpdateEvent{
			Kind:      "artifact-update",
			TaskID:    tr.taskID,
			ContextID: tr.contextID,
			Artifact:  artifact,
			LastChunk: true,
		}},
	}
	tr.history = append(tr.history, item)
	subs := tr.copySubsLocked()
	tr.mu.Unlock()

	tr.persist(snapshot)
	tr.broadcast(subs, item)
}

func (tr *taskRun) addHistory(message Message) {
	tr.mu.Lock()
	tr.task.History = append(tr.task.History, message)
	snapshot := cloneTask(tr.task)
	tr.mu.Unlock()
	tr.persist(snapshot)
}

func (tr *taskRun) setMetadata(key string, value any) {
	tr.mu.Lock()
	if tr.task.Metadata == nil {
		tr.task.Metadata = make(map[string]any)
	}
	tr.task.Metadata[key] = value
	snapshot := cloneTask(tr.task)
	tr.mu.Unlock()
	tr.persist(snapshot)
}

func (tr *taskRun) copySubsLocked() []chan streamItem {
	out := make([]chan streamItem, 0, len(tr.subs))
	for _, ch := range tr.subs {
		out = append(out, ch)
	}
	return out
}

func (tr *taskRun) broadcast(subs []chan streamItem, item streamItem) {
	for _, ch := range subs {
		select {
		case ch <- item:
		default:
		}
	}
}

func (tr *taskRun) persist(task *Task) {
	if tr.store == nil {
		return
	}
	_ = tr.store.Save(task)
}

func (s *Server) execute(tr *taskRun, prompt string) {
	defer close(tr.done)
	defer s.removeRun(tr)

	ctx, cancel := context.WithCancel(context.Background())
	tr.mu.Lock()
	tr.cancel = cancel
	tr.mu.Unlock()
	defer cancel()

	tr.setState(StateWorking, nil)
	result, err := s.runner.Run(ctx, runtime.Request{
		Agent:     tr.agent,
		Prompt:    prompt,
		Model:     tr.model,
		SessionID: tr.contextID,
	}, s.emitterFor(tr), &taskInteractor{run: tr})

	tr.mu.Lock()
	cancelled := result.Stop == "cancelled"
	answer := tr.answer.String()
	errText := tr.errText
	tr.mu.Unlock()

	switch {
	case cancelled:
		tr.setStateFinal(StateCanceled, nil, true)
	case err != nil:
		if errText == "" {
			errText = err.Error()
		}
		tr.setStateFinal(StateFailed, messagePtr(textMessage("agent", errText, tr.taskID, tr.contextID)), true)
	case result.Stop == "error":
		if errText == "" {
			errText = "agent run failed"
		}
		tr.setStateFinal(StateFailed, messagePtr(textMessage("agent", errText, tr.taskID, tr.contextID)), true)
	default:
		tr.addArtifact(answer)
		tr.setStateFinal(StateCompleted, nil, true)
	}
}

func (s *Server) emitterFor(tr *taskRun) runtime.EmitFunc {
	return func(event runtime.Event) {
		switch event.Type {
		case runtime.EventRunStart:
			if data, ok := event.Data.(runtime.RunStartData); ok && data.Session != "" {
				tr.mu.Lock()
				if tr.contextID == "" {
					tr.contextID = data.Session
					tr.task.ContextID = data.Session
				}
				tr.mu.Unlock()
			}
		case runtime.EventContent:
			if data, ok := event.Data.(runtime.TextData); ok {
				tr.mu.Lock()
				tr.answer.WriteString(data.Text)
				tr.mu.Unlock()
			}
		case runtime.EventReasoning:
			if data, ok := event.Data.(runtime.TextData); ok {
				tr.mu.Lock()
				tr.thought.WriteString(data.Text)
				tr.mu.Unlock()
			}
		case runtime.EventToolStart:
			if data, ok := event.Data.(runtime.ToolStartData); ok {
				tr.addToolMetadata(data.Name, data.Arguments, "")
			}
		case runtime.EventToolEnd:
			if data, ok := event.Data.(runtime.ToolEndData); ok {
				tr.addToolMetadata(data.Name, "", data.Result)
			}
		case runtime.EventTodos:
			if data, ok := event.Data.(runtime.TodosData); ok {
				tr.setMetadata("todos", data.Items)
			}
		case runtime.EventError:
			if data, ok := event.Data.(runtime.ErrorData); ok {
				tr.mu.Lock()
				tr.errText = data.Message
				tr.mu.Unlock()
			}
		}
	}
}

func (tr *taskRun) addToolMetadata(name, arguments, result string) {
	tr.mu.Lock()
	if tr.task.Metadata == nil {
		tr.task.Metadata = make(map[string]any)
	}
	items := toolCalls(tr.task.Metadata["toolCalls"])
	if arguments != "" {
		items = append(items, map[string]any{"name": name, "arguments": arguments})
	} else if len(items) > 0 {
		items[len(items)-1]["result"] = result
	}
	tr.task.Metadata["toolCalls"] = items
	snapshot := cloneTask(tr.task)
	tr.mu.Unlock()
	tr.persist(snapshot)
}

func toolCalls(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				out = append(out, entry)
			}
		}
		return out
	default:
		return nil
	}
}

type taskInteractor struct {
	run *taskRun
}

func (i *taskInteractor) Ask(ctx context.Context, data runtime.InterruptData) (runtime.Response, error) {
	return i.wait(ctx, "question", data)
}

func (i *taskInteractor) Approve(ctx context.Context, data runtime.InterruptData) (runtime.Response, error) {
	return i.wait(ctx, "approval", data)
}

func (i *taskInteractor) Todos(_ context.Context, data runtime.TodosData) error {
	i.run.setMetadata("todos", data.Items)
	return nil
}

func (i *taskInteractor) wait(ctx context.Context, kind string, data runtime.InterruptData) (runtime.Response, error) {
	tr := i.run
	text := data.Question
	if kind == "approval" {
		text = data.Request.Summary
		if text == "" {
			text = "Approve tool " + data.Request.Tool
		}
	}
	message := textMessage("agent", text, tr.taskID, tr.contextID)
	if tr.task.Metadata != nil {
		message.Metadata = map[string]any{"kind": kind}
		if kind == "approval" {
			message.Metadata["tool"] = data.Request.Tool
			message.Metadata["detail"] = data.Request.Detail
		}
		if len(data.Options) > 0 {
			message.Metadata["options"] = data.Options
		}
	}

	ch := make(chan resumeInput, 1)
	tr.mu.Lock()
	tr.pending = &pendingInput{kind: kind, request: data, ch: ch}
	tr.task.Status = TaskStatus{State: StateInputRequired, Message: &message, Timestamp: nowTimestamp()}
	snapshot := cloneTask(tr.task)
	tr.seq++
	item := streamItem{
		seq: tr.seq,
		response: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			Kind:      "status-update",
			TaskID:    tr.taskID,
			ContextID: tr.contextID,
			Status:    tr.task.Status,
			Final:     true,
		}},
		final: true,
	}
	tr.history = append(tr.history, item)
	subs := tr.copySubsLocked()
	tr.mu.Unlock()
	tr.persist(snapshot)
	tr.broadcast(subs, item)

	timer := time.NewTimer(interruptLimit)
	defer timer.Stop()
	select {
	case input := <-ch:
		tr.mu.Lock()
		tr.pending = nil
		tr.mu.Unlock()
		if kind == "approval" {
			return runtime.Response{Approved: input.approved()}, nil
		}
		return runtime.Response{Value: input.text}, nil
	case <-timer.C:
		return runtime.Response{}, fmt.Errorf("input request expired")
	case <-ctx.Done():
		return runtime.Response{}, ctx.Err()
	}
}

func (in resumeInput) approved() bool {
	for _, key := range []string{"approved", "allow", "value"} {
		switch value := in.metadata[key].(type) {
		case bool:
			if value {
				return true
			}
		case string:
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "true", "yes", "y", "allow", "approve", "approved":
				return true
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(in.text)) {
	case "true", "yes", "y", "allow", "approve", "approved", "同意", "允许":
		return true
	}
	return false
}

func messagePtr(message Message) *Message {
	return &message
}

// ensurePartData renders structured data as text when a message has no text.
func ensurePartData(parts []Part) string {
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			return part.Text
		}
	}
	for _, part := range parts {
		if part.Data != nil {
			data, err := json.Marshal(part.Data)
			if err == nil {
				return string(data)
			}
		}
	}
	return ""
}
