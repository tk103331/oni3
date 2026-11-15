package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tk103331/oni3/config"
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
