package agent

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
	"github.com/tk103331/oni3/mcp"
	"github.com/tk103331/oni3/session"
	"github.com/tk103331/oni3/state"
	"github.com/tk103331/oni3/tools"
)

// sortedKeys returns the sorted keys of a string-keyed map.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// themeModeName renders the active background mode for the status line.
func themeModeName(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

// ---- AgentApp slash commands ----

// effectiveAgentConfig applies runtime overrides (model / skill) on top of the
// configured agent.
func (app *AgentApp) effectiveAgentConfig() (config.Agent, bool) {
	cfg := config.GetConfig()
	if cfg == nil {
		return config.Agent{}, false
	}
	ac, ok := cfg.Agents[app.agentName]
	if !ok {
		return config.Agent{}, false
	}
	if app.modelOverride != "" {
		ac.Model = app.modelOverride
	} else if ac.Model == "" {
		// An agent without its own model follows the default model.
		ac.Model = cfg.DefaultModelName()
	}
	if app.skill != "" {
		if sk, ok := cfg.Skills[app.skill]; ok {
			if s := strings.TrimSpace(sk.System); s != "" {
				if strings.TrimSpace(ac.System) != "" {
					ac.System = s + "\n\n" + ac.System
				} else {
					ac.System = s
				}
			}
			if len(sk.Tools) > 0 {
				ac.Tools = append(append([]string{}, ac.Tools...), sk.Tools...)
			}
		}
	}
	// Interactive sessions always expose the ask/task tools; the TUI
	// implements their host side.
	ac.Tools = appendUnique(ac.Tools, tools.AskUserToolName, tools.TodoListToolName)
	// MCP servers disabled at runtime are dropped from the effective config
	// so the rebuilt agent no longer mounts their tools.
	if len(app.mcpDisabled) > 0 {
		kept := make([]string, 0, len(ac.MCPServers))
		for _, s := range ac.MCPServers {
			if !app.mcpDisabled[s] {
				kept = append(kept, s)
			}
		}
		ac.MCPServers = kept
	}
	return ac, true
}

// rememberedModel returns the model remembered for an agent (see /model).
// It only applies to agents that follow the default model: an agent with its
// own model pinned in the config keeps it, so the config always wins there.
func rememberedModel(cfg *config.Config, agentName string) string {
	if cfg == nil || !cfg.AgentUsesDefaultModel(agentName) {
		return ""
	}
	model := state.RememberedModel(agentName)
	if model == "" {
		return ""
	}
	if _, ok := cfg.Models[model]; !ok {
		return ""
	}
	if ac := cfg.Agents[agentName]; model == ac.Model {
		return ""
	}
	return model
}

// rememberModel persists a runtime model choice for agents that follow the
// default model. Choosing the configured model again clears the override.
func rememberModel(cfg *config.Config, agentName, model string) {
	if cfg == nil || !cfg.AgentUsesDefaultModel(agentName) {
		return
	}
	if ac := cfg.Agents[agentName]; model == ac.Model {
		model = ""
	}
	if err := state.SetRememberedModel(agentName, model); err != nil {
		logger.Warn("UI-AGENT", fmt.Sprintf("failed to remember model for %s: %v", agentName, err))
	}
}

// rebuildAgent recreates the underlying Eino agent with current overrides.
func (app *AgentApp) rebuildAgent() error {
	ac, ok := app.effectiveAgentConfig()
	if !ok {
		return fmt.Errorf("agent configuration does not exist: %s", app.agentName)
	}
	ra := agent.NewReactAgent(app.agentName, &ac)
	if err := ra.Init(); err != nil {
		return err
	}
	app.agent = ra
	return nil
}

// selectModel commits the override only when the replacement agent initializes.
func (app *AgentApp) selectModel(name string) CommandResult {
	cfg := config.GetConfig()
	previous := app.modelOverride
	app.modelOverride = name
	if err := app.rebuildAgent(); err != nil {
		app.modelOverride = previous
		return CommandResult{Message: fmt.Sprintf("切换模型失败: %v", err), Error: true}
	}
	rememberModel(cfg, app.agentName, name)
	app.syncSessionConfig()
	app.registerCommands()
	msg := "已切换模型: " + name
	if cfg.AgentUsesDefaultModel(app.agentName) {
		msg += "（已记住，下次启动沿用）"
	}
	return CommandResult{Message: msg, Subtitle: app.headerSubtitle()}
}

func (app *AgentApp) currentModelName() string {
	if app.modelOverride != "" {
		return app.modelOverride
	}
	if cfg := config.GetConfig(); cfg != nil {
		if ac, ok := cfg.Agents[app.agentName]; ok {
			if ac.Model != "" {
				return ac.Model
			}
			return cfg.DefaultModelName()
		}
	}
	return ""
}

func (app *AgentApp) skillName() string {
	if app.skill == "" {
		return "off"
	}
	return app.skill
}

// slashCommands describes the commands available in agent mode.
func (app *AgentApp) slashCommands() []SlashCommand {
	cfg := config.GetConfig()
	if cfg == nil {
		return nil
	}
	skillValues := append([]string{"off"}, sortedKeys(cfg.Skills)...)
	mcpDesc := "管理 MCP 服务"
	if len(cfg.MCPServers) > 0 {
		mcpDesc = fmt.Sprintf("管理 MCP 服务（当前启用 %d/%d）", len(cfg.MCPServers)-len(app.mcpDisabled), len(cfg.MCPServers))
	}
	return []SlashCommand{
		{Name: "agent", Args: "<name>", Description: fmt.Sprintf("切换 agent（当前: %s）", app.agentName), Values: sortedKeys(cfg.Agents), ValueLabels: agentValueLabels(cfg)},
		{Name: "model", Args: "<name>", Description: fmt.Sprintf("切换模型（当前: %s）", app.currentModelName()), Values: sortedKeys(cfg.Models), ValueLabels: modelValueLabels(cfg)},
		{Name: "provider", Args: "[name]", Description: "选择 provider 并添加模型", Values: sortedKeys(cfg.Providers), ValueLabels: providerValueLabels(cfg)},
		{Name: "skill", Args: "<name> [消息]", Description: fmt.Sprintf("选择 skill，可附带一条消息直接执行（/skill:coder 也可以；当前: %s）", app.skillName()), Values: skillValues, ValueLabels: skillValueLabels(cfg), KeepArgs: true},
		{Name: "mcp", Args: "[enable|disable] [name]", Description: mcpDesc},
		{Name: "theme", Args: "<name> [light|dark]", Description: fmt.Sprintf("切换颜色主题（当前: %s/%s）", app.model.ThemeName(), themeModeName(app.model.DarkMode())), Values: themeNames(), ValueLabels: app.themeValueLabels()},
		{Name: "tools", Description: "列出当前可用工具"},
		{Name: "status", Description: "显示当前会话状态"},
		{Name: "resume", Args: "<编号|id>", Description: "恢复历史会话（不带参数列出会话）", Values: recentSessionIDs(session.KindAgent, 10), ValueLabels: resumeValueLabels(session.KindAgent, 10)},
	}
}

// remainderAfter drops the first n whitespace-separated words from line and
// returns the rest unchanged (trimmed). Commands that take a free-form
// message as their trailing argument use it so the message keeps its inner
// spacing instead of being split into fields.
func remainderAfter(line string, n int) string {
	rest := strings.TrimSpace(line)
	for i := 0; i < n; i++ {
		rest = strings.TrimLeft(rest, " \t")
		cut := strings.IndexAny(rest, " \t")
		if cut < 0 {
			return ""
		}
		rest = rest[cut:]
	}
	return strings.TrimSpace(rest)
}

// sortedKeysWithLabels returns the sorted keys of a map and their labels.
func sortedKeysWithLabels[V any](m map[string]V, label func(string, V) string) ([]string, []string) {
	keys := sortedKeys(m)
	labels := make([]string, len(keys))
	for i, k := range keys {
		labels[i] = label(k, m[k])
	}
	return keys, labels
}

// mcpServerLabel renders the palette label of an MCP server: its configured
// description, or a summary of the endpoint when none is set. stdio servers
// show "cmd args…"; HTTP servers show the URL without query parameters.
func mcpServerLabel(name string, server config.MCPServer) string {
	if d := strings.TrimSpace(server.Description); d != "" {
		return d
	}
	return mcpEndpointLabel(server)
}

// mcpEndpointLabel summarizes how to reach a server.
func mcpEndpointLabel(server config.MCPServer) string {
	switch server.Type {
	case config.MCPTypeStdio:
		if server.Cmd != "" {
			parts := append([]string{server.Cmd}, server.Args...)
			return strings.Join(parts, " ")
		}
	case config.MCPTypeSSE, config.MCPTypeStreamableHTTP:
		if server.URL != "" {
			return stripURLQuery(server.URL)
		}
	}
	return server.Type
}

// stripURLQuery drops the query string and fragment from a URL for display.
func stripURLQuery(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// agentValueLabels annotates the /agent candidates with the agent's
// description (or model, so agents are told apart even without one).
func agentValueLabels(cfg *config.Config) []string {
	names := sortedKeys(cfg.Agents)
	labels := make([]string, len(names))
	for i, name := range names {
		a := cfg.Agents[name]
		if d := strings.TrimSpace(a.Description); d != "" {
			labels[i] = d
			continue
		}
		if a.Model != "" {
			labels[i] = "model: " + a.Model
		}
	}
	return labels
}

// modelValueLabels annotates the /model candidates with "provider.model-id".
func modelValueLabels(cfg *config.Config) []string {
	names := sortedKeys(cfg.Models)
	labels := make([]string, len(names))
	for i, name := range names {
		m := cfg.Models[name]
		label := m.Model
		if m.Provider != "" {
			label = m.Provider + " · " + m.Model
		}
		labels[i] = label
	}
	return labels
}

// providerValueLabels annotates the /provider candidates with their base URL.
func providerValueLabels(cfg *config.Config) []string {
	names := sortedKeys(cfg.Providers)
	labels := make([]string, len(names))
	for i, name := range names {
		p := cfg.Providers[name]
		labels[i] = p.BaseURL
	}
	return labels
}

// skillValueLabels annotates the /skill candidates with the skill description
// from the configuration or its markdown file.
func skillValueLabels(cfg *config.Config) []string {
	values := append([]string{"off"}, sortedKeys(cfg.Skills)...)
	labels := make([]string, len(values))
	for i, name := range values {
		if name == "off" {
			labels[i] = "不使用 skill"
			continue
		}
		if s, ok := cfg.Skills[name]; ok && strings.TrimSpace(s.Description) != "" {
			labels[i] = s.Description
		}
	}
	return labels
}

// resumeValueLabels annotates the /resume candidates with the session's
// title summary and update time.
func resumeValueLabels(kind string, limit int) []string {
	sessions, err := session.List(kind, limit)
	if err != nil {
		return nil
	}
	labels := make([]string, len(sessions))
	for i, s := range sessions {
		label := s.Updated.Local().Format("01-02 15:04")
		if msg := s.LastUserMessage(); msg != "" {
			label += "  " + firstLine(msg)
		} else if s.Agent != "" {
			label += "  agent " + s.Agent
		}
		labels[i] = label
	}
	return labels
}

// themeValueLabels annotates the /theme candidates with the theme's color
// swatches: colored blocks of the current mode's palette.
func (app *AgentApp) themeValueLabels() []string {
	names := themeNames()
	labels := make([]string, len(names))
	dark := app.model.DarkMode()
	for i, name := range names {
		t, ok := lookupTheme(name)
		if !ok {
			continue
		}
		v := t.Light
		if dark {
			v = t.Dark
		}
		block := lipgloss.NewStyle().Foreground(lipgloss.Color(v.Accent)).Render("●") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(v.Primary)).Render("●") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(v.Success)).Render("●") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(v.Warning)).Render("●") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(v.Error)).Render("●")
		if d := strings.TrimSpace(t.Description); d != "" {
			block += " " + d
		}
		labels[i] = block
	}
	return labels
}

// executeSlash handles agent-mode commands.
func (app *AgentApp) executeSlash(input string) CommandResult {
	// "/skill:coder xxx" is the same command line as "/skill coder xxx";
	// normalizing here keeps every handler on whitespace splitting.
	if command, rest, sep := slashSplit(input); sep == ":" {
		input = strings.TrimSpace(command + " " + rest)
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return CommandResult{Message: "用法：/agent、/model、/provider、/skill、/mcp、/resume、/tools、/status", Error: true}
	}
	name := strings.ToLower(fields[0])
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	cfg := config.GetConfig()
	if cfg == nil {
		return CommandResult{Message: "配置未初始化", Error: true}
	}

	switch name {
	case "agent":
		if arg == "" {
			return CommandResult{Message: "用法：/agent <name>\n可用 agent：" + strings.Join(sortedKeys(cfg.Agents), ", "), Error: true}
		}
		if _, ok := cfg.Agents[arg]; !ok {
			return CommandResult{Message: fmt.Sprintf("agent 不存在: %s\n可用 agent：%s", arg, strings.Join(sortedKeys(cfg.Agents), ", ")), Error: true}
		}
		app.agentName = arg
		app.modelOverride = rememberedModel(cfg, arg) // restore that agent's remembered model
		if err := app.rebuildAgent(); err != nil {
			return CommandResult{Message: fmt.Sprintf("切换 agent 失败: %v", err), Error: true}
		}
		_ = state.SetLastAgent(arg) // remember for the next no-argument start
		app.syncSessionConfig()
		app.registerCommands()
		return CommandResult{Message: "已切换到 agent: " + arg, Subtitle: app.headerSubtitle()}

	case "model":
		if arg == "" {
			return CommandResult{Message: "用法：/model <name>\n可用模型：" + strings.Join(sortedKeys(cfg.Models), ", "), Error: true}
		}
		if _, ok := cfg.Models[arg]; !ok {
			return CommandResult{Message: fmt.Sprintf("模型不存在: %s\n可用模型：%s", arg, strings.Join(sortedKeys(cfg.Models), ", ")), Error: true}
		}
		return app.selectModel(arg)

	case "provider":
		return app.model.openProvider(cfg, app.ctx, arg, app.addProviderModels)

	case "mcp":
		// /mcp opens the management panel; enable/disable toggles without it.
		if arg == "" || arg == "on" || arg == "panel" {
			if len(cfg.MCPServers) == 0 {
				return CommandResult{Message: "配置中没有 MCP 服务（mcp_servers）", Error: true}
			}
			app.openMCPPanel()
			return CommandResult{}
		}
		switch strings.ToLower(arg) {
		case "enable", "disable":
			if len(fields) < 3 {
				return CommandResult{Message: "用法：/mcp enable|disable <name|all>", Error: true}
			}
			target := fields[2]
			enable := strings.ToLower(arg) == "enable"
			if strings.EqualFold(target, "all") {
				servers := sortedKeys(cfg.MCPServers)
				var msgs []string
				for _, s := range servers {
					if res := app.setMCPServer(s, enable); res.Error {
						return res
					}
					msgs = append(msgs, s)
				}
				verb := "已启用"
				if !enable {
					verb = "已禁用"
				}
				return CommandResult{Message: fmt.Sprintf("%s全部 MCP 服务（%d 个，本次会话生效）", verb, len(msgs))}
			}
			return app.setMCPServer(target, enable)
		default:
			return CommandResult{Message: "用法：/mcp [enable|disable] [name|all]（不带参数打开管理面板）", Error: true}
		}

	case "skill":
		if arg == "" {
			return CommandResult{Message: "用法：/skill <name> [消息] 或 /skill:<name> [消息]（用 /skill off 取消选择）\n带消息时选择 skill 并立即用它处理这条消息\n可用 skill：" + strings.Join(append([]string{"off"}, sortedKeys(cfg.Skills)...), ", ") +
				"\n目录：" + strings.Join(config.SkillDirectories(), ", "), Error: true}
		}
		// Everything after the skill name is the message for the turn, so
		// "/skill coder 修复登录 bug" selects coder and runs it right away.
		message := remainderAfter(input, 2)
		if arg == "off" || arg == "none" {
			if message != "" {
				return CommandResult{Message: "用法：/skill off（不带消息；取消选择后直接输入即可）", Error: true}
			}
			app.skill = ""
			if err := app.rebuildAgent(); err != nil {
				return CommandResult{Message: fmt.Sprintf("取消 skill 选择失败: %v", err), Error: true}
			}
			app.syncSessionConfig()
			app.registerCommands()
			return CommandResult{Message: "已取消 skill 选择"}
		}
		if _, ok := cfg.Skills[arg]; !ok {
			return CommandResult{Message: fmt.Sprintf("skill 不存在: %s\n可用 skill：%s", arg, strings.Join(sortedKeys(cfg.Skills), ", ")), Error: true}
		}
		// Executing a skill needs a free turn; refuse before switching so the
		// active skill and the transcript stay consistent.
		if message != "" && app.model.isWaiting {
			return CommandResult{Message: "当前回复还在进行中：先按两次 Esc 停止，或等它结束后再执行 skill", Error: true}
		}
		previous := app.skill
		app.skill = arg
		if err := app.rebuildAgent(); err != nil {
			// Keep the interface state truthful: the skill is not active.
			app.skill = previous
			return CommandResult{Message: fmt.Sprintf("选择 skill 失败: %v", err), Error: true}
		}
		app.syncSessionConfig()
		app.registerCommands()
		if message == "" {
			return CommandResult{Message: "已选择 skill: " + arg}
		}

		// Execute the skill: the message becomes this turn's input, so the
		// skill's prompt and tools apply to it without a second keystroke.
		app.model.appendCommandMessage("已选择 skill: "+arg, false)
		cmd := app.model.startTurn(message)
		if cmd == nil {
			return CommandResult{Message: "当前回复还在进行中，消息未发送", Error: true}
		}
		return CommandResult{Cmd: cmd}

	case "theme":
		if arg == "" {
			return CommandResult{Message: "用法：/theme <name> [light|dark]\n可用主题：" + strings.Join(themeNames(), ", "), Error: true}
		}
		mode := ""
		if len(fields) > 2 {
			mode = fields[2]
		}
		if err := app.model.ApplyTheme(arg, mode); err != nil {
			return CommandResult{Message: err.Error(), Error: true}
		}
		app.registerCommands()
		return CommandResult{Message: fmt.Sprintf("已切换主题: %s（%s）", app.model.ThemeName(), themeModeName(app.model.DarkMode()))}

	case "tools":
		ac, ok := app.effectiveAgentConfig()
		if !ok {
			return CommandResult{Message: "无法读取当前 agent 配置", Error: true}
		}
		var b strings.Builder
		b.WriteString("当前工具：")
		if len(ac.Tools) == 0 {
			b.WriteString("（无）")
		} else {
			for _, t := range ac.Tools {
				b.WriteString("\n  · " + toolDisplayName(t))
			}
		}
		if len(ac.MCPServers) > 0 {
			b.WriteString("\nMCP 服务：")
			for _, s := range ac.MCPServers {
				b.WriteString("\n  · " + s + " — " + mcpServerLabel(s, cfg.MCPServers[s]))
			}
		} else if len(cfg.MCPServers) > 0 {
			disabled := len(app.mcpDisabled)
			b.WriteString(fmt.Sprintf("\nMCP 服务：（agent 未配置；全局 %d 个，%d 个已禁用）", len(cfg.MCPServers), disabled))
		}
		return CommandResult{Message: b.String()}

	case "status":
		return CommandResult{Message: fmt.Sprintf("当前状态：\n  agent: %s\n  model: %s\n  skill: %s\n  MCP: %s\n  审批: %s\n  theme: %s（%s）\n  会话: %s",
			app.agentName, app.currentModelName(), app.skillName(), app.mcpStatusSummary(), approvalModeLabel(),
			app.model.ThemeName(), themeModeName(app.model.DarkMode()), app.sessionSummary())}

	case "resume":
		return app.resumeSession(arg)
	}

	return CommandResult{Message: "未知命令: /" + name + "（输入 /help 查看可用命令）", Error: true}
}

// setMCPServer enables or disables an MCP server for this session. The change
// is runtime-only: the configuration file is untouched, and a restart restores
// every server.
func (app *AgentApp) setMCPServer(name string, enable bool) CommandResult {
	cfg := config.GetConfig()
	if cfg == nil {
		return CommandResult{Message: "配置未初始化", Error: true}
	}
	if _, ok := cfg.MCPServers[name]; !ok {
		return CommandResult{Message: fmt.Sprintf("MCP 服务不存在: %s\n可用：%s", name, strings.Join(sortedKeys(cfg.MCPServers), ", ")), Error: true}
	}
	// A server whose connection failed at startup has no tools to switch on:
	// say so instead of reporting a success that changes nothing.
	if enable {
		if manager := mcp.GetGlobalManager(); manager != nil {
			if err := manager.ServerErrors()[name]; err != nil {
				return CommandResult{Message: fmt.Sprintf("MCP 服务 %s 本次启动连接失败（%s）；重启 Oni3 后重试", name, mcp.ShortError(err)), Error: true}
			}
		}
	}

	already := app.mcpDisabled[name] == !enable
	if !already {
		if enable {
			// Enabled entries leave the map so len() stays the disabled count.
			delete(app.mcpDisabled, name)
		} else {
			if app.mcpDisabled == nil {
				app.mcpDisabled = make(map[string]bool)
			}
			app.mcpDisabled[name] = true
		}
	}
	verb := "启用"
	if !enable {
		verb = "禁用"
	}

	// The current agent only picks the change up if it references the server.
	ac, ok := cfg.Agents[app.agentName]
	if ok && !containsString(ac.MCPServers, name) {
		if already {
			return CommandResult{Message: fmt.Sprintf("MCP 服务 %s 已是%s状态（当前 agent 未引用它）", name, verb)}
		}
		return CommandResult{Message: fmt.Sprintf("已%s MCP 服务: %s（当前 agent 未引用它，工具不变）", verb, name)}
	}
	if already {
		return CommandResult{Message: fmt.Sprintf("MCP 服务 %s 已是%s状态", name, verb)}
	}
	if err := app.rebuildAgent(); err != nil {
		// Revert so the UI state stays truthful.
		if enable {
			app.mcpDisabled[name] = true
		} else {
			delete(app.mcpDisabled, name)
		}
		return CommandResult{Message: fmt.Sprintf("%s MCP 服务失败: %v", verb, err), Error: true}
	}
	app.registerCommands()
	return CommandResult{Message: fmt.Sprintf("已%s MCP 服务: %s（本次会话生效）", verb, name)}
}

// containsString reports whether list contains s.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// mcpStatusSummary renders the enabled/disabled MCP counts for /status.
func (app *AgentApp) mcpStatusSummary() string {
	cfg := config.GetConfig()
	if cfg == nil || len(cfg.MCPServers) == 0 {
		return "未配置"
	}
	enabled := len(cfg.MCPServers) - len(app.mcpDisabled)
	summary := fmt.Sprintf("%d/%d 启用", enabled, len(cfg.MCPServers))
	if manager := mcp.GetGlobalManager(); manager != nil {
		if failures := manager.ServerErrors(); len(failures) > 0 {
			summary += fmt.Sprintf("，%d 个未就绪：%s", len(failures), strings.Join(sortedKeys(failures), "、"))
		}
	}
	return summary
}

// sessionSummary renders the active session for /status.
func (app *AgentApp) sessionSummary() string {
	app.sessionMu.Lock()
	defer app.sessionMu.Unlock()
	if app.sess == nil {
		return "（无）"
	}
	return fmt.Sprintf("%s（%d 条，创建于 %s）", app.sess.ID, app.sess.TurnCount(), sessionClock(app.sess.Created))
}

// resumeSession lists saved agent sessions or switches to one of them.
func (app *AgentApp) resumeSession(arg string) CommandResult {
	cfg := config.GetConfig()
	if cfg == nil {
		return CommandResult{Message: "配置未初始化", Error: true}
	}
	if arg == "" {
		return CommandResult{Message: sessionListText(session.KindAgent, 10)}
	}

	s, err := resolveSession(session.KindAgent, arg)
	if err != nil {
		return CommandResult{Message: err.Error(), Error: true}
	}
	// Resumed conversations start with a clean approval state.
	app.approvals.reset()

	// Restore the agent (if it still exists), then the model/skill overrides.
	if s.Agent != "" {
		if _, ok := cfg.Agents[s.Agent]; ok {
			app.agentName = s.Agent
		}
	}
	ac, ok := cfg.Agents[app.agentName]
	if !ok {
		return CommandResult{Message: fmt.Sprintf("agent 不存在: %s", app.agentName), Error: true}
	}
	if ac.Model == "" {
		ac.Model = cfg.DefaultModelName()
	}
	// The remembered model is the baseline; a model recorded in the session
	// itself takes precedence for the resumed conversation.
	app.modelOverride = rememberedModel(cfg, app.agentName)
	if s.Model != "" && s.Model != ac.Model {
		if _, ok := cfg.Models[s.Model]; ok {
			app.modelOverride = s.Model
		}
	}
	app.skill = ""
	if s.Skill != "" {
		if _, ok := cfg.Skills[s.Skill]; ok {
			app.skill = s.Skill
		}
	}
	if err := app.rebuildAgent(); err != nil {
		return CommandResult{Message: fmt.Sprintf("恢复会话失败: %v", err), Error: true}
	}

	app.sessionMu.Lock()
	app.sess = s
	app.sessionMu.Unlock()
	_ = state.SetLastAgent(app.agentName)

	app.model.SetTranscript(sessionTranscript(s))
	app.model.SetTodos(viewTodos(s.Todos()))
	app.refreshSubtitle()
	app.registerCommands()

	return CommandResult{Message: fmt.Sprintf("已恢复会话 %s（agent: %s，%d 条消息）", s.ID, app.agentName, s.TurnCount())}
}

// registerCommands wires the command palette into the view model.
func (app *AgentApp) registerCommands() {
	if app.model != nil {
		app.model.SetSlashCommands(app.slashCommands(), app.executeSlash)
	}
}
