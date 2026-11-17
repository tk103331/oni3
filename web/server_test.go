package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/tk103331/oni3/config"
)

type mockRequest struct {
	Model    string           `json:"model"`
	Messages []map[string]any `json:"messages"`
}

type mockLLM struct {
	server  *httptest.Server
	mu      sync.Mutex
	reqs    []mockRequest
	respond func(w http.ResponseWriter, req mockRequest, call int)
}

func newMockLLM(t *testing.T, respond func(w http.ResponseWriter, req mockRequest, call int)) *mockLLM {
	t.Helper()
	mock := &mockLLM{respond: respond}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var req mockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mock.mu.Lock()
		call := len(mock.reqs)
		mock.reqs = append(mock.reqs, req)
		mock.mu.Unlock()
		mock.respond(w, req, call)
	}))
	t.Cleanup(mock.server.Close)
	return mock
}

func (m *mockLLM) requests() []mockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mockRequest(nil), m.reqs...)
}

func streamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
}

func writeChunk(w http.ResponseWriter, model string, delta map[string]any, finish any) {
	chunk := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": model,
		"choices": []any{map[string]any{
			"index": 0, "delta": delta, "finish_reason": finish,
		}},
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func streamText(w http.ResponseWriter, model, text string) {
	streamHeaders(w)
	writeChunk(w, model, map[string]any{"role": "assistant"}, nil)
	writeChunk(w, model, map[string]any{"content": text}, nil)
	writeChunk(w, model, map[string]any{}, "stop")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func streamToolCall(w http.ResponseWriter, model, name, arguments string) {
	streamHeaders(w)
	writeChunk(w, model, map[string]any{
		"role": "assistant",
		"tool_calls": []any{map[string]any{
			"index": 0,
			"id":    "call_1",
			"type":  "function",
			"function": map[string]any{
				"name":      name,
				"arguments": arguments,
			},
		}},
	}, nil)
	writeChunk(w, model, map[string]any{}, "tool_calls")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// testConfig loads a config through config.LoadConfig so the global config
// used by the model factory points at the mock upstream.
func testConfig(t *testing.T, llmURL, extra string) *config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	source := fmt.Sprintf(`providers:
  mock:
    type: openai
    base_url: %s/v1
    api_key: test-key
models:
  base_model:
    provider: mock
    model: mock-base
  override_model:
    provider: mock
    model: mock-override
agents:
  helper:
    system: You are a test helper.
    model: base_model
`, llmURL)
	source += extra

	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func newTestServer(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()
	app, err := New(cfg, Options{
		DefaultAgent:    "helper",
		WorkDir:         "/tmp/oni3",
		Version:         "test",
		EnableMCPServer: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return server
}

func getJSON(t *testing.T, url string, target any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			t.Fatalf("decode %s: %v (%s)", url, err, body)
		}
	}
	return resp
}

func postJSON(t *testing.T, url string, value any) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// readEvents decodes the SSE frames of a chat response.
func readEvents(t *testing.T, body io.Reader, onEvent func(map[string]any)) []map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var events []map[string]any
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode event: %v (%s)", err, line)
		}
		events = append(events, event)
		if onEvent != nil {
			onEvent(event)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func eventText(events []map[string]any, kind string) string {
	var text strings.Builder
	for _, event := range events {
		if event["type"] == kind {
			text.WriteString(fmt.Sprint(event["text"]))
		}
	}
	return text.String()
}

func lastDone(events []map[string]any) map[string]any {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i]["type"] == "done" {
			return events[i]
		}
	}
	return nil
}

func findEvent(events []map[string]any, kind string) map[string]any {
	for _, event := range events {
		if event["type"] == kind {
			return event
		}
	}
	return nil
}

func TestBootstrapAndStaticPage(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "zeta",
		Models: map[string]config.Model{
			"zeta":  {Provider: "openai", Model: "gpt-4"},
			"alpha": {Provider: "deepseek", Model: "deepseek-chat"},
		},
		Agents: map[string]config.Agent{
			"helper":       {Description: "Test helper", Model: "alpha"},
			"search agent": {Model: "zeta"},
		},
	}
	server := newTestServer(t, cfg)

	var boot struct {
		DefaultAgent string   `json:"default_agent"`
		DefaultModel string   `json:"default_model"`
		Models       []string `json:"models"`
		Agents       []struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			DefaultModel string `json:"default_model"`
		} `json:"agents"`
		WorkDir string `json:"workdir"`
		Version string `json:"version"`
	}
	getJSON(t, server.URL+"/api/bootstrap", &boot)
	if boot.DefaultAgent != "helper" || boot.WorkDir != "/tmp/oni3" || boot.Version != "test" {
		t.Fatalf("bootstrap = %+v", boot)
	}
	if strings.Join(boot.Models, ",") != "alpha,zeta" {
		t.Fatalf("models = %v", boot.Models)
	}
	if boot.DefaultModel != "zeta" {
		t.Fatalf("default model = %q", boot.DefaultModel)
	}
	if len(boot.Agents) != 2 || boot.Agents[0].Name != "helper" || boot.Agents[0].DefaultModel != "alpha" {
		t.Fatalf("agents = %+v", boot.Agents)
	}
	if boot.Agents[1].Description != "search agent" {
		t.Fatalf("description fallback = %q", boot.Agents[1].Description)
	}

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "ONI3 Web") {
		t.Fatalf("static page: status=%d len=%d", resp.StatusCode, len(page))
	}
	for _, marker := range []string{`id="sessionSearch"`, `id="themeBtn"`, `class="composer-selects"`, `href="/docs"`} {
		if !strings.Contains(string(page), marker) {
			t.Fatalf("static page is missing %s", marker)
		}
	}

	docsResp, err := http.Get(server.URL + "/docs")
	if err != nil {
		t.Fatal(err)
	}
	defer docsResp.Body.Close()
	docs, _ := io.ReadAll(docsResp.Body)
	if docsResp.StatusCode != http.StatusOK {
		t.Fatalf("docs status = %d", docsResp.StatusCode)
	}
	for _, marker := range []string{"/api/chat", "/agui", "/a2a", "/mcp", "call_agent"} {
		if !strings.Contains(string(docs), marker) {
			t.Fatalf("docs page is missing %s", marker)
		}
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	foreign, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Body.Close()
	if foreign.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", foreign.StatusCode)
	}
}

func TestMCPHTTPEndpoint(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 0,
			"model":   "mock-base",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "answer from mcp web"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer modelServer.Close()
	cfg := testConfig(t, modelServer.URL, "")
	server := newTestServer(t, cfg)

	endpoint := server.URL + "/mcp"
	mcpClient, err := mcpclient.NewStreamableHttpClient(endpoint)
	if err != nil {
		t.Fatalf("NewStreamableHttpClient: %v", err)
	}
	defer mcpClient.Close()
	ctx := context.Background()
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "web-test", Version: "1.0.0"}
	if _, err := mcpClient.Initialize(ctx, initRequest); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	list, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(list.Tools))
	for _, item := range list.Tools {
		names = append(names, item.Name)
	}
	if strings.Join(names, ",") != "call_agent,list_agents,list_models" {
		t.Fatalf("tools = %v", names)
	}

	request := mcp.CallToolRequest{}
	request.Params.Name = "call_agent"
	request.Params.Arguments = map[string]any{"agent": "helper", "prompt": "hello"}
	result, err := mcpClient.CallTool(ctx, request)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool result = %+v", result)
	}
	if got := result.Content[0].(mcp.TextContent).Text; got != "answer from mcp web" {
		t.Fatalf("answer = %q", got)
	}
}

func TestMCPSSEEndpoint(t *testing.T) {
	cfg := testConfig(t, "http://127.0.0.1:1", "")
	server := newTestServer(t, cfg)

	mcpClient, err := mcpclient.NewSSEMCPClient(server.URL + "/mcp/sse")
	if err != nil {
		t.Fatalf("NewSSEMCPClient: %v", err)
	}
	defer mcpClient.Close()
	ctx := context.Background()
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "web-sse-test", Version: "1.0.0"}
	if _, err := mcpClient.Initialize(ctx, initRequest); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	list, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 3 {
		t.Fatalf("tools = %+v", list.Tools)
	}
}

func TestDisableWebKeepsProtocols(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{"base": {Provider: "openai", Model: "gpt-4"}},
		Agents: map[string]config.Agent{"helper": {Model: "base"}},
	}
	app, err := New(cfg, Options{
		DefaultAgent:    "helper",
		WorkDir:         "/tmp/oni3",
		Version:         "test",
		DisableWeb:      true,
		EnableMCPServer: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	for _, path := range []string{"/", "/docs", "/login", "/api/models", "/api/bootstrap"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, resp.StatusCode)
		}
	}

	resp, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}

	mcpClient, err := mcpclient.NewStreamableHttpClient(server.URL + "/mcp")
	if err != nil {
		t.Fatalf("NewStreamableHttpClient: %v", err)
	}
	defer mcpClient.Close()
	ctx := context.Background()
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "no-web-test", Version: "1.0.0"}
	if _, err := mcpClient.Initialize(ctx, initRequest); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 3 {
		t.Fatalf("tools = %+v", tools.Tools)
	}
}

func TestNewRequiresAtLeastOneEndpoint(t *testing.T) {
	_, err := New(&config.Config{
		Models: map[string]config.Model{"base": {Provider: "openai", Model: "gpt-4"}},
		Agents: map[string]config.Agent{"helper": {Model: "base"}},
	}, Options{DisableWeb: true})
	if err == nil {
		t.Fatal("expected an error when every endpoint is disabled")
	}
}

func TestChatStreamsPersistsAndReplaysHistory(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		streamText(w, req.Model, "mock answer from "+req.Model)
	})
	cfg := testConfig(t, llm.server.URL, "")
	server := newTestServer(t, cfg)

	resp := postJSON(t, server.URL+"/api/chat", map[string]any{
		"agent": "helper", "prompt": "hello",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("chat status = %d: %s", resp.StatusCode, body)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q", contentType)
	}

	events := readEvents(t, resp.Body, nil)
	sessionEvent := findEvent(events, "session")
	if sessionEvent == nil || sessionEvent["id"] == "" {
		t.Fatalf("missing session event: %+v", events)
	}
	sessionID := fmt.Sprint(sessionEvent["id"])
	if got := eventText(events, "content"); got != "mock answer from mock-base" {
		t.Fatalf("content = %q", got)
	}
	if done := lastDone(events); done == nil || done["stop"] != "end" {
		t.Fatalf("done = %+v", done)
	}

	reqs := llm.requests()
	if len(reqs) != 1 || reqs[0].Model != "mock-base" {
		t.Fatalf("upstream requests = %+v", reqs)
	}
	if len(reqs[0].Messages) < 2 || reqs[0].Messages[len(reqs[0].Messages)-1]["role"] != "user" {
		t.Fatalf("messages = %+v", reqs[0].Messages)
	}

	var sessions struct {
		Sessions []SessionSummary `json:"sessions"`
	}
	getJSON(t, server.URL+"/api/sessions", &sessions)
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].ID != sessionID || sessions.Sessions[0].Turns != 1 {
		t.Fatalf("sessions = %+v", sessions.Sessions)
	}
	if sessions.Sessions[0].Title != "hello" {
		t.Fatalf("title = %q", sessions.Sessions[0].Title)
	}

	var detail struct {
		Session SessionSummary `json:"session"`
		Events  []struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"events"`
	}
	getJSON(t, server.URL+"/api/sessions/"+sessionID, &detail)
	types := make([]string, 0, len(detail.Events))
	for _, event := range detail.Events {
		types = append(types, event.Type)
	}
	if strings.Join(types, ",") != "session,user,assistant" {
		t.Fatalf("event types = %v", types)
	}

	// The second turn replays history and honors an explicit model override.
	resp2 := postJSON(t, server.URL+"/api/chat", map[string]any{
		"agent": "helper", "prompt": "again", "model": "override_model", "session_id": sessionID,
	})
	defer resp2.Body.Close()
	events2 := readEvents(t, resp2.Body, nil)
	if got := eventText(events2, "content"); got != "mock answer from mock-override" {
		t.Fatalf("override content = %q", got)
	}
	reqs = llm.requests()
	if len(reqs) != 2 || reqs[1].Model != "mock-override" {
		t.Fatalf("upstream requests = %+v", reqs)
	}
	messages := reqs[1].Messages
	sawAssistant := false
	for _, message := range messages {
		if message["role"] == "assistant" && message["content"] == "mock answer from mock-base" {
			sawAssistant = true
		}
	}
	if !sawAssistant {
		t.Fatalf("history not replayed: %+v", messages)
	}
	if event := findEvent(events2, "session"); event == nil || event["id"] != sessionID {
		t.Fatalf("session id changed: %+v", event)
	}
}

func TestSessionSearch(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		streamText(w, req.Model, "answer "+fmt.Sprint(call))
	})
	cfg := testConfig(t, llm.server.URL, "")
	server := newTestServer(t, cfg)

	for _, prompt := range []string{"alpha topic", "beta topic"} {
		resp := postJSON(t, server.URL+"/api/chat", map[string]any{
			"agent": "helper", "prompt": prompt,
		})
		events := readEvents(t, resp.Body, nil)
		resp.Body.Close()
		if done := lastDone(events); done == nil {
			t.Fatalf("chat %q did not finish: %+v", prompt, events)
		}
	}

	type sessionList struct {
		Sessions []SessionSummary `json:"sessions"`
	}
	for _, tc := range []struct {
		query string
		want  int
		title string
	}{
		{"BETA", 1, "beta topic"},
		{"answer 0", 1, "alpha topic"},
		{"helper", 2, ""},
		{"missing", 0, ""},
	} {
		var sessions sessionList
		getJSON(t, server.URL+"/api/sessions?q="+url.QueryEscape(tc.query), &sessions)
		if len(sessions.Sessions) != tc.want {
			t.Fatalf("query %q = %+v", tc.query, sessions.Sessions)
		}
		if tc.title != "" && sessions.Sessions[0].Title != tc.title {
			t.Fatalf("query %q title = %q", tc.query, sessions.Sessions[0].Title)
		}
	}
}

func TestChatValidation(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		streamText(w, req.Model, "ok")
	})
	cfg := testConfig(t, llm.server.URL, "")
	server := newTestServer(t, cfg)

	cases := []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"missing prompt", map[string]any{"agent": "helper"}, http.StatusBadRequest},
		{"missing agent", map[string]any{"prompt": "hi"}, http.StatusBadRequest},
		{"unknown agent", map[string]any{"agent": "nope", "prompt": "hi"}, http.StatusBadRequest},
		{"unknown model", map[string]any{"agent": "helper", "prompt": "hi", "model": "nope"}, http.StatusBadRequest},
		{"unknown session", map[string]any{"agent": "helper", "prompt": "hi", "session_id": "does-not-exist"}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, server.URL+"/api/chat", tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tc.status, body)
			}
		})
	}
	if len(llm.requests()) != 0 {
		t.Fatalf("invalid requests reached the model: %+v", llm.requests())
	}
}

func TestAskUserBridgesToBrowser(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		if call == 0 {
			streamToolCall(w, req.Model, "ask_user",
				`{"question":"Pick one","options":["a","b"]}`)
			return
		}
		streamText(w, req.Model, "answer received")
	})
	cfg := testConfig(t, llm.server.URL, `  interactive:
    system: You ask questions.
    model: base_model
    tools: [ask_user]
tools:
  ask_user:
    type: ask_user
`)
	server := newTestServer(t, cfg)

	resp := postJSON(t, server.URL+"/api/chat", map[string]any{
		"agent": "interactive", "prompt": "start",
	})
	defer resp.Body.Close()

	var questionAnswered bool
	events := readEvents(t, resp.Body, func(event map[string]any) {
		if event["type"] != "question" {
			return
		}
		if event["question"] != "Pick one" {
			t.Errorf("question = %+v", event)
		}
		answer := postJSON(t, server.URL+"/api/respond", map[string]any{
			"id": event["id"], "value": "b",
		})
		defer answer.Body.Close()
		if answer.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(answer.Body)
			t.Errorf("respond status = %d (%s)", answer.StatusCode, body)
		}
		questionAnswered = true
	})
	if !questionAnswered {
		t.Fatalf("no question event: %+v", events)
	}
	if got := eventText(events, "content"); got != "answer received" {
		t.Fatalf("content = %q", got)
	}
	if done := lastDone(events); done == nil || done["stop"] != "end" {
		t.Fatalf("done = %+v", done)
	}

	// The answer must be persisted and handed back to the model as a tool result.
	reqs := llm.requests()
	if len(reqs) != 2 {
		t.Fatalf("upstream calls = %d", len(reqs))
	}
	var toolResult string
	for _, message := range reqs[1].Messages {
		if message["role"] == "tool" {
			toolResult = fmt.Sprint(message["content"])
		}
	}
	if !strings.Contains(toolResult, "b") {
		t.Fatalf("tool result = %q", toolResult)
	}
}

func TestApprovalDenyBridgesToBrowser(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		if call == 0 {
			streamToolCall(w, req.Model, "command_line", `{"command":"echo hi"}`)
			return
		}
		streamText(w, req.Model, "stopped")
	})
	cfg := testConfig(t, llm.server.URL, `  shell:
    system: You run commands.
    model: base_model
    tools: [command_line]
tools:
  command_line:
    type: command_line
`)
	server := newTestServer(t, cfg)

	resp := postJSON(t, server.URL+"/api/chat", map[string]any{
		"agent": "shell", "prompt": "run it",
	})
	defer resp.Body.Close()

	var approvalSeen bool
	events := readEvents(t, resp.Body, func(event map[string]any) {
		if event["type"] != "approval" {
			return
		}
		approvalSeen = true
		if event["tool"] != "command_line" {
			t.Errorf("approval = %+v", event)
		}
		answer := postJSON(t, server.URL+"/api/respond", map[string]any{
			"id": event["id"], "approved": false,
		})
		defer answer.Body.Close()
		if answer.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(answer.Body)
			t.Errorf("respond status = %d (%s)", answer.StatusCode, body)
		}
	})
	if !approvalSeen {
		t.Fatalf("no approval event: %+v", events)
	}
	if done := lastDone(events); done == nil || done["stop"] != "end" {
		t.Fatalf("done = %+v", done)
	}

	reqs := llm.requests()
	if len(reqs) != 2 {
		t.Fatalf("upstream calls = %d", len(reqs))
	}
	var toolResult string
	for _, message := range reqs[1].Messages {
		if message["role"] == "tool" {
			toolResult = fmt.Sprint(message["content"])
		}
	}
	if !strings.Contains(toolResult, "denied") {
		t.Fatalf("tool result = %q", toolResult)
	}
}

func TestCancelUnknownSession(t *testing.T) {
	server := newTestServer(t, &config.Config{
		Models: map[string]config.Model{"base": {Provider: "openai", Model: "gpt-4"}},
		Agents: map[string]config.Agent{"helper": {Model: "base"}},
	})
	resp := postJSON(t, server.URL+"/api/cancel", map[string]any{"session_id": "nope"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d (%s)", resp.StatusCode, body)
	}
}
