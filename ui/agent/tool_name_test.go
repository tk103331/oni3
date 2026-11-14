package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestToolDisplayName(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"command_line", "CommandLine"},
		{"file_system", "FileSystem"},
		{"http_request", "HTTPRequest"},
		{"javascript", "JavaScript"},
		{"duckduckgo", "DuckDuckGo"},
		{"weather_api", "WeatherAPI"},
		{"demo__greet", "Demo · Greet"},
		{"repository__read_file", "Repository · ReadFile"},
		{"", ""},
	} {
		if got := toolDisplayName(tc.name); got != tc.want {
			t.Errorf("display %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestToolDisplayKeepsOriginalCallIdentity(t *testing.T) {
	m := NewViewModel(nil)
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = drive(m, ToolStartMsg{Name: "repository__read_file", Arguments: `{"path":"example.txt"}`})
	if !strings.Contains(m.View(), "Repository · ReadFile") {
		t.Fatal("running row did not use the display label")
	}
	// Exercise matching by original name without a call ID, including after a render.
	m = drive(m, ToolEndMsg{Name: "repository__read_file", Result: "done"})
	if len(m.messages) != 1 || m.messages[0].Name != "repository__read_file" || m.messages[0].ToolStatus != ToolSuccess {
		t.Fatalf("display formatting changed call identity: %+v", m.messages)
	}
	m.selected = 0
	if !strings.Contains(m.View(), "Repository · ReadFile") {
		t.Fatal("selected row did not use the same display label")
	}
}
