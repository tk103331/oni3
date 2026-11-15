package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tk103331/oni3/config"
)

// 集成冒烟：/mcp 打开管理面板 + KeepArgs 的 /skill 流程
func TestPaletteMCPAndSkillIntegration(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"filesystem"})

	m := app.model
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	app.registerCommands()

	// /mcp 打开管理面板（不再走候选列表）
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/mcp")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mcpPanelState == nil {
		t.Fatalf("panel did not open:\n%s", m.View())
	}
	out := m.View()
	for _, want := range []string{"filesystem", "文件系统", "grep", "https://mcp.grep.app"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mcp panel missing %q:\n%s", want, out)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// /skill 候选至少有 off；没有配置 skill 时也不报错
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/skill ")})
	out = m.View()
	if !strings.Contains(out, "/skill") || !strings.Contains(out, "off") {
		t.Fatalf("skill palette should offer off:\n%s", out)
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
	if _, err := config.LoadConfig(dir + "/fixture.yml"); err != nil {
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
