package agui

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func readFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(body))
	var events []map[string]any
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode frame: %v (%s)", err, line)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func frameTypes(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event["type"].(string))
	}
	return out
}

func TestEmitterClosesTextBeforeToolAndRun(t *testing.T) {
	recorder := httptest.NewRecorder()
	counter := 0
	nextID := func(prefix string) string {
		counter++
		return prefix + "-" + string(rune('0'+counter))
	}
	emitter, err := NewEmitter(recorder, "thread-1", "run-1", nextID, nil)
	if err != nil {
		t.Fatal(err)
	}
	emitter.RunStarted()
	emitter.TextContent("hel")
	emitter.TextContent("lo")
	emitter.ToolStart("call-1", "echo", `{"text":"hi"}`, "")
	emitter.ToolResult("call-1", "ok")
	emitter.RunFinishedSuccess()

	got := strings.Join(frameTypes(readFrames(t, recorder.Body.String())), ",")
	want := strings.Join([]string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_END",
		"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "TOOL_CALL_RESULT",
		"RUN_FINISHED",
	}, ",")
	if got != want {
		t.Fatalf("event order:\n got %s\nwant %s", got, want)
	}

	events := readFrames(t, recorder.Body.String())
	last := events[len(events)-1]
	outcome, _ := last["outcome"].(map[string]any)
	if outcome["type"] != "success" {
		t.Fatalf("outcome = %v", last["outcome"])
	}
}

func TestEmitterReasoningBlockAndInterrupt(t *testing.T) {
	recorder := httptest.NewRecorder()
	emitter, err := NewEmitter(recorder, "thread-1", "run-1", func(prefix string) string { return prefix + "-x" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	emitter.ReasoningContent("thinking")
	emitter.TextContent("answer")
	emitter.RunFinishedInterrupt([]Interrupt{{ID: "int-1", Reason: "input_required"}})

	events := readFrames(t, recorder.Body.String())
	types := frameTypes(events)
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "REASONING_MESSAGE_END,REASONING_END,TEXT_MESSAGE_START") {
		t.Fatalf("reasoning was not closed before text: %s", joined)
	}
	last := events[len(events)-1]
	if last["type"] != "RUN_FINISHED" {
		t.Fatalf("last event = %v", last)
	}
	outcome, _ := last["outcome"].(map[string]any)
	if outcome["type"] != "interrupt" {
		t.Fatalf("outcome = %v", last["outcome"])
	}
}

func TestHistoryConversion(t *testing.T) {
	input := RunAgentInput{Messages: []Message{
		{Role: "system", Content: json.RawMessage(`"be brief"`)},
		{Role: "user", Content: json.RawMessage(`"first"`)},
		{Role: "assistant", Content: json.RawMessage(`""`), ToolCalls: []ToolCall{{
			ID: "call-1", Type: "function", Function: FunctionCall{Name: "echo", Arguments: `{"x":1}`},
		}}},
		{Role: "tool", ToolCallID: "call-1", Content: json.RawMessage(`"result"`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"second"}]`)},
	}}
	history, prompt, err := History(input)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "second" {
		t.Fatalf("prompt = %q", prompt)
	}
	if len(history) != 4 {
		t.Fatalf("history length = %d", len(history))
	}
	if history[0].Role != "system" || history[2].Role != "assistant" || history[3].Role != "tool" {
		t.Fatalf("roles = %v %v %v", history[0].Role, history[2].Role, history[3].Role)
	}
	if len(history[2].ToolCalls) != 1 || history[2].ToolCalls[0].ID != "call-1" {
		t.Fatalf("tool calls = %+v", history[2].ToolCalls)
	}
}

func TestResumePayloadParsing(t *testing.T) {
	approval := decodeResume("approval", Resume{Payload: json.RawMessage(`{"approved":true,"remember":true}`)})
	if !approval.Approved || !approval.Remember {
		t.Fatalf("approval = %+v", approval)
	}
	question := decodeResume("question", Resume{Payload: json.RawMessage(`{"answer":"go on"}`)})
	if question.Value != "go on" {
		t.Fatalf("question = %+v", question)
	}
}
