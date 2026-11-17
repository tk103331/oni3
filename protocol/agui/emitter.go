package agui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Emitter writes AG-UI events as SSE frames. It tracks open text and
// reasoning messages so the server can close them before starting another
// block or finishing the run.
type Emitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	onError func()
	newID   func(prefix string) string

	threadID string
	runID    string

	mu                   sync.Mutex
	err                  error
	openTextID           string
	openReasoningID      string
	openReasoningMessage string
}

// NewEmitter starts an SSE response. onError is called once when the client
// connection can no longer be written to.
func NewEmitter(w http.ResponseWriter, threadID, runID string, newID func(string) string, onError func()) (*Emitter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming is not supported by this connection")
	}
	if newID == nil {
		newID = func(prefix string) string { return prefix }
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &Emitter{
		w:        w,
		flusher:  flusher,
		onError:  onError,
		newID:    newID,
		threadID: threadID,
		runID:    runID,
	}, nil
}

// Err reports the first transport error.
func (e *Emitter) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

func (e *Emitter) emit(value map[string]any) {
	if e == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return
	}
	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", data); err != nil {
		e.err = err
		if e.onError != nil {
			e.onError()
		}
		return
	}
	e.flusher.Flush()
}

// RunStarted announces a run on this connection.
func (e *Emitter) RunStarted() {
	e.emit(map[string]any{
		"type":     "RUN_STARTED",
		"threadId": e.threadID,
		"runId":    e.runID,
	})
}

// RunFinishedSuccess terminates the run successfully.
func (e *Emitter) RunFinishedSuccess() {
	e.closeOpenBlocks()
	e.emit(map[string]any{
		"type":     "RUN_FINISHED",
		"threadId": e.threadID,
		"runId":    e.runID,
		"outcome":  map[string]any{"type": "success"},
	})
}

// RunFinishedInterrupt terminates this connection's stream while leaving the
// run parked for a later resume.
func (e *Emitter) RunFinishedInterrupt(interrupts []Interrupt) {
	e.closeOpenBlocks()
	e.emit(map[string]any{
		"type":     "RUN_FINISHED",
		"threadId": e.threadID,
		"runId":    e.runID,
		"outcome": map[string]any{
			"type":       "interrupt",
			"interrupts": interrupts,
		},
	})
}

// RunError reports a terminal failure.
func (e *Emitter) RunError(message string) {
	e.closeOpenBlocks()
	e.emit(map[string]any{
		"type":    "RUN_ERROR",
		"message": message,
		"runId":   e.runID,
	})
}

// TextStart opens an assistant text message.
func (e *Emitter) TextStart() string {
	e.closeReasoning()
	e.mu.Lock()
	if e.openTextID != "" {
		id := e.openTextID
		e.mu.Unlock()
		return id
	}
	id := e.newID("msg")
	e.openTextID = id
	e.mu.Unlock()
	e.emit(map[string]any{
		"type":      "TEXT_MESSAGE_START",
		"messageId": id,
		"role":      "assistant",
	})
	return id
}

// TextContent appends a text delta.
func (e *Emitter) TextContent(delta string) {
	if delta == "" {
		return
	}
	id := e.TextStart()
	e.emit(map[string]any{
		"type":      "TEXT_MESSAGE_CONTENT",
		"messageId": id,
		"delta":     delta,
	})
}

// TextEnd closes the open text message.
func (e *Emitter) TextEnd() {
	e.mu.Lock()
	id := e.openTextID
	e.openTextID = ""
	e.mu.Unlock()
	if id == "" {
		return
	}
	e.emit(map[string]any{"type": "TEXT_MESSAGE_END", "messageId": id})
}

// ReasoningStart opens a reasoning block.
func (e *Emitter) ReasoningStart() {
	e.closeText()
	e.mu.Lock()
	if e.openReasoningID != "" {
		e.mu.Unlock()
		return
	}
	blockID := e.newID("reasoning")
	messageID := e.newID("reasoning-msg")
	e.openReasoningID = blockID
	e.openReasoningMessage = messageID
	e.mu.Unlock()

	e.emit(map[string]any{"type": "REASONING_START", "messageId": blockID})
	e.emit(map[string]any{
		"type":      "REASONING_MESSAGE_START",
		"messageId": messageID,
		"role":      "reasoning",
	})
}

// ReasoningContent appends a reasoning delta.
func (e *Emitter) ReasoningContent(delta string) {
	if delta == "" {
		return
	}
	e.ReasoningStart()
	e.mu.Lock()
	messageID := e.openReasoningMessage
	e.mu.Unlock()
	e.emit(map[string]any{
		"type":      "REASONING_MESSAGE_CONTENT",
		"messageId": messageID,
		"delta":     delta,
	})
}

// ReasoningEnd closes the reasoning block.
func (e *Emitter) ReasoningEnd() {
	e.mu.Lock()
	blockID := e.openReasoningID
	messageID := e.openReasoningMessage
	e.openReasoningID = ""
	e.openReasoningMessage = ""
	e.mu.Unlock()
	if blockID == "" {
		return
	}
	e.emit(map[string]any{"type": "REASONING_MESSAGE_END", "messageId": messageID})
	e.emit(map[string]any{"type": "REASONING_END", "messageId": blockID})
}

// ToolStart announces one tool call. Arguments are delivered as a single
// delta and the call block is closed immediately; the result may arrive after
// an interrupt and resume on another connection.
func (e *Emitter) ToolStart(callID, name, arguments, parentMessageID string) {
	e.closeOpenBlocks()
	event := map[string]any{
		"type":         "TOOL_CALL_START",
		"toolCallId":   callID,
		"toolCallName": name,
	}
	if parentMessageID != "" {
		event["parentMessageId"] = parentMessageID
	}
	e.emit(event)
	if arguments != "" {
		e.emit(map[string]any{
			"type":       "TOOL_CALL_ARGS",
			"toolCallId": callID,
			"delta":      arguments,
		})
	}
	e.emit(map[string]any{"type": "TOOL_CALL_END", "toolCallId": callID})
}

// ToolResult reports the result of a tool call.
func (e *Emitter) ToolResult(callID, content string) {
	if content == "" {
		content = "(empty)"
	}
	e.emit(map[string]any{
		"type":       "TOOL_CALL_RESULT",
		"messageId":  e.newID("tool-msg"),
		"toolCallId": callID,
		"content":    content,
		"role":       "tool",
	})
}

// StateSnapshot publishes the current todo list as AG-UI state.
func (e *Emitter) StateSnapshot(snapshot map[string]any) {
	e.emit(map[string]any{"type": "STATE_SNAPSHOT", "snapshot": snapshot})
}

// LastTextMessageID returns the open text message ID, or the last one when
// called after it was closed.
func (e *Emitter) closeOpenBlocks() {
	e.closeText()
	e.closeReasoning()
}

func (e *Emitter) closeText() {
	e.TextEnd()
}

func (e *Emitter) closeReasoning() {
	e.ReasoningEnd()
}
