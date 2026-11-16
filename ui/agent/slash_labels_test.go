package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/session"
	"gopkg.in/yaml.v3"
)

// paletteTestModel builds a view model with one slash command.
func paletteTestModel(t *testing.T, cmds ...SlashCommand) *ViewModel {
	t.Helper()
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands(cmds, func(string) CommandResult { return CommandResult{} })
	return m
}

func TestPaletteShowsValueLabels(t *testing.T) {
	m := paletteTestModel(t, SlashCommand{
		Name: "model", Args: "<name>",
		Values:      []string{"gpt4", "ds41"},
		ValueLabels: []string{"provider/gpt-4o", "deepseek/deepseek-chat"},
	})

	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/model ")})
	out := m.View()
	for _, want := range []string{"gpt4", "provider/gpt-4o", "ds41", "deepseek/deepseek-chat"} {
		if !strings.Contains(out, want) {
			t.Fatalf("palette missing %q:\n%s", want, out)
		}
	}
}

func TestPaletteValueLabelsWithFewerLabels(t *testing.T) {
	// Labels shorter than Values are simply not shown for the tail.
	m := paletteTestModel(t, SlashCommand{
		Name: "skill", Args: "<name>",
		Values:      []string{"off", "coder"},
		ValueLabels: []string{"不使用 skill"},
	})

	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/skill ")})
	out := m.View()
	if !strings.Contains(out, "不使用 skill") {
		t.Fatalf("first label missing:\n%s", out)
	}
	if !strings.Contains(out, "coder") {
		t.Fatalf("unlabeled value missing:\n%s", out)
	}
}

func TestKeepArgsFillsInputInsteadOfExecuting(t *testing.T) {
	executed := ""
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands([]SlashCommand{
		{Name: "skill", Args: "<name>", Values: []string{"off", "coder"}, KeepArgs: true},
	}, func(input string) CommandResult {
		executed = input
		return CommandResult{Message: "executed: " + input}
	})

	// Type "/skill ", pick "coder" from the palette.
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/skill ")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown}) // "off" -> "coder"
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})

	if executed != "" {
		t.Fatalf("KeepArgs command executed immediately: %q", executed)
	}
	if m.input != "/skill coder " {
		t.Fatalf("input = %q, want %q", m.input, "/skill coder ")
	}
	if m.cursor != len([]rune("/skill coder ")) {
		t.Fatalf("cursor = %d, want end of input", m.cursor)
	}

	// The palette must not re-appear once the argument is complete.
	if m.hasPalette() {
		t.Fatalf("palette still visible after a kept argument:\n%s", m.View())
	}

	// Typing continues as the message body.
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("写一个排序")})
	if m.input != "/skill coder 写一个排序" {
		t.Fatalf("continued input = %q", m.input)
	}

	// Enter now executes the whole line: the app handler receives it.
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if executed != "skill coder 写一个排序" {
		t.Fatalf("handler got %q", executed)
	}
}

func TestNonKeepArgsValueStillExecutes(t *testing.T) {
	executed := ""
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands([]SlashCommand{
		{Name: "model", Args: "<name>", Values: []string{"gpt4"}, ValueLabels: []string{"provider/gpt-4o"}},
	}, func(input string) CommandResult {
		executed = input
		return CommandResult{Message: "ok"}
	})

	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/model ")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if executed != "model gpt4" {
		t.Fatalf("handler got %q", executed)
	}
	if m.input != "" {
		t.Fatalf("input should be cleared, got %q", m.input)
	}
}

func TestMCPServerLabel(t *testing.T) {
	cases := []struct {
		name   string
		server config.MCPServer
		want   string
	}{
		{"described", config.MCPServer{Type: "stdio", Cmd: "npx", Description: " 文件系统访问 "}, "文件系统访问"},
		{"stdio", config.MCPServer{Type: "stdio", Cmd: "npx -y fs-server"}, "npx -y fs-server"},
		{"http", config.MCPServer{Type: "streamable_http", URL: "https://mcp.example.com"}, "https://mcp.example.com"},
		{"bare", config.MCPServer{Type: "sse"}, "sse"},
	}
	for _, tc := range cases {
		if got := mcpServerLabel(tc.name, tc.server); got != tc.want {
			t.Errorf("%s: label = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormatValueRowEmphasizesValue(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})

	// The row contains the bold value and the muted annotation; the plain
	// text carries both, emphasis is applied through styles.
	m.SetSlashCommands([]SlashCommand{
		{Name: "agent", Args: "<name>", Values: []string{"worker"}, ValueLabels: []string{"model: gpt"}},
	}, nil)
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/agent ")})
	out := m.View()
	if !strings.Contains(out, "worker") || !strings.Contains(out, "model: gpt") {
		t.Fatalf("value row missing parts:\n%s", out)
	}
	// The value is rendered with the bold style, the label is not.
	raw := m.formatValueRow("agent", "worker", "model: gpt")
	plain := ansi.Strip(raw)
	if !strings.Contains(plain, "worker") || !strings.Contains(plain, "model: gpt") {
		t.Fatalf("value row text: %q", plain)
	}
}

func TestModelLabelUsesMiddleDotSeparator(t *testing.T) {
	cfg := configWith(t, `
providers:
  cloud: {type: openai}
models:
  local: {provider: cloud, model: gpt-4o-mini}
`)
	labels := modelValueLabels(cfg)
	if len(labels) != 1 || labels[0] != "cloud · gpt-4o-mini" {
		t.Fatalf("labels = %v", labels)
	}
}

func TestProviderLabelIsURL(t *testing.T) {
	cfg := configWith(t, `
providers:
  deepseek: {type: deepseek, base_url: "https://api.deepseek.com/v1"}
`)
	labels := providerValueLabels(cfg)
	// Built-in providers are seeded beside the configured one; every label is
	// the provider's base URL.
	names := sortedKeys(cfg.Providers)
	if len(labels) != len(names) {
		t.Fatalf("labels = %v for %v", labels, names)
	}
	for i, name := range names {
		if labels[i] != cfg.Providers[name].BaseURL {
			t.Fatalf("label for %s = %q", name, labels[i])
		}
	}
	if !containsString(labels, "https://api.deepseek.com/v1") {
		t.Fatalf("configured provider missing: %v", labels)
	}
}

func TestMCPEndpointLabel(t *testing.T) {
	// stdio shows cmd + args.
	if got := mcpEndpointLabel(config.MCPServer{Type: "stdio", Cmd: "npx", Args: []string{"-y", "fs-server"}}); got != "npx -y fs-server" {
		t.Fatalf("stdio label = %q", got)
	}
	// HTTP shows the URL without query parameters.
	if got := mcpEndpointLabel(config.MCPServer{Type: "streamable_http", URL: "https://mcp.example.com/rpc?token=abc"}); got != "https://mcp.example.com/rpc" {
		t.Fatalf("http label = %q", got)
	}
	// A configured description still wins.
	if got := mcpServerLabel("fs", config.MCPServer{Type: "stdio", Cmd: "x", Description: "描述"}); got != "描述" {
		t.Fatalf("description label = %q", got)
	}
}

// configWith loads a YAML snippet as the global config for one test.
func configWith(t *testing.T, yamlText string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "fixture.yml")
	if err := os.WriteFile(path, []byte(yamlText), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	restore := filepath.Join(t.TempDir(), "restore.yml")
	if previous := config.GetConfig(); previous != nil {
		if data, err := yaml.Marshal(previous); err == nil {
			_ = os.WriteFile(restore, data, 0600)
		}
	}
	t.Cleanup(func() {
		if _, err := config.LoadConfig(restore); err != nil {
			t.Error(err)
		}
	})
	return cfg
}

func TestThemeValueLabelsShowSwatches(t *testing.T) {
	// The label contains five color blocks (any theme) and the description.
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	app := &AgentApp{model: m}
	labels := app.themeValueLabels()
	if len(labels) == 0 {
		t.Fatal("no theme labels")
	}
	// Every label carries at least one colored block.
	if !strings.Contains(labels[0], "●") {
		t.Fatalf("first theme label has no swatch: %q", labels[0])
	}
}

func TestResumeValueLabels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfgText := `
providers:
  cloud: {type: openai, base_url: "https://example.com/v1", api_key_optional: true}
models:
  gpt: {provider: cloud, model: gpt-4o}
agents:
  OniThree: {system: help, model: gpt}
`
	if err := os.WriteFile(filepath.Join(dir, "f.yml"), []byte(cfgText), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConfig(filepath.Join(dir, "f.yml")); err != nil {
		t.Fatal(err)
	}
	// Create one session with a user message.
	s := session.New("agent", "OniThree", "gpt", "")
	if err := s.AddUser("帮我写排序算法"); err != nil {
		t.Fatal(err)
	}
	labels := resumeValueLabels("agent", 10)
	if len(labels) != 1 {
		t.Fatalf("labels = %v", labels)
	}
	if !strings.Contains(labels[0], "帮我写排序算法") {
		t.Fatalf("label missing the session title: %q", labels[0])
	}
	// The time prefix is a sortable date.
	if !regexp.MustCompile(`^\d{2}-\d{2} \d{2}:\d{2}`).MatchString(labels[0]) {
		t.Fatalf("label missing the time: %q", labels[0])
	}
}
