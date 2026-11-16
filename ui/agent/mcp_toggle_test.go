package agent

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	before, err := marshalConfig(previous)
	if err != nil {
		t.Fatal(err)
	}
	restore := dir + "/restore.yml"
	if err := writeFile(restore, before); err != nil {
		t.Fatal(err)
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

	filename := dir + "/fixture.yml"
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
	app.model.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	return app, &cfg
}

func TestMCPEnableDisableCommand(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem", "grep"})

	// Disabling a referenced server drops it from the effective config and
	// reports the change.
	res := app.executeSlash("mcp disable grep")
	if res.Error {
		t.Fatalf("mcp disable failed: %s", res.Message)
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
	res = app.executeSlash("mcp enable grep")
	if res.Error {
		t.Fatalf("mcp enable failed: %s", res.Message)
	}
	ac, _ = app.effectiveAgentConfig()
	if !containsString(ac.MCPServers, "grep") {
		t.Fatalf("grep should be enabled again: %v", ac.MCPServers)
	}

	// Toggling to the same state is a no-op message, not an error.
	res = app.executeSlash("mcp enable grep")
	if res.Error || !strings.Contains(res.Message, "已是") {
		t.Fatalf("idempotent message = %q", res.Message)
	}

	// Unknown server.
	res = app.executeSlash("mcp enable nope")
	if !res.Error {
		t.Fatal("unknown MCP server should be an error")
	}

	// /status carries the counts.
	app.executeSlash("mcp disable filesystem")
	res = app.executeSlash("status")
	if !strings.Contains(res.Message, "MCP: 1/2 启用") {
		t.Fatalf("status after disable = %q", res.Message)
	}

	// all restores everything.
	res = app.executeSlash("mcp enable all")
	if res.Error || !strings.Contains(res.Message, "已启用全部") {
		t.Fatalf("enable all = %q", res.Message)
	}
	res = app.executeSlash("status")
	if !strings.Contains(res.Message, "MCP: 2/2 启用") {
		t.Fatalf("status after enable all = %q", res.Message)
	}

	// disable all empties the effective list.
	app.executeSlash("mcp disable all")
	ac, _ = app.effectiveAgentConfig()
	if len(ac.MCPServers) != 0 {
		t.Fatalf("disable all left servers: %v", ac.MCPServers)
	}
}

func TestMCPPanelOpensAndToggles(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem", "grep"})

	// /mcp opens the panel instead of printing to the transcript.
	res := app.executeSlash("mcp")
	if res.Error || res.Message != "" {
		t.Fatalf("mcp result = %+v", res)
	}
	m := app.model
	if m.mcpPanelState == nil {
		t.Fatal("panel did not open")
	}

	// The panel lists both servers with state, tool count, label and both
	// summary rows.
	out := m.View()
	for _, want := range []string{"filesystem", "文件系统", "grep", "https://mcp.grep.app", "✓ 启用", "0 工具", "全部启用", "全部禁用"} {
		if !strings.Contains(out, want) {
			t.Fatalf("panel missing %q:\n%s", want, out)
		}
	}

	// Space on the first row toggles filesystem and keeps the panel open.
	m = drive(m, tea.KeyMsg{Type: tea.KeySpace})
	if m.mcpPanelState == nil {
		t.Fatal("panel should stay open after a space toggle")
	}
	ac, ok := app.effectiveAgentConfig()
	if !ok || containsString(ac.MCPServers, "filesystem") {
		t.Fatalf("filesystem should be disabled after space toggle: %v", ac.MCPServers)
	}
	// The row state refreshes in place.
	out = m.View()
	if !strings.Contains(out, "⊘ 禁用") {
		t.Fatalf("row state did not refresh:\n%s", out)
	}

	// Space again re-enables it.
	m = drive(m, tea.KeyMsg{Type: tea.KeySpace})
	ac, _ = app.effectiveAgentConfig()
	if !containsString(ac.MCPServers, "filesystem") {
		t.Fatalf("filesystem should be re-enabled: %v", ac.MCPServers)
	}

	// Enter closes the panel.
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mcpPanelState != nil {
		t.Fatal("Enter should close the panel")
	}
}

func TestMCPPanelEnableAllRow(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem", "grep"})
	app.executeSlash("mcp disable grep")

	app.openMCPPanel()
	m := app.model
	// Rows: filesystem, grep, 全部启用, 全部禁用. Two downs select 全部启用.
	for range 2 {
		m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = drive(m, tea.KeyMsg{Type: tea.KeySpace})
	ac, ok := app.effectiveAgentConfig()
	if !ok || len(ac.MCPServers) != 2 {
		t.Fatalf("enable-all should restore both servers: %v", ac.MCPServers)
	}
}

func TestMCPPanelDisableAllRow(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem", "grep"})

	app.openMCPPanel()
	m := app.model
	// Three downs select 全部禁用.
	for range 3 {
		m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = drive(m, tea.KeyMsg{Type: tea.KeySpace})
	ac, ok := app.effectiveAgentConfig()
	if !ok || len(ac.MCPServers) != 0 {
		t.Fatalf("disable-all should empty the server list: %v", ac.MCPServers)
	}
}

func TestMCPEnableDisableUnreferencedServer(t *testing.T) {
	// The agent does not use "grep": toggling it reports that tools are
	// unchanged and does not rebuild the agent.
	app, _ := mcpTestApp(t, []string{"filesystem"})

	res := app.executeSlash("mcp disable grep")
	if res.Error {
		t.Fatalf("mcp disable failed: %s", res.Message)
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
	if len(model.ValueLabels) != len(model.Values) || !strings.Contains(strings.Join(model.ValueLabels, ","), "cloud.gpt-4o") {
		t.Fatalf("model labels = %v for values %v", model.ValueLabels, model.Values)
	}
	if len(agent.ValueLabels) != len(agent.Values) {
		t.Fatalf("agent labels = %v for values %v", agent.ValueLabels, agent.Values)
	}
	if !strings.Contains(mcp.Description, "2/2") {
		t.Fatalf("mcp description = %q", mcp.Description)
	}
	if len(mcp.Values) != 0 {
		t.Fatalf("/mcp should open the panel, not list candidates: %v", mcp.Values)
	}
}

func TestMCPEnableDisableKeepsConfigFileUntouched(t *testing.T) {
	app, cfg := mcpTestApp(t, []string{"filesystem", "grep"})
	app.executeSlash("mcp disable filesystem")
	app.executeSlash("mcp disable grep")
	// The global config's agent definition is untouched: only the effective
	// view filters disabled servers.
	if ac := cfg.Agents["OniThree"]; len(ac.MCPServers) != 2 {
		t.Fatalf("config agent was modified: %v", ac.MCPServers)
	}
}

func TestMCPPanelWithoutServers(t *testing.T) {
	// /mcp without configured servers is an error message, not a panel.
	cfg := &config.Config{
		Providers: map[string]config.Provider{"cloud": {Type: "openai"}},
		Models:    map[string]config.Model{"gpt": {Provider: "cloud", Model: "gpt-4o"}},
		Agents:    map[string]config.Agent{"OniThree": {System: "Help", Model: "gpt"}},
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	data, err := marshalConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(dir+"/fixture.yml", data); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConfig(dir + "/fixture.yml"); err != nil {
		t.Fatal(err)
	}
	app, err := NewAgentApp("OniThree")
	if err != nil {
		t.Fatal(err)
	}
	res := app.executeSlash("mcp")
	if !res.Error || !strings.Contains(res.Message, "没有 MCP 服务") {
		t.Fatalf("result = %+v", res)
	}
}
