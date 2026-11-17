package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/tools/approval"
	"gopkg.in/yaml.v3"
)

func useFakeRemoteAgentRunner(t *testing.T, fn RemoteAgentRunner) {
	t.Helper()
	previous := remoteAgentRunner
	SetRemoteAgentRunner(fn)
	t.Cleanup(func() { SetRemoteAgentRunner(previous) })
}

func newA2ATestTool(t *testing.T, spec string) tool.InvokableTool {
	t.Helper()
	var cfg config.Tool
	if err := yaml.Unmarshal([]byte(spec), &cfg); err != nil {
		t.Fatalf("cannot parse test config: %v", err)
	}
	tl, err := NewA2ATool("remote_a2a", cfg)
	if err != nil {
		t.Fatalf("NewA2ATool: %v", err)
	}
	return tl
}

func TestA2AInvokesConfiguredAgent(t *testing.T) {
	var got RemoteAgentSpec
	useFakeRemoteAgentRunner(t, func(ctx context.Context, spec RemoteAgentSpec) (string, error) {
		got = spec
		return `{"agent":"remote","state":"TASK_STATE_COMPLETED","answer":"ok"}`, nil
	})

	tl := newA2ATestTool(t, "type: a2a\nconfig:\n  timeout: 7\n  poll_interval_ms: 25\n")
	out := runFS(t, tl, `{"agent":" remote ","prompt":" hello ","task_id":" task-1 ","context_id":" ctx-1 "}`)
	if !strings.Contains(out, `"answer":"ok"`) {
		t.Fatalf("output = %q", out)
	}
	if got.Agent != "remote" || got.Prompt != "hello" || got.TaskID != "task-1" || got.ContextID != "ctx-1" {
		t.Fatalf("spec = %+v", got)
	}
	if got.Timeout != 7*time.Second || got.PollInterval != 25*time.Millisecond {
		t.Fatalf("timeouts = %s/%s", got.Timeout, got.PollInterval)
	}
}

func TestA2AAllowsPollingWithoutPrompt(t *testing.T) {
	called := false
	useFakeRemoteAgentRunner(t, func(ctx context.Context, spec RemoteAgentSpec) (string, error) {
		called = true
		if spec.Prompt != "" || spec.TaskID != "task-2" {
			t.Fatalf("spec = %+v", spec)
		}
		return `{"agent":"remote","state":"timeout","task_id":"task-2"}`, nil
	})

	tl := newA2ATestTool(t, "type: a2a\nconfig:\n  agents: [remote]\n")
	out := runFS(t, tl, `{"agent":"remote","task_id":"task-2"}`)
	if !called || !strings.Contains(out, `"state":"timeout"`) {
		t.Fatalf("called = %v, output = %q", called, out)
	}
}

func TestA2AArgumentAndAllowlistValidation(t *testing.T) {
	useFakeRemoteAgentRunner(t, func(ctx context.Context, spec RemoteAgentSpec) (string, error) {
		t.Fatal("runner must not run for invalid arguments")
		return "", nil
	})

	tl := newA2ATestTool(t, "type: a2a\nconfig:\n  agents: [worker]\n")
	cases := map[string]string{
		`{}`:                             "agent is required",
		`{"agent":"worker"}`:             "prompt is required",
		`{"agent":"other","prompt":"x"}`: "not allowed",
	}
	for args, want := range cases {
		out := runFS(t, tl, args)
		if !strings.Contains(out, want) {
			t.Fatalf("%s -> %q, want %q", args, out, want)
		}
	}
}

func TestA2AEmptyOutputAndRunnerError(t *testing.T) {
	useFakeRemoteAgentRunner(t, func(ctx context.Context, spec RemoteAgentSpec) (string, error) {
		return "   ", nil
	})
	tl := newA2ATestTool(t, "type: a2a\n")
	if out := runFS(t, tl, `{"agent":"remote","prompt":"hi"}`); !strings.Contains(out, "returned no output") {
		t.Fatalf("empty output = %q", out)
	}

	useFakeRemoteAgentRunner(t, func(ctx context.Context, spec RemoteAgentSpec) (string, error) {
		return "", context.DeadlineExceeded
	})
	if out := runFS(t, tl, `{"agent":"remote","prompt":"hi"}`); !strings.Contains(out, "remote A2A agent failed") {
		t.Fatalf("runner error = %q", out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tl.InvokableRun(ctx, `{"agent":"remote","prompt":"hi"}`); err == nil {
		t.Fatal("cancelled parent should surface an error")
	}
}

func TestA2ARunnerUnavailable(t *testing.T) {
	useFakeRemoteAgentRunner(t, nil)
	tl := newA2ATestTool(t, "type: a2a\n")
	if out := runFS(t, tl, `{"agent":"remote","prompt":"hi"}`); !strings.Contains(out, "runner is not available") {
		t.Fatalf("missing runner = %q", out)
	}
}

func TestA2AApprovalRequestAndInfo(t *testing.T) {
	tl := newA2ATestTool(t, "type: a2a\ndescription: custom description\n")
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "remote_a2a" || !strings.Contains(info.Desc, "custom description") {
		t.Fatalf("info = %+v", info)
	}
	if info.ParamsOneOf == nil {
		t.Fatal("params must be set")
	}

	approvalTool, ok := tl.(interface {
		ApprovalRequest(string) (approval.Request, bool)
	})
	if !ok {
		t.Fatal("tool must expose ApprovalRequest")
	}
	request, dangerous := approvalTool.ApprovalRequest(`{"agent":"remote","prompt":"secret","task_id":"task-1"}`)
	if !dangerous || request.Tool != "remote_a2a" || request.Detail != "secret" || !strings.Contains(request.Summary, "task-1") {
		t.Fatalf("approval = %+v, %v", request, dangerous)
	}
}

func TestA2AFactoryRegistration(t *testing.T) {
	tl, err := CreateTool("a2a", config.Tool{Type: "a2a"})
	if err != nil {
		t.Fatalf("a2a type not registered: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil || info.Name != "a2a" {
		t.Fatalf("info = %+v, %v", info, err)
	}
}
