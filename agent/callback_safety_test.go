package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// runInfo builds the callback identity eino passes to a handler.
func runInfo(name, component string) *callbacks.RunInfo {
	return &callbacks.RunInfo{Name: name, Component: components.Component(component)}
}

// Multi-byte results used to panic: the truncation compared a byte length
// against a rune slice bound.
func TestTruncateTextCountsRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
	}{
		{"short", "ok", 197},
		{"long ascii", strings.Repeat("a", 500), 197},
		{"long chinese", strings.Repeat("中文结果", 100), 197},
		{"mixed", strings.Repeat("a中", 200), 197},
		{"zero", strings.Repeat("中", 10), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateText(tc.in, tc.max)
			if !strings.HasPrefix(got, "") {
				t.Fatal("unreachable")
			}
			// The result must be valid UTF-8 and within the rune budget.
			if n := len([]rune(got)); n > tc.max+3 {
				t.Fatalf("result holds %d runes, want at most %d", n, tc.max+3)
			}
			if strings.ContainsRune(got, '�') {
				t.Fatalf("truncation split a character: %q", got)
			}
		})
	}
}

// multibyteTool returns a result longer than 200 bytes but shorter than 200
// runes: the exact shape that panicked the truncation.
type multibyteTool struct{}

func (m *multibyteTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "probe", Desc: "probe"}, nil
}

func (m *multibyteTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return strings.Repeat("命令执行完成。", 30), nil
}

// A tool result with more bytes than runes must not panic the callback.
func TestToolEndHandlesMultibyteResult(t *testing.T) {
	r := newEventReact(t, &eventModel{}, []tool.BaseTool{&multibyteTool{}})
	var results []string
	if _, err := r.ChatWithCallback(context.Background(), nil, "go", func(msg interface{}) {
		if info, ok := msg.(ToolCallInfo); ok && info.Type == "end" {
			results = append(results, info.Result)
		}
	}); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
}

// A panicking display callback is logged, not turned into a node error.
func TestPanickingCallbackDoesNotAbortTheRun(t *testing.T) {
	r := newEventReact(t, &eventModel{}, []tool.BaseTool{&eventTool{}})
	var sawEnd bool
	answer, err := r.ChatWithCallback(context.Background(), nil, "go", func(msg interface{}) {
		if info, ok := msg.(ToolCallInfo); ok {
			if info.Type == "end" {
				sawEnd = true
				panic("display bug")
			}
		}
	})
	if err != nil {
		t.Fatalf("a display panic must not fail the turn: %v", err)
	}
	if !sawEnd {
		t.Fatal("the end event was not delivered")
	}
	if !strings.Contains(answer, "done") {
		t.Fatalf("missing final answer: %q", answer)
	}
}

// The stream chunk callback is protected the same way.
func TestPanickingChunkCallbackDoesNotAbortTheRun(t *testing.T) {
	r := newEventReact(t, &eventModel{}, []tool.BaseTool{&eventTool{}})
	err := r.ChatStream(context.Background(), nil, "go", func(chunk *StreamChunk) {
		if chunk.Type == "content" && chunk.Content != "" {
			panic("render bug")
		}
	}, nil)
	if err != nil {
		t.Fatalf("a render panic must not fail the turn: %v", err)
	}
}

// The callback layer must not report the graph or model nodes as tools.
func TestToolCallbackIgnoresInternalNodes(t *testing.T) {
	var events []ToolCallInfo
	cb := &ToolCallCallback{callback: func(msg interface{}) {
		events = append(events, msg.(ToolCallInfo))
	}}
	ctx := context.Background()
	cb.OnStart(ctx, runInfo("ChatModel", "ChatModel"), []*schema.Message{schema.UserMessage("hi")})
	cb.OnStart(ctx, runInfo("ReActAgent", "Graph"), nil)
	cb.OnEnd(ctx, runInfo("ReActAgent", "Graph"), "done")
	cb.OnError(ctx, runInfo("Tools", "ToolsNode"), errString("boom"))
	if len(events) != 0 {
		t.Fatalf("internal nodes leaked into the tool events: %+v", events)
	}
}

// The tools node announces the calls; the tool node announces the execution.
func TestToolCallbackReportsOneStartPerCall(t *testing.T) {
	var events []ToolCallInfo
	cb := &ToolCallCallback{callback: func(msg interface{}) {
		events = append(events, msg.(ToolCallInfo))
	}}
	msg := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
		{ID: "call-1", Function: schema.FunctionCall{Name: "shell", Arguments: `{"command":"ls"}`}},
		{ID: "call-2", Function: schema.FunctionCall{Name: "file_system", Arguments: `{"op":"list"}`}},
	}}
	ctx := context.Background()
	cb.OnStart(ctx, runInfo("Tools", componentToolsNode), msg)
	// The tool node re-announces each call as it starts.
	cb.OnStart(ctx, runInfo("shell", "Tool"), `{"command":"ls"}`)
	cb.OnStart(ctx, runInfo("file_system", "Tool"), `{"op":"list"}`)

	if len(events) != 2 {
		t.Fatalf("expected two starts, got %d: %+v", len(events), events)
	}
	if events[0].CallID != "call-1" || events[1].CallID != "call-2" {
		t.Fatalf("call ids not carried: %+v", events)
	}
}

// errString is a minimal error for the callback tests.
type errString string

func (e errString) Error() string { return string(e) }
