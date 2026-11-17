// Package agui implements the AG-UI protocol over Server-Sent Events without
// depending on a protocol SDK.
package agui

import "encoding/json"

// RunAgentInput is the body of a POST /agui request.
type RunAgentInput struct {
	ThreadID       string          `json:"threadId"`
	RunID          string          `json:"runId"`
	Messages       []Message       `json:"messages"`
	Tools          []Tool          `json:"tools,omitempty"`
	Context        json.RawMessage `json:"context,omitempty"`
	State          json.RawMessage `json:"state,omitempty"`
	ForwardedProps map[string]any  `json:"forwardedProps,omitempty"`
	Resume         []Resume        `json:"resume,omitempty"`
}

// Message is one AG-UI chat message. Content is a string or a list of parts.
type Message struct {
	ID         string          `json:"id,omitempty"`
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
}

// ContentPart is one part of a multi-part message.
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Tool describes a client-side tool. Oni3 uses its configured tools, so the
// list is accepted for compatibility and not registered with the model.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is an OpenAI-compatible assistant tool call.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name and JSON-encoded arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Interrupt asks the client for input before the run can continue.
type Interrupt struct {
	ID             string         `json:"id"`
	Reason         string         `json:"reason"`
	Message        string         `json:"message,omitempty"`
	ToolCallID     string         `json:"toolCallId,omitempty"`
	ResponseSchema map[string]any `json:"responseSchema,omitempty"`
	ExpiresAt      string         `json:"expiresAt,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Resume answers one interrupt.
type Resume struct {
	InterruptID string          `json:"interruptId"`
	Status      string          `json:"status"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// Text returns the textual content of a message.
func (m Message) Text() string {
	if len(m.Content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return text
	}
	var parts []ContentPart
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		out := ""
		for _, part := range parts {
			if part.Type == "text" {
				out += part.Text
			}
		}
		return out
	}
	return ""
}
