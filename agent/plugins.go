package agent

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/plugins"
	"github.com/tk103331/oni3/tools"
)

// startPluginHooks brackets a complete turn, including streaming and subagents.
func (r *ReactAgent) startPluginHooks(ctx context.Context, prompt string) (func(string, error) error, error) {
	cfg := config.GetConfig()
	if !cfg.HasPluginHook("before_agent") && !cfg.HasPluginHook("after_agent") {
		return func(_ string, err error) error { return err }, nil
	}
	model := r.config.Model
	if model == "" && cfg != nil {
		model = cfg.DefaultModelName()
	}
	event := plugins.Event{Type: "before_agent", Agent: r.agentName, Model: model, Prompt: prompt}
	if err := cfg.EmitPluginEvent(ctx, event); err != nil {
		return nil, err
	}
	return func(result string, runErr error) error {
		if ctx.Err() != nil {
			return runErr
		}
		event.Type = "after_agent"
		event.Result = result
		if runErr != nil {
			event.Error = runErr.Error()
		}
		if hookErr := cfg.EmitPluginEvent(ctx, event); hookErr != nil {
			return errors.Join(runErr, hookErr)
		}
		return runErr
	}, nil
}

type pluginHookTool struct {
	tool.InvokableTool
	cfg         *config.Config
	agent, name string
}

func (t pluginHookTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	event := plugins.Event{Type: "before_tool", Agent: t.agent, Model: tools.ActiveModel(ctx), Tool: t.name,
		CallID: compose.GetToolCallID(ctx), Arguments: args}
	if err := t.cfg.EmitPluginEvent(ctx, event); err != nil {
		return "", err
	}
	result, err := t.InvokableTool.InvokableRun(ctx, args, opts...)
	if ctx.Err() != nil {
		return result, err
	}
	event.Type = "after_tool"
	event.Result = result
	if err != nil {
		event.Error = err.Error()
	}
	if hookErr := t.cfg.EmitPluginEvent(ctx, event); hookErr != nil {
		return result, errors.Join(err, hookErr)
	}
	return result, err
}

func (r *ReactAgent) wrapPluginHooks(configured []tool.BaseTool) []tool.BaseTool {
	cfg := config.GetConfig()
	if !cfg.HasPluginHook("before_tool") && !cfg.HasPluginHook("after_tool") {
		return configured
	}
	for i, tl := range configured {
		invokable, ok := tl.(tool.InvokableTool)
		if !ok {
			continue
		}
		info, err := invokable.Info(context.Background())
		if err == nil && info != nil {
			configured[i] = pluginHookTool{InvokableTool: invokable, cfg: cfg, agent: r.agentName, name: info.Name}
		}
	}
	return configured
}
