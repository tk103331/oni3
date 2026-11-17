// Package a2a implements the A2A 1.0 JSON-RPC binding, agent cards and task
// storage without depending on a protocol SDK.
package a2a

import (
	"encoding/json"
	"fmt"
	"strings"
)

const ProtocolVersion = "1.0"

// Message roles defined by A2A 1.0.
const (
	RoleUser  = "ROLE_USER"
	RoleAgent = "ROLE_AGENT"
)

// Task states defined by A2A 1.0.
const (
	StateSubmitted     = "TASK_STATE_SUBMITTED"
	StateWorking       = "TASK_STATE_WORKING"
	StateInputRequired = "TASK_STATE_INPUT_REQUIRED"
	StateCompleted     = "TASK_STATE_COMPLETED"
	StateCanceled      = "TASK_STATE_CANCELED"
	StateFailed        = "TASK_STATE_FAILED"
	StateRejected      = "TASK_STATE_REJECTED"
	StateAuthRequired  = "TASK_STATE_AUTH_REQUIRED"
)

// AgentCard is the discovery document published at the well-known path.
type AgentCard struct {
	ProtocolVersion      string                    `json:"protocolVersion,omitempty"`
	Name                 string                    `json:"name"`
	Description          string                    `json:"description"`
	URL                  string                    `json:"url,omitempty"`
	PreferredTransport   string                    `json:"preferredTransport,omitempty"`
	SupportedInterfaces  []AgentInterface          `json:"supportedInterfaces"`
	Provider             *AgentProvider            `json:"provider,omitempty"`
	Version              string                    `json:"version"`
	DocumentationURL     string                    `json:"documentationUrl,omitempty"`
	Capabilities         AgentCapabilities         `json:"capabilities"`
	SecuritySchemes      map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	Security             []map[string][]string     `json:"security,omitempty"`
	SecurityRequirements []map[string][]string     `json:"securityRequirements,omitempty"`
	DefaultInputModes    []string                  `json:"defaultInputModes"`
	DefaultOutputModes   []string                  `json:"defaultOutputModes"`
	Skills               []AgentSkill              `json:"skills"`
}

// AgentInterface is one URL/transport/protocol combination.
type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
}

// AgentProvider identifies the operator of an agent.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url"`
}

// AgentCapabilities declares optional A2A features.
type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
	ExtendedAgentCard bool `json:"extendedAgentCard,omitempty"`
}

// SecurityScheme follows the OpenAPI security scheme object.
type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Description  string `json:"description,omitempty"`
	Name         string `json:"name,omitempty"`
	In           string `json:"in,omitempty"`
}

// AgentSkill describes one capability exposed by an agent.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// Message is one A2A message.
type Message struct {
	Kind             string         `json:"kind,omitempty"`
	MessageID        string         `json:"messageId"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	Role             string         `json:"role"`
	Parts            []Part         `json:"parts"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
}

// Part is a text, file or structured-data message part. The kind field is
// accepted for A2A 0.3 compatibility; A2A 1.0 infers it from the set field.
type Part struct {
	Kind      string         `json:"kind,omitempty"`
	Text      string         `json:"text,omitempty"`
	File      *FilePart      `json:"file,omitempty"`
	Data      any            `json:"data,omitempty"`
	URL       string         `json:"url,omitempty"`
	Raw       []byte         `json:"raw,omitempty"`
	Filename  string         `json:"filename,omitempty"`
	MediaType string         `json:"mediaType,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// FilePart is a file referenced by URI or supplied inline.
type FilePart struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    string `json:"bytes,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// TextPart builds a text part.
func TextPart(text string) Part {
	return Part{Kind: "text", Text: text}
}

// NormalizeRole maps A2A 0.3 lowercase roles and 1.0 enum values to the 1.0
// wire form.
func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "role_user", "user":
		return RoleUser
	case "role_agent", "agent", "assistant":
		return RoleAgent
	default:
		return strings.TrimSpace(role)
	}
}

// IsUserRole reports whether role is the A2A 1.0 user role. The 0.3 form is
// accepted as well.
func IsUserRole(role string) bool {
	return NormalizeRole(role) == RoleUser
}

// IsAgentRole reports whether role is the A2A 1.0 agent role. The 0.3 form is
// accepted as well.
func IsAgentRole(role string) bool {
	return NormalizeRole(role) == RoleAgent
}

// Text returns the concatenated text of all text parts.
func (m Message) Text() string {
	var out strings.Builder
	for _, part := range m.Parts {
		if part.Text != "" {
			out.WriteString(part.Text)
		}
	}
	return out.String()
}

// Task is one stateful A2A operation.
type Task struct {
	Kind      string         `json:"kind,omitempty"`
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	History   []Message      `json:"history,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus is the current state plus an optional explanatory message.
type TaskStatus struct {
	State     string   `json:"state"`
	Message   *Message `json:"message,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
}

// Artifact is a result generated by a task.
type Artifact struct {
	ArtifactID string         `json:"artifactId"`
	Name       string         `json:"name,omitempty"`
	Parts      []Part         `json:"parts"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// TaskStatusUpdateEvent reports a state transition.
type TaskStatusUpdateEvent struct {
	Kind      string         `json:"kind,omitempty"`
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Final     bool           `json:"final,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskArtifactUpdateEvent reports an artifact chunk.
type TaskArtifactUpdateEvent struct {
	Kind      string         `json:"kind,omitempty"`
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Artifact  Artifact       `json:"artifact"`
	Append    bool           `json:"append,omitempty"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// StreamResponse wraps one event with the A2A 1.0 oneof field name.
type StreamResponse struct {
	Task           *Task                    `json:"task,omitempty"`
	Message        *Message                 `json:"message,omitempty"`
	StatusUpdate   *TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

// SendMessageRequest is the message/send and message/stream parameter object.
type SendMessageRequest struct {
	Message       *Message       `json:"message"`
	Configuration map[string]any `json:"configuration,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// UnmarshalJSON accepts both {"message":{...}} and a bare message object.
func (r *SendMessageRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Message       *Message       `json:"message"`
		Configuration map[string]any `json:"configuration"`
		Metadata      map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.Message = wire.Message
	r.Configuration = wire.Configuration
	r.Metadata = wire.Metadata
	if r.Message == nil {
		var message Message
		if err := json.Unmarshal(data, &message); err == nil && message.Role != "" {
			r.Message = &message
		}
	}
	return nil
}

// GetTaskRequest is the task lookup parameter object.
type GetTaskRequest struct {
	ID            string `json:"id"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

// ListTasksRequest filters the task list.
type ListTasksRequest struct {
	ContextID     string `json:"contextId,omitempty"`
	Status        string `json:"status,omitempty"`
	PageSize      int    `json:"pageSize,omitempty"`
	PageToken     string `json:"pageToken,omitempty"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

// ListTasksResponse is the result of tasks/list.
type ListTasksResponse struct {
	Tasks         []*Task `json:"tasks"`
	TotalSize     int     `json:"totalSize"`
	PageSize      int     `json:"pageSize"`
	NextPageToken string  `json:"nextPageToken"`
}

// CancelTaskRequest cancels a running task.
type CancelTaskRequest struct {
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SubscribeToTaskRequest subscribes to future updates of a task.
type SubscribeToTaskRequest struct {
	ID string `json:"id"`
}

func nowTimestamp() string {
	return timeNowUTC()
}

func textMessage(role, text, taskID, contextID string) Message {
	return Message{
		Kind:      "message",
		MessageID: "msg-" + randomID(),
		Role:      role,
		TaskID:    taskID,
		ContextID: contextID,
		Parts:     []Part{TextPart(text)},
	}
}

func validateTaskID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("task id is required")
	}
	if len(id) > 128 {
		return fmt.Errorf("task id is too long")
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '-', r == '_':
		default:
			return fmt.Errorf("invalid task id: %q", id)
		}
	}
	return nil
}
