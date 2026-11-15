package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk103331/oni3/config"
	"gopkg.in/yaml.v3"
)

func marshalConfig(cfg *config.Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}

// mcpTestApp builds an AgentApp around a config with two MCP servers, one of
// them referenced by the agent.
func mcpTestApp(t *testing.T, agentServers []string) (*AgentApp, *config.Config) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	previous := config.GetConfig()
	if previous == nil {
		previous = &config.Config{}
	}
	restore := filepath.Join(t.TempDir(), "restore.yml")
	if data, err := marshalConfig(previous); err == nil {
		_ = writeFile(restore, data)
	}
	t.Cleanup(func() { _, _ = config.LoadConfig(restore) })

	cfg := config.Config{
		Providers:  map[string]config.Provider{"cloud": {Type: "openai"}},
		Models:     map[string]config.Model{"gpt": {Provider: "cloud", Model: "gpt-4o"}},
		MCPServers: map[string]config.MCPServer{},
		Agents: map[string]config.Agent{"OniThree": {
			System: "Help", Model: "gpt", MCPServers: agentServers,
		}},
	}
	cfg.MCPServers["filesystem"] = config.MCPServer{Type: "stdio", Cmd: "npx fs", Description: "文件系统"}
	cfg.MCPServers["grep"] = config.MCPServer{Type: "streamable_http", URL: "https://mcp.grep.app"}

	filename := filepath.Join(dir, "fixture.yml")
	data, err := marshalConfig(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filename, data); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConfig(filename); err != nil {
		t.Fatal(err)
	}
	app, err := NewAgentApp("OniThree")
	if err != nil {
		t.Fatal(err)
	}
	return app, &cfg
}

func TestMCPToggleCommand(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem", "grep"})

	// Disabling a referenced server drops it from the effective config and
	// reports the change.
	res := app.executeSlash("mcp-off grep")
	if res.Error {
		t.Fatalf("mcp-off failed: %s", res.Message)
	}
	if !strings.Contains(res.Message, "已禁用") || !strings.Contains(res.Message, "grep") {
		t.Fatalf("message = %q", res.Message)
	}
	ac, ok := app.effectiveAgentConfig()
	if !ok {
		t.Fatal("no effective config")
	}
	if containsString(ac.MCPServers, "grep") {
		t.Fatalf("grep should be disabled: %v", ac.MCPServers)
	}
	if !containsString(ac.MCPServers, "filesystem") {
		t.Fatalf("filesystem should stay enabled: %v", ac.MCPServers)
	}

	// Re-enabling restores it.
	res = app.executeSlash("mcp grep")
	if res.Error {
		t.Fatalf("mcp failed: %s", res.Message)
	}
	ac, _ = app.effectiveAgentConfig()
	if !containsString(ac.MCPServers, "grep") {
		t.Fatalf("grep should be enabled again: %v", ac.MCPServers)
	}

	// Unknown server.
	res = app.executeSlash("mcp nope")
	if !res.Error {
		t.Fatal("unknown MCP server should be an error")
	}

	// No argument lists the servers with their state.
	res = app.executeSlash("mcp-off")
	if !res.Error || !strings.Contains(res.Message, "filesystem") || !strings.Contains(res.Message, "文件系统") {
		t.Fatalf("listing = %q", res.Message)
	}

	// /status carries the counts.
	res = app.executeSlash("status")
	if !strings.Contains(res.Message, "MCP: 2/2 启用") {
		t.Fatalf("status = %q", res.Message)
	}
	app.executeSlash("mcp-off filesystem")
	res = app.executeSlash("status")
	if !strings.Contains(res.Message, "MCP: 1/2 启用") {
		t.Fatalf("status after disable = %q", res.Message)
	}
}

func TestMCPToggleUnreferencedServer(t *testing.T) {
	// The agent does not use "grep": toggling it reports that tools are
	// unchanged and does not rebuild the agent.
	app, _ := mcpTestApp(t, []string{"filesystem"})

	res := app.executeSlash("mcp-off grep")
	if res.Error {
		t.Fatalf("mcp-off failed: %s", res.Message)
	}
	if !strings.Contains(res.Message, "未引用") {
		t.Fatalf("message should mention the server is not referenced: %q", res.Message)
	}
}

func TestSlashCommandsCarryValueLabels(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem"})

	var skill, model, agent, mcp SlashCommand
	for _, c := range app.slashCommands() {
		switch c.Name {
		case "skill":
			skill = c
		case "model":
			model = c
		case "agent":
			agent = c
		case "mcp":
			mcp = c
		}
	}
	if !skill.KeepArgs {
		t.Fatal("/skill should keep its argument in the input box")
	}
	if model.KeepArgs {
		t.Fatal("/model should execute immediately")
	}
	if len(model.ValueLabels) != len(model.Values) || !strings.Contains(strings.Join(model.ValueLabels, ","), "cloud/gpt-4o") {
		t.Fatalf("model labels = %v for values %v", model.ValueLabels, model.Values)
	}
	if len(agent.ValueLabels) != len(agent.Values) {
		t.Fatalf("agent labels = %v for values %v", agent.ValueLabels, agent.Values)
	}
	if !strings.Contains(strings.Join(mcp.ValueLabels, ","), "文件系统") {
		t.Fatalf("mcp labels = %v", mcp.ValueLabels)
	}
	if len(mcp.Values) != 2 {
		t.Fatalf("mcp values = %v", mcp.Values)
	}
	if !strings.Contains(mcp.Description, "2/2") {
		t.Fatalf("mcp description = %q", mcp.Description)
	}

	// /mcp-off marks already-disabled servers.
	app.executeSlash("mcp-off grep")
	labels := strings.Join(app.mcpServerValueLabels(config.GetConfig()), "|")
	if !strings.Contains(labels, "[已禁用]") {
		t.Fatalf("disabled marker missing: %s", labels)
	}
}

func TestMCPToggleKeepsConfigFileUntouched(t *testing.T) {
	app, cfg := mcpTestApp(t, []string{"filesystem", "grep"})
	app.executeSlash("mcp-off filesystem")
	app.executeSlash("mcp-off grep")
	// The global config's agent definition is untouched: only the effective
	// view filters disabled servers.
	if ac := cfg.Agents["OniThree"]; len(ac.MCPServers) != 2 {
		t.Fatalf("config agent was modified: %v", ac.MCPServers)
	}
}

func configLoad(path string) (interface{}, error) {
	return config.LoadConfig(path)
}
