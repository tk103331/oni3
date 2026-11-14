package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

// eventTool is a tool that fails when fail is set.
type eventTool struct {
	fail bool
}

func (p *eventTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "probe", Desc: "Run a local probe"}, nil
}

func (p *eventTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	if p.fail {
		return "", fmt.Errorf("probe exploded")
	}
	return "probe ok", nil
}

// eventModel asks for one tool call and then answers.
type eventModel struct {
	calls    int
	seen     []*schema.Message
	failTool bool
}

func (m *eventModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *eventModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, fmt.Errorf("expected streaming")
}

func (m *eventModel) Stream(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	m.seen = messages
	if m.calls == 1 {
		index := 0
		return schema.StreamReaderFromArray([]*schema.Message{
			{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
				Index:    &index,
				ID:       "call-1",
				Type:     "function",
				Function: schema.FunctionCall{Name: "probe", Arguments: `{"mode":"fast"}`},
			}}},
		}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("done", nil)}), nil
}

// newEventReact builds a ReAct agent that reports its tool events.
func newEventReact(t *testing.T, mdl model.ToolCallingChatModel, tools []tool.BaseTool) *ReactAgent {
	t.Helper()
	ra, err := react.NewAgent(context.Background(), &react.AgentConfig{
		ToolCallingModel:      mdl,
		ToolsConfig:           compose.ToolsNodeConfig{Tools: tools},
		StreamToolCallChecker: streamHasToolCalls,
		MaxStep:               10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &ReactAgent{agentName: "test", config: &config.Agent{}, agent: ra, ctx: context.Background()}
}

// collectToolEvents runs one turn and returns the reported tool events.
func collectToolEvents(t *testing.T, r *ReactAgent) []ToolCallInfo {
	t.Helper()
	var events []ToolCallInfo
	if _, err := r.ChatWithCallback(context.Background(), nil, "go", func(msg interface{}) {
		if info, ok := msg.(ToolCallInfo); ok {
			events = append(events, info)
		}
	}); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	return events
}

// One tool call must be reported exactly once: the eino callback layer
// announces the call twice (tools node and tool node), which used to show two
// rows for a single call.
func TestToolCallIsReportedOnce(t *testing.T) {
	r := newEventReact(t, &eventModel{}, []tool.BaseTool{&eventTool{}})
	events := collectToolEvents(t, r)

	var starts, ends []ToolCallInfo
	for _, e := range events {
		switch e.Type {
		case "start":
			starts = append(starts, e)
		case "end":
			ends = append(ends, e)
		}
	}
	if len(starts) != 1 {
		t.Fatalf("expected one start event, got %d: %+v", len(starts), events)
	}
	if len(ends) != 1 {
		t.Fatalf("expected one end event, got %d: %+v", len(ends), events)
	}
	if starts[0].CallID != "call-1" || ends[0].CallID != "call-1" {
		t.Fatalf("call id not paired: start=%q end=%q", starts[0].CallID, ends[0].CallID)
	}
	if starts[0].Name != "probe" {
		t.Fatalf("unexpected tool name %q", starts[0].Name)
	}
	if starts[0].Arguments != `{"mode":"fast"}` {
		t.Fatalf("unexpected arguments %q", starts[0].Arguments)
	}
}

// A failing tool must not end the turn: the failure is handed back to the
// model as a result so the agent can keep working.
func TestFailingToolKeepsTheTurnAlive(t *testing.T) {
	mdl := &eventModel{}
	r := newEventReact(t, mdl, wrapRecoverable([]tool.BaseTool{&eventTool{fail: true}}))

	events := collectToolEvents(t, r)

	var ends []ToolCallInfo
	for _, e := range events {
		if e.Type == "end" {
			ends = append(ends, e)
		}
	}
	if len(ends) != 1 {
		t.Fatalf("expected one end event, got %d: %+v", len(ends), events)
	}
	if !IsToolError(ends[0].Result) {
		t.Fatalf("failure not reported as a tool error: %q", ends[0].Result)
	}
	if !strings.Contains(ends[0].Result, "probe exploded") {
		t.Fatalf("error message lost: %q", ends[0].Result)
	}
	// The model saw the failure and produced the final answer.
	if mdl.calls != 2 {
		t.Fatalf("expected the model to be called twice, got %d", mdl.calls)
	}
	found := false
	for _, msg := range mdl.seen {
		if msg != nil && msg.Role == schema.Tool && strings.Contains(msg.Content, "probe exploded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the tool result was not passed back to the model: %+v", mdl.seen)
	}
}

// A cancelled turn must still abort instead of being turned into a result.
func TestCancelledToolIsNotRecovered(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wrapped, ok := wrapRecoverable([]tool.BaseTool{&eventTool{fail: true}})[0].(tool.InvokableTool)
	if !ok {
		t.Fatal("the wrapped tool is not invokable")
	}
	if _, err := wrapped.InvokableRun(ctx, "{}"); err == nil {
		t.Fatal("a cancelled tool call must return an error")
	}
}
