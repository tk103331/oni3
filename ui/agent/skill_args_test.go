package agent

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/tk103331/oni3/config"
)

// addCoderSkill registers a skill on the global config used by rebuildAgent.
func addCoderSkill(t *testing.T) {
	t.Helper()
	cfg := config.GetConfig()
	if cfg == nil {
		t.Fatal("config not loaded")
	}
	if cfg.Skills == nil {
		cfg.Skills = map[string]config.Skill{}
	}
	cfg.Skills["coder"] = config.Skill{Description: "编码", System: "You write code."}
}

// recordSends replaces the app's send callback and returns the recorded input.
func recordSends(app *AgentApp) *[]string {
	sent := &[]string{}
	app.model.onSendMsg = func(text string) error {
		*sent = append(*sent, text)
		return nil
	}
	return sent
}

// runCmd executes a command the way the event loop does, expanding batches so
// the send callback actually runs.
func runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return
	}
	for _, inner := range batch {
		if inner != nil {
			inner()
		}
	}
}

// lastUserMessage returns the content of the newest user bubble.
func lastUserMessage(m *ViewModel) string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Type == UserMessage {
			return m.messages[i].Content
		}
	}
	return ""
}

// TestSkillColonExecutesWithMessage covers the shorthand: /skill:coder <消息>
// selects the skill and starts that turn in one command.
func TestSkillColonExecutesWithMessage(t *testing.T) {
	app, _ := mcpTestApp(t, nil)
	addCoderSkill(t)
	sent := recordSends(app)

	res := app.executeSlash("skill:coder 修复  登录 bug")
	if res.Error {
		t.Fatalf("skill execution failed: %s", res.Message)
	}
	if app.skill != "coder" {
		t.Fatalf("skill not selected: %q", app.skill)
	}
	if res.Cmd == nil {
		t.Fatal("no turn started")
	}
	if got := lastUserMessage(app.model); got != "修复  登录 bug" {
		t.Fatalf("user message = %q (inner spacing must survive)", got)
	}
	if !app.model.isWaiting {
		t.Fatal("turn did not start")
	}
	if got := ansi.Strip(app.model.View()); !strings.Contains(got, "修复  登录 bug") {
		t.Fatalf("message missing from the transcript:\n%s", got)
	}

	runCmd(res.Cmd)
	if len(*sent) != 1 || (*sent)[0] != "修复  登录 bug" {
		t.Fatalf("sent = %v", *sent)
	}
}

// TestSkillSpaceFormStillWorks keeps the space-separated spelling working.
func TestSkillSpaceFormStillWorks(t *testing.T) {
	app, _ := mcpTestApp(t, nil)
	addCoderSkill(t)
	sent := recordSends(app)

	res := app.executeSlash("skill coder 检查这个仓库")
	if res.Error || res.Cmd == nil {
		t.Fatalf("space form failed: %+v", res)
	}
	runCmd(res.Cmd)
	if len(*sent) != 1 || (*sent)[0] != "检查这个仓库" {
		t.Fatalf("sent = %v", *sent)
	}
}

// TestSkillWithoutMessageOnlySelects keeps the old behaviour: no message means
// select, and the next typed message uses the skill.
func TestSkillWithoutMessageOnlySelects(t *testing.T) {
	app, _ := mcpTestApp(t, nil)
	addCoderSkill(t)
	sent := recordSends(app)

	res := app.executeSlash("skill:coder")
	if res.Error {
		t.Fatalf("selection failed: %s", res.Message)
	}
	if res.Cmd != nil {
		t.Fatal("selection alone must not start a turn")
	}
	if app.skill != "coder" || app.model.isWaiting {
		t.Fatalf("unexpected state: skill=%q waiting=%v", app.skill, app.model.isWaiting)
	}
	if got := lastUserMessage(app.model); got != "" {
		t.Fatalf("a user message was added: %q", got)
	}
	if len(*sent) != 0 {
		t.Fatalf("sent = %v", *sent)
	}
}

// TestSkillExecutionRejectsBadInput keeps failures side-effect free.
func TestSkillExecutionRejectsBadInput(t *testing.T) {
	app, _ := mcpTestApp(t, nil)
	addCoderSkill(t)
	sent := recordSends(app)

	if res := app.executeSlash("skill:missing 做事"); !res.Error || !strings.Contains(res.Message, "不存在") {
		t.Fatalf("unknown skill accepted: %+v", res)
	}
	if res := app.executeSlash("skill:off 做事"); !res.Error || !strings.Contains(res.Message, "用法") {
		t.Fatalf("off with a message accepted: %+v", res)
	}
	if app.skill != "" || app.model.isWaiting || lastUserMessage(app.model) != "" || len(*sent) != 0 {
		t.Fatalf("rejected command changed state: skill=%q sent=%v", app.skill, *sent)
	}

	// "/skill:off" cancels too: the colon form is not special-cased away.
	if res := app.executeSlash("skill:coder"); res.Error {
		t.Fatalf("colon selection failed: %s", res.Message)
	}
	if res := app.executeSlash("skill:off"); res.Error || app.skill != "" {
		t.Fatalf("colon off failed: %+v (skill=%q)", res, app.skill)
	}

	// A turn in flight blocks a new one instead of racing it.
	app.model.isWaiting = true
	if res := app.executeSlash("skill:coder 做事"); !res.Error || !strings.Contains(res.Message, "进行中") {
		t.Fatalf("busy state accepted: %+v", res)
	}
	if app.skill != "" || len(*sent) != 0 {
		t.Fatalf("busy rejection changed state: skill=%q sent=%v", app.skill, *sent)
	}
}

// TestSkillColonPaletteCompletion checks the command palette understands the
// colon form: it offers skill values, keeps the typed separator, and stops
// offering values once the message begins.
func TestSkillColonPaletteCompletion(t *testing.T) {
	sent := []string{}
	m := NewViewModel(func(text string) error {
		sent = append(sent, text)
		return nil
	})
	m = drive(m, tea.WindowSizeMsg{Width: 110, Height: 30})
	m.SetSlashCommands([]SlashCommand{
		{Name: "skill", Args: "<name> [消息]", Description: "选择 skill", Values: []string{"off", "coder"}, KeepArgs: true},
	}, func(input string) CommandResult {
		sent = append(sent, "cmd:"+input)
		return CommandResult{}
	})

	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/skill:cod")})
	if items := m.paletteItems(); len(items) != 1 || items[0].fill != "/skill:coder" {
		t.Fatalf("colon form not completed with the typed separator: %+v", items)
	}

	// Accepting the candidate must leave a trailing space for the message.
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.input != "/skill:coder " {
		t.Fatalf("input after completion = %q", m.input)
	}
	if m.hasPalette() {
		t.Fatal("palette reopened after a complete skill value")
	}

	m = drive(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("修复登录")})
	if m.hasPalette() {
		t.Fatalf("palette offers skill values during the message:\n%s", m.View())
	}
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(sent) != 1 || sent[0] != "cmd:skill:coder 修复登录" {
		t.Fatalf("handler got %v", sent)
	}
	if m.isWaiting {
		t.Fatal("a written handler result must not start a turn by itself")
	}
}
