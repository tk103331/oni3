package agent

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/mcp"
)

// minInt returns the smaller of a and b.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// openMCPPanel opens the management panel wired to this app's state.
func (app *AgentApp) openMCPPanel() {
	app.model.onMCPDisabled = func() map[string]bool { return app.mcpDisabled }
	app.model.openMCPPanel(config.GetConfig(), app.mcpDisabled, app.setMCPServer)
}

// mcpPanel is the interactive /mcp management panel: one row per configured
// server with its state, tool count and endpoint, plus an action bar on top.
// Space marks a server, Tab switches between the action bar and the list, and
// the actions enable or disable the marked servers. The change is
// runtime-only and the configuration file stays untouched.
type mcpPanel struct {
	cfg    *config.Config
	sel    int // selected server row
	rows   []mcpRow
	picked map[string]bool // servers marked with space
	focus  paneFocus
	action int    // selected entry of the action bar
	notice string // last action result, shown under the title
	// onToggle applies an enable/disable pair and returns its result; it runs
	// on the TUI update goroutine so rebuilds stay ordered.
	onToggle func(server string, enable bool) CommandResult
	// onChanged refreshes the command descriptions after a toggle.
	onChanged func()
}

type mcpRow struct {
	server   string
	disabled bool
	tools    int
	label    string
}

// The action bar entries of the MCP panel.
const (
	mcpActionSelectAll  = "全部选中"
	mcpActionClearAll   = "全部取消"
	mcpActionEnableAll  = "启用全部"
	mcpActionEnableSel  = "启用选中"
	mcpActionDisableAll = "禁用全部"
	mcpActionDisableSel = "禁用选中"
)

func mcpActionNames() []string {
	return []string{
		mcpActionSelectAll,
		mcpActionClearAll,
		mcpActionEnableAll,
		mcpActionEnableSel,
		mcpActionDisableAll,
		mcpActionDisableSel,
	}
}

func (m *ViewModel) openMCPPanel(cfg *config.Config, disabled map[string]bool, onToggle func(string, bool) CommandResult) {
	if m.isWaiting || m.question != nil {
		return
	}
	if cfg == nil || len(cfg.MCPServers) == 0 {
		return
	}
	counts := map[string]int{}
	if manager := mcp.GetGlobalManager(); manager != nil {
		counts = manager.ToolCountsByServer()
	}
	if disabled == nil {
		disabled = map[string]bool{}
	}
	rows := make([]mcpRow, 0, len(cfg.MCPServers))
	for _, name := range sortedKeys(cfg.MCPServers) {
		rows = append(rows, mcpRow{
			server:   name,
			disabled: disabled[name],
			tools:    counts[name],
			label:    mcpServerLabel(name, cfg.MCPServers[name]),
		})
	}
	m.mcpPanelState = &mcpPanel{cfg: cfg, rows: rows, picked: map[string]bool{}, focus: paneFocusList, onToggle: onToggle}
	m.input, m.cursor = "", 0
	m.paletteSel = 0
	m.markDirty()
}

func (m *ViewModel) closeMCPPanel() {
	m.mcpPanelState = nil
	m.input, m.cursor = "", 0
	m.markDirty()
}

// pickedRows returns the marked server rows in list order.
func (p *mcpPanel) pickedRows() []mcpRow {
	rows := make([]mcpRow, 0, len(p.picked))
	for _, r := range p.rows {
		if p.picked[r.server] {
			rows = append(rows, r)
		}
	}
	return rows
}

// mcpRefreshRows re-reads each row's disabled state after a toggle.
func (m *ViewModel) mcpRefreshRows() {
	p := m.mcpPanelState
	if p == nil {
		return
	}
	disabled := m.mcpPanelDisabled()
	for i := range p.rows {
		p.rows[i].disabled = disabled[p.rows[i].server]
	}
	m.markDirty()
}

// mcpPanelDisabled is set by the host so the panel can refresh after toggles.
// It is nil until the host wires it in openMCPPanel.
func (m *ViewModel) mcpPanelDisabled() map[string]bool {
	if m.onMCPDisabled == nil {
		return map[string]bool{}
	}
	return m.onMCPDisabled()
}

func (m *ViewModel) handleMCPPanelKey(msg tea.KeyMsg) tea.Cmd {
	p := m.mcpPanelState
	if p == nil {
		return nil
	}
	actions := mcpActionNames()
	switch msg.Type {
	case tea.KeyEsc:
		m.closeMCPPanel()
		return nil
	case tea.KeyTab, tea.KeyShiftTab:
		if p.focus == paneFocusActions {
			p.focus = paneFocusList
		} else {
			p.focus = paneFocusActions
		}
	case tea.KeyLeft, tea.KeyRight:
		if p.focus == paneFocusActions && len(actions) > 0 {
			delta := 1
			if msg.Type == tea.KeyLeft {
				delta = -1
			}
			p.action = (p.action%len(actions) + delta + len(actions)) % len(actions)
		}
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
		if p.focus == paneFocusActions {
			// Down leaves the action bar and enters the list.
			if msg.Type == tea.KeyDown || msg.Type == tea.KeyPgDown {
				p.focus = paneFocusList
			}
			break
		}
		if len(p.rows) == 0 {
			return nil
		}
		delta := 1
		if msg.Type == tea.KeyUp || msg.Type == tea.KeyPgUp {
			delta = -1
		}
		if msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown {
			delta *= 5
		}
		p.sel = (p.sel + delta + len(p.rows)) % len(p.rows)
	case tea.KeySpace:
		if p.focus == paneFocusActions || len(p.rows) == 0 {
			break
		}
		p.sel = min(max(p.sel, 0), len(p.rows)-1)
		name := p.rows[p.sel].server
		if p.picked == nil {
			p.picked = map[string]bool{}
		}
		if p.picked[name] {
			delete(p.picked, name)
		} else {
			p.picked[name] = true
		}
	case tea.KeyEnter:
		if p.focus == paneFocusActions {
			p.action = min(max(p.action, 0), len(actions)-1)
			m.mcpRunAction(actions[p.action])
			return nil
		}
		// Enter on the list applies and closes the panel.
		m.closeMCPPanel()
		if p.onChanged != nil {
			p.onChanged()
		}
		return nil
	default:
		return nil
	}
	m.markDirty()
	return nil
}

// mcpRunAction executes one entry of the MCP action bar.
func (m *ViewModel) mcpRunAction(action string) {
	p := m.mcpPanelState
	if p == nil {
		return
	}
	p.notice = ""
	switch action {
	case mcpActionSelectAll:
		p.picked = map[string]bool{}
		for _, r := range p.rows {
			p.picked[r.server] = true
		}
	case mcpActionClearAll:
		p.picked = map[string]bool{}
	case mcpActionEnableAll:
		m.mcpApplyTo(p.rows, true)
	case mcpActionDisableAll:
		m.mcpApplyTo(p.rows, false)
	case mcpActionEnableSel:
		m.mcpApplyToPicked(true)
	case mcpActionDisableSel:
		m.mcpApplyToPicked(false)
	}
	m.markDirty()
}

// mcpApplyToPicked applies the target state to the marked servers.
func (m *ViewModel) mcpApplyToPicked(enable bool) {
	p := m.mcpPanelState
	if p == nil {
		return
	}
	rows := p.pickedRows()
	if len(rows) == 0 {
		p.notice = "请先用空格选中 MCP 服务"
		return
	}
	m.mcpApplyTo(rows, enable)
}

// mcpApplyTo enables or disables the given rows, skipping servers already in
// the target state, and reports a single summary instead of one message per
// server.
func (m *ViewModel) mcpApplyTo(rows []mcpRow, enable bool) {
	p := m.mcpPanelState
	if p == nil || p.onToggle == nil {
		return
	}
	verb := "启用"
	if !enable {
		verb = "禁用"
	}
	changed, failed := 0, 0
	for _, r := range rows {
		// disabled is the opposite of the current state: skip servers that are
		// already in the target state.
		if r.disabled != enable {
			continue
		}
		if res := p.onToggle(r.server, enable); res.Error {
			failed++
			continue
		}
		changed++
	}
	switch {
	case failed > 0:
		p.notice = fmt.Sprintf("已%s %d 个服务，%d 个失败", verb, changed, failed)
	case changed > 0:
		p.notice = fmt.Sprintf("已%s %d 个服务", verb, changed)
	default:
		p.notice = "没有需要" + verb + "的服务"
	}
	m.mcpRefreshRows()
}

// mcpPanelLines renders the panel: title, action bar, hint and one row per
// server.
func (m *ViewModel) mcpPanelLines() []string {
	p := m.mcpPanelState
	if p == nil {
		return nil
	}
	width := maxInt(m.width-2, 1)
	title := fmt.Sprintf(" MCP 服务管理（已选 %d 个）", len(p.picked))
	lines := []string{m.primaryStyle(true).Render(truncate(title, width))}
	if p.notice != "" {
		lines = append(lines, m.muted().Render(truncate(" "+p.notice, width)))
	}
	lines = append(lines, m.mcpActionBar(p, width))
	hint := " 空格 选中/取消 · ↑↓ 选择（首尾循环） · Tab 切换操作栏 · Enter 关闭 · Esc 取消（仅本次会话生效）"
	lines = append(lines, m.muted().Render(truncate(hint, width)))
	sel := min(max(p.sel, 0), len(p.rows)-1)
	maxRows := minInt(8, maxInt(m.height-12, 1))
	start := maxInt(sel-maxRows+1, 0)
	end := minInt(start+maxRows, len(p.rows))
	for i := start; i < end; i++ {
		r := p.rows[i]
		marker := "   "
		if i == sel {
			marker = " ❯ "
		}
		box := "[ ]"
		if p.picked[r.server] {
			box = "[x]"
		}
		state := "✓ 启用"
		if r.disabled {
			state = "⊘ 禁用"
		}
		text := fmt.Sprintf("%s %s  %s  %d 工具  %s", box, r.server, state, r.tools, r.label)
		line := truncate(marker+oneLine(ansi.Strip(text)), width)
		if i == sel && p.focus == paneFocusList {
			line = m.accentStyle(true).Render(line)
		} else if r.disabled {
			line = m.muted().Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

// mcpActionBar renders the top actions; the focused one is emphasized while
// Tab owns the bar.
func (m *ViewModel) mcpActionBar(p *mcpPanel, width int) string {
	names := mcpActionNames()
	parts := make([]string, 0, len(names))
	sep := m.muted().Render("│")
	for i, name := range names {
		label := " " + name + " "
		if p.focus == paneFocusActions && i == p.action {
			label = m.accentStyle(true).Render(" ❯ " + name + " ")
		} else {
			label = m.muted().Render(label)
		}
		parts = append(parts, label)
	}
	return truncate(" "+strings.Join(parts, sep), width)
}
