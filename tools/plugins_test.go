package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/plugins"
)

func TestPluginToolUsesStandardFactory(t *testing.T) {
	p, err := plugins.Load(context.Background(), "demo", plugins.Config{Path: filepath.Join("..", "examples", "plugins", "demo.js")})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := CreateTool("demo__stats", config.Tool{Type: "plugin", PluginTool: p.Tools[1]})
	if err != nil {
		t.Fatal(err)
	}
	result, err := instance.InvokableRun(context.Background(), `{"text":"hello world"}`)
	if err != nil || !strings.Contains(result, `"characters":11`) || !strings.Contains(result, `"words":2`) {
		t.Fatalf("result: %q, %v", result, err)
	}
	if _, err := CreateTool("missing", config.Tool{Type: "plugin"}); err == nil {
		t.Fatal("accepted an unloaded plugin tool")
	}
}
