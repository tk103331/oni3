package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk103331/oni3/config"
)

func protocolConfig(t *testing.T, llmURL string, withAsk bool) *config.Config {
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
`, llmURL)
	if withAsk {
		source += `tools:
  ask_user:
    type: ask_user
agents:
  helper:
    system: You are a test helper.
    model: base_model
    tools: [ask_user]
`
	} else {
		source += `agents:
  helper:
    system: You are a test helper.
    model: base_model
`
	}

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

func newProtocolServer(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()
	app, err := New(cfg, Options{
		DefaultAgent: "helper",
		WorkDir:      "/tmp/oni3",
		Version:      "test",
		EnableAGUI:   true,
		EnableA2A:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return server
}

func TestAGUIStreamingRun(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		streamText(w, req.Model, "hello from agui")
	})
	server := newProtocolServer(t, protocolConfig(t, llm.server.URL, false))

	body := map[string]any{
		"threadId": "thread-agui-1",
		"runId":    "run-agui-1",
		"messages": []any{
			map[string]any{"id": "m1", "role": "user", "content": "hi"},
		},
		"forwardedProps": map[string]any{"agent": "helper"},
	}
	resp := postJSON(t, server.URL+"/agui", body)
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}
	events := readEvents(t, resp.Body, nil)
	if findEvent(events, "RUN_STARTED") == nil {
		t.Fatalf("missing RUN_STARTED: %v", events)
	}
	var text strings.Builder
	for _, event := range events {
		if event["type"] == "TEXT_MESSAGE_CONTENT" {
			text.WriteString(fmt.Sprint(event["delta"]))
		}
	}
	if text.String() != "hello from agui" {
		t.Fatalf("text = %q", text.String())
	}
	finished := findEvent(events, "RUN_FINISHED")
	if finished == nil {
		t.Fatalf("missing RUN_FINISHED: %v", events)
	}
	outcome, _ := finished["outcome"].(map[string]any)
	if outcome["type"] != "success" {
		t.Fatalf("outcome = %v", finished["outcome"])
	}
}

func TestAGUIInterruptAndResume(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		if call == 0 {
			streamToolCall(w, req.Model, "ask_user", `{"question":"Continue?","options":["yes","no"]}`)
			return
		}
		streamText(w, req.Model, "resumed answer")
	})
	server := newProtocolServer(t, protocolConfig(t, llm.server.URL, true))

	body := map[string]any{
		"threadId": "thread-agui-int",
		"runId":    "run-agui-int",
		"messages": []any{
			map[string]any{"role": "user", "content": "start"},
		},
		"forwardedProps": map[string]any{"agent": "helper"},
	}
	resp := postJSON(t, server.URL+"/agui", body)
	events := readEvents(t, resp.Body, nil)
	resp.Body.Close()

	finished := findEvent(events, "RUN_FINISHED")
	if finished == nil {
		t.Fatalf("missing RUN_FINISHED: %v", events)
	}
	outcome, _ := finished["outcome"].(map[string]any)
	if outcome["type"] != "interrupt" {
		t.Fatalf("outcome = %v", finished["outcome"])
	}
	interrupts, _ := outcome["interrupts"].([]any)
	if len(interrupts) != 1 {
		t.Fatalf("interrupts = %v", outcome["interrupts"])
	}
	interrupt, _ := interrupts[0].(map[string]any)
	id := fmt.Sprint(interrupt["id"])
	if id == "" || id == "<nil>" {
		t.Fatalf("interrupt id = %v", interrupt)
	}
	if !strings.Contains(fmt.Sprint(interrupt["message"]), "Continue?") {
		t.Fatalf("interrupt message = %v", interrupt["message"])
	}

	resume := map[string]any{
		"threadId": "thread-agui-int",
		"runId":    "run-agui-int",
		"messages": body["messages"],
		"resume": []any{map[string]any{
			"interruptId": id,
			"status":      "resolved",
			"payload":     map[string]any{"value": "yes"},
		}},
	}
	resp = postJSON(t, server.URL+"/agui", resume)
	events = readEvents(t, resp.Body, nil)
	resp.Body.Close()

	var text strings.Builder
	for _, event := range events {
		if event["type"] == "TEXT_MESSAGE_CONTENT" {
			text.WriteString(fmt.Sprint(event["delta"]))
		}
	}
	if text.String() != "resumed answer" {
		t.Fatalf("resumed text = %q, events=%v", text.String(), events)
	}
	if len(llm.requests()) < 2 {
		t.Fatalf("model was not called after resume: %d", len(llm.requests()))
	}
}

func TestA2ACardAndSendMessage(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		streamText(w, req.Model, "a2a answer")
	})
	server := newProtocolServer(t, protocolConfig(t, llm.server.URL, false))

	var card map[string]any
	getJSON(t, server.URL+"/.well-known/agent-card.json", &card)
	if card["name"] != "helper" || card["protocolVersion"] != "1.0" {
		t.Fatalf("card = %v", card)
	}
	interfaces, _ := card["supportedInterfaces"].([]any)
	if len(interfaces) != 1 {
		t.Fatalf("interfaces = %v", card["supportedInterfaces"])
	}
	iface := interfaces[0].(map[string]any)
	if iface["protocolBinding"] != "JSONRPC" || !strings.HasSuffix(fmt.Sprint(iface["url"]), "/a2a") {
		t.Fatalf("interface = %v", iface)
	}

	result := a2aRPC(t, server.URL+"/a2a", "SendMessage", map[string]any{
		"message": map[string]any{
			"messageId": "m-a2a-1",
			"role":      "user",
			"parts":     []any{map[string]any{"kind": "text", "text": "hello"}},
		},
	})
	task, _ := result["result"].(map[string]any)
	if task == nil {
		t.Fatalf("send result = %v", result)
	}
	taskID := fmt.Sprint(task["id"])
	completed := waitTaskState(t, server.URL+"/a2a", taskID, "TASK_STATE_COMPLETED")

	artifacts, _ := completed["artifacts"].([]any)
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %v", completed["artifacts"])
	}
	artifact := artifacts[0].(map[string]any)
	parts := artifact["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "a2a answer" {
		t.Fatalf("artifact = %v", artifact)
	}
}

func TestProtocolEndpointsUseTokenAuthentication(t *testing.T) {
	const token = "test" + "-token"
	cfg := &config.Config{
		DefaultModel: "base",
		Models:       map[string]config.Model{"base": {Provider: "openai", Model: "gpt-4"}},
		Agents:       map[string]config.Agent{"helper": {Model: "base"}},
	}
	app, err := New(cfg, Options{
		DefaultAgent: "helper", WorkDir: "/tmp/oni3", Version: "test",
		Token: token, EnableAGUI: true, EnableA2A: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("card without token = %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/.well-known/agent-card.json", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("card with token = %d", resp.StatusCode)
	}

	resp, err = http.Post(server.URL+"/agui", "application/json", strings.NewReader(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("agui without token = %d", resp.StatusCode)
	}
}

func TestPerAgentRoutes(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		streamText(w, req.Model, "per-agent answer")
	})
	server := newProtocolServer(t, protocolConfig(t, llm.server.URL, false))

	var card map[string]any
	getJSON(t, server.URL+"/agents/helper/.well-known/agent-card.json", &card)
	interfaces, _ := card["supportedInterfaces"].([]any)
	iface := interfaces[0].(map[string]any)
	if !strings.HasSuffix(fmt.Sprint(iface["url"]), "/agents/helper/a2a") {
		t.Fatalf("per-agent card url = %v", iface["url"])
	}

	body := map[string]any{
		"threadId": "thread-per-agent",
		"runId":    "run-per-agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	resp := postJSON(t, server.URL+"/agents/helper/agui", body)
	defer resp.Body.Close()
	events := readEvents(t, resp.Body, nil)
	var text strings.Builder
	for _, event := range events {
		if event["type"] == "TEXT_MESSAGE_CONTENT" {
			text.WriteString(fmt.Sprint(event["delta"]))
		}
	}
	if text.String() != "per-agent answer" {
		t.Fatalf("per-agent text = %q events=%v", text.String(), events)
	}
}

func TestA2AStreamingAndInterrupt(t *testing.T) {
	llm := newMockLLM(t, func(w http.ResponseWriter, req mockRequest, call int) {
		if call == 0 {
			streamToolCall(w, req.Model, "ask_user", `{"question":"Proceed?"}`)
			return
		}
		streamText(w, req.Model, "after approval")
	})
	server := newProtocolServer(t, protocolConfig(t, llm.server.URL, true))

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "SendStreamingMessage",
		"params": map[string]any{
			"message": map[string]any{
				"messageId": "m-stream-1",
				"role":      "user",
				"parts":     []any{map[string]any{"text": "start"}},
			},
		},
	}
	resp := postJSON(t, server.URL+"/a2a", body)
	events := readEvents(t, resp.Body, nil)
	resp.Body.Close()
	if len(events) == 0 {
		t.Fatal("no streaming events")
	}

	var taskID string
	inputRequired := false
	for _, event := range events {
		result, _ := event["result"].(map[string]any)
		if result == nil {
			continue
		}
		if snapshot, ok := result["task"].(map[string]any); ok {
			taskID = fmt.Sprint(snapshot["id"])
		}
		if update, ok := result["statusUpdate"].(map[string]any); ok {
			status := update["status"].(map[string]any)
			if status["state"] == "TASK_STATE_INPUT_REQUIRED" {
				inputRequired = true
			}
		}
	}
	if taskID == "" || !inputRequired {
		t.Fatalf("task=%q inputRequired=%v events=%v", taskID, inputRequired, events)
	}

	resume := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "SendStreamingMessage",
		"params": map[string]any{
			"message": map[string]any{
				"messageId": "m-stream-2",
				"role":      "user",
				"taskId":    taskID,
				"parts":     []any{map[string]any{"text": "yes"}},
			},
		},
	}
	resp = postJSON(t, server.URL+"/a2a", resume)
	events = readEvents(t, resp.Body, nil)
	resp.Body.Close()

	completed := false
	artifactText := ""
	for _, event := range events {
		result, _ := event["result"].(map[string]any)
		if result == nil {
			continue
		}
		if update, ok := result["statusUpdate"].(map[string]any); ok {
			status := update["status"].(map[string]any)
			if status["state"] == "TASK_STATE_COMPLETED" {
				completed = true
			}
		}
		if update, ok := result["artifactUpdate"].(map[string]any); ok {
			artifact := update["artifact"].(map[string]any)
			parts := artifact["parts"].([]any)
			artifactText = fmt.Sprint(parts[0].(map[string]any)["text"])
		}
	}
	if !completed || artifactText != "after approval" {
		t.Fatalf("resumed stream completed=%v artifact=%q events=%v", completed, artifactText, events)
	}
	waitTaskState(t, server.URL+"/a2a", taskID, "TASK_STATE_COMPLETED")
}

func a2aRPC(t *testing.T, url, method string, params any) map[string]any {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
	resp := postJSON(t, url, payload)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode rpc response: %v (%s)", err, body)
	}
	if rpcErr := result["error"]; rpcErr != nil {
		t.Fatalf("rpc error = %v", rpcErr)
	}
	return result
}

func waitTaskState(t *testing.T, url, id, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		result := a2aRPC(t, url, "GetTask", map[string]any{"id": id})
		task, _ := result["result"].(map[string]any)
		if task == nil {
			t.Fatalf("get task result = %v", result)
		}
		last = task
		status, _ := task["status"].(map[string]any)
		if status["state"] == want {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s: %v", id, want, last)
	return nil
}
