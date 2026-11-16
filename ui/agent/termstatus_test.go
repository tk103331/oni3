package agent

import (
	"encoding/base64"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// captureStatus redirects status reports to a buffer for the test's duration.
func captureStatus(t *testing.T) *strings.Builder {
	t.Helper()
	buf := &strings.Builder{}
	termStatusWriter.Lock()
	old := termStatusWriter.w
	termStatusWriter.w = buf
	termStatusWriter.Unlock()
	t.Cleanup(func() {
		termStatusWriter.Lock()
		termStatusWriter.w = old
		termStatusWriter.Unlock()
	})
	return buf
}

// lastReport decodes the newest OSC 7501 report into its key=value pairs.
func lastReport(t *testing.T, buf *strings.Builder) map[string]string {
	t.Helper()
	raw := buf.String()
	idx := strings.LastIndex(raw, "\x1b]7501;")
	if idx < 0 {
		t.Fatal("no OSC 7501 report was written")
	}
	body := raw[idx+len("\x1b]7501;"):]
	if end := strings.Index(body, "\x1b\\"); end >= 0 {
		body = body[:end]
	}
	pairs := map[string]string{}
	for _, pair := range strings.Split(body, ":") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		pairs[key] = value
	}
	return pairs
}

func decodeB64(t *testing.T, s string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("msg is not valid base64: %q", s)
	}
	return string(b)
}

func TestReportStatusSequence(t *testing.T) {
	buf := captureStatus(t)
	reportStatus("working", "", "正在处理：hello")
	pairs := lastReport(t, buf)
	if pairs["state"] != "working" {
		t.Fatalf("state = %q, want working", pairs["state"])
	}
	if pairs["app"] != "oni3" {
		t.Fatalf("app = %q, want oni3 (repeated on every report)", pairs["app"])
	}
	if _, ok := pairs["kind"]; ok {
		t.Fatal("kind must not appear without blocked")
	}
	if got := decodeB64(t, pairs["msg"]); got != "正在处理：hello" {
		t.Fatalf("msg = %q", got)
	}

	// blocked carries a kind; a kind passed with another state is dropped.
	buf.Reset()
	reportStatus("blocked", "permission", "允许执行？")
	pairs = lastReport(t, buf)
	if pairs["kind"] != "permission" {
		t.Fatalf("kind = %q, want permission", pairs["kind"])
	}
	buf.Reset()
	reportStatus("done", "question", "完成")
	pairs = lastReport(t, buf)
	if _, ok := pairs["kind"]; ok {
		t.Fatal("kind must be dropped when state is not blocked")
	}

	// Empty state sends nothing.
	buf.Reset()
	reportStatus("", "", "")
	if buf.Len() != 0 {
		t.Fatalf("empty state wrote %q", buf.String())
	}
}

func TestStatusMessageStripsIllegalText(t *testing.T) {
	if got := statusMessage("line1\nline2"); got != "line1" {
		t.Fatalf("multi-line message = %q, want first line only", got)
	}
	if got := statusMessage("a\x00b\x1bc\x7fd"); got != "abcd" {
		t.Fatalf("control characters not stripped: %q", got)
	}
	if got := statusMessage("  padded  "); got != "padded" {
		t.Fatalf("message not trimmed: %q", got)
	}
	long := statusMessage(strings.Repeat("长", 3000))
	if n := len([]rune(long)); n == 0 || n > 2048 {
		t.Fatalf("long message not capped: %d runes", n)
	}
}

func TestTurnLifecycleReportsStatus(t *testing.T) {
	buf := captureStatus(t)
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// Sending a message reports working.
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.isWaiting {
		t.Fatal("enter did not start a turn")
	}
	pairs := lastReport(t, buf)
	if pairs["state"] != "working" {
		t.Fatalf("state after send = %q, want working", pairs["state"])
	}

	// A completed stream reports done.
	m = drive(m, StreamChunkMsg("答案"))
	m = drive(m, StreamEndMsg{})
	if pairs := lastReport(t, buf); pairs["state"] != "done" {
		t.Fatalf("state after stream end = %q, want done", pairs["state"])
	}

	// A failed turn reports error.
	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("again")})
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drive(m, ErrorMsg("boom"))
	pairs = lastReport(t, buf)
	if pairs["state"] != "error" {
		t.Fatalf("state after error = %q, want error", pairs["state"])
	}
	if got := decodeB64(t, pairs["msg"]); got != "boom" {
		t.Fatalf("error msg = %q", got)
	}
}

func TestQuestionReportsBlocked(t *testing.T) {
	buf := captureStatus(t)
	m := NewViewModel(func(string) error { return nil })
	m = drive(m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// An approval question blocks on permission.
	m = drive(m, AskQuestionMsg{Question: "允许执行？", Options: []string{"允许", "拒绝"}, Approval: true})
	pairs := lastReport(t, buf)
	if pairs["state"] != "blocked" || pairs["kind"] != "permission" {
		t.Fatalf("approval = state %q kind %q, want blocked/permission", pairs["state"], pairs["kind"])
	}

	// Answering goes back to working (the turn is still running).
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	pairs = lastReport(t, buf)
	if pairs["state"] != "working" && pairs["state"] != "idle" {
		t.Fatalf("state after answer = %q, want working or idle", pairs["state"])
	}

	// A plain question blocks on question.
	m = drive(m, AskQuestionMsg{Question: "选哪个？"})
	pairs = lastReport(t, buf)
	if pairs["state"] != "blocked" || pairs["kind"] != "question" {
		t.Fatalf("question = state %q kind %q, want blocked/question", pairs["state"], pairs["kind"])
	}

	// Esc dismisses it and goes back to working.
	m = drive(m, tea.KeyMsg{Type: tea.KeyEsc})
	pairs = lastReport(t, buf)
	if pairs["state"] != "working" && pairs["state"] != "idle" {
		t.Fatalf("state after dismiss = %q, want working or idle", pairs["state"])
	}
}
