package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
)

func testMCPConfig() *config.Config {
	return &config.Config{
		DefaultModel: "zeta",
		Models: map[string]config.Model{
			"zeta":  {Provider: "openai", Model: "gpt-4"},
			"alpha": {Provider: "deepseek", Model: "deepseek-chat"},
		},
		Agents: map[string]config.Agent{
			"helper":       {Description: "Help with small tasks", Model: "alpha"},
			"search agent": {Model: "zeta"},
		},
	}
}

func findTool(t *testing.T, tools []tool.InvokableTool, name string) tool.InvokableTool {
	t.Helper()
	for _, instance := range tools {
		info, err := instance.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info != nil && info.Name == name {
			return instance
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func TestMCPTools(t *testing.T) {
	cfg := testMCPConfig()
	exposed, err := mcpTools(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(exposed) != 3 {
		t.Fatalf("tools = %d, want 3", len(exposed))
	}

	call := findTool(t, exposed, mcpCallAgentToolName)
	info, err := call.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if js == nil || js.Properties == nil {
		t.Fatalf("schema = %+v", js)
	}
	for _, name := range []string{"agent", "prompt", "model"} {
		if _, ok := js.Properties.Get(name); !ok {
			t.Fatalf("missing parameter %q", name)
		}
	}
	if len(js.Required) != 2 || js.Required[0] != "agent" || js.Required[1] != "prompt" {
		t.Fatalf("required = %v", js.Required)
	}

	for _, name := range []string{mcpListAgentsToolName, mcpModelsToolName} {
		instance := findTool(t, exposed, name)
		info, err := instance.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.ParamsOneOf != nil {
			t.Fatalf("%s unexpectedly declares parameters", name)
		}
	}
}

func TestMCPListAgents(t *testing.T) {
	cfg := testMCPConfig()
	exposed, err := mcpTools(cfg)
	if err != nil {
		t.Fatal(err)
	}
	instance := findTool(t, exposed, mcpListAgentsToolName)
	result, err := instance.InvokableRun(context.Background(), "{}")
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Agents []struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			DefaultModel string `json:"default_model"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("invalid JSON result %q: %v", result, err)
	}
	if len(payload.Agents) != 2 {
		t.Fatalf("agents = %+v", payload.Agents)
	}
	if payload.Agents[0].Name != "helper" ||
		payload.Agents[0].Description != "Help with small tasks" ||
		payload.Agents[0].DefaultModel != "alpha" {
		t.Fatalf("helper = %+v", payload.Agents[0])
	}
	if payload.Agents[1].Name != "search agent" ||
		payload.Agents[1].Description != "search agent" ||
		payload.Agents[1].DefaultModel != "zeta" {
		t.Fatalf("search agent = %+v", payload.Agents[1])
	}
}

func TestMCPModelListTool(t *testing.T) {
	cfg := testMCPConfig()
	exposed, err := mcpTools(cfg)
	if err != nil {
		t.Fatal(err)
	}
	instance := findTool(t, exposed, mcpModelsToolName)
	result, err := instance.InvokableRun(context.Background(), "{}")
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		DefaultModel string   `json:"default_model"`
		Models       []string `json:"models"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("invalid JSON result %q: %v", result, err)
	}
	if payload.DefaultModel != "zeta" {
		t.Fatalf("default model = %q", payload.DefaultModel)
	}
	if len(payload.Models) != 2 || payload.Models[0] != "alpha" || payload.Models[1] != "zeta" {
		t.Fatalf("models = %+v", payload.Models)
	}
}

func TestCallAgentToolValidatesArguments(t *testing.T) {
	cfg := testMCPConfig()
	exposed, err := mcpTools(cfg)
	if err != nil {
		t.Fatal(err)
	}
	instance := findTool(t, exposed, mcpCallAgentToolName)
	if _, err := instance.InvokableRun(context.Background(), `{`); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := instance.InvokableRun(context.Background(), `{"prompt":"hi"}`); err == nil ||
		!strings.Contains(err.Error(), "agent is required") {
		t.Fatalf("missing agent error = %v", err)
	}
	if _, err := instance.InvokableRun(context.Background(), `{"agent":"helper"}`); err == nil ||
		!strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("missing prompt error = %v", err)
	}
	if _, err := instance.InvokableRun(context.Background(), `{"agent":"missing","prompt":"hi"}`); err == nil ||
		!strings.Contains(err.Error(), `agent "missing" does not exist`) ||
		!strings.Contains(err.Error(), "helper, search agent") {
		t.Fatalf("unknown agent error = %v", err)
	}
	if _, err := instance.InvokableRun(context.Background(), `{"agent":"helper","prompt":"hi","model":"missing"}`); err == nil ||
		!strings.Contains(err.Error(), `model "missing" does not exist`) ||
		!strings.Contains(err.Error(), "alpha, zeta") {
		t.Fatalf("unknown model error = %v", err)
	}
}

func TestMCPCommandRegistered(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != mcpCmd {
		t.Fatalf("mcp resolves to %q", cmd.Name())
	}
	if flag := mcpCmd.Flags().Lookup("agent"); flag != nil {
		t.Fatal("mcp still has the obsolete --agent flag")
	}
}
