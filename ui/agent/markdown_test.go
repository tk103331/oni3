package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestMarkdownKeepsTerminalBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%v", dark), func(t *testing.T) {
			renderer, err := glamour.NewTermRenderer(
				glamour.WithStyles(markdownStyles(resolveTheme(defaultThemeName, dark), dark)),
				glamour.WithColorProfile(termenv.TrueColor),
				glamour.WithWordWrap(76),
			)
			if err != nil {
				t.Fatal(err)
			}
			out, err := renderer.Render("### 测试结果\n\n使用 `subagent`，返回 **巴黎**。\n\n| 调用 | 结果 |\n| --- | --- |\n| `subagent` | `error: 未找到 agent` |\n")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "48;") || strings.Contains(out, "7m") {
				t.Fatalf("Markdown painted a background: %q", out)
			}
			plain := ansi.Strip(out)
			if strings.Contains(plain, "###") || !strings.Contains(plain, "`subagent`") {
				t.Fatalf("unexpected heading/code presentation:\n%s", plain)
			}
			for _, line := range strings.Split(plain, "\n") {
				if ansi.StringWidth(line) > 76 {
					t.Fatalf("Markdown exceeds its width: %q", line)
				}
			}
		})
	}
}

func TestMarkdownFitsConversationWidth(t *testing.T) {
	const content = "### 工具测试\n\n使用 `subagent` 返回 **巴黎**。\n\n" +
		"| 调用 | 结果 |\n| --- | --- |\n" +
		"| `subagent` | `error: agent configuration does not exist: __invalid_agent_probe__` |\n\n" +
		"1. 参数校验\n2. 查看原始结果\n"
	for _, width := range []int{32, 60, 100} {
		m := NewViewModel(nil)
		m.width = width
		m.ensureRenderer()
		for _, line := range m.renderAssistantContent(content, false) {
			if ansi.StringWidth(line) > width {
				t.Fatalf("Markdown exceeds terminal width %d: %q", width, line)
			}
		}
	}
}

func TestLongFailureLeavesToolNameVisible(t *testing.T) {
	for _, width := range []int{40, 60, 100} {
		m := NewViewModel(nil)
		m.width = width
		msg := Message{
			Type: ToolStartMessage, Name: "subagent", ToolStatus: ToolError,
			Arguments: `{"agent":"__invalid_agent_probe__","prompt":"测试"}`,
			Result:    "error: subagent failed: agent configuration does not exist: __invalid_agent_probe__",
		}
		for _, selected := range []int{-1, 0} {
			m.selected = selected
			lines := m.renderToolMessage(&msg, 0)
			if len(lines) != 1 || !strings.Contains(ansi.Strip(lines[0]), "Subagent") {
				t.Fatalf("width=%d selected=%d: lost tool name: %q", width, selected, lines)
			}
			if ansi.StringWidth(lines[0]) > width {
				t.Fatalf("tool row exceeds width %d: %q", width, lines[0])
			}
		}
	}
}
