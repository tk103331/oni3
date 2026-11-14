package agent

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestInputViewportFollowsCursor(t *testing.T) {
	m := NewViewModel(nil)
	m.width = 24
	m.input = "A" + strings.Repeat("中", 40) + "界" + strings.Repeat("z", 40)
	for _, tc := range []struct {
		cursor  int
		visible string
	}{{0, "A"}, {41, "界"}, {len([]rune(m.input)), "z"}} {
		m.cursor = tc.cursor
		out := m.renderEditableInput()
		if !strings.Contains(out, tc.visible) || lipgloss.Width(out) > m.width-6 {
			t.Fatalf("cursor %d not visible within input width: %q", tc.cursor, out)
		}
	}
	m.input, m.cursor = "first\nsecond", 5
	if out := m.renderEditableInput(); strings.Contains(out, "\n") || !strings.Contains(out, "↵") {
		t.Fatalf("multiline input should occupy one physical row: %q", out)
	}
	if m.input != "first\nsecond" {
		t.Fatal("display changed the submitted prompt")
	}
}

func TestShortTerminalPanelsFollowSelection(t *testing.T) {
	for _, height := range []int{10, 12, 18} {
		for _, panel := range []string{"palette", "question"} {
			t.Run(fmt.Sprintf("%d/%s", height, panel), func(t *testing.T) {
				m := NewViewModel(nil)
				m = drive(m, tea.WindowSizeMsg{Width: 60, Height: height})
				for i := 0; i < 12; i++ {
					m.todos = append(m.todos, TodoItem{Content: fmt.Sprintf("task-%d", i), Status: TodoInProgress})
				}
				values := make([]string, 16)
				for i := range values {
					values[i] = fmt.Sprintf("option-%02d", i)
				}
				if panel == "palette" {
					m.SetSlashCommands([]SlashCommand{{Name: "model", Args: "<name>", Values: values}}, nil)
					m.input, m.paletteSel = "/model ", 15
				} else {
					m.question = &questionState{question: "choose", options: values, sel: 15, approval: true, detail: "command details"}
				}
				out := m.View()
				lines := strings.Split(out, "\n")
				if len(lines) != height || !strings.HasPrefix(lines[len(lines)-2], "╰") {
					t.Fatalf("panels pushed input outside terminal:\n%s", out)
				}
				if !strings.Contains(out, "option-15") {
					t.Fatalf("selected item is hidden:\n%s", out)
				}
			})
		}
	}
}

func TestPaletteIncludesCommandsBeyondFirstEight(t *testing.T) {
	m := NewViewModel(nil)
	commands := make([]SlashCommand, 12)
	for i := range commands {
		commands[i] = SlashCommand{Name: fmt.Sprintf("command-%02d", i)}
	}
	m.SetSlashCommands(commands, nil)
	m.input = "/"
	items := m.paletteItems()
	if items[len(items)-1].fill != "/command-11" {
		t.Fatal("last command is not selectable")
	}
}

func TestResponsiveLayoutStates(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		for _, state := range []string{"idle", "waiting", "stop", "quit", "multiline"} {
			t.Run(fmt.Sprintf("%d/%s", width, state), func(t *testing.T) {
				m := NewViewModel(func(string) error { return nil })
				m = drive(m, tea.WindowSizeMsg{Width: width, Height: 24})
				m.SetSubtitle("代码助手 · a-very-long-model-name")
				switch state {
				case "waiting", "stop":
					m.isWaiting = true
					m.stopArmed = state == "stop"
				case "quit":
					m.confirmQuit = true
				case "multiline":
					m.SetTranscript([]Message{{Type: UserMessage, Content: "第一行\n第二行\n第三行"}})
				}
				lines := strings.Split(m.View(), "\n")
				if len(lines) != 24 {
					t.Fatalf("height = %d, want 24", len(lines))
				}
				for i, line := range lines {
					if w := lipgloss.Width(line); w > width {
						t.Fatalf("line %d width = %d, want <= %d", i, w, width)
					}
				}
				status := "ready"
				if m.isWaiting {
					status = "thinking"
				}
				if !strings.Contains(lines[0], status) {
					t.Fatalf("header lost status: %s", lines[0])
				}
				if !strings.HasPrefix(lines[len(lines)-2], "╰") {
					t.Fatal("input moved away from the bottom")
				}
				if len(m.lineOwners) != len(m.renderedLines) {
					t.Fatal("mouse hit targets no longer match transcript rows")
				}
				for _, line := range m.renderedLines {
					if strings.Contains(line, "\n") {
						t.Fatal("cached transcript row contains multiple physical lines")
					}
				}
			})
		}
	}
}
