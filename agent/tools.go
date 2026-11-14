package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
	"github.com/tk103331/oni3/tools"
	"github.com/tk103331/oni3/tools/approval"
)

// createTool creates tool instances
func createTool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	return tools.CreateTool(name, cfg)
}

// validateToolNames rejects ambiguous runtime registrations across configured,
// plugin and MCP tools before constructing the agent.
func validateToolNames(ctx context.Context, configured []tool.BaseTool) error {
	seen := make(map[string]bool, len(configured))
	for _, instance := range configured {
		info, err := instance.Info(ctx)
		if err != nil {
			return fmt.Errorf("cannot read tool metadata: %w", err)
		}
		if info == nil || info.Name == "" {
			return fmt.Errorf("tool must have a nonempty runtime name")
		}
		if seen[info.Name] {
			return fmt.Errorf("duplicate tool name %q", info.Name)
		}
		seen[info.Name] = true
	}
	return nil
}

// approvalPolicy builds the tool-approval policy from global settings. Without
// explicit configuration dangerous tools (shell commands, file writes) ask for
// confirmation in interactive sessions.
func approvalPolicy() approval.Policy {
	policy := approval.Policy{Mode: approval.ModeDangerous}
	cfg := config.GetConfig()
	if cfg == nil || cfg.Settings.Approval == nil {
		return policy
	}
	policy.Mode = cfg.Settings.Approval.Mode
	policy.Allow = nameSet(cfg.Settings.Approval.Allow)
	policy.Always = nameSet(cfg.Settings.Approval.Always)
	return policy
}

// wrapApprovals applies the approval policy to every configured tool. The
// wrapper is transparent outside interactive sessions, where no host is
// attached to the context.
func wrapApprovals(configured []tool.BaseTool) ([]tool.BaseTool, error) {
	policy := approvalPolicy()
	for i, tl := range configured {
		invokable, ok := tl.(tool.InvokableTool)
		if !ok {
			continue
		}
		wrapped, err := tools.WithApproval(invokable, policy)
		if err != nil {
			return configured, err
		}
		configured[i] = wrapped
	}
	return configured, nil
}

// nameSet converts a name list into a lookup set.
func nameSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// ToolErrorPrefix marks a tool result that carries a failure. The UI shows
// such a result as a failed call instead of a successful one.
const ToolErrorPrefix = "tool execution failed: "

// IsToolError reports whether a tool result carries a failure.
func IsToolError(result string) bool {
	return strings.HasPrefix(strings.TrimSpace(result), ToolErrorPrefix)
}

// recoverableTool turns a failing tool into a normal result. The model then
// sees what went wrong and can adjust, instead of one broken tool ending the
// whole ReAct turn.
type recoverableTool struct {
	tool.InvokableTool
	name string
}

func (t recoverableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	result, err := t.InvokableTool.InvokableRun(ctx, argumentsInJSON, opts...)
	if err == nil {
		return result, nil
	}
	// Cancellation and interrupts still have to end the turn.
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	if _, ok := compose.IsInterruptRerunError(err); ok {
		return "", err
	}
	logger.Error("AGENT", fmt.Sprintf("Tool %s failed: %v", t.name, err))
	return ToolErrorPrefix + err.Error(), nil
}

// wrapRecoverable makes every tool failure recoverable: the error is handed
// back to the model as a tool result so the agent can keep working.
func wrapRecoverable(configured []tool.BaseTool) []tool.BaseTool {
	for i, tl := range configured {
		invokable, ok := tl.(tool.InvokableTool)
		if !ok {
			continue
		}
		name := ""
		if info, err := invokable.Info(context.Background()); err == nil && info != nil {
			name = info.Name
		}
		configured[i] = recoverableTool{InvokableTool: invokable, name: name}
	}
	return configured
}
