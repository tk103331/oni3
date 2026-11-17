package a2a

import (
	"encoding/json"
	"testing"
)

func TestPartWireFormats(t *testing.T) {
	legacy := json.RawMessage(`{"kind":"text","text":"hello"}`)
	var part Part
	if err := json.Unmarshal(legacy, &part); err != nil {
		t.Fatal(err)
	}
	if part.Text != "hello" || part.Kind != "text" {
		t.Fatalf("legacy part = %+v", part)
	}

	v1 := json.RawMessage(`{"text":"world","mediaType":"text/plain"}`)
	if err := json.Unmarshal(v1, &part); err != nil {
		t.Fatal(err)
	}
	if part.Text != "world" || part.MediaType != "text/plain" {
		t.Fatalf("v1 part = %+v", part)
	}
}

func TestNormalizeRole(t *testing.T) {
	cases := map[string]string{
		"user":       RoleUser,
		"ROLE_USER":  RoleUser,
		"role_user":  RoleUser,
		"agent":      RoleAgent,
		"assistant":  RoleAgent,
		"ROLE_AGENT": RoleAgent,
		"unexpected": "unexpected",
	}
	for input, want := range cases {
		if got := NormalizeRole(input); got != want {
			t.Fatalf("NormalizeRole(%q) = %q, want %q", input, got, want)
		}
	}
	if !IsUserRole("user") || IsUserRole("ROLE_AGENT") {
		t.Fatal("IsUserRole disagrees")
	}
	if !IsAgentRole("agent") || IsAgentRole("ROLE_USER") {
		t.Fatal("IsAgentRole disagrees")
	}
}

func TestSendMessageRequestAcceptsBareMessage(t *testing.T) {
	var wrapped SendMessageRequest
	if err := json.Unmarshal([]byte(`{"message":{"role":"user","parts":[{"text":"hi"}]}}`), &wrapped); err != nil {
		t.Fatal(err)
	}
	if wrapped.Message == nil || wrapped.Message.Text() != "hi" {
		t.Fatalf("wrapped = %+v", wrapped)
	}

	var bare SendMessageRequest
	if err := json.Unmarshal([]byte(`{"role":"user","parts":[{"text":"hi"}]}`), &bare); err != nil {
		t.Fatal(err)
	}
	if bare.Message == nil || bare.Message.Text() != "hi" {
		t.Fatalf("bare = %+v", bare)
	}
}

func TestStreamResponseOneofNames(t *testing.T) {
	data, err := json.Marshal(StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
		TaskID: "task-1", ContextID: "ctx-1",
		Status: TaskStatus{State: StateWorking},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["statusUpdate"]; !ok {
		t.Fatalf("missing statusUpdate wrapper: %s", data)
	}
}

func TestStorePersistsAndFilters(t *testing.T) {
	store := NewStore(t.TempDir())
	first := &Task{
		ID: "task-1", ContextID: "ctx-1",
		Status: TaskStatus{State: StateCompleted, Timestamp: "2026-01-01T00:00:00.000Z"},
	}
	second := &Task{
		ID: "task-2", ContextID: "ctx-2",
		Status: TaskStatus{State: StateWorking, Timestamp: "2026-01-02T00:00:00.000Z"},
	}
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}

	reloaded := NewStore(store.dir)
	task, ok := reloaded.Get("task-1")
	if !ok || task.ContextID != "ctx-1" {
		t.Fatalf("reloaded = %+v ok=%v", task, ok)
	}
	if got := reloaded.List("ctx-2", ""); len(got) != 1 || got[0].ID != "task-2" {
		t.Fatalf("context filter = %+v", got)
	}
	if got := reloaded.List("", StateCompleted); len(got) != 1 || got[0].ID != "task-1" {
		t.Fatalf("state filter = %+v", got)
	}
}

func TestCanonicalMethodAliases(t *testing.T) {
	cases := map[string]string{
		"message/send":      MethodSendMessage,
		"message/stream":    MethodSendStreamingMessage,
		"tasks/get":         MethodGetTask,
		"tasks/list":        MethodListTasks,
		"tasks/cancel":      MethodCancelTask,
		"tasks/resubscribe": MethodSubscribeToTask,
		"SendMessage":       MethodSendMessage,
	}
	for input, want := range cases {
		if got := CanonicalMethod(input); got != want {
			t.Fatalf("CanonicalMethod(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResumeApprovalParsing(t *testing.T) {
	yes := resumeInput{metadata: map[string]any{"approved": true}}
	if !yes.approved() {
		t.Fatal("boolean approval was not parsed")
	}
	no := resumeInput{text: "no"}
	if no.approved() {
		t.Fatal("text denial was parsed as approval")
	}
	allow := resumeInput{text: "ALLOW"}
	if !allow.approved() {
		t.Fatal("text approval was not parsed")
	}
}
