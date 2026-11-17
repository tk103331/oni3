// Package client implements the non-streaming A2A 1.0 JSON-RPC client.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tk103331/oni3/protocol/a2a/wire"
)

const (
	clientBodyLimit = 8 << 20
	clientHTTPDelay = 60 * time.Second
)

const (
	MethodSendMessage          = wire.MethodSendMessage
	MethodSendStreamingMessage = wire.MethodSendStreamingMessage
	MethodGetTask              = wire.MethodGetTask
	MethodListTasks            = wire.MethodListTasks
	MethodCancelTask           = wire.MethodCancelTask
	MethodSubscribeToTask      = wire.MethodSubscribeToTask
)

const (
	StateSubmitted     = wire.StateSubmitted
	StateWorking       = wire.StateWorking
	StateInputRequired = wire.StateInputRequired
	StateCompleted     = wire.StateCompleted
	StateCanceled      = wire.StateCanceled
	StateFailed        = wire.StateFailed
	StateRejected      = wire.StateRejected
	StateAuthRequired  = wire.StateAuthRequired
)

const (
	RoleUser  = wire.RoleUser
	RoleAgent = wire.RoleAgent
)

type (
	AgentCard          = wire.AgentCard
	Task               = wire.Task
	TaskStatus         = wire.TaskStatus
	Message            = wire.Message
	Part               = wire.Part
	SendMessageRequest = wire.SendMessageRequest
	GetTaskRequest     = wire.GetTaskRequest
	CancelTaskRequest  = wire.CancelTaskRequest
	Request            = wire.Request
	RPCError           = wire.RPCError
)

// Client is a non-streaming A2A JSON-RPC client bound to one remote agent.
type Client struct {
	Name     string
	Card     AgentCard
	Endpoint string

	token  string
	http   *http.Client
	nextID atomic.Int64
}

// Discover fetches an Agent Card and resolves its JSON-RPC endpoint. An empty
// path on cardURL is treated as a base URL and gets the well-known card path
// appended.
func Discover(ctx context.Context, name, cardURL, token string, httpClient *http.Client) (*Client, error) {
	cardURL = strings.TrimSpace(cardURL)
	if cardURL == "" {
		return nil, fmt.Errorf("card_url is required")
	}
	parsed, err := url.Parse(cardURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("card_url must be an http(s) URL")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/.well-known/agent-card.json"
		cardURL = parsed.String()
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: clientHTTPDelay}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch agent card: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, clientBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("read agent card: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch agent card: HTTP %d: %s", response.StatusCode, shortBody(body))
	}

	var card AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, fmt.Errorf("decode agent card: %w", err)
	}
	endpoint, err := resolveEndpoint(card, parsed)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(card.Name) == "" {
		card.Name = strings.TrimSpace(name)
	}
	if strings.TrimSpace(card.URL) == "" {
		card.URL = endpoint
	}
	return &Client{
		Name:     strings.TrimSpace(name),
		Card:     card,
		Endpoint: endpoint,
		token:    token,
		http:     httpClient,
	}, nil
}

func resolveEndpoint(card AgentCard, base *url.URL) (string, error) {
	for _, iface := range card.SupportedInterfaces {
		if strings.EqualFold(strings.TrimSpace(iface.ProtocolBinding), "JSONRPC") && strings.TrimSpace(iface.URL) != "" {
			return absoluteEndpoint(base, iface.URL)
		}
	}
	if strings.TrimSpace(card.URL) != "" {
		return absoluteEndpoint(base, card.URL)
	}
	for _, iface := range card.SupportedInterfaces {
		if strings.TrimSpace(iface.URL) != "" {
			return absoluteEndpoint(base, iface.URL)
		}
	}
	return "", fmt.Errorf("agent card has no A2A endpoint")
}

func absoluteEndpoint(base *url.URL, raw string) (string, error) {
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid A2A endpoint: %w", err)
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf("A2A endpoint must use http or https")
	}
	if resolved.Host == "" {
		return "", fmt.Errorf("A2A endpoint has no host")
	}
	return resolved.String(), nil
}

// SendMessage sends a text message. When taskID is set the message resumes
// that task; otherwise a new task is started. The result is either a Task to
// poll or a direct Message when the remote agent answers synchronously.
func (c *Client) SendMessage(ctx context.Context, text, contextID, taskID string) (*Task, *Message, error) {
	message := Message{
		Kind:      "message",
		MessageID: "msg-" + randomID(),
		Role:      RoleUser,
		TaskID:    strings.TrimSpace(taskID),
		ContextID: strings.TrimSpace(contextID),
		Parts:     []Part{wire.TextPart(text)},
	}
	raw, err := c.rpc(ctx, MethodSendMessage, SendMessageRequest{Message: &message})
	if err != nil {
		return nil, nil, err
	}
	raw = unwrapResult(raw)
	if task, ok := decodeTask(raw); ok {
		return task, nil, nil
	}
	if message, ok := decodeMessage(raw); ok {
		return nil, message, nil
	}
	return nil, nil, fmt.Errorf("unexpected SendMessage result")
}

// GetTask fetches the current task state.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	raw, err := c.rpc(ctx, MethodGetTask, GetTaskRequest{ID: strings.TrimSpace(taskID)})
	if err != nil {
		return nil, err
	}
	raw = unwrapResult(raw)
	task, ok := decodeTask(raw)
	if !ok {
		return nil, fmt.Errorf("unexpected GetTask result")
	}
	return task, nil
}

// CancelTask requests cancellation of a running task.
func (c *Client) CancelTask(ctx context.Context, taskID string) (*Task, error) {
	raw, err := c.rpc(ctx, MethodCancelTask, CancelTaskRequest{ID: strings.TrimSpace(taskID)})
	if err != nil {
		return nil, err
	}
	raw = unwrapResult(raw)
	task, ok := decodeTask(raw)
	if !ok {
		return nil, fmt.Errorf("unexpected CancelTask result")
	}
	return task, nil
}

// WaitTask polls until the task completes, fails, is canceled, or asks for
// input. Transient lookup errors are retried a few times before giving up.
func (c *Client) WaitTask(ctx context.Context, taskID string, interval time.Duration) (*Task, error) {
	if interval <= 0 {
		interval = time.Second
	}
	failures := 0
	for {
		task, err := c.GetTask(ctx, taskID)
		if err == nil {
			failures = 0
			state := NormalizeState(task.Status.State)
			switch {
			case state == "":
				return nil, fmt.Errorf("remote task %s has no state", taskID)
			case IsTerminal(state), IsInputRequired(state):
				return task, nil
			}
		} else {
			failures++
			if failures >= 3 {
				return nil, err
			}
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Answer extracts the textual result of a finished task.
func (c *Client) Answer(task *Task) string {
	if task == nil {
		return ""
	}
	var out strings.Builder
	for _, artifact := range task.Artifacts {
		text := partsText(artifact.Parts)
		if text == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(text)
	}
	if out.Len() > 0 {
		return out.String()
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		if !wire.IsAgentRole(task.History[i].Role) {
			continue
		}
		if text := strings.TrimSpace(task.History[i].Text()); text != "" {
			return text
		}
	}
	if task.Status.Message != nil {
		return strings.TrimSpace(task.Status.Message.Text())
	}
	return ""
}

// Question returns the prompt attached to an input-required task.
func (c *Client) Question(task *Task) string {
	if task != nil && task.Status.Message != nil {
		if text := strings.TrimSpace(task.Status.Message.Text()); text != "" {
			return text
		}
	}
	return "remote agent requires additional input"
}

// Options returns the choices attached to an input-required task, when present.
func (c *Client) Options(task *Task) []string {
	if task == nil || task.Status.Message == nil || task.Status.Message.Metadata == nil {
		return nil
	}
	return stringList(task.Status.Message.Metadata["options"])
}

func (c *Client) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	body, err := json.Marshal(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, clientBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", method, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d: %s", method, response.StatusCode, shortBody(payload))
	}

	var result struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", method, err)
	}
	if result.Error != nil {
		return nil, result.Error
	}
	if len(result.Result) == 0 {
		return nil, fmt.Errorf("%s: response has no result", method)
	}
	return result.Result, nil
}

func unwrapResult(raw json.RawMessage) json.RawMessage {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return raw
	}
	if task, ok := wrapper["task"]; ok {
		return task
	}
	if message, ok := wrapper["message"]; ok {
		return message
	}
	return raw
}

func decodeTask(raw json.RawMessage) (*Task, bool) {
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, false
	}
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Status.State) == "" {
		return nil, false
	}
	for i := range task.History {
		task.History[i].Role = wire.NormalizeRole(task.History[i].Role)
	}
	if task.Status.Message != nil {
		task.Status.Message.Role = wire.NormalizeRole(task.Status.Message.Role)
	}
	return &task, true
}

func decodeMessage(raw json.RawMessage) (*Message, bool) {
	var message Message
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, false
	}
	if strings.TrimSpace(message.MessageID) == "" || strings.TrimSpace(message.Role) == "" {
		return nil, false
	}
	message.Role = wire.NormalizeRole(message.Role)
	return &message, true
}

// NormalizeState maps A2A 0.3 lowercase states and 1.0 enum values to the 1.0
// wire form.
func NormalizeState(state string) string {
	return wire.NormalizeState(state)
}

// IsTerminal reports whether state ends a task.
func IsTerminal(state string) bool {
	return wire.IsTerminal(state)
}

// IsInputRequired reports whether state pauses a task for user input.
func IsInputRequired(state string) bool {
	return wire.IsInputRequired(state)
}

func partsText(parts []Part) string {
	var out strings.Builder
	for _, part := range parts {
		if part.Text == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(part.Text)
	}
	return out.String()
}

func stringList(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func shortBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		text = text[:300] + "..."
	}
	return text
}

func randomID() string {
	var buf [10]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(buf[:])
}
