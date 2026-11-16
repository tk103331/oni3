package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSlashPalette(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands([]SlashCommand{
		{Name: "model", Args: "<name>", Description: "切换模型", Values: []string{"gpt4", "ds41"}},
		{Name: "skill", Args: "<name>", Description: "选择 skill", Values: []string{"off", "translator"}},
	}, nil)

	// Typing "/" lists builtin and host commands
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	out := m.View()
	for _, want := range []string{"/help", "/clear", "/model", "/skill"} {
		if !strings.Contains(out, want) {
			t.Fatalf("palette missing %s:\n%s", want, out)
		}
	}

	// Prefix filtering
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mod")})
	out = m.View()
	if !strings.Contains(out, "/model") {
		t.Fatalf("palette missing /model:\n%s", out)
	}
	if strings.Contains(out, "/skill") {
		t.Fatalf("palette should filter out /skill:\n%s", out)
	}

	// Argument suggestions
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("el ")})
	out = m.View()
	if !strings.Contains(out, "gpt4") || !strings.Contains(out, "ds41") {
		t.Fatalf("palette missing model candidates:\n%s", out)
	}
}

func TestSlashExecution(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})

	var got string
	m.SetSlashCommands([]SlashCommand{
		{Name: "model", Args: "<name>", Description: "切换模型", Values: []string{"gpt4"}},
	}, func(input string) CommandResult {
		got = input
		return CommandResult{Message: "已切换模型: gpt4", Subtitle: "gpt4"}
	})

	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/model gpt4")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})

	if got != "model gpt4" {
		t.Fatalf("handler got %q", got)
	}
	if m.input != "" {
		t.Fatalf("input should be cleared, got %q", m.input)
	}
	if !strings.Contains(m.View(), "已切换模型: gpt4") {
		t.Fatalf("command result missing:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "gpt4") {
		t.Fatalf("subtitle not updated:\n%s", m.View())
	}
	if len(m.messages) == 0 || m.messages[len(m.messages)-1].Type != InfoMessage {
		t.Fatalf("command output should be an InfoMessage")
	}
}

func TestSlashReturnsAsyncCommandAndOptionalProviderOpensPicker(t *testing.T) {
	m := NewViewModel(nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	called := false
	m.SetSlashCommands([]SlashCommand{{Name: "provider", Args: "[name]", Description: "Choose provider", Values: []string{"cloud"}}}, func(input string) CommandResult {
		if input != "provider" {
			t.Fatalf("command: %q", input)
		}
		return CommandResult{Cmd: func() tea.Msg { called = true; return "loaded" }}
	})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/provider")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if called {
		t.Fatal("async command ran on the update goroutine")
	}
	if cmd == nil {
		t.Fatal("command was dropped")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, next := range batch {
			if next != nil {
				next()
			}
		}
	}
	if !called {
		t.Fatal("async command never ran")
	}
}

func TestSlashUnknownAndHelp(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})

	// Unknown command without handler
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/nope")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "未知命令") {
		t.Fatalf("unknown command hint missing:\n%s", m.View())
	}

	// /help lists builtin commands
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/help")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	out := m.View()
	if !strings.Contains(out, "可用命令") || !strings.Contains(out, "/clear") || !strings.Contains(out, "/help") {
		t.Fatalf("/help output wrong:\n%s", out)
	}

	// /clear still clears the conversation
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/clear")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.messages) != 0 {
		t.Fatalf("/clear failed: %d messages", len(m.messages))
	}
}

func TestSlashArrowSelection(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})

	var got string
	m.SetSlashCommands([]SlashCommand{
		{Name: "model", Args: "<name>", Description: "切换模型", Values: []string{"ds41", "gpt4"}},
	}, func(input string) CommandResult {
		got = input
		return CommandResult{Message: "ok: " + input}
	})

	// Type "/" — first row selected (builtin /help).
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.paletteSel != 0 {
		t.Fatalf("initial palette selection = %d", m.paletteSel)
	}

	// Down twice: /help -> /clear -> /model
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	items := m.paletteItems()
	if len(items) == 0 || !strings.Contains(items[m.paletteSel].line, "/model") {
		t.Fatalf("selection should be /model, sel=%d items=%v", m.paletteSel, items)
	}

	// Enter on /model fills the input with "/model " for arguments.
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.input != "/model " {
		t.Fatalf("expected input %q, got %q", "/model ", m.input)
	}

	// Value rows are now shown; move down and execute the second value.
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got != "model gpt4" {
		t.Fatalf("expected execute model gpt4, got %q", got)
	}
	if m.input != "" {
		t.Fatalf("input should be cleared after execution, got %q", m.input)
	}
}

func TestPaletteClampAndNormalScroll(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands([]SlashCommand{{Name: "model", Args: "<name>", Values: []string{"a"}}}, nil)

	// Up at the top wraps to the last row.
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyUp})
	items := m.paletteItems()
	if m.paletteSel != len(items)-1 {
		t.Fatalf("palette selection should wrap to the last row, got %d of %d", m.paletteSel, len(items))
	}
	// Down from the last row wraps back to the first.
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.paletteSel != 0 {
		t.Fatalf("palette selection should wrap to the first row, got %d", m.paletteSel)
	}

	// Without a palette, arrows scroll the conversation again.
	m2 := NewViewModel(func(string) error { return nil })
	m2 = drive(m2, tea.WindowSizeMsg{Width: 110, Height: 10})
	m2 = drive(m2, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	m2.View() // build the rendered line cache so scrolling can clamp
	m2 = drive(m2, tea.KeyMsg{Type: tea.KeyUp})
	if m2.scrollOffset != 1 {
		t.Fatalf("arrow should scroll without palette, scrollOffset=%d", m2.scrollOffset)
	}
}

func TestPaletteEnterSubLevelAndBottomInput(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands([]SlashCommand{
		{Name: "agent", Args: "<name>", Description: "切换 agent", Values: []string{"alpha", "beta"}},
	}, func(string) CommandResult { return CommandResult{} })

	// Type "/" and select /agent (builtin /help, /clear come first).
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})

	// One Enter moves straight into the agent selection list.
	if m.input != "/agent " {
		t.Fatalf("expected sub-level input, got %q", m.input)
	}
	items := m.paletteItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 agent candidates, got %d", len(items))
	}
	out := m.View()
	if !strings.Contains(out, "❯ /agent  →  alpha") {
		t.Fatalf("agent selection list missing cursor:\n%s", out)
	}

	// The help bar is the bottom-most element, directly below the input box.
	helpIdx := strings.LastIndex(out, "↑↓ select")
	inputIdx := strings.LastIndex(out, "╭")
	if helpIdx < 0 || inputIdx < 0 || helpIdx < inputIdx {
		t.Fatalf("help bar should be below the input box (input=%d help=%d)", inputIdx, helpIdx)
	}
}

func TestPaletteHintAboveCandidates(t *testing.T) {
	m := paletteTestModel(t, SlashCommand{
		Name: "model", Args: "<name>", Description: "切换模型",
		Values: []string{"alpha", "beta"}, ValueLabels: []string{"cloud · m", "cloud · n"},
	})
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/model ")})
	lines := m.paletteView()
	if len(lines) < 3 {
		t.Fatalf("palette lines = %v", lines)
	}
	if hint := ansi.Strip(lines[0]); !strings.Contains(hint, "/model") || !strings.Contains(hint, "↑↓ 选择") {
		t.Fatalf("first line is not the hint: %q", hint)
	}
	if row := ansi.Strip(lines[1]); !strings.Contains(row, "alpha") {
		t.Fatalf("candidates should start right under the hint: %q", row)
	}
}

func TestPaletteUsageWithoutValues(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands([]SlashCommand{
		{Name: "agent", Args: "<name>", Description: "切换 agent"},
	}, func(string) CommandResult { return CommandResult{} })

	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/agent ")})
	out := m.View()
	if strings.Contains(out, "未知命令") {
		t.Fatalf("known command without candidates must not show unknown hint:\n%s", out)
	}
	if !strings.Contains(out, "/agent <name>") {
		t.Fatalf("usage line missing:\n%s", out)
	}
}
