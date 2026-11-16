package agent

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SlashCommand describes a command available in the TUI command palette.
type SlashCommand struct {
	Name        string   // command name without the leading slash
	Args        string   // argument hint, e.g. "<name>" (optional)
	Description string   // short description shown in the palette
	Values      []string // optional argument suggestions
	// ValueLabels optionally annotates each Values entry (same order and
	// length). The label is appended after the value in the palette.
	ValueLabels []string
	// KeepArgs keeps a chosen value in the input box instead of executing,
	// so the user can continue typing (e.g. "/skill coder " + a message).
	KeepArgs bool
}

// CommandResult is returned by a command handler.
type CommandResult struct {
	Message  string  // transcript message (empty = nothing to show)
	Error    bool    // render the message as an error
	Subtitle string  // new header subtitle (empty = unchanged)
	Cmd      tea.Cmd // asynchronous work started by the command
}

// SlashHandler executes a command line without the leading slash.
type SlashHandler func(input string) CommandResult

// SetSlashCommands registers host-provided commands and their handler.
// Commands are shown in the palette while typing "/" and executed on enter.
func (m *ViewModel) SetSlashCommands(cmds []SlashCommand, handler SlashHandler) {
	m.slashCommands = append([]SlashCommand{}, cmds...)
	m.onSlash = handler
}

// builtinCommands returns commands handled inside the view model.
func builtinCommands() []SlashCommand {
	return []SlashCommand{
		{Name: "help", Description: "显示可用命令"},
		{Name: "clear", Description: "清空当前对话"},
	}
}

// allCommands merges builtin commands with host-registered ones.
func (m *ViewModel) allCommands() []SlashCommand {
	return append(builtinCommands(), m.slashCommands...)
}

// executeSlashCommand runs a slash command typed into the input box.
func (m *ViewModel) executeSlashCommand(line string) tea.Cmd {
	body := strings.TrimSpace(strings.TrimPrefix(line, "/"))
	if body == "" {
		m.appendCommandMessage(m.helpText(), false)
		return nil
	}

	name := body
	if i := strings.IndexAny(body, " \t"); i >= 0 {
		name = body[:i]
	}

	switch strings.ToLower(name) {
	case "clear":
		m.clearConversation()
		return nil
	case "help":
		m.appendCommandMessage(m.helpText(), false)
		return nil
	}

	if m.onSlash == nil {
		m.appendCommandMessage("未知命令：/"+name+"（输入 /help 查看可用命令）", true)
		return nil
	}

	res := m.onSlash(body)
	m.applyCommandResult(res)
	return res.Cmd
}

func (m *ViewModel) applyCommandResult(res CommandResult) {
	if res.Subtitle != "" {
		m.SetSubtitle(res.Subtitle)
	}
	if res.Message != "" {
		m.appendCommandMessage(res.Message, res.Error)
	}
}

// appendCommandMessage appends a command result to the transcript.
func (m *ViewModel) appendCommandMessage(text string, isErr bool) {
	msgType := InfoMessage
	if isErr {
		msgType = ErrorMessage
	}
	m.messages = append(m.messages, Message{
		Type:    msgType,
		Content: text,
	})
	m.scrollOffset = 0
	m.markDirty()
}

// helpText renders the command list for /help.
func (m *ViewModel) helpText() string {
	var b strings.Builder
	b.WriteString("可用命令：")
	for _, c := range m.allCommands() {
		b.WriteString("\n  /" + c.Name)
		if c.Args != "" {
			b.WriteString(" " + c.Args)
		}
		b.WriteString("   " + c.Description)
	}
	b.WriteString("\n\n提示：输入 / 时上方会显示候选命令，Tab/↑↓ 用于滚动对话。")
	return b.String()
}

// containsFold reports whether list contains s, ignoring case and spaces.
func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(s)) {
			return true
		}
	}
	return false
}

// paletteItem is one selectable row in the slash command palette.
type paletteItem struct {
	line     string // rendered row (without the selection marker)
	fill     string // input text applied when chosen (includes leading "/")
	exec     bool   // execute immediately instead of filling the input
	complete bool   // the argument is complete: keep it plus a trailing space
}

// paletteActive reports whether the palette should be visible at all.
func (m *ViewModel) paletteActive() bool {
	raw := strings.TrimLeft(m.input, " \t")
	return !m.isWaiting && m.provider == nil && strings.HasPrefix(raw, "/")
}

// paletteItems returns the selectable rows for the current input.
func (m *ViewModel) paletteItems() []paletteItem {
	if !m.paletteActive() {
		return nil
	}
	raw := strings.TrimLeft(m.input, " \t")
	body := strings.TrimPrefix(raw, "/")

	// Still typing the command name: filter commands by prefix.
	if !strings.ContainsAny(body, " \t") {
		prefix := strings.TrimSpace(body)
		var items []paletteItem
		for _, c := range m.allCommands() {
			if prefix != "" && !strings.HasPrefix(strings.ToLower(c.Name), strings.ToLower(prefix)) {
				continue
			}
			fill := "/" + c.Name
			exec := true
			if c.Args != "" && !strings.HasPrefix(c.Args, "[") {
				fill += " "
				exec = false // continue typing the argument
			}
			items = append(items, paletteItem{line: m.formatCommandHint(c), fill: fill, exec: exec})
		}
		return items
	}

	// Command chosen, suggesting argument values (one selectable row each).
	fields := strings.SplitN(body, " ", 2)
	name := fields[0]
	argPrefix := strings.TrimSpace(fields[1])
	for _, c := range m.allCommands() {
		if !strings.EqualFold(c.Name, name) || len(c.Values) == 0 {
			continue
		}
		// A kept argument (KeepArgs) is already complete; typing continues as
		// a message, so the palette must not offer the same values again.
		if c.KeepArgs && containsFold(c.Values, argPrefix) {
			return nil
		}
		var items []paletteItem
		for i, v := range c.Values {
			if argPrefix != "" && !strings.HasPrefix(strings.ToLower(v), strings.ToLower(argPrefix)) {
				continue
			}
			label := ""
			if i < len(c.ValueLabels) {
				label = c.ValueLabels[i]
			}
			items = append(items, paletteItem{
				line:     m.formatValueRow(c.Name, v, label),
				fill:     "/" + c.Name + " " + v,
				exec:     !c.KeepArgs,
				complete: c.KeepArgs,
			})
		}
		return items
	}
	return nil
}

// formatValueRow renders one argument candidate: the command name and the
// value are emphasized (accent + bold), the annotation after it is muted.
func (m *ViewModel) formatValueRow(command, value, label string) string {
	head := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Accent)).Bold(true).Render("/" + command)
	valueStyled := lipgloss.NewStyle().Bold(true).Render(value)
	width := maxInt(m.width-6, 10)
	// The visible row is "  →  value  label"; emphasis stays on the value.
	row := head + m.muted().Render("  →  ") + valueStyled
	if strings.TrimSpace(label) != "" {
		visible := lipgloss.Width(head) + 5 + lipgloss.Width(valueStyled) + 2
		row += m.muted().Render("  " + truncate(label, maxInt(width-visible, 0)))
	}
	return row
}

// paletteUsage returns the usage line of the command currently being typed,
// used when the command has no argument candidates to list.
func (m *ViewModel) paletteUsage() string {
	raw := strings.TrimLeft(m.input, " \t")
	body := strings.TrimPrefix(raw, "/")
	if !strings.ContainsAny(body, " \t") {
		return ""
	}
	fields := strings.SplitN(body, " ", 2)
	name := fields[0]
	for _, c := range m.allCommands() {
		if !strings.EqualFold(c.Name, name) {
			continue
		}
		usage := "/" + c.Name
		if c.Args != "" {
			usage += " " + c.Args
		}
		return usage + "   " + c.Description
	}
	return ""
}

// hasPalette reports whether there are selectable palette rows right now.
func (m *ViewModel) hasPalette() bool {
	return m.paletteActive() && len(m.paletteItems()) > 0
}

// movePalette moves the palette selection, wrapping around at both ends.
func (m *ViewModel) movePalette(delta int) {
	items := m.paletteItems()
	if len(items) == 0 {
		return
	}
	m.paletteSel = ((m.paletteSel+delta)%len(items) + len(items)) % len(items)
	m.markDirty()
}

// selectedPaletteItem returns the current selection, if any.
func (m *ViewModel) selectedPaletteItem() (paletteItem, bool) {
	items := m.paletteItems()
	if len(items) == 0 {
		return paletteItem{}, false
	}
	sel := m.paletteSel
	if sel < 0 {
		sel = 0
	}
	if sel >= len(items) {
		sel = len(items) - 1
	}
	return items[sel], true
}

// slashHints builds the palette lines shown while the input starts with "/".
func (m *ViewModel) slashHints() []string {
	if !m.paletteActive() {
		return nil
	}
	items := m.paletteItems()
	if len(items) == 0 {
		if usage := m.paletteUsage(); usage != "" {
			return []string{m.muted().Render("  " + usage)}
		}
		return []string{m.muted().Render("  未知命令，输入 /help 查看可用命令")}
	}
	sel := m.paletteSel
	if sel < 0 {
		sel = 0
	}
	if sel >= len(items) {
		sel = len(items) - 1
	}
	cursor := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Accent)).Bold(true).Render("❯ ")
	out := make([]string, 0, len(items))
	for i, it := range items {
		marker := "  "
		if i == sel {
			marker = cursor
		}
		out = append(out, marker+it.line)
	}
	return out
}

// formatCommandHint renders one command row in the palette.
func (m *ViewModel) formatCommandHint(c SlashCommand) string {
	name := "/" + c.Name
	if c.Args != "" && m.width >= 80 {
		name += " " + c.Args
	}
	column := min(32, maxInt(m.width/2, 1))
	name = truncate(name, column)
	nameStyled := lipgloss.NewStyle().Bold(true).Render(name)
	gap := strings.Repeat(" ", maxInt(column-lipgloss.Width(name)+2, 2))
	return nameStyled + gap + m.muted().Render(truncate(c.Description, maxInt(m.width-column-4, 0)))
}
