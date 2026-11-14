package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The rendered frame must fit the terminal exactly. A taller frame makes the
// renderer drop the header, push the input box off the screen and scroll the
// transcript on every repaint.
func TestFrameNeverExceedsTerminalHeight(t *testing.T) {
	for _, height := range []int{20, 24, 30, 45} {
		t.Run("height", func(t *testing.T) {
			m := NewViewModel(func(string) error { return nil })
			m = drive(m, tea.WindowSizeMsg{Width: 100, Height: height})
			m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
			m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})

			// A task whose text spans two lines used to make the frame
			// taller than the terminal.
			m = drive(m, TodoUpdateMsg{Items: []TodoItem{
				{Content: "write the report\nand send it", Status: TodoInProgress},
				{Content: "review the result", Status: TodoPending},
			}})

			// A tool call with a line break in its arguments.
			m = drive(m, ToolStartMsg{Name: "file_system", CallID: "c1",
				Arguments: `{"path":"a.txt","content":"line one\nline two"}`})
			m = drive(m, ToolEndMsg{Name: "file_system", CallID: "c1", Result: "ok"})

			// A question with a line break in it.
			m = drive(m, AskQuestionMsg{Question: "which one?\nthe second one", Options: []string{"a", "b"}})

			for i := 0; i < 30; i++ {
				m = drive(m, ToolStartMsg{Name: "shell", CallID: string(rune('a' + i%26)),
					Arguments: `{"command":"echo hello"}`})
				m = drive(m, ToolEndMsg{Name: "shell", CallID: string(rune('a' + i%26)), Result: "ok"})
			}
			m = drive(m, StreamChunkMsg("all done"))
			m = drive(m, StreamEndMsg{})

			for _, scroll := range []int{0, 1, 5, 40} {
				m.scrollOffset = scroll
				m.markDirty()
				out := m.View()
				lines := strings.Split(out, "\n")
				if len(lines) != height {
					t.Fatalf("scroll %d: frame has %d lines, want %d", scroll, len(lines), height)
				}
				for i, line := range lines {
					if strings.Contains(line, "\n") {
						t.Fatalf("scroll %d: line %d holds a line break", scroll, i)
					}
				}
				// The header and the input box must survive the scrolling.
				if !strings.Contains(out, "Oni3") {
					t.Fatalf("scroll %d: the header is missing", scroll)
				}
				if !strings.Contains(out, "╭") {
					t.Fatalf("scroll %d: the input box is missing", scroll)
				}
				if !strings.Contains(out, "↑↓") {
					t.Fatalf("scroll %d: the help bar is missing", scroll)
				}
			}
		})
	}
}
