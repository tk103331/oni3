package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/mcp"
)

// failedMCPManager points a configured server at a refused port and runs the
// manager so the UI can be given a real recorded failure.
func failedMCPManager(t *testing.T) *mcp.Manager {
	t.Helper()
	cfg := config.GetConfig()
	if cfg == nil {
		t.Fatal("fixture config missing")
	}
	if _, ok := cfg.MCPServers["grep"]; !ok {
		t.Fatal("fixture has no grep MCP server")
	}
	// Port 1 on loopback refuses connections immediately: a deterministic
	// stand-in for the TLS timeout of a dead endpoint.
	cfg.MCPServers["grep"] = config.MCPServer{Type: config.MCPTypeStreamableHTTP, URL: "http://127.0.0.1:1/mcp"}

	t.Cleanup(func() { _ = mcp.CloseGlobalManager() })
	if err := mcp.InitializeGlobalManager(context.Background(), cfg); err == nil {
		t.Fatal("a configuration whose only server is unreachable initialized without an error")
	}
	return mcp.GetGlobalManager()
}

// TestMCPPanelShowsFailedServer keeps a failed server visible where the user
// looks for its tools instead of showing it as enabled with zero tools.
func TestMCPPanelShowsFailedServer(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"grep", "filesystem"})
	failedMCPManager(t)

	app.openMCPPanel()
	out := ansi.Strip(app.model.View())
	for _, want := range []string{"grep", "✗ 失败", "connection refused"} {
		if !strings.Contains(out, want) {
			t.Fatalf("panel output missing %q:\n%s", want, out)
		}
	}
}

// TestMCPFailureNoticeIsOneLinePerServer checks the startup notice that
// replaces the old stderr write: it must name each server and stay short
// enough to be wrapped by the transcript renderer.
func TestMCPFailureNoticeIsOneLinePerServer(t *testing.T) {
	if notice := mcpFailureNotice(nil); notice != "" {
		t.Fatalf("healthy startup produced a notice: %q", notice)
	}

	notice := mcpFailureNotice(map[string]error{
		"b": fmt.Errorf("failed to send request: %w", errors.New(`Post "https://mcp.example/mcp": net/http: TLS handshake timeout`)),
		"a": fmt.Errorf("failed to initialize MCP client for server a: %w", errors.New("connection refused")),
	})
	lines := strings.Split(notice, "\n")
	if len(lines) != 4 {
		t.Fatalf("notice is not one header, one line per server and a hint: %q", notice)
	}
	if !strings.Contains(lines[1], "a — connection refused") || !strings.Contains(lines[2], "b — ") && strings.Contains(lines[2], "net/http: TLS handshake timeout") {
		t.Fatalf("servers are not sorted with their causes: %q", notice)
	}
	if strings.Contains(notice, "failed to send request") || strings.Contains(notice, "failed to initialize") {
		t.Fatalf("notice repeats the wrapper instead of the cause: %q", notice)
	}
}

// TestMCPStatusAndToggleReportFailures keeps /status and /mcp enable honest
// about a server that has no tools this session.
func TestMCPStatusAndToggleReportFailures(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"grep"})
	failedMCPManager(t)

	if summary := app.mcpStatusSummary(); !strings.Contains(summary, "未就绪") || !strings.Contains(summary, "grep") {
		t.Fatalf("status summary hides the failure: %q", summary)
	}

	res := app.executeSlash("mcp enable grep")
	if !res.Error || !strings.Contains(res.Message, "连接失败") {
		t.Fatalf("enabling an unreachable server reported success: %+v", res)
	}
}

// TestMCPFailureNoticeFitsNarrowTerminals guards the layout: a long transport
// cause (URL, TLS error) must be wrapped by the renderer, never painted past
// the frame edge.
func TestMCPFailureNoticeFitsNarrowTerminals(t *testing.T) {
	app, _ := mcpTestApp(t, []string{"grep"})
	app.model.Update(tea.WindowSizeMsg{Width: 40, Height: 20})

	notice := mcpFailureNotice(map[string]error{
		"repo_grep": fmt.Errorf("Post %q: %w", "https://mcp.grep.app/mcp", errors.New("net/http: TLS handshake timeout")),
	})
	app.model.Update(NoticeMsg(notice))

	out := ansi.Strip(app.model.View())
	// Wrapping breaks long causes across lines, so check the fragments.
	if !strings.Contains(out, "TLS") || !strings.Contains(out, "timeout") {
		t.Fatalf("notice missing from the transcript:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if width := ansi.StringWidth(line); width > 40 {
			t.Fatalf("line wider than the terminal (%d): %q", width, line)
		}
	}
}
