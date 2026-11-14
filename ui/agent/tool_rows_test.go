package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The same tool call reported twice must stay a single row.
func TestRepeatedToolStartDoesNotDuplicateTheRow(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(m, ToolStartMsg{Name: "shell", CallID: "call-1", Arguments: `{"command":"echo hi"}`})
	m = drive(m, ToolStartMsg{Name: "shell", CallID: "call-1", Arguments: `{"command":"echo hi"}`})
	m = drive(m, ToolEndMsg{Name: "shell", CallID: "call-1", Result: "ok"})

	rows := 0
	for _, msg := range m.messages {
		if msg.Type == ToolStartMessage {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("expected one tool row, got %d", rows)
	}
	if m.messages[0].ToolStatus != ToolSuccess {
		t.Fatalf("tool row not completed: %v", m.messages[0].ToolStatus)
	}
}

// Two calls of the same tool with the same arguments are two rows.
func TestIdenticalToolCallsStaySeparate(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(m, ToolStartMsg{Name: "todo_list", CallID: "call-1", Arguments: `{"todos":[{"content":"a"}]}`})
	m = drive(m, ToolStartMsg{Name: "todo_list", CallID: "call-2", Arguments: `{"todos":[{"content":"a"}]}`})
	m = drive(m, ToolEndMsg{Name: "todo_list", CallID: "call-2", Result: "second"})

	rows := 0
	for _, msg := range m.messages {
		if msg.Type == ToolStartMessage {
			rows++
		}
	}
	if rows != 2 {
		t.Fatalf("expected two tool rows, got %d", rows)
	}
	// The result belongs to the call it names.
	for _, msg := range m.messages {
		if msg.Type != ToolStartMessage {
			continue
		}
		switch msg.CallID {
		case "call-1":
			if msg.ToolStatus != ToolWaiting {
				t.Fatalf("the first call must still be running: %v", msg.ToolStatus)
			}
		case "call-2":
			if msg.ToolStatus != ToolSuccess || msg.Result != "second" {
				t.Fatalf("the second call was not completed: %+v", msg)
			}
		}
	}
}

// A failed call shows what went wrong on its own row.
func TestFailedToolShowsTheReason(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(m, ToolStartMsg{Name: "browser_use", CallID: "call-1", Arguments: `{"action":"web_search"}`})
	m = drive(m, ToolEndMsg{Name: "browser_use", CallID: "call-1", Result: "tool execution failed: no search tool found", Failed: true})

	out := m.View()
	if !strings.Contains(out, "no search tool found") {
		t.Fatalf("the failure is not shown:\n%s", out)
	}
	if strings.Count(out, "BrowserUse") != 1 {
		t.Fatalf("expected a single tool row:\n%s", out)
	}
}

// Arguments are summarized on one line, nested structures included.
func TestSummarizeArgs(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"empty", "", ""},
		{"object", "", ""},
		{"plain", `{"command":"ls -l"}`, "command=ls -l"},
		{"list", `{"todos":[{"content":"write docs","status":"in_progress"}]}`, "todos=write docs"},
		{"many", `{"todos":[{"content":"a"},{"content":"b"}]}`, "todos=2 项"},
		{"newline", "line one\nline two", "line one line two"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeArgs(tc.args)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("the summary must stay on one line: %q", got)
			}
		})
	}
}

// A pending question of a failed turn must not stay on screen.
func TestErrorClearsThePendingQuestion(t *testing.T) {
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(m, AskQuestionMsg{Question: "which one?", Options: []string{"a"}})

	if m.question == nil {
		t.Fatal("the question was not registered")
	}
	m = drive(m, ErrorMsg("boom"))
	if m.question != nil {
		t.Fatal("the pending question survived the error")
	}
}
