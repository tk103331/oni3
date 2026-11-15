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

// mcpPanel is the interactive /mcp management panel: one row per configured
// server with its state, tool count and endpoint, plus an "enable all" row.
// Enter toggles the selected server for the current session; the change is
// runtime-only and the configuration file stays untouched.
type mcpPanel struct {
	cfg  *config.Config
	sel  int // selected row (server list, then the enable-all row)
	rows []mcpRow
	// onToggle applies an enable/disable pair and returns its result; it runs
	// on the TUI update goroutine so rebuilds stay ordered.
	onToggle func(server string, enable bool) CommandResult
}

type mcpRow struct {
	server   string
	disabled bool
	tools    int
	label    string
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

// panelRows returns the server rows followed by the enable-all row.
func (p *mcpPanel) panelRows() []mcpRow {
	rows := make([]mcpRow, len(p.rows))
	copy(rows, p.rows)
	// An extra synthetic row enables every disabled server at once.
	allEnabled := true
	for _, r := range p.rows {
		if r.disabled {
			allEnabled = false
			break
		}
	}
	if !allEnabled {
		rows = append(rows, mcpRow{server: "", label: "全部启用"})
	}
	return rows
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
	case tea.KeyEnter:
		if len(rows) == 0 {
			return nil
		}
		p.sel = min(max(p.sel, 0), len(rows)-1)
		row := rows[p.sel]
		if row.server == "" {
			// The enable-all row.
			m.closeMCPPanel()
			for _, r := range p.rows {
				if r.disabled && p.onToggle != nil {
					p.onToggle(r.server, true)
				}
			}
			return nil
		}
		enable := row.disabled
		m.closeMCPPanel()
		if p.onToggle != nil {
			m.applyCommandResult(p.onToggle(row.server, enable))
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
	lines := []string{m.primaryStyle(true).Render(truncate(" MCP 服务 · Enter 切换启用状态", width))}
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
		if r.server == "" {
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
		} else if r.disabled && r.server != "" {
			line = m.muted().Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, m.muted().Render(truncate(" ↑↓ 选择 · Enter 启用/禁用 · Esc 关闭（仅本次会话生效）", width)))
	return lines
}
