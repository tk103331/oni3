package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// 集成冒烟：带标签的 /mcp 面板 + KeepArgs 的 /skill 流程
func TestPaletteMCPAndSkillIntegration(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem"})

	m := app.model
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	app.registerCommands()

	// /mcp 列出两个 server，带描述标签
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/mcp ")})
	out := m.View()
	for _, want := range []string{"filesystem", "文件系统", "grep", "https://mcp.grep.app"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mcp palette missing %q:\n%s", want, out)
		}
	}

	// /skill 的候选带描述；选择后留在输入框
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/skill ")})
	if m.hasPalette() {
		t.Fatal("no skills configured: palette should be empty for /skill")
	}
}

func TestSkillKeepArgsWithRealSkill(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfgText := `providers:
  cloud: {type: openai, base_url: "https://example.com/v1", api_key_optional: true}
models:
  gpt: {provider: cloud, model: gpt-4o}
skills:
  coder:
    description: 编码助手
    system: 你是工程师
agents:
  OniThree:
    system: help
    model: gpt
`
	if err := writeFile(dir+"/fixture.yml", []byte(cfgText)); err != nil {
		t.Fatal(err)
	}
	if _, err := configLoadForTest(dir + "/fixture.yml"); err != nil {
		t.Fatal(err)
	}
	app, err := NewAgentApp("OniThree")
	if err != nil {
		t.Fatal(err)
	}
	m := app.model
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	app.registerCommands()

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/skill ")})
	out := m.View()
	if !strings.Contains(out, "coder") || !strings.Contains(out, "编码助手") {
		t.Fatalf("skill palette:\n%s", out)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // off -> coder
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.input != "/skill coder " {
		t.Fatalf("input = %q", m.input)
	}
	if m.hasPalette() {
		t.Fatal("palette should close after keeping the argument")
	}
}

func configLoadForTest(path string) (interface{}, error) {
	return configLoad(path)
}
