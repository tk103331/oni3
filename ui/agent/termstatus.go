package agent

import (
	"encoding/base64"
	"io"
	"os"
	"strings"
	"sync"
)

// OSC 7501 (Program Status Protocol, Rex terminal) reports what the program is
// doing to the terminal itself: idle, working, blocked on the user, finished
// or failed. Terminals that do not know the sequence ignore it, so reports are
// always safe to send and no feature detection is needed.
//
//	OSC 7501 ; state=working:app=oni3:msg=<base64> ST
//
// A report replaces the terminal's record completely, so app is repeated on
// every report and msg is one base64-encoded UTF-8 line without control
// characters. See https://www.superlogical.com/rex/docs/build/program-status

// termStatusWriter is where status reports are written. Replaceable in tests.
var termStatusWriter = struct {
	sync.Mutex
	w io.Writer
}{w: os.Stdout}

// reportStatus sends one OSC 7501 report for the root record.
//
// state: idle, working, done, error, or blocked. kind is only sent with
// blocked (permission, question or auth) and is ignored otherwise. msg is
// plain UTF-8 text (it is base64-encoded here) and must be a single line;
// control characters and multi-line text are stripped to stay within the
// protocol. An empty state sends nothing.
func reportStatus(state, kind, msg string) {
	state = strings.TrimSpace(state)
	if state == "" {
		return
	}
	if state != "blocked" {
		kind = ""
	}
	var b strings.Builder
	b.WriteString("\x1b]7501;state=")
	b.WriteString(state)
	if kind != "" {
		b.WriteString(":kind=")
		b.WriteString(kind)
	}
	b.WriteString(":app=oni3")
	if text := statusMessage(msg); text != "" {
		b.WriteString(":msg=")
		b.WriteString(base64.StdEncoding.EncodeToString([]byte(text)))
	}
	b.WriteString("\x1b\\")
	termStatusWriter.Lock()
	defer termStatusWriter.Unlock()
	if termStatusWriter.w != nil {
		// The sequence occupies no grid cells, so writing it between frames
		// cannot corrupt the layout. A write error (closed pipe) is not the
		// program's business: the terminal is going away.
		_, _ = io.WriteString(termStatusWriter.w, b.String())
	}
}

// statusMessage reduces free text to the single line the protocol allows:
// the first line, control characters removed, and capped well under the
// 2048-byte decoded limit.
func statusMessage(msg string) string {
	if i := strings.IndexAny(msg, "\n\r"); i >= 0 {
		msg = msg[:i]
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(msg) {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		if b.Len()+4 >= 2048 {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
