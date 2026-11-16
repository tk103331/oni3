package acp

import (
	"context"
	"fmt"
	"strings"

	"github.com/coder/acp-go-sdk"
	"github.com/tk103331/oni3/config"
)

// Slash commands advertised to ACP clients through the
// available_commands_update session notification. Clients show them in their
// command palette and send them back as ordinary prompt text; Prompt
// recognizes the leading slash and answers directly without calling the
// model. Only commands that work without the TUI's interactive panels are
// listed (no /theme, /provider, /mcp, /agent or /resume), and /model + /skill
// mirror the session/set_config_option selectors.
func availableCommands(cfg *config.Config) []acp.AvailableCommand {
	commands := []acp.AvailableCommand{
		{Name: "clear", Description: "清空当前对话"},
		{
			Name:        "model",
			Description: "切换模型（不带参数列出可用模型）",
			Input:       unstructuredInput("模型名"),
		},
	}
	if len(cfg.Skills) > 0 {
		commands = append(commands, acp.AvailableCommand{
			Name:        "skill",
			Description: "切换 skill（off 取消选择）",
			Input:       unstructuredInput("skill 名或 off"),
		})
	}
	commands = append(commands,
		acp.AvailableCommand{Name: "tools", Description: "列出当前 agent 可用的工具"},
		acp.AvailableCommand{Name: "status", Description: "显示当前会话状态"},
	)
	return commands
}

// unstructuredInput wraps a hint in the command input schema.
func unstructuredInput(hint string) *acp.AvailableCommandInput {
	return &acp.AvailableCommandInput{
		Unstructured: &acp.UnstructuredCommandInput{Hint: hint},
	}
}

// sendAvailableCommands advertises the slash command list for one session.
func (s *Service) sendAvailableCommands(ctx context.Context, sid string) {
	s.sendUpdate(ctx, sid, acp.SessionUpdate{
		AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
			AvailableCommands: availableCommands(s.cfg),
		},
	})
}

// runSlashCommand executes an advertised slash command and returns its reply.
// Every prompt that starts with "/" is treated as a command, mirroring the
// TUI; unknown names get an error instead of reaching the model.
func (s *Service) runSlashCommand(ctx context.Context, session *acpSession, prompt string) string {
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prompt), "/"))
	name := body
	arg := ""
	if i := strings.IndexAny(body, " \t"); i >= 0 {
		name, arg = body[:i], strings.TrimSpace(body[i+1:])
	}
	switch strings.ToLower(name) {
	case "clear":
		return s.clearSession(session)
	case "model":
		return s.commandSwitchModel(ctx, session, arg)
	case "skill":
		return s.commandSwitchSkill(ctx, session, arg)
	case "tools":
		return s.toolsText(session)
	case "status":
		return s.statusText(session)
	default:
		return "未知命令：/" + name
	}
}

// clearSession starts a blank conversation: in-memory history is dropped and
// the on-disk persistence continues under a fresh session file, mirroring
// the TUI /clear. Model and skill selections carry over.
func (s *Service) clearSession(session *acpSession) string {
	s.mu.Lock()
	session.history = nil
	session.titled = false
	session.store = newSessionStore(s.agentName, s.sessionModel(session))
	s.mu.Unlock()
	return "已清空当前对话。"
}

// commandSwitchModel mirrors session/set_config_option model switches for
// the /model command.
func (s *Service) commandSwitchModel(ctx context.Context, session *acpSession, arg string) string {
	if arg == "" {
		return "可用模型：" + strings.Join(sortedModelNames(s.cfg), ", ")
	}
	if _, ok := s.cfg.Models[arg]; !ok {
		return "模型不存在: " + arg + "\n可用模型：" + strings.Join(sortedModelNames(s.cfg), ", ")
	}
	if err := s.rebuildSessionAgent(session, arg, session.skillOverride); err != nil {
		return fmt.Sprintf("切换模型失败: %v", err)
	}
	session.modelOverride = arg
	session.store.setModel(arg)
	s.notifyConfigOptions(ctx, session)
	return "已切换模型: " + arg
}

// commandSwitchSkill mirrors session/set_config_option skill switches for
// the /skill command. Without an argument it lists the configured skills.
func (s *Service) commandSwitchSkill(ctx context.Context, session *acpSession, arg string) string {
	if arg == "" {
		return "用法：/skill <name>（用 /skill off 取消选择）\n可用 skill：" +
			strings.Join(append([]string{"off"}, sortedKeys(s.cfg.Skills)...), ", ")
	}
	skill := ""
	if arg != optionOff {
		if _, ok := s.cfg.Skills[arg]; !ok {
			return "skill 不存在: " + arg + "\n可用 skill：" +
				strings.Join(sortedKeys(s.cfg.Skills), ", ")
		}
		skill = arg
	}
	if err := s.rebuildSessionAgent(session, session.modelOverride, skill); err != nil {
		return fmt.Sprintf("切换 skill 失败: %v", err)
	}
	session.skillOverride = skill
	session.store.setSkill(skill)
	s.notifyConfigOptions(ctx, session)
	if skill == "" {
		return "已取消 skill 选择。"
	}
	return "已选择 skill: " + skill
}

// toolsText lists the tools of the session's effective agent configuration.
func (s *Service) toolsText(session *acpSession) string {
	agentCfg, ok := s.cfg.Agents[s.agentName]
	if !ok {
		return "无法读取当前 agent 配置"
	}
	toolNames := append([]string{}, agentCfg.Tools...)
	if session.skillOverride != "" {
		if sk, ok := s.cfg.Skills[session.skillOverride]; ok && len(sk.Tools) > 0 {
			toolNames = append(toolNames, sk.Tools...)
		}
	}
	var b strings.Builder
	b.WriteString("当前工具：")
	if len(toolNames) == 0 {
		b.WriteString("（无）")
	}
	for _, t := range toolNames {
		b.WriteString("\n  · " + t)
	}
	if len(agentCfg.MCPServers) > 0 {
		b.WriteString("\nMCP 服务：")
		for _, name := range agentCfg.MCPServers {
			b.WriteString("\n  · " + name)
		}
	}
	return b.String()
}

// statusText renders the session status for /status.
func (s *Service) statusText(session *acpSession) string {
	skill := session.skillOverride
	if skill == "" {
		skill = "off"
	}
	mcp := "未配置"
	if agentCfg, ok := s.cfg.Agents[s.agentName]; ok && len(agentCfg.MCPServers) > 0 {
		mcp = fmt.Sprintf("%d 个已启用", len(agentCfg.MCPServers))
	}
	return fmt.Sprintf("当前状态：\n  agent: %s\n  model: %s\n  skill: %s\n  MCP: %s\n  工作目录: %s",
		s.agentName, s.sessionModel(session), skill, mcp, session.cwd)
}

// sessionModel is the model the session currently resolves to.
func (s *Service) sessionModel(session *acpSession) string {
	if session.modelOverride != "" {
		return session.modelOverride
	}
	return s.currentModelName()
}

// notifyConfigOptions pushes the option state to the client after a command
// switched the model or skill, so its selectors stay in sync with the
// session/set_config_option values.
func (s *Service) notifyConfigOptions(ctx context.Context, session *acpSession) {
	s.mu.Lock()
	options := s.sessionConfigOptions(session)
	s.mu.Unlock()
	s.sendUpdate(ctx, session.id, acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{ConfigOptions: options},
	})
}
