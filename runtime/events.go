// Package runtime holds the protocol-neutral execution layer shared by the
// AG-UI and A2A servers. It turns one agent turn into a stream of events and
// leaves wire formatting to the protocol adapters.
package runtime

import (
	"context"

	"github.com/tk103331/oni3/tools"
	"github.com/tk103331/oni3/tools/approval"
)

// Event types emitted by Runner.
const (
	EventRunStart  = "run_start"
	EventContent   = "content"
	EventReasoning = "reasoning"
	EventToolStart = "tool_start"
	EventToolEnd   = "tool_end"
	EventTodos     = "todos"
	EventInterrupt = "interrupt"
	EventError     = "error"
	EventRunEnd    = "run_end"
)

// Event is one protocol-neutral execution event.
type Event struct {
	Seq      int64  `json:"seq"`
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
	Type     string `json:"type"`
	Data     any    `json:"data,omitempty"`
}

// RunStartData announces a run and the session it belongs to.
type RunStartData struct {
	Agent   string `json:"agent"`
	Model   string `json:"model"`
	Session string `json:"session_id"`
}

// TextData carries an incremental text or reasoning delta.
type TextData struct {
	Text string `json:"text"`
}

// ToolStartData reports a tool call requested by the model.
type ToolStartData struct {
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
}

// ToolEndData reports a completed tool call.
type ToolEndData struct {
	Name   string `json:"name"`
	CallID string `json:"call_id"`
	Result string `json:"result"`
	Failed bool   `json:"failed"`
}

// TodosData carries the latest task list snapshot.
type TodosData struct {
	Items []tools.TodoItem `json:"items"`
}

// InterruptData describes a question or an approval request. The protocol
// adapter decides how to present it and how to resume the run.
type InterruptData struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"` // question or approval
	Question string           `json:"question,omitempty"`
	Options  []string         `json:"options,omitempty"`
	Request  approval.Request `json:"request,omitempty"`
}

// ErrorData reports a failed turn.
type ErrorData struct {
	Message string `json:"message"`
}

// RunEndData reports why a turn stopped.
type RunEndData struct {
	Stop      string `json:"stop"` // end, error, cancelled
	SessionID string `json:"session_id"`
	Answer    string `json:"answer,omitempty"`
}

// Response is the answer to an interrupt.
type Response struct {
	Value    string `json:"value,omitempty"`
	Approved bool   `json:"approved,omitempty"`
	Remember bool   `json:"remember,omitempty"`
}

// Interactor bridges ask_user, approvals and todo updates to a protocol.
// Implementations may block until a client answers; the run goroutine is kept
// alive by the adapter while the HTTP response is parked.
type Interactor interface {
	Ask(ctx context.Context, data InterruptData) (Response, error)
	Approve(ctx context.Context, data InterruptData) (Response, error)
	Todos(ctx context.Context, data TodosData) error
}
