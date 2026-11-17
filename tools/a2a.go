package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/tools/approval"
)

// RemoteAgentSpec describes one non-streaming A2A call.
type RemoteAgentSpec struct {
	Agent        string
	Prompt       string
	TaskID       string
	ContextID    string
	Timeout      time.Duration
	PollInterval time.Duration
}

// RemoteAgentRunner runs a remote A2A agent. The agent package installs the
// implementation so the tools package does not depend on the protocol server.
type RemoteAgentRunner func(context.Context, RemoteAgentSpec) (string, error)

var remoteAgentRunner RemoteAgentRunner

// SetRemoteAgentRunner installs the remote A2A agent runner.
func SetRemoteAgentRunner(r RemoteAgentRunner) {
	remoteAgentRunner = r
}

type a2aTool struct {
	info         *schema.ToolInfo
	agents       map[string]bool
	timeout      time.Duration
	pollInterval time.Duration
}

// NewA2ATool creates a tool that calls configured remote A2A agents without
// streaming.
//
// Config options:
//   - agents: allowlist of names from a2a_agents (default: all)
//   - timeout: overall per-call timeout in seconds (default 300)
//   - poll_interval_ms: task polling interval in milliseconds (default 1000)
func NewA2ATool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	t := &a2aTool{
		timeout:      5 * time.Minute,
		pollInterval: time.Second,
	}
	if cfg.Config != nil {
		if value, ok := cfg.Config["agents"]; ok && value.IsArray() {
			if agents := stringSet(value); len(agents) > 0 {
				t.agents = agents
			}
		}
		if value, ok := cfg.Config["timeout"]; ok {
			if seconds := value.Int(); seconds > 0 {
				t.timeout = time.Duration(seconds) * time.Second
			}
		}
		if value, ok := cfg.Config["poll_interval_ms"]; ok {
			if millis := value.Int(); millis > 0 {
				t.pollInterval = time.Duration(millis) * time.Millisecond
			}
		}
	}

	global := config.GetConfig()
	if global != nil && t.agents != nil {
		for remote := range t.agents {
			if _, ok := global.A2AAgents[remote]; !ok {
				return nil, fmt.Errorf("a2a_agents does not define %q", remote)
			}
		}
	}

	desc := strings.TrimSpace(cfg.Description)
	if desc == "" {
		desc = "Call a remote agent over the A2A protocol without streaming. " +
			"The result is JSON containing state, task_id, context_id, answer or message. " +
			"For a new task pass `agent` and `prompt`. When the state is TASK_STATE_INPUT_REQUIRED, " +
			"call again with the same `task_id` and put the answer in `prompt`. Omit `prompt` with " +
			"`task_id` to keep polling a task that is still running."
	}
	desc += " " + remoteAgentSummary(global, t.agents)

	t.info = &schema.ToolInfo{
		Name: name,
		Desc: desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"agent": {
				Type:     schema.String,
				Desc:     "Remote agent name configured under a2a_agents.",
				Required: true,
			},
			"prompt": {
				Type: schema.String,
				Desc: "Task text for a new task, or the answer when resuming an input-required task. Omit to only poll an existing task.",
			},
			"task_id": {
				Type: schema.String,
				Desc: "Existing remote task ID. Use it to resume, continue polling, or start another turn in the same context.",
			},
			"context_id": {
				Type: schema.String,
				Desc: "Optional remote context ID for a new task. The response always reports the effective context ID.",
			},
		}),
	}
	return t, nil
}

func remoteAgentSummary(cfg *config.Config, allowed map[string]bool) string {
	if cfg == nil || len(cfg.A2AAgents) == 0 {
		return "No remote agents are configured under a2a_agents."
	}
	names := make([]string, 0, len(cfg.A2AAgents))
	for name := range cfg.A2AAgents {
		if allowed != nil && !allowed[name] {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "No remote agents are available to this tool."
	}
	sort.Strings(names)
	var out strings.Builder
	out.WriteString("Available remote agents:")
	for _, name := range names {
		out.WriteString(" ")
		out.WriteString(name)
		if description := strings.TrimSpace(cfg.A2AAgents[name].Description); description != "" {
			out.WriteString(" (")
			out.WriteString(description)
			out.WriteString(")")
		}
		out.WriteString(";")
	}
	return strings.TrimSuffix(out.String(), ";")
}

func (t *a2aTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *a2aTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var in struct {
		Agent     string `json:"agent"`
		Prompt    string `json:"prompt"`
		TaskID    string `json:"task_id"`
		ContextID string `json:"context_id"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return t.fail("invalid arguments: %v", err)
	}
	in.Agent = strings.TrimSpace(in.Agent)
	in.Prompt = strings.TrimSpace(in.Prompt)
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.ContextID = strings.TrimSpace(in.ContextID)

	if in.Agent == "" {
		return t.fail("agent is required")
	}
	if t.agents != nil && !t.agents[in.Agent] {
		return t.fail("agent %q is not allowed by this tool's configuration", in.Agent)
	}
	if in.Prompt == "" && in.TaskID == "" {
		return t.fail("prompt is required for a new task")
	}
	if remoteAgentRunner == nil {
		return t.fail("A2A remote agent runner is not available")
	}

	out, err := remoteAgentRunner(ctx, RemoteAgentSpec{
		Agent:        in.Agent,
		Prompt:       in.Prompt,
		TaskID:       in.TaskID,
		ContextID:    in.ContextID,
		Timeout:      t.timeout,
		PollInterval: t.pollInterval,
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return t.fail("remote A2A agent failed: %v", err)
	}
	if out = strings.TrimSpace(out); out == "" {
		return "(remote agent returned no output)", nil
	}
	return out, nil
}

// ApprovalRequest makes remote calls visible to the approval policy: prompts
// leave the machine and may contain sensitive data.
func (t *a2aTool) ApprovalRequest(arguments string) (approval.Request, bool) {
	var in struct {
		Agent  string `json:"agent"`
		Prompt string `json:"prompt"`
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(arguments), &in)
	summary := "Call remote A2A agent " + strings.TrimSpace(in.Agent)
	if taskID := strings.TrimSpace(in.TaskID); taskID != "" {
		summary += " for task " + taskID
	}
	return approval.Request{
		Tool:    t.info.Name,
		Summary: summary,
		Detail:  strings.TrimSpace(in.Prompt),
	}, true
}

func (t *a2aTool) fail(format string, args ...any) (string, error) {
	return "error: " + fmt.Sprintf(format, args...), nil
}
