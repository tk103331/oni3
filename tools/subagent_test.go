package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
	"gopkg.in/yaml.v3"
)

// useFakeSubagentRunner installs a runner for the duration of the test.
func useFakeSubagentRunner(t *testing.T, fn SubagentRunner) {
	t.Helper()
	previous := subagentRunner
	SetSubagentRunner(fn)
	t.Cleanup(func() { SetSubagentRunner(previous) })
}

func newSubagentTestTool(t *testing.T, spec string) tool.InvokableTool {
	t.Helper()
	var cfg config.Tool
	if err := yaml.Unmarshal([]byte(spec), &cfg); err != nil {
		t.Fatalf("cannot parse test config: %v", err)
	}
	tl, err := NewSubagentTool(SubagentToolName, cfg)
	if err != nil {
		t.Fatalf("NewSubagentTool: %v", err)
	}
	return tl
}

func TestSubagentRunsConfiguredAgent(t *testing.T) {
	var got SubagentSpec
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		got = spec
		return "  child answer  ", nil
	})

	tl := newSubagentTestTool(t, "type: subagent\n")
	out := runFS(t, tl, `{"agent":"worker","prompt":"summarize the repo"}`)
	if out != "child answer" {
		t.Fatalf("output = %q", out)
	}
	if got.Agent != "worker" || got.System != "" || got.Prompt != "summarize the repo" {
		t.Fatalf("spec = %+v", got)
	}
}

func TestSubagentCreatesTemporaryAgent(t *testing.T) {
	var got SubagentSpec
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		got = spec
		return "ok", nil
	})

	tl := newSubagentTestTool(t, "type: subagent\n")
	out := runFS(t, tl, `{"system":"You are a reviewer.","model":"ds","tools":["file_system","file_system"," file_system "],"prompt":"review"}`)
	if out != "ok" {
		t.Fatalf("output = %q", out)
	}
	if got.System != "You are a reviewer." || got.Model != "ds" || got.Prompt != "review" {
		t.Fatalf("spec = %+v", got)
	}
	if len(got.Tools) != 1 || got.Tools[0] != "file_system" {
		t.Fatalf("tools should be trimmed and deduped: %v", got.Tools)
	}
}

// A bare prompt is the common call shape: the child is a temporary agent that
// inherits the parent's model and tools.
func TestSubagentAcceptsPromptOnly(t *testing.T) {
	var got SubagentSpec
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		got = spec
		return "ok", nil
	})
	tl := newSubagentTestTool(t, "type: subagent\n")

	ctx := WithActiveModel(context.Background(), "parent-model")
	ctx = WithActiveTools(ctx, []string{"file_system", "command_line"})
	out, err := tl.InvokableRun(ctx, `{"prompt":"只回复一句话：hello"}`)
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("output = %q", out)
	}
	if got.Agent != "" {
		t.Fatalf("a bare prompt must not pick a configured agent: %+v", got)
	}
	if got.System == "" {
		t.Fatal("the temporary agent needs a system prompt")
	}
	if got.Model != "parent-model" {
		t.Fatalf("model = %q, want the parent's", got.Model)
	}
	if len(got.Tools) != 2 || got.Tools[0] != "file_system" || got.Tools[1] != "command_line" {
		t.Fatalf("tools = %v, want the parent's tools", got.Tools)
	}

	// An explicit system prompt still replaces the default.
	if _, err := tl.InvokableRun(ctx, `{"system":"You are a reviewer.","prompt":"x"}`); err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if got.System != "You are a reviewer." {
		t.Fatalf("system = %q", got.System)
	}
}

// Inherited tools are filtered by the allowlist instead of failing the call.
func TestSubagentInheritedToolsRespectAllowlist(t *testing.T) {
	var got SubagentSpec
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		got = spec
		return "ok", nil
	})
	tl := newSubagentTestTool(t, "type: subagent\nconfig:\n  tools: [file_system]\n")

	ctx := WithActiveTools(context.Background(), []string{"file_system", "command_line"})
	if _, err := tl.InvokableRun(ctx, `{"prompt":"x"}`); err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0] != "file_system" {
		t.Fatalf("tools = %v, want only the allowed one", got.Tools)
	}

	// An explicit request for a disallowed tool is still an error.
	if out, _ := tl.InvokableRun(ctx, `{"tools":["command_line"],"prompt":"x"}`); !strings.Contains(out, "not allowed") {
		t.Fatalf("explicit disallowed tool = %q", out)
	}
}

func TestSubagentArgumentValidation(t *testing.T) {
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		t.Fatal("runner must not be called for invalid arguments")
		return "", nil
	})
	tl := newSubagentTestTool(t, "type: subagent\n")

	cases := map[string]string{
		`{"agent":"a","system":"s","prompt":"x"}`: "not both",
		`{"agent":"a"}`: "prompt is required",
		`{}`:            "prompt is required",
	}
	for args, want := range cases {
		out := runFS(t, tl, args)
		if !strings.Contains(out, want) {
			t.Fatalf("%s -> %q, want %q", args, out, want)
		}
	}
}

func TestSubagentAllowlists(t *testing.T) {
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		return "ok", nil
	})

	tl := newSubagentTestTool(t, "type: subagent\nconfig:\n  agents: [worker]\n  tools: [file_system]\n")
	if out := runFS(t, tl, `{"agent":"other","prompt":"x"}`); !strings.Contains(out, "not allowed") {
		t.Fatalf("agent allowlist = %q", out)
	}
	if out := runFS(t, tl, `{"agent":"worker","prompt":"x"}`); out != "ok" {
		t.Fatalf("allowed agent = %q", out)
	}
	if out := runFS(t, tl, `{"system":"s","tools":["command_line"],"prompt":"x"}`); !strings.Contains(out, "not allowed for temporary agents") {
		t.Fatalf("tool allowlist = %q", out)
	}

	noTemp := newSubagentTestTool(t, "type: subagent\nconfig:\n  allow_temp: false\n")
	if out := runFS(t, noTemp, `{"system":"s","prompt":"x"}`); !strings.Contains(out, "temporary agents are disabled") {
		t.Fatalf("allow_temp=false = %q", out)
	}
}

func TestSubagentDepthLimit(t *testing.T) {
	called := false
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		called = true
		if depth := SubagentDepth(ctx); depth != 1 {
			t.Fatalf("child depth = %d, want 1", depth)
		}
		return "ok", nil
	})

	tl := newSubagentTestTool(t, "type: subagent\nconfig:\n  max_depth: 1\n")
	ctx := WithSubagentDepth(context.Background(), 1)
	out, err := tl.InvokableRun(ctx, `{"agent":"worker","prompt":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nesting limit reached") {
		t.Fatalf("depth limit = %q", out)
	}
	if called {
		t.Fatal("runner must not run past the depth limit")
	}

	// A top-level call runs and marks the child context.
	out, err = tl.InvokableRun(context.Background(), `{"agent":"worker","prompt":"x"}`)
	if err != nil || out != "ok" {
		t.Fatalf("top-level run = %q, %v", out, err)
	}
}

func TestSubagentTimeoutAndErrors(t *testing.T) {
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	tl := newSubagentTestTool(t, "type: subagent\nconfig:\n  timeout: 1\n")
	out := runFS(t, tl, `{"agent":"worker","prompt":"x"}`)
	if !strings.Contains(out, "timed out after 1s") {
		t.Fatalf("timeout = %q", out)
	}

	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		return "", context.DeadlineExceeded
	})
	if out := runFS(t, tl, `{"agent":"worker","prompt":"x"}`); !strings.Contains(out, "subagent failed") {
		t.Fatalf("error result = %q", out)
	}

	// Cancelling the parent turn is a real error, not a tool result.
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		return "", ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tl.InvokableRun(ctx, `{"agent":"worker","prompt":"x"}`); err == nil {
		t.Fatal("cancelled parent should surface an error")
	}
}

func TestSubagentFactoryRegistration(t *testing.T) {
	tl, err := CreateTool("subagent", config.Tool{Type: "subagent"})
	if err != nil {
		t.Fatalf("subagent type not registered: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil || info.Name != "subagent" {
		t.Fatalf("info = %+v, %v", info, err)
	}
}

func TestSubagentRunnerUnavailable(t *testing.T) {
	useFakeSubagentRunner(t, nil)
	tl := newSubagentTestTool(t, "type: subagent\n")
	if out := runFS(t, tl, `{"agent":"worker","prompt":"x"}`); !strings.Contains(out, "runner is not available") {
		t.Fatalf("missing runner = %q", out)
	}
}

func TestSubagentTimeoutConfigParsing(t *testing.T) {
	tl := newSubagentTestTool(t, "type: subagent\nconfig:\n  timeout: 2\n  max_depth: 3\n")
	if got := tl.(*subagentTool).timeout; got != 2*time.Second {
		t.Fatalf("timeout = %s", got)
	}
	if got := tl.(*subagentTool).maxDepth; got != 3 {
		t.Fatalf("max_depth = %d", got)
	}
}

func TestSubagentInheritsActiveModel(t *testing.T) {
	var got SubagentSpec
	useFakeSubagentRunner(t, func(ctx context.Context, spec SubagentSpec) (string, error) {
		got = spec
		return "ok", nil
	})
	tl := newSubagentTestTool(t, "type: subagent\n")
	// The parent ReactAgent annotates the context with the model it runs on.
	ctx := WithActiveModel(context.Background(), "switched")

	if _, err := tl.InvokableRun(ctx, `{"system":"You are a helper.","prompt":"hi"}`); err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if got.Model != "switched" {
		t.Fatalf("temporary agent model = %q, want the parent's model", got.Model)
	}

	// An explicit model in the call still wins.
	if _, err := tl.InvokableRun(ctx, `{"system":"You are a helper.","model":"explicit","prompt":"hi"}`); err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if got.Model != "explicit" {
		t.Fatalf("explicit model = %q, want explicit", got.Model)
	}
}
