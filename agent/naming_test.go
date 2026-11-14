package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/tools"
)

func TestAgentRejectsDuplicateRuntimeToolNames(t *testing.T) {
	first, err := tools.NewJavaScriptTool("catalog__calculate", config.Tool{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := tools.NewAskUserTool("catalog__calculate", config.Tool{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateToolNames(context.Background(), []tool.BaseTool{first, second}); err == nil || !strings.Contains(err.Error(), `duplicate tool name "catalog__calculate"`) {
		t.Fatalf("allowed a collision between tool sources: %v", err)
	}
	third, err := tools.NewAskUserTool("other__calculate", config.Tool{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateToolNames(context.Background(), []tool.BaseTool{first, third}); err != nil {
		t.Fatalf("rejected distinct namespaced tools: %v", err)
	}
}
