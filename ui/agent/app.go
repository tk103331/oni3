package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
	"github.com/tk103331/oni3/mcp"
	"github.com/tk103331/oni3/session"
	"github.com/tk103331/oni3/tools"
)

// AgentApp represents the Agent application structure
type AgentApp struct {
	agentName     string
	modelOverride string // runtime model override via /model
	skill         string // selected skill via /skill
	mouseEnabled  bool
	program       *tea.Program
	model         *ViewModel
	agent         agent.Agent
	ctx           context.Context

	// mcpDisabled holds MCP servers turned off at runtime via /mcp disable
	// or the /mcp panel.
	// The map is only read and written on the UI goroutine, like the other
	// overrides above.
	mcpDisabled map[string]bool

	// Tool approvals granted "for this session" (in memory only).
	approvals approver

	// cancelRun stops the in-flight turn (Esc twice in the TUI).
	runMu     sync.Mutex
	cancelRun context.CancelFunc

	sessionMu sync.Mutex
	sess      *session.Session // persisted current conversation
}

// NewAgentApp creates a new Agent application
func NewAgentApp(agentName string) (*AgentApp, error) {
	logger.Info("UI-AGENT", fmt.Sprintf("Creating agent app: %s", agentName))

	cfg := config.GetConfig()
	if cfg == nil {
		logger.Error("UI-AGENT", "Global configuration not initialized")
		return nil, fmt.Errorf("global configuration not initialized")
	}

	// Check if Agent configuration exists
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		logger.Error("UI-AGENT", fmt.Sprintf("Agent configuration does not exist: %s", agentName))
		return nil, fmt.Errorf("Agent configuration does not exist: %s", agentName)
	}

	logger.Debug("UI-AGENT", fmt.Sprintf("Agent config: Model=%s, Tools=%v, MCP=%v",
		agentConfig.Model, agentConfig.Tools, agentConfig.MCPServers))

	// Interactive tools (ask_user / todo_list) need config entries and a TUI host;
	// they are added through the effective config so config files stay clean.
	ensureInteractiveToolConfigs(cfg)

	logger.Info("UI-AGENT", fmt.Sprintf("Successfully created agent: %s", agentName))

	app := &AgentApp{
		agentName:     agentName,
		modelOverride: rememberedModel(cfg, agentName),
		ctx:           context.Background(),
	}
	// The session header records the model actually in use, not the configured
	// default that a remembered /model switch may override.
	app.sess = session.New(session.KindAgent, agentName, app.currentModelName(), "")
	// A provider-only configuration can enter the TUI and import its first model.
	if app.currentModelName() != "" {
		if err := app.rebuildAgent(); err != nil {
			logger.Error("UI-AGENT", fmt.Sprintf("Failed to create agent: %v", err))
			return nil, fmt.Errorf("failed to create Agent: %v", err)
		}
	}

	// Create Agent model, passing in the callback function for sending messages
	agentModel := NewViewModel(app.sendMessage)
	agentModel.SetSubtitle(app.headerSubtitle())
	agentModel.SetWorkDir(app.workDirLabel())
	app.model = agentModel
	reportStatus("idle", "", "")
	agentModel.SetMouseControl(app.setMouse)
	agentModel.SetOnClear(app.startNewSession)
	agentModel.SetOnInterrupt(app.Interrupt)
	app.registerCommands()
	if app.currentModelName() == "" {
		agentModel.appendCommandMessage("还没有配置模型：请选择一个 provider，填写 API Key 后导入模型（也可随时输入 /provider）", false)
	} else {
		agentModel.appendCommandMessage("鼠标捕获已启用：点击思考/工具/错误标题行可展开或折叠；点击其他区域会自动释放捕获以便复制（Ctrl+Y 切换）", false)
	}

	// Create Bubble Tea program
	// Capture starts enabled: clicking a reasoning/tool/error header folds
	// or expands it right away. A click on any other row releases the
	// capture so text can be selected and copied; the next turn restores it.
	app.mouseEnabled = true
	app.program = tea.NewProgram(agentModel, tea.WithAltScreen(), tea.WithMouseCellMotion())

	logger.Info("UI-AGENT", fmt.Sprintf("Agent app created successfully: %s", agentName))
	return app, nil
}

// mcpStatusWait bounds how long the interface waits for the background MCP
// initialization before giving up on a startup notice.
const mcpStatusWait = 45 * time.Second

// mcpFailureNotice renders the transcript notice for servers that could not be
// initialized. It returns an empty string when every server is usable.
func mcpFailureNotice(failures map[string]error) string {
	if len(failures) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("MCP 服务未就绪，其余服务与内置工具不受影响：")
	for _, name := range sortedKeys(failures) {
		b.WriteString("\n  · ")
		b.WriteString(name)
		b.WriteString(" — ")
		b.WriteString(mcp.ShortError(failures[name]))
	}
	b.WriteString("\n用 /mcp 查看服务状态")
	return b.String()
}

// watchMCPStatus reports MCP servers that failed to initialize.
//
// Server connections happen in the background while the TUI already owns the
// terminal, so their outcome cannot be printed to stderr: a stray write lands
// in the middle of a frame and garbles the whole layout. It is delivered as a
// transcript notice instead, once initialization settled. Servers that did
// connect and the built-in tools keep working; only the failed ones are
// missing.
func (app *AgentApp) watchMCPStatus() {
	cfg := config.GetConfig()
	if cfg == nil || len(cfg.MCPServers) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(app.ctx, mcpStatusWait)
		defer cancel()
		manager := mcp.WaitGlobalManager(ctx)
		if manager == nil {
			return
		}
		if notice := mcpFailureNotice(manager.ServerErrors()); notice != "" {
			app.program.Send(NoticeMsg(notice))
		}
	}()
}

// headerSubtitle is the label next to the window title: the active agent and
// the model actually in use (a /model switch or a remembered choice wins over
// the configured default).
func (app *AgentApp) headerSubtitle() string {
	return headerLabel(app.displayName(), app.currentModelName())
}

// workDirLabel returns the process working directory for the header; tools
// resolve relative paths against it, so showing it keeps the user oriented.
func (app *AgentApp) workDirLabel() string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return ""
}

// displayName returns the label of the active agent. It is the configured
// name when the agent defines one, and the configuration key otherwise.
func (app *AgentApp) displayName() string {
	cfg := config.GetConfig()
	if cfg == nil {
		return app.agentName
	}
	return cfg.AgentDisplayName(app.agentName)
}

// headerLabel joins the agent and model names, skipping empty parts.
func headerLabel(agentName, modelName string) string {
	agentName = strings.TrimSpace(agentName)
	modelName = strings.TrimSpace(modelName)
	switch {
	case agentName == "":
		return modelName
	case modelName == "":
		return agentName
	case agentName == modelName:
		return agentName
	}
	return agentName + " · " + modelName
}

// refreshSubtitle keeps the header in sync after an agent/model switch.
func (app *AgentApp) refreshSubtitle() {
	if app.model != nil {
		app.model.SetSubtitle(app.headerSubtitle())
	}
}

// setMouse enables or disables mouse capture and reports the applied state.
func (app *AgentApp) setMouse(enabled bool) bool {
	if enabled != app.mouseEnabled {
		app.mouseEnabled = enabled
		if enabled {
			app.program.EnableMouseCellMotion()
		} else {
			app.program.DisableMouseCellMotion()
		}
	}
	return app.mouseEnabled
}

// Run runs the Agent application
func (app *AgentApp) Run() error {
	defer app.model.closeProvider()
	// done/error records survive process exit; an interrupted or idle
	// session leaves an idle record behind. After this the working record
	// (if any) would be dropped by the terminal anyway.
	defer reportStatus("idle", "", "")
	app.openFirstRunProvider()
	// Started here, not in the constructor: the notice is delivered through the
	// running program, and nothing must queue on a program that never runs.
	app.watchMCPStatus()
	return runTUI(app.program)
}

// openFirstRunProvider opens the provider picker when no model is configured,
// so a fresh install can choose a built-in provider, enter its API key and
// import the first model without editing the configuration file. It reports
// whether the picker was opened.
func (app *AgentApp) openFirstRunProvider() bool {
	if app.model == nil || app.currentModelName() != "" {
		return false
	}
	cfg := config.GetConfig()
	if cfg == nil || len(cfg.Providers) == 0 {
		return false
	}
	app.model.openProvider(cfg, app.ctx, "", app.addProviderModels)
	return app.model.provider != nil
}

// runTUI starts the bubbletea program with wheel scrolling via the terminal's
// alternate-scroll mode (DECSET 1007): in the alternate screen the wheel is
// translated to arrow keys, so scrolling works while the mouse stays
// uncaptured and native text selection/copy keeps working.
func runTUI(program *tea.Program) error {
	// Best-effort: terminals that do not support 1007 simply ignore it.
	fmt.Print("\x1b[?1007h")
	defer fmt.Print("\x1b[?1007l")
	_, err := program.Run()
	return err
}

// sendMessage sends a message to AI
func (app *AgentApp) sendMessage(message string) error {
	if app.currentModelName() == "" {
		return fmt.Errorf("请先通过 /provider 添加模型，或通过 /model 选择模型")
	}
	if app.agent == nil {
		if err := app.rebuildAgent(); err != nil {
			return err
		}
	}
	logger.Info("UI-AGENT", fmt.Sprintf("Sending message: %s", truncateForLog(message)))

	// Get the effective agent configuration (config + model/skill overrides)
	agentConfig, ok := app.effectiveAgentConfig()
	if !ok {
		app.program.Send(ErrorMsg(fmt.Sprintf("Agent configuration does not exist: %s", app.agentName)))
		return fmt.Errorf("Agent configuration does not exist: %s", app.agentName)
	}

	// Log the system prompt and replay the persisted turns as context.
	if agentConfig.System != "" {
		logger.Debug("UI-AGENT", fmt.Sprintf("Using system prompt: %s", truncateForLog(agentConfig.System)))
	}
	history := app.historySnapshot()
	app.recordTurn(session.RoleUser, message)

	// Each turn gets its own context so Esc twice can cancel it without
	// affecting the app's lifetime.
	ctx, cancel := context.WithCancel(app.ctx)
	app.runMu.Lock()
	app.cancelRun = cancel
	app.runMu.Unlock()

	// Handle conversation in goroutine to avoid blocking UI
	go func() {
		defer func() {
			cancel()
			app.runMu.Lock()
			app.cancelRun = nil
			app.runMu.Unlock()
		}()
		app.processConversation(ctx, history, message)
	}()

	return nil
}

// Interrupt stops the reply that is currently streaming, if any.
func (app *AgentApp) Interrupt() {
	app.runMu.Lock()
	cancel := app.cancelRun
	app.runMu.Unlock()
	if cancel != nil {
		logger.Info("UI-AGENT", "Interrupting the running reply")
		cancel()
	}
}

// processConversation handles conversation (using streaming output)
func (app *AgentApp) processConversation(ctx context.Context, history []*schema.Message, prompt string) {
	logger.Info("UI-AGENT", "Starting conversation processing")

	if prompt == "" {
		logger.Error("UI-AGENT", "User message not found in conversation")
		app.program.Send(ErrorMsg("User message not found"))
		return
	}

	logger.Info("UI-AGENT", fmt.Sprintf("Processing prompt: %s", truncateForLog(prompt)))

	// Persisted reasoning is flushed as one event at each boundary (reasoning
	// arrives token by token; one JSONL line per token would be wasteful).
	var thought strings.Builder
	flushThought := func() {
		if thought.Len() == 0 {
			return
		}
		app.recordReasoning(thought.String())
		thought.Reset()
	}

	// The eino callback layer reports one event per tool call. Track the
	// calls that are still running so every start is paired with its end and
	// the persisted transcript has no duplicates.
	type pendingCall struct {
		id        string
		name      string
		arguments string
	}
	var pending []pendingCall
	seenCalls := make(map[string]bool)
	recordToolStart := func(name, callID, arguments string) {
		flushThought()
		if callID != "" {
			if seenCalls[callID] {
				return
			}
			seenCalls[callID] = true
		} else {
			for _, p := range pending {
				if p.name == name && p.arguments == arguments {
					return
				}
			}
		}
		pending = append(pending, pendingCall{id: callID, name: name, arguments: arguments})
		app.recordToolStart(name, arguments)
	}
	recordToolEnd := func(name, callID, result string, failed bool) {
		for i := range pending {
			if callID != "" && pending[i].id == callID {
				pending = append(pending[:i], pending[i+1:]...)
				break
			}
			if callID == "" && pending[i].name == name {
				pending = append(pending[:i], pending[i+1:]...)
				break
			}
		}
		app.recordToolEnd(name, result, failed)
	}

	// Create tool call callback function
	toolCallback := func(msg interface{}) {
		logger.Debug("UI-CALLBACK", fmt.Sprintf("Received: %T", msg))

		switch v := msg.(type) {
		case agent.ToolCallInfo:
			// Handle ToolCallInfo from ReactAgent
			isInternal := isInternalComponent(v.Name)
			logger.Debug("UI-CALLBACK", fmt.Sprintf("ToolCallInfo: Name=%s, Type=%s, Internal=%v", v.Name, v.Type, isInternal))
			if !isInternal {
				switch v.Type {
				case "start":
					logger.Info("UI-TOOL", fmt.Sprintf("Starting: %s", v.Name))
					logger.Debug("UI-TOOL", fmt.Sprintf("Arguments: %s", v.Arguments))
					recordToolStart(v.Name, v.CallID, v.Arguments)
					app.program.Send(ToolStartMsg{
						Name:      v.Name,
						CallID:    v.CallID,
						Arguments: v.Arguments,
					})
				case "end":
					logger.Info("UI-TOOL", fmt.Sprintf("Completed: %s", v.Name))
					logger.Debug("UI-TOOL", fmt.Sprintf("Result: %s", truncateForLog(v.Result)))
					failed := agent.IsToolError(v.Result)
					recordToolEnd(v.Name, v.CallID, v.Result, failed)
					app.program.Send(ToolEndMsg{
						Name:   v.Name,
						CallID: v.CallID,
						Result: v.Result,
						Failed: failed,
					})
				case "error":
					// A failed call is shown on its own row instead of as an
					// extra error block: the same failure used to be reported
					// up to four times.
					logger.Error("UI-TOOL", fmt.Sprintf("Tool %s error: %s", v.Name, v.Error))
					recordToolEnd(v.Name, v.CallID, v.Error, true)
					app.program.Send(ToolEndMsg{
						Name:   v.Name,
						CallID: v.CallID,
						Result: v.Error,
						Failed: true,
					})
				}
			} else {
				logger.Debug("UI-CALLBACK", fmt.Sprintf("Filtered internal component: %s", v.Name))
			}
		case struct {
			Name      string
			Arguments string
		}:
			// Handle legacy format
			isInternal := isInternalComponent(v.Name)
			logger.Debug("UI-CALLBACK", fmt.Sprintf("Legacy start: Name=%s, Internal=%v", v.Name, isInternal))
			if !isInternal {
				logger.Info("UI-TOOL", fmt.Sprintf("Starting (legacy): %s", v.Name))
				logger.Debug("UI-TOOL", fmt.Sprintf("Arguments: %s", v.Arguments))
				recordToolStart(v.Name, "", v.Arguments)
				app.program.Send(ToolStartMsg{
					Name:      v.Name,
					Arguments: v.Arguments,
				})
			}
		case struct {
			Name   string
			Result string
		}:
			// Handle legacy format
			isInternal := isInternalComponent(v.Name)
			logger.Debug("UI-CALLBACK", fmt.Sprintf("Legacy end: Name=%s, Internal=%v", v.Name, isInternal))
			if !isInternal {
				logger.Info("UI-TOOL", fmt.Sprintf("Completed (legacy): %s", v.Name))
				logger.Debug("UI-TOOL", fmt.Sprintf("Result: %s", truncateForLog(v.Result)))
				recordToolEnd(v.Name, "", v.Result, false)
				app.program.Send(ToolEndMsg{
					Name:   v.Name,
					Result: v.Result,
				})
			}
		default:
			logger.Warn("UI-CALLBACK", fmt.Sprintf("Unknown callback type: %T", msg))
			if errMsg, ok := msg.(string); ok {
				logger.Error("UI-CALLBACK", fmt.Sprintf("Error message: %s", errMsg))
				app.program.Send(ErrorMsg(errMsg))
			}
		}
	}

	// Create streaming content callback function
	var answer strings.Builder
	// A failure is reported once: the stream callback and the returned error
	// describe the same problem, showing both repeated it in the transcript.
	errorReported := false
	chunkCallback := func(chunk *agent.StreamChunk) {
		switch chunk.Type {
		case "content":
			if chunk.Content != "" {
				answer.WriteString(chunk.Content)
				app.program.Send(StreamChunkMsg(chunk.Content))
			} else {
				app.program.Send(StreamEndMsg{})
			}
		case "reasoning":
			if chunk.Content != "" {
				thought.WriteString(chunk.Content)
				app.program.Send(ReasoningChunkMsg(chunk.Content))
			}
		case "error":
			errorReported = true
			app.program.Send(ErrorMsg(chunk.Content))
		}
	}

	// Use Agent's ChatStream method for streaming conversation. The context
	// carries the host so ask_user/todo_list can reach the TUI.
	logger.Info("UI-AGENT", "Calling agent ChatStream")
	ctx = tools.WithHost(ctx, app)
	err := app.agent.ChatStream(ctx, history, prompt, chunkCallback, toolCallback)
	flushThought()
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			// Stopped by the user: keep whatever streamed so far instead of
			// reporting a failure.
			logger.Info("UI-AGENT", "Reply stopped by user")
			app.program.Send(StreamEndMsg{})
			if answer.Len() > 0 {
				app.recordTurn(session.RoleAssistant, answer.String())
			}
			app.program.Send(NoticeMsg("已停止当前回复"))
			return
		}
		logger.Error("UI-AGENT", fmt.Sprintf("AI response error: %v", err))
		if !errorReported {
			app.program.Send(ErrorMsg(fmt.Sprintf("AI response error: %v", err)))
		}
	} else {
		logger.Info("UI-AGENT", "ChatStream completed successfully")
		app.recordTurn(session.RoleAssistant, answer.String())
	}
}

// isInternalComponent determines if it is an internal component that should not be displayed to users
func isInternalComponent(name string) bool {
	// Filter out internal ReAct components that don't represent actual tool calls
	// but still show actual tool invocations
	internalComponents := []string{
		"ChatModel",
		"Tools", // This is the ReAct tools orchestrator node, not actual tool calls
	}

	for _, component := range internalComponents {
		if name == component {
			return true
		}
	}
	return false
}

// truncateForLog truncates text for logging to avoid huge log entries. The cut
// is made on a rune boundary so multi-byte text stays valid.
func truncateForLog(text string) string {
	const max = 197
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "..."
}

// Stop stops the Agent application
func (app *AgentApp) Stop() {
	if app.program != nil {
		app.program.Quit()
	}
}
