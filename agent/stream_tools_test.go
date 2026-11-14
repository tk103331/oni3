package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

func TestStreamHasToolCalls(t *testing.T) {
	call := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Name: "probe", Arguments: "{}"}}}}
	for _, tc := range []struct {
		name   string
		chunks []*schema.Message
		want   bool
	}{
		{"text before tools", []*schema.Message{schema.AssistantMessage("I'll check", nil), call}, true},
		{"reasoning before tools", []*schema.Message{{ReasoningContent: "thinking"}, schema.AssistantMessage("I'll check", nil), call}, true},
		{"tools first", []*schema.Message{call}, true},
		{"text only", []*schema.Message{schema.AssistantMessage("hello", nil)}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := streamHasToolCalls(context.Background(), schema.StreamReaderFromArray(tc.chunks))
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	t.Run("stream error after text", func(t *testing.T) {
		sr, sw := schema.Pipe[*schema.Message](2)
		expected := errors.New("broken stream")
		sw.Send(schema.AssistantMessage("starting", nil), nil)
		sw.Send(nil, expected)
		sw.Close()
		if _, err := streamHasToolCalls(context.Background(), sr); !errors.Is(err, expected) {
			t.Fatalf("lost stream error: %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := streamHasToolCalls(ctx, schema.StreamReaderFromArray([]*schema.Message{})); !errors.Is(err, context.Canceled) {
			t.Fatalf("lost cancellation: %v", err)
		}
	})
}

// Each of two tool rounds begins with visible prose. A final answer is only
// emitted after both results have been supplied to the model.
type lateToolModel struct {
	calls   int
	results int
}

func (m *lateToolModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *lateToolModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("expected streaming")
}
func (m *lateToolModel) Stream(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	m.results = 0
	for _, msg := range messages {
		if msg.Role == schema.Tool {
			m.results++
		}
	}
	if m.calls > 3 {
		return nil, errors.New("unexpected extra model call")
	}
	if m.calls == 3 {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("Checks complete.", nil)}), nil
	}
	index := 0
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, ReasoningContent: "Thinking first."},
		schema.AssistantMessage("I'll check now.", nil),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Index: &index, ID: "probe-call", Type: "function", Function: schema.FunctionCall{Name: "probe", Arguments: "{}"}}}},
	}), nil
}

type countingTool struct{ calls int }

func (p *countingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "probe", Desc: "Run a local probe"}, nil
}
func (p *countingTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	p.calls++
	return "probe succeeded", nil
}

func TestLateToolCallsCompleteWithoutUserContinuation(t *testing.T) {
	for _, mode := range []string{"stream", "callback"} {
		t.Run(mode, func(t *testing.T) {
			mdl, probe := &lateToolModel{}, &countingTool{}
			ra, err := react.NewAgent(context.Background(), &react.AgentConfig{
				ToolCallingModel:      mdl,
				ToolsConfig:           compose.ToolsNodeConfig{Tools: []tool.BaseTool{probe}},
				StreamToolCallChecker: streamHasToolCalls,
				MaxStep:               10,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := &ReactAgent{agentName: "test", config: &config.Agent{}, agent: ra, ctx: context.Background()}
			var answer strings.Builder
			if mode == "stream" {
				err = r.ChatStream(context.Background(), nil, "run both checks", func(chunk *StreamChunk) {
					if chunk.Type == "content" {
						answer.WriteString(chunk.Content)
					}
				}, nil)
			} else {
				var result string
				result, err = r.ChatWithCallback(context.Background(), nil, "run both checks", func(interface{}) {})
				answer.WriteString(result)
			}
			if err != nil && !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			if probe.calls != 2 || mdl.calls != 3 || mdl.results != 2 {
				t.Fatalf("incomplete turn: tools=%d model=%d results=%d", probe.calls, mdl.calls, mdl.results)
			}
			if !strings.Contains(answer.String(), "Checks complete.") {
				t.Fatalf("missing final answer: %q", answer.String())
			}
		})
	}
}
