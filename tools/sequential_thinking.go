package tools

import (
	"context"

	"github.com/cloudwego/eino-ext/components/tool/sequentialthinking"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

// NewSequentialThinkingTool uses the configured name and description rather than
// exposing the upstream implementation's fixed tool name.
func NewSequentialThinkingTool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	inner, err := sequentialthinking.NewTool()
	if err != nil {
		return nil, err
	}
	info, err := inner.Info(context.Background())
	if err != nil {
		return nil, err
	}
	metadata := *info
	metadata.Name = name
	if cfg.Description != "" {
		metadata.Desc = cfg.Description
	}
	return &sequentialThinkingTool{InvokableTool: inner, info: &metadata}, nil
}

type sequentialThinkingTool struct {
	tool.InvokableTool
	info *schema.ToolInfo
}

func (t *sequentialThinkingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}
