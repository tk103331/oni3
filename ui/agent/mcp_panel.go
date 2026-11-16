package agent

import (
	"fmt"

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
// server with its state, tool count and endpoint, plus "enable all" and
// "disable all" rows. Space toggles the selected server for the current
// session; Enter applies and closes. The change is runtime-only and the
// configuration file stays untouched.
type mcpPanel struct {
	cfg  *config.Config
	sel  int // selected row (server list, then the summary rows)
	rows []mcpRow
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

// summary rows appended after the server rows.
const (
	mcpRowEnableAll  = "‹全部启用›"
	mcpRowDisableAll = "‹全部禁用›"
)

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
	m.mcpPanelState = &mcpPanel{cfg: cfg, rows: rows, onToggle: onToggle}
	m.input, m.cursor = "", 0
	m.paletteSel = 0
	m.markDirty()
}

func (m *ViewModel) closeMCPPanel() {
	m.mcpPanelState = nil
	m.input, m.cursor = "", 0
	m.markDirty()
}

// panelRows returns the server rows followed by the enable-all/disable-all rows.
func (p *mcpPanel) panelRows() []mcpRow {
	rows := make([]mcpRow, len(p.rows))
	copy(rows, p.rows)
	return append(rows,
		mcpRow{server: mcpRowEnableAll, label: mcpRowEnableAll},
		mcpRow{server: mcpRowDisableAll, label: mcpRowDisableAll},
	)
}

// toggleSelected flips the selected server's enabled state and keeps the
// panel open so several servers can be toggled in a row.
func (m *ViewModel) mcpToggleSelected() {
	p := m.mcpPanelState
	if p == nil || p.onToggle == nil {
		return
	}
	rows := p.panelRows()
	sel := min(max(p.sel, 0), len(rows)-1)
	row := rows[sel]
	switch row.server {
	case mcpRowEnableAll:
		for _, r := range p.rows {
			if r.disabled {
				p.onToggle(r.server, true)
			}
		}
	case mcpRowDisableAll:
		for _, r := range p.rows {
			if !r.disabled {
				p.onToggle(r.server, false)
			}
		}
	default:
		// Toggle: an enabled server turns off, a disabled one turns on.
		res := p.onToggle(row.server, row.disabled)
		m.applyCommandResult(res)
		// Refresh the row state from the host's disabled set.
		m.mcpRefreshRows()
	}
}

// mcpRefreshRows re-reads each row's disabled state after a toggle.
func (m *ViewModel) mcpRefreshRows() {
	p := m.mcpPanelState
	if p == nil {
		return
	}
	for i := range p.rows {
		p.rows[i].disabled = m.mcpPanelDisabled()[p.rows[i].server]
	}
	m.markDirty()
}

// mcpPanelDisabled is set by the host so the panel can refresh after toggles.
// It is nil until the host wires it in openMCPPanelHost.
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
	rows := p.panelRows()
	switch msg.Type {
	case tea.KeyEsc:
		m.closeMCPPanel()
		return nil
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
		if len(rows) == 0 {
			return nil
		}
		delta := 1
		if msg.Type == tea.KeyUp || msg.Type == tea.KeyPgUp {
			delta = -1
		}
		if msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown {
			delta *= 5
		}
		p.sel = (p.sel + delta + len(rows)) % len(rows)
	case tea.KeySpace:
		m.mcpToggleSelected()
	case tea.KeyEnter:
		// Enter applies the current state and closes the panel.
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

// mcpPanelLines renders the panel: title, one row per server, hint line.
func (m *ViewModel) mcpPanelLines() []string {
	p := m.mcpPanelState
	if p == nil {
		return nil
	}
	width := maxInt(m.width-2, 1)
	lines := []string{m.primaryStyle(true).Render(truncate(" MCP 服务 · 空格切换启用 · Enter 关闭", width))}
	rows := p.panelRows()
	sel := min(max(p.sel, 0), len(rows)-1)
	maxRows := minInt(8, maxInt(m.height-10, 1))
	start := maxInt(sel-maxRows+1, 0)
	end := minInt(start+maxRows, len(rows))
	for i := start; i < end; i++ {
		r := rows[i]
		marker := "   "
		if i == sel {
			marker = " ❯ "
		}
		text := r.server
		if r.server == mcpRowEnableAll || r.server == mcpRowDisableAll {
			text = r.label
		} else {
			state := "✓ 启用"
			if r.disabled {
				state = "⊘ 禁用"
			}
			text = fmt.Sprintf("%s  %s  %d 工具  %s", r.server, state, r.tools, r.label)
		}
		line := truncate(marker+oneLine(ansi.Strip(text)), width)
		if i == sel {
			line = m.accentStyle(true).Render(line)
		} else if r.disabled && r.server != mcpRowEnableAll && r.server != mcpRowDisableAll {
			line = m.muted().Render(line)
		}
		lines = append(lines, line)
	}
	hint := " ↑↓ 选择（首尾循环） · 空格 启用/禁用 · Enter 关闭 · Esc 取消（仅本次会话生效）"
	lines = append(lines, m.muted().Render(truncate(hint, width)))
	return lines
}
