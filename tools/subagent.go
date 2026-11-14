package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

// defaultSubagentSystem instructs a child agent created from a bare prompt.
const defaultSubagentSystem = "You are a focused subagent. Complete the task you are given " +
	"and reply with the result only. Do not ask questions; if the task cannot be completed, " +
	"state the concrete blocker."

// SubagentSpec describes a child-agent run requested by the model. Agent names
// a configured agent; otherwise System describes a temporary one (it falls back
// to a default instruction when the call only carried a prompt).
type SubagentSpec struct {
	Agent  string
	System string
	Model  string
	Tools  []string
	Prompt string
}

// SubagentRunner runs a child agent and returns its final answer. The agent
// package installs the real implementation; injecting it keeps the dependency
// direction one-way (agent imports tools).
type SubagentRunner func(ctx context.Context, spec SubagentSpec) (string, error)

var subagentRunner SubagentRunner

// SetSubagentRunner installs the child-agent runner.
func SetSubagentRunner(r SubagentRunner) {
	subagentRunner = r
}

type subagentDepthKey struct{}

// WithSubagentDepth records how many subagents deep the current run is.
func WithSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, subagentDepthKey{}, depth)
}

// SubagentDepth reports the nesting depth carried by ctx.
func SubagentDepth(ctx context.Context) int {
	depth, _ := ctx.Value(subagentDepthKey{}).(int)
	return depth
}

type activeModelKey struct{}

// WithActiveModel records the model the current run is using, so nested agents
// (the subagent tool) run on the same model, including a runtime /model switch.
func WithActiveModel(ctx context.Context, model string) context.Context {
	if model == "" {
		return ctx
	}
	return context.WithValue(ctx, activeModelKey{}, model)
}

// ActiveModel returns the model recorded by WithActiveModel, or "".
func ActiveModel(ctx context.Context) string {
	model, _ := ctx.Value(activeModelKey{}).(string)
	return model
}

type activeToolsKey struct{}

// WithActiveTools records the tool names the current run has, so a subagent
// asked for nothing but a prompt inherits the same capabilities.
func WithActiveTools(ctx context.Context, names []string) context.Context {
	if len(names) == 0 {
		return ctx
	}
	return context.WithValue(ctx, activeToolsKey{}, append([]string(nil), names...))
}

// ActiveTools returns the tool names recorded by WithActiveTools.
func ActiveTools(ctx context.Context) []string {
	names, _ := ctx.Value(activeToolsKey{}).([]string)
	return names
}

// subagentTool delegates a self-contained task to another agent.
type subagentTool struct {
	info     *schema.ToolInfo
	agents   map[string]bool // nil = any configured agent
	tools    map[string]bool // nil = any configured tool
	allowNew bool
	maxDepth int
	timeout  time.Duration
}

// NewSubagentTool creates the "subagent" tool.
//
// Config options:
//   - agents: allowlist of configured agent names (default: all)
//   - tools: allowlist of tool names a temporary agent may request (default: all)
//   - allow_temp: allow ad-hoc agents built from a system prompt (default true)
//   - max_depth: maximum subagent nesting depth (default 1)
//   - timeout: per-child-run timeout in seconds (default 300)
func NewSubagentTool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	t := &subagentTool{
		allowNew: true,
		maxDepth: 1,
		timeout:  5 * time.Minute,
	}
	if cfg.Config != nil {
		if v, ok := cfg.Config["agents"]; ok && v.IsArray() {
			// An empty list means "no restriction", not "nothing allowed".
			if agents := stringSet(v); len(agents) > 0 {
				t.agents = agents
			}
		}
		if v, ok := cfg.Config["tools"]; ok && v.IsArray() {
			if toolNames := stringSet(v); len(toolNames) > 0 {
				t.tools = toolNames
			}
		}
		if v, ok := cfg.Config["allow_temp"]; ok {
			t.allowNew = v.Bool()
		}
		if v, ok := cfg.Config["max_depth"]; ok {
			if n := v.Int(); n > 0 {
				t.maxDepth = n
			}
		}
		if v, ok := cfg.Config["timeout"]; ok {
			if secs := v.Int(); secs > 0 {
				t.timeout = time.Duration(secs) * time.Second
			}
		}
	}

	desc := cfg.Description
	if desc == "" {
		desc = "Run a child agent to complete a self-contained task and return its final answer. " +
			"Only `prompt` is required: without `agent` or `system` the child is a temporary agent " +
			"that inherits the current model and tools. " +
			"Pass `agent` to run an agent from the configuration, or `system` (with optional `model` and `tools`) " +
			"to give the temporary agent its own instructions. The child has its own conversation and does not see the current chat."
	}

	t.info = &schema.ToolInfo{
		Name: name,
		Desc: desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"agent": {
				Type: schema.String,
				Desc: "Name of a configured agent to run. Cannot be combined with `system`.",
			},
			"system": {
				Type: schema.String,
				Desc: "System prompt for a temporary agent (used when `agent` is empty).",
			},
			"model": {
				Type: schema.String,
				Desc: "Model for a temporary agent; defaults to the model the current agent is using.",
			},
			"tools": {
				Type: schema.Array,
				Desc: "Tool names for a temporary agent. Each must be defined in the configuration.",
				ElemInfo: &schema.ParameterInfo{
					Type: schema.String,
				},
			},
			"prompt": {
				Type:     schema.String,
				Desc:     "The task for the child agent.",
				Required: true,
			},
		}),
	}
	return t, nil
}

func (t *subagentTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *subagentTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var in struct {
		Agent  string   `json:"agent"`
		System string   `json:"system"`
		Model  string   `json:"model"`
		Tools  []string `json:"tools"`
		Prompt string   `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return t.fail("invalid arguments: %v", err)
	}

	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return t.fail("prompt is required")
	}
	agentName := strings.TrimSpace(in.Agent)
	system := strings.TrimSpace(in.System)
	if agentName != "" && system != "" {
		return t.fail("pass either agent or system, not both")
	}

	if agentName != "" {
		if t.agents != nil && !t.agents[agentName] {
			return t.fail("agent %q is not allowed by this tool's configuration", agentName)
		}
	} else {
		if !t.allowNew {
			return t.fail("temporary agents are disabled; pass `agent` to name a configured agent")
		}
		// A bare prompt is the common case: the child is a temporary agent
		// that inherits the current model and tools. It used to be rejected,
		// which made the tool unusable for models that omit the optional
		// fields.
		inherited := false
		if system == "" {
			system = defaultSubagentSystem
		}
		if len(cleanToolNames(in.Tools)) == 0 {
			in.Tools = ActiveTools(ctx)
			inherited = true
		}
		toolNames := cleanToolNames(in.Tools)
		if t.tools != nil {
			kept := make([]string, 0, len(toolNames))
			for _, toolName := range toolNames {
				if t.tools[toolName] {
					kept = append(kept, toolName)
					continue
				}
				if !inherited {
					return t.fail("tool %q is not allowed for temporary agents", toolName)
				}
			}
			toolNames = kept
		}
		in.Tools = toolNames
	}

	if subagentRunner == nil {
		return t.fail("subagent runner is not available")
	}

	depth := SubagentDepth(ctx)
	if t.maxDepth > 0 && depth >= t.maxDepth {
		return t.fail("subagent nesting limit reached (max_depth=%d)", t.maxDepth)
	}

	runCtx := WithSubagentDepth(ctx, depth+1)
	if t.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, t.timeout)
		defer cancel()
	}

	// A temporary agent runs on the model the parent is using right now
	// (including a /model switch) unless the call names one explicitly.
	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = ActiveModel(ctx)
	}

	out, err := subagentRunner(runCtx, SubagentSpec{
		Agent:  agentName,
		System: system,
		Model:  model,
		Tools:  in.Tools,
		Prompt: prompt,
	})
	if err != nil {
		if ctx.Err() != nil { // the parent turn was cancelled
			return "", ctx.Err()
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return t.fail("subagent timed out after %s", t.timeout)
		}
		return t.fail("subagent failed: %v", err)
	}

	if out = strings.TrimSpace(out); out == "" {
		return "(subagent returned no output)", nil
	}
	return out, nil
}

// fail reports a problem as a tool result so the parent turn can recover.
func (t *subagentTool) fail(format string, args ...any) (string, error) {
	return "error: " + fmt.Sprintf(format, args...), nil
}

// stringSet converts a YAML string array into a set, ignoring blanks.
func stringSet(v config.Value) map[string]bool {
	set := make(map[string]bool)
	for _, entry := range v.Array() {
		if name := strings.TrimSpace(entry.String()); name != "" {
			set[name] = true
		}
	}
	return set
}

// cleanToolNames trims, drops empty entries and removes duplicates.
func cleanToolNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
