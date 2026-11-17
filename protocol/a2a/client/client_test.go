package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func completedTask(id string) map[string]any {
	return map[string]any{
		"id":        id,
		"contextId": "ctx-1",
		"status":    map[string]any{"state": StateCompleted},
		"artifacts": []any{
			map[string]any{
				"artifactId": "artifact-1",
				"parts":      []any{map[string]any{"text": "remote answer"}},
			},
		},
	}
}

func TestDiscoverCardAtRootAndRelativeEndpoint(t *testing.T) {
	var cardHits, rpcHits atomic.Int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/.well-known/agent-card.json", func(w http.ResponseWriter, r *http.Request) {
		cardHits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("card authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "remote",
			"supportedInterfaces": []any{
				map[string]any{"url": "/rpc", "protocolBinding": "JSONRPC", "protocolVersion": "1.0"},
			},
		})
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		rpcHits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("rpc authorization = %q", got)
		}
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		var params struct {
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if params.Message.Role != RoleUser {
			t.Errorf("send role = %q, want %q", params.Message.Role, RoleUser)
		}
		writeRPCResult(t, w, request.ID, map[string]any{"task": completedTask("task-1")})
	})

	ctx := context.Background()
	remote, err := Discover(ctx, "remote", server.URL, "secret", server.Client())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if remote.Endpoint != server.URL+"/rpc" {
		t.Fatalf("endpoint = %q", remote.Endpoint)
	}
	if cardHits.Load() != 1 {
		t.Fatalf("card hits = %d", cardHits.Load())
	}
	task, message, err := remote.SendMessage(ctx, "hello", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if message != nil || task == nil || task.ID != "task-1" {
		t.Fatalf("task = %+v, message = %+v", task, message)
	}
	if rpcHits.Load() != 1 {
		t.Fatalf("rpc hits = %d", rpcHits.Load())
	}
}

func TestSendMessageReturnsDirectMessage(t *testing.T) {
	server := newRPCServer(t, func(t *testing.T, request rpcRequest) any {
		return map[string]any{"message": map[string]any{
			"kind":      "message",
			"messageId": "msg-1",
			"role":      "agent",
			"parts":     []any{map[string]any{"text": "fast answer"}},
		}}
	})
	defer server.Close()
	remote := discoverTestClient(t, server)

	task, message, err := remote.SendMessage(context.Background(), "hello", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if task != nil || message == nil {
		t.Fatalf("task = %+v, message = %+v", task, message)
	}
	if message.Text() != "fast answer" {
		t.Fatalf("message text = %q", message.Text())
	}
}

func TestGetTaskArtifactAnswer(t *testing.T) {
	server := newRPCServer(t, func(t *testing.T, request rpcRequest) any {
		if request.Method != MethodGetTask {
			t.Errorf("method = %q", request.Method)
		}
		return completedTask("task-9")
	})
	defer server.Close()
	remote := discoverTestClient(t, server)

	task, err := remote.GetTask(context.Background(), "task-9")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if answer := remote.Answer(task); answer != "remote answer" {
		t.Fatalf("answer = %q", answer)
	}
}

func TestAnswerAcceptsV1AgentRole(t *testing.T) {
	task := &Task{
		ID:     "task-history",
		Status: wireTaskStatus(StateCompleted, nil),
		History: []Message{
			{MessageID: "msg-user", Role: RoleUser, Parts: []Part{{Text: "hello"}}},
			{MessageID: "msg-agent", Role: RoleAgent, Parts: []Part{{Text: "history answer"}}},
		},
	}
	remote := &Client{}
	if got := remote.Answer(task); got != "history answer" {
		t.Fatalf("answer = %q", got)
	}
}

func TestInputRequiredQuestionAndOptions(t *testing.T) {
	task := &Task{
		ID: "task-2",
		Status: wireTaskStatus(StateInputRequired, &Message{
			MessageID: "msg-2",
			Role:      "agent",
			Parts:     []Part{{Text: "Which city?"}},
			Metadata:  map[string]any{"options": []any{"Paris", "Tokyo"}},
		}),
	}
	remote := &Client{}
	if got := remote.Question(task); got != "Which city?" {
		t.Fatalf("question = %q", got)
	}
	options := remote.Options(task)
	if len(options) != 2 || options[0] != "Paris" || options[1] != "Tokyo" {
		t.Fatalf("options = %v", options)
	}
	if !IsInputRequired(task.Status.State) || IsTerminal(task.Status.State) {
		t.Fatalf("state helpers disagree for %s", task.Status.State)
	}
}

func TestRPCErrorAndHTTPStatus(t *testing.T) {
	server := newRPCServer(t, func(t *testing.T, request rpcRequest) any {
		return nil
	})
	defer server.Close()
	remote := discoverTestClient(t, server)

	_, err := remote.GetTask(context.Background(), "task-1")
	if err == nil || !strings.Contains(err.Error(), "JSON-RPC -32601") {
		t.Fatalf("rpc error = %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer bad.Close()
	broken := &Client{Endpoint: bad.URL, http: bad.Client()}
	_, err = broken.GetTask(context.Background(), "task-1")
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("http error = %v", err)
	}
}

func TestWaitTaskPollsUntilCompleted(t *testing.T) {
	var calls atomic.Int32
	server := newRPCServer(t, func(t *testing.T, request rpcRequest) any {
		if calls.Add(1) < 3 {
			return map[string]any{"id": "task-3", "status": map[string]any{"state": StateWorking}}
		}
		return completedTask("task-3")
	})
	defer server.Close()
	remote := discoverTestClient(t, server)

	task, err := remote.WaitTask(context.Background(), "task-3", time.Millisecond)
	if err != nil {
		t.Fatalf("WaitTask: %v", err)
	}
	if task.Status.State != StateCompleted || remote.Answer(task) != "remote answer" {
		t.Fatalf("task = %+v", task)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestWaitTaskRetriesTransientLookupErrors(t *testing.T) {
	var calls atomic.Int32
	server := newRPCServer(t, func(t *testing.T, request rpcRequest) any {
		if calls.Add(1) == 1 {
			return nil
		}
		return completedTask("task-4")
	})
	defer server.Close()
	remote := discoverTestClient(t, server)

	task, err := remote.WaitTask(context.Background(), "task-4", time.Millisecond)
	if err != nil {
		t.Fatalf("WaitTask: %v", err)
	}
	if task.Status.State != StateCompleted || calls.Load() != 2 {
		t.Fatalf("task = %+v, calls = %d", task, calls.Load())
	}
}

// newRPCServer serves a card and routes JSON-RPC calls to result. Returning nil
// produces a JSON-RPC method-not-found error.
func newRPCServer(t *testing.T, result func(*testing.T, rpcRequest) any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/agent-card.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "remote",
			"url":  server.URL + "/rpc",
		})
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		value := result(t, request)
		if value == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"error":   map[string]any{"code": -32601, "message": "not found"},
			})
			return
		}
		writeRPCResult(t, w, request.ID, value)
	})
	return server
}

func discoverTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	remote, err := Discover(context.Background(), "remote", server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return remote
}

func wireTaskStatus(state string, message *Message) (status TaskStatus) {
	status.State = state
	status.Message = message
	return status
}
