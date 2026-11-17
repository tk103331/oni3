package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/logger"
	"github.com/tk103331/oni3/mcp"
	"github.com/tk103331/oni3/models"
	"github.com/tk103331/oni3/tools"
)

// ReactAgent implements Agent using React pattern from cloudwego/eino library
type ReactAgent struct {
	config    *config.Agent
	agent     *react.Agent
	ctx       context.Context
	agentName string
}

// formatArguments formats tool arguments for better readability
func formatArguments(args string) string {
	// Try to parse as JSON first
	var jsonArgs interface{}
	if err := json.Unmarshal([]byte(args), &jsonArgs); err == nil {
		if formatted, err := json.MarshalIndent(jsonArgs, "   ", "  "); err == nil {
			return string(formatted)
		}
	}

	// Handle Go struct format (e.g., &{key value key2 value2})
	if strings.HasPrefix(args, "&{") {
		return formatGoStruct(args)
	}

	// Handle map-like format
	if strings.Contains(args, ":") && strings.Contains(args, "{") {
		return formatMapLike(args)
	}

	// Clean up common formatting issues
	cleaned := strings.ReplaceAll(args, "\n", " ")
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)

	// Truncate if still too long
	if len(cleaned) > 300 {
		return truncateText(cleaned, 297)
	}

	return cleaned
}

// formatResult formats tool results for better readability
func formatResult(result string) string {
	// Clean up result first
	cleaned := strings.TrimSpace(result)

	// Handle JSON results
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(cleaned), &jsonResult); err == nil {
		if formatted, err := json.MarshalIndent(jsonResult, "   ", "  "); err == nil {
			formattedStr := string(formatted)
			if len(formattedStr) > 500 {
				return truncateText(formattedStr, 497)
			}
			return formattedStr
		}
	}

	// Handle multiline results
	lines := strings.Split(cleaned, "\n")
	if len(lines) > 10 {
		return strings.Join(lines[:10], "\n") + "\n... (truncated)"
	}

	// Truncate single line if too long
	if len(cleaned) > 500 {
		return truncateText(cleaned, 497)
	}

	return cleaned
}

// formatGoStruct formats Go struct-like strings into readable format
func formatGoStruct(structStr string) string {
	// Remove &{ and }
	content := strings.TrimPrefix(structStr, "&{")
	content = strings.TrimSuffix(content, "}")

	// Split by spaces and try to parse key-value pairs
	parts := strings.Fields(content)
	var result []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		// Skip memory addresses and pointers
		if strings.HasPrefix(part, "0x") || len(part) == 14 && part[0] == '0' && part[1] == 'x' {
			continue
		}

		// Skip empty brackets and special chars
		if part == "[]" || part == "<nil>" || part == "map[]" {
			continue
		}

		// Clean up the part
		if strings.Contains(part, ":") {
			result = append(result, part)
		} else if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "0x") {
			// Assume it's a key-value pair
			result = append(result, part+": "+parts[i+1])
			i++ // Skip next part as it's the value
		} else {
			result = append(result, part)
		}
	}

	formatted := strings.Join(result, ", ")
	if len(formatted) > 300 {
		return truncateText(formatted, 297)
	}
	return formatted
}

// formatMapLike formats map-like strings into readable format
func formatMapLike(mapStr string) string {
	// Try to extract key-value pairs
	re := regexp.MustCompile(`(\w+):\s*([^{,}\[\]]+)|(\w+):\s*\{([^}]*)\}`)
	matches := re.FindAllStringSubmatch(mapStr, -1)

	var result []string
	for _, match := range matches {
		if match[1] != "" { // Simple key: value
			key := match[1]
			value := strings.TrimSpace(match[2])
			result = append(result, key+": "+value)
		} else if match[3] != "" { // key: {complex}
			key := match[3]
			value := strings.TrimSpace(match[4])
			if value != "" {
				result = append(result, key+": {"+value+"}")
			} else {
				result = append(result, key+": {}")
			}
		}
	}

	if len(result) > 0 {
		formatted := "{ " + strings.Join(result, ", ") + " }"
		if len(formatted) > 300 {
			return truncateText(formatted, 297)
		}
		return formatted
	}

	// Fallback: clean up the original string
	cleaned := regexp.MustCompile(`\s+`).ReplaceAllString(mapStr, " ")
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > 300 {
		return truncateText(cleaned, 297)
	}
	return cleaned
}

// formatGeneralInfo formats general callback information
func formatGeneralInfo(info string) string {
	// Skip empty or memory address info
	if info == "" || regexp.MustCompile(`^0x[a-fA-F0-9]+$`).MatchString(info) {
		return ""
	}

	// Handle ChatModel messages
	if strings.Contains(info, "system:") && strings.Contains(info, "user:") {
		return formatChatMessages(info)
	}

	// Handle tool call information
	if strings.Contains(info, "tool_calls:") {
		return formatToolCallInfo(info)
	}

	// Clean up and truncate
	cleaned := strings.ReplaceAll(info, "\n", " ")
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)

	if len(cleaned) > 200 {
		return truncateText(cleaned, 197)
	}

	return cleaned
}

// formatChatMessages formats chat message information for ChatModel node
func formatChatMessages(info string) string {
	// For ChatModel node, we only want to show a simple indicator
	// instead of the complex message content
	return "🤖 Processing messages with model"
}

// formatToolCallInfo formats tool call information for Tools node
func formatToolCallInfo(info string) string {
	// Parse and format tool call information
	if strings.Contains(info, "tool_calls:") {
		return formatToolCalls(info)
	}

	return truncateString(info, 150)
}

// formatToolCalls extracts and formats individual tool calls
func formatToolCalls(info string) string {
	// Look for tool call patterns in the response
	lines := strings.Split(info, "\n")
	var result []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "tool_calls:") {
			result = append(result, "🔧 Processing tool calls...")
		} else if strings.Contains(line, "Function:{Name:") {
			// Extract tool name
			if start := strings.Index(line, "Name:"); start != -1 {
				nameStart := start + 5
				if end := strings.Index(line[nameStart:], " "); end != -1 {
					toolName := line[nameStart : nameStart+end]
					result = append(result, fmt.Sprintf("   📋 Tool: %s", toolName))
				}
			}
		} else if strings.Contains(line, "Arguments:") {
			// Extract tool arguments
			if start := strings.Index(line, "Arguments:"); start != -1 {
				args := line[start+11:]
				args = strings.TrimSpace(args)
				if args == "{}" {
					result = append(result, "   📝 Arguments: (none)")
				} else {
					result = append(result, fmt.Sprintf("   📝 Arguments: %s", args))
				}
			}
		} else if strings.Contains(line, "finish_reason:") {
			// Extract finish reason
			if start := strings.Index(line, "finish_reason:"); start != -1 {
				reason := strings.TrimSpace(line[start+14:])
				if reason == "tool_calls" {
					result = append(result, "   ✅ Reason: Tool calls completed")
				} else if reason == "stop" {
					result = append(result, "   ✅ Reason: Response completed")
				} else {
					result = append(result, fmt.Sprintf("   ✅ Reason: %s", reason))
				}
			}
		}
	}

	if len(result) == 0 {
		return "🔧 Tool calls detected in response"
	}

	return strings.Join(result, "\n")
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	return truncateText(s, maxLen-3)
}

// truncateText cuts s to at most max runes and appends an ellipsis. Counting
// bytes instead would split a multi-byte character, and slicing runes by a
// byte-derived limit panics when the text holds fewer runes than the limit.
func truncateText(s string, max int) string {
	if max < 0 {
		max = 0
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// ToolCallInfo represents structured tool call information
type ToolCallInfo struct {
	Type      string // "start", "end", "error"
	Name      string
	CallID    string // eino tool call id: pairs a call with its result
	Arguments string
	Result    string
	Error     string
}

// componentToolsNode is the eino component name of the ReAct tools node. The
// node announces the tool calls the model requested; the tool node itself
// announces the execution of one of them.
const componentToolsNode = "ToolsNode"

// isToolComponent reports whether a callback event belongs to an actual tool
// execution. Model, tools-node and graph events are internal plumbing and
// must not be reported as tool calls.
func isToolComponent(info *callbacks.RunInfo) bool {
	return info != nil && info.Component == components.ComponentOfTool
}

// isToolsNode reports whether a callback event belongs to the ReAct tools
// orchestrator.
func isToolsNode(info *callbacks.RunInfo) bool {
	return info != nil && (info.Name == "Tools" || info.Component == componentToolsNode)
}

// ToolCallCallback custom callback handler for capturing tool call information
type ToolCallCallback struct {
	callback func(interface{})
}

// emit reports one event to the display. A panic in the callback must not
// abort the agent run: eino turns a panicking handler into a node error, which
// ended the whole turn on a display bug.
func (t *ToolCallCallback) emit(info ToolCallInfo) {
	if t.callback == nil {
		return
	}
	safeCallback(func() { t.callback(info) })
}

// safeCallback runs a display callback, turning a panic into a logged error.
func safeCallback(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("AGENT", fmt.Sprintf("callback panicked: %v", r))
		}
	}()
	fn()
}

// OnStart callback when node starts
func (t *ToolCallCallback) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	if t.callback == nil || info == nil {
		return ctx
	}

	// The tools node announces every tool call the model requested, together
	// with the raw arguments and the tool call id. The tool node re-announces
	// the same call when it starts executing, so only the tools node is
	// reported here - reporting both produced a duplicate row per call.
	if !isToolsNode(info) {
		return ctx
	}
	msg, ok := input.(*schema.Message)
	if !ok {
		logger.Debug("AGENT", fmt.Sprintf("Tools node received input type: %T", input))
		return ctx
	}
	for _, tc := range msg.ToolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			logger.Warn("AGENT", "Tool call without a name is ignored")
			continue
		}
		logger.Debug("AGENT", fmt.Sprintf("Tool call %s: %s", tc.ID, name))
		t.emit(ToolCallInfo{
			Type:      "start",
			Name:      name,
			CallID:    tc.ID,
			Arguments: tc.Function.Arguments,
		})
	}
	return ctx
}

// OnEnd callback when node ends
func (t *ToolCallCallback) OnEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	if t.callback == nil || !isToolComponent(info) {
		return ctx
	}

	result := ""
	switch v := output.(type) {
	case string:
		result = v
	case *tool.CallbackOutput:
		result = v.Response
	default:
		result = fmt.Sprintf("%v", output)
	}

	logger.Debug("AGENT", fmt.Sprintf("Tool %s finished", info.Name))
	t.emit(ToolCallInfo{
		Type:   "end",
		Name:   info.Name,
		CallID: compose.GetToolCallID(ctx),
		Result: result,
	})
	return ctx
}

// OnError callback when node encounters error
func (t *ToolCallCallback) OnError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	if t.callback == nil || !isToolComponent(info) || err == nil {
		return ctx
	}
	logger.Debug("AGENT", fmt.Sprintf("Tool %s failed: %v", info.Name, err))
	t.emit(ToolCallInfo{
		Type:   "error",
		Name:   info.Name,
		CallID: compose.GetToolCallID(ctx),
		Error:  err.Error(),
	})
	return ctx
}

// OnStartWithStreamInput callback when stream input starts
func (t *ToolCallCallback) OnStartWithStreamInput(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
	return ctx
}

// OnEndWithStreamOutput callback when stream output ends
func (t *ToolCallCallback) OnEndWithStreamOutput(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
	return ctx
}

// NewReactAgent creates a new ReactAgent
func NewReactAgent(agentName string, cfg *config.Agent) *ReactAgent {
	return &ReactAgent{
		config:    cfg,
		ctx:       context.Background(),
		agentName: agentName,
	}
}

// Init initializes Agent
func (r *ReactAgent) Init() error {
	// Create model
	model, err := r.createModel()
	if err != nil {
		return fmt.Errorf("failed to create model: %w", err)
	}

	// Create tools configuration
	toolsConfig, err := r.createToolsConfig()
	if err != nil {
		return fmt.Errorf("failed to create tools configuration: %w", err)
	}

	// Create Agent configuration
	agentConfig := &react.AgentConfig{
		ToolCallingModel:      model,
		ToolsConfig:           toolsConfig,
		MaxStep:               r.maxSteps(),
		StreamToolCallChecker: streamHasToolCalls,
	}

	// Create Agent
	agent, err := react.NewAgent(r.ctx, agentConfig)
	if err != nil {
		return fmt.Errorf("failed to create Agent: %w", err)
	}

	// Save agent instance
	r.agent = agent
	return nil
}

// streamHasToolCalls allows models to explain their next action before emitting
// tool calls. Eino's default checker stops at the first non-empty content chunk,
// which can end a turn before a later tool call is executed.
func streamHasToolCalls(ctx context.Context, stream *schema.StreamReader[*schema.Message]) (bool, error) {
	defer stream.Close()
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if msg != nil && len(msg.ToolCalls) > 0 {
			return true, nil
		}
	}
}

// maxSteps returns the ReAct loop cap: the agent's max_steps when set,
// otherwise config.DefaultMaxSteps.
func (r *ReactAgent) maxSteps() int {
	if r.config != nil && r.config.MaxSteps > 0 {
		return r.config.MaxSteps
	}
	return config.DefaultMaxSteps
}

// buildMessages assembles the system prompt, the prior conversation turns and
// the current user prompt into the message list for one model call.
func (r *ReactAgent) buildMessages(history []*schema.Message, prompt string) []*schema.Message {
	messages := make([]*schema.Message, 0, len(history)+2)
	if system := r.systemPrompt(config.GetConfig()); system != "" {
		messages = append(messages, schema.SystemMessage(system))
	}
	for _, msg := range history {
		if msg != nil {
			messages = append(messages, msg)
		}
	}
	return append(messages, schema.UserMessage(prompt))
}

// Run runs Agent with optimized output formatting
func (r *ReactAgent) Run(prompt string) error {
	if r.agent == nil {
		if err := r.Init(); err != nil {
			return err
		}
	}

	// Use ChatStream method with optimized output formatting
	return r.ChatStream(r.ctx, nil, prompt, func(chunk *StreamChunk) {
		switch chunk.Type {
		case "content":
			if chunk.Content != "" {
				fmt.Print(chunk.Content)
			} else {
				// Empty content marks the end of the stream
				fmt.Println()
			}
		case "reasoning":
			// Dim italic: visibly separates the model's thinking from the answer
			if chunk.Content != "" {
				fmt.Printf("\x1b[2;3m%s\x1b[0m", chunk.Content)
			}
		case "tool_start":
			fmt.Printf("\n🔧 Using tool: %s\n", chunk.Tool)
		case "tool_end":
			fmt.Printf("✅ Tool completed: %s\n", chunk.Tool)
		case "error":
			fmt.Printf("\n❌ Error: %s\n", chunk.Content)
		}
	}, func(toolInfo interface{}) {
		// Show detailed tool information with optimized formatting
		switch info := toolInfo.(type) {
		case ToolCallInfo:
			switch info.Type {
			case "start":
				fmt.Printf("   📋 %s\n", info.Name)
				if info.Arguments != "" {
					// Format arguments for better readability
					formattedArgs := formatArguments(info.Arguments)
					fmt.Printf("   📝 Arguments: %s\n", formattedArgs)
				}
			case "end":
				if info.Result != "" {
					// Format result for better readability
					formattedResult := formatResult(info.Result)
					fmt.Printf("   📊 Result: %s\n", formattedResult)
				} else {
					fmt.Printf("   ✅ Completed successfully\n")
				}
			case "error":
				fmt.Printf("   ❌ Error: %s\n", info.Error)
			}
		default:
			// Format general callback information
			formattedInfo := formatGeneralInfo(fmt.Sprintf("%v", info))
			if formattedInfo != "" {
				fmt.Printf("   ℹ️  %s\n", formattedInfo)
			}
		}
	})
}

// Chat performs conversation, returns response content
func (r *ReactAgent) Chat(ctx context.Context, history []*schema.Message, prompt string) (output string, retErr error) {
	if r.agent == nil {
		if err := r.Init(); err != nil {
			return "", err
		}
	}

	// Create messages
	messages := r.buildMessages(history, prompt)
	ctx = r.withActiveModel(ctx)
	finish, hookErr := r.startPluginHooks(ctx, prompt)
	if hookErr != nil {
		return "", hookErr
	}
	defer func() { retErr = finish(output, retErr) }()

	// Use Generate method for synchronous call
	response, err := r.agent.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("Chat failed: %w", err)
	}

	return response.Content, nil
}

// ChatWithCallback performs conversation with streaming output and callback support
func (r *ReactAgent) ChatWithCallback(ctx context.Context, history []*schema.Message, prompt string, callback func(interface{})) (output string, retErr error) {
	if r.agent == nil {
		if err := r.Init(); err != nil {
			return "", err
		}
	}

	// Create messages
	messages := r.buildMessages(history, prompt)
	ctx = r.withActiveModel(ctx)
	finish, hookErr := r.startPluginHooks(ctx, prompt)
	if hookErr != nil {
		return "", hookErr
	}
	defer func() { retErr = finish(output, retErr) }()

	// If no callback function, use Generate method directly
	if callback == nil {
		response, err := r.agent.Generate(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("Chat failed: %w", err)
		}
		return response.Content, nil
	}

	// A display callback must never abort the run.
	display := callback
	callback = func(msg interface{}) { safeCallback(func() { display(msg) }) }

	// Create tool call callback handler
	toolCallback := &ToolCallCallback{callback: callback}

	// Use Stream method for streaming call and add callback handler via agent.WithComposeOptions
	sr, err := r.agent.Stream(ctx, messages, agent.WithComposeOptions(compose.WithCallbacks(toolCallback)))
	if err != nil {
		return "", fmt.Errorf("Stream failed: %w", err)
	}
	defer sr.Close()

	// Split reasoning sections from visible content while streaming
	tf := NewThinkFilter()
	var result strings.Builder
	emit := func(visible, thought string) {
		if thought != "" && callback != nil {
			callback(map[string]interface{}{"type": "reasoning", "content": thought})
		}
		if visible != "" {
			if callback != nil {
				callback(visible)
			}
			result.WriteString(visible)
		}
	}
	for {
		msg, err := sr.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Stream ends
				break
			}
			return "", fmt.Errorf("failed to receive stream message: %w", err)
		}

		// Reasoning-capable models (e.g. deepseek-reasoner) stream their
		// thinking in ReasoningContent instead of <think> tags in Content.
		if msg.ReasoningContent != "" && callback != nil {
			callback(map[string]interface{}{"type": "reasoning", "content": msg.ReasoningContent})
		}
		if msg.Content != "" {
			emit(tf.Feed(msg.Content))
		}
	}
	emit(tf.Flush())

	return result.String(), nil
}

// ChatStream performs streaming conversation, handles streaming output via chunk callback
func (r *ReactAgent) ChatStream(ctx context.Context, history []*schema.Message, prompt string, chunkCallback func(*StreamChunk), toolCallback func(interface{})) (retErr error) {
	if r.agent == nil {
		if err := r.Init(); err != nil {
			return err
		}
	}

	// Create messages
	messages := r.buildMessages(history, prompt)
	ctx = r.withActiveModel(ctx)
	finish, hookErr := r.startPluginHooks(ctx, prompt)
	if hookErr != nil {
		return hookErr
	}
	var hookOutput strings.Builder
	collectHookOutput := config.GetConfig().HasPluginHook("after_agent")
	defer func() { retErr = finish(hookOutput.String(), retErr) }()

	// Log the messages for debugging
	logger.Info("AGENT", "Starting ChatStream with messages:")
	logger.Debug("AGENT", fmt.Sprintf("System prompt: %s", r.config.System))
	logger.Debug("AGENT", fmt.Sprintf("User prompt: %s", prompt))

	if chunkCallback != nil {
		display := chunkCallback
		chunkCallback = func(chunk *StreamChunk) { safeCallback(func() { display(chunk) }) }
	}

	// Create tool call callback handler
	var toolCallCallback *ToolCallCallback
	if toolCallback != nil {
		toolCallCallback = &ToolCallCallback{callback: toolCallback}
		logger.Debug("AGENT", "Tool callback configured")
	} else {
		logger.Debug("AGENT", "No tool callback provided")
	}

	// Use Stream method for streaming call
	var sr *schema.StreamReader[*schema.Message]
	var err error

	logger.Info("AGENT", "Calling agent.Stream method")
	if toolCallCallback != nil {
		logger.Debug("AGENT", "Using stream with callbacks")
		sr, err = r.agent.Stream(ctx, messages, agent.WithComposeOptions(compose.WithCallbacks(toolCallCallback)))
	} else {
		logger.Debug("AGENT", "Using stream without callbacks")
		sr, err = r.agent.Stream(ctx, messages)
	}
	if err != nil {
		logger.Error("AGENT", fmt.Sprintf("Stream call failed: %v", err))
		if chunkCallback != nil {
			chunkCallback(&StreamChunk{
				Type:    "error",
				Content: fmt.Sprintf("Stream failed: %v", err),
			})
		}
		return fmt.Errorf("Stream failed: %w", err)
	}
	defer sr.Close()

	logger.Info("AGENT", "Stream call successful, starting to read messages")

	// Read streaming response, stripping <think>...</think> (thinking)
	// sections some gateways embed in delta.content.
	tf := NewThinkFilter()
	messageCount := 0
	for {
		msg, err := sr.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Info("AGENT", fmt.Sprintf("Stream ended after receiving %d messages", messageCount))
				// Flush buffered content, then send the end marker
				if chunkCallback != nil {
					restVisible, restThought := tf.Flush()
					if restThought != "" {
						chunkCallback(&StreamChunk{Type: "reasoning", Content: restThought})
					}
					if restVisible != "" {
						chunkCallback(&StreamChunk{Type: "content", Content: restVisible})
					}
					chunkCallback(&StreamChunk{
						Type:    "content",
						Content: "",
					})
				}
				break
			}
			logger.Error("AGENT", fmt.Sprintf("Failed to receive stream message: %v", err))
			if chunkCallback != nil {
				chunkCallback(&StreamChunk{
					Type:    "error",
					Content: fmt.Sprintf("failed to receive stream message: %v", err),
				})
			}
			return fmt.Errorf("failed to receive stream message: %w", err)
		}

		if collectHookOutput {
			hookOutput.WriteString(msg.Content)
		}
		messageCount++
		logger.Debug("AGENT", fmt.Sprintf("Received message #%d: role=%s, tool_calls=%d, content_len=%d",
			messageCount, msg.Role, len(msg.ToolCalls), len(msg.Content)))

		// Log if message contains tool calls
		if len(msg.ToolCalls) > 0 {
			logger.Info("AGENT", fmt.Sprintf("Message contains %d tool calls", len(msg.ToolCalls)))
			for i, tc := range msg.ToolCalls {
				logger.Info("AGENT", fmt.Sprintf("Tool call #%d: name='%s', args_len=%d", i+1, tc.Function.Name, len(tc.Function.Arguments)))
				logger.Debug("AGENT", fmt.Sprintf("Tool call #%d full struct: %+v", i+1, tc))
				logger.Debug("AGENT", fmt.Sprintf("Tool call #%d ID: %s", i+1, tc.ID))
				logger.Debug("AGENT", fmt.Sprintf("Tool call #%d Function struct: %+v", i+1, tc.Function))
				if tc.Function.Name != "" {
					logger.Debug("AGENT", fmt.Sprintf("Tool call #%d arguments: %s", i+1, tc.Function.Arguments))
				}
			}
		}

		// Reasoning-capable models (e.g. deepseek-reasoner) stream their
		// thinking in ReasoningContent instead of <think> tags in Content.
		if chunkCallback != nil && msg.ReasoningContent != "" {
			chunkCallback(&StreamChunk{
				Type:    "reasoning",
				Content: msg.ReasoningContent,
			})
		}

		// Send content chunk, splitting reasoning from the visible answer
		if chunkCallback != nil && msg.Content != "" {
			visible, thought := tf.Feed(msg.Content)
			if thought != "" {
				chunkCallback(&StreamChunk{
					Type:    "reasoning",
					Content: thought,
				})
			}
			if visible != "" {
				chunkCallback(&StreamChunk{
					Type:    "content",
					Content: visible,
				})
			}
		}
	}

	return nil
}

// withActiveModel annotates ctx with the model and tools this agent runs, so
// nested agents (the subagent tool) inherit the current capabilities —
// including a runtime /model switch, which is already folded into r.config by
// the caller.
func (r *ReactAgent) withActiveModel(ctx context.Context) context.Context {
	name := r.config.Model
	if name == "" {
		if cfg := config.GetConfig(); cfg != nil {
			name = cfg.DefaultModelName()
		}
	}
	ctx = tools.WithActiveModel(ctx, name)
	return tools.WithActiveTools(ctx, r.config.Tools)
}

// createModel creates model
func (r *ReactAgent) createModel() (model.ToolCallingChatModel, error) {
	// Get global configuration from Factory
	globalCfg := config.GetConfig()
	if globalCfg == nil {
		return nil, fmt.Errorf("global configuration not initialized")
	}

	// Create model factory
	factory := models.NewFactory(globalCfg)

	// An agent without its own model follows the default model.
	modelName := r.config.Model
	if modelName == "" {
		modelName = globalCfg.DefaultModelName()
	}
	if modelName == "" {
		return nil, fmt.Errorf("no model configured for agent %q", r.agentName)
	}

	// Use factory to create model
	return factory.CreateChatModel(r.ctx, modelName)
}

// createToolsConfig creates tools configuration
func (r *ReactAgent) createToolsConfig() (compose.ToolsNodeConfig, error) {
	// Create tools configuration
	toolsConfig := compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{},
	}

	// Get global configuration
	globalCfg := config.GetConfig()
	if globalCfg == nil {
		return toolsConfig, fmt.Errorf("global configuration not initialized")
	}

	// Log agent configuration
	logger.Info("AGENT", fmt.Sprintf("Initializing agent: %s", r.agentName))
	logger.Debug("AGENT", fmt.Sprintf("Regular tools: %v", r.config.Tools))
	logger.Debug("AGENT", fmt.Sprintf("MCP servers: %v", r.config.MCPServers))

	// Add regular tools
	for _, toolName := range r.config.Tools {
		// Get tool configuration
		toolCfg, ok := globalCfg.Tools[toolName]
		if !ok {
			return toolsConfig, fmt.Errorf("tool configuration does not exist: %s", toolName)
		}

		// Create tool instance
		toolInstance, err := createTool(toolName, toolCfg)
		if err != nil {
			logger.Error("AGENT", fmt.Sprintf("Failed to create tool %s: %v", toolName, err))
			return toolsConfig, err
		}

		logger.Debug("AGENT", fmt.Sprintf("Added regular tool: %s", toolName))
		toolsConfig.Tools = append(toolsConfig.Tools, toolInstance)
	}

	// Add MCP tools
	if len(r.config.MCPServers) > 0 {
		logger.Info("AGENT", "Looking for MCP tools...")
		mcpManager := mcp.GetGlobalManager()
		if mcpManager != nil {
			// Get current Agent's MCP tools
			mcpTools, err := mcpManager.GetToolsForAgent(r.agentName)
			if err != nil {
				logger.Error("AGENT", fmt.Sprintf("Failed to get MCP tools: %v", err))
				return toolsConfig, fmt.Errorf("failed to get MCP tools: %w", err)
			}

			logger.Info("AGENT", fmt.Sprintf("Found %d MCP tools", len(mcpTools)))
			// Add MCP tools to tools configuration
			for _, mcpTool := range mcpTools {
				if info, err := mcpTool.Info(context.Background()); err == nil {
					logger.Info("AGENT", fmt.Sprintf("Added MCP tool: %s - %s", info.Name, info.Desc))
				} else {
					logger.Warn("AGENT", fmt.Sprintf("Added MCP tool but couldn't get info: %v", err))
				}
				toolsConfig.Tools = append(toolsConfig.Tools, mcpTool)
			}
		} else {
			logger.Warn("AGENT", "MCP manager is nil")
		}
	} else {
		logger.Info("AGENT", "No MCP servers configured")
	}

	if err := validateToolNames(r.ctx, toolsConfig.Tools); err != nil {
		return toolsConfig, err
	}

	// Wrap every tool (built-in and MCP) with the approval policy.
	wrapped, err := wrapApprovals(toolsConfig.Tools)
	if err != nil {
		return toolsConfig, err
	}
	toolsConfig.Tools = wrapped

	// A failing tool must not end the turn: the failure is reported back to
	// the model as a result so it can try something else.
	toolsConfig.Tools = wrapRecoverable(r.wrapPluginHooks(toolsConfig.Tools))

	logger.Info("AGENT", fmt.Sprintf("Total tools available: %d", len(toolsConfig.Tools)))
	return toolsConfig, nil
}
