package agui

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// History converts all messages except the final user prompt into Eino chat
// history, and returns the final prompt separately.
func History(input RunAgentInput) ([]*schema.Message, string, error) {
	if len(input.Messages) == 0 {
		return nil, "", fmt.Errorf("messages must contain at least one user message")
	}

	lastUser := -1
	for i := len(input.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(input.Messages[i].Role, "user") {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return nil, "", fmt.Errorf("messages must contain a user message")
	}
	prompt := strings.TrimSpace(input.Messages[lastUser].Text())
	if prompt == "" {
		return nil, "", fmt.Errorf("the last user message is empty")
	}

	history := make([]*schema.Message, 0, lastUser)
	for i := 0; i < lastUser; i++ {
		message, err := toSchema(input.Messages[i])
		if err != nil {
			return nil, "", err
		}
		if message != nil {
			history = append(history, message)
		}
	}
	return history, prompt, nil
}

func toSchema(message Message) (*schema.Message, error) {
	text := message.Text()
	switch strings.ToLower(strings.TrimSpace(message.Role)) {
	case "user":
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}
		return schema.UserMessage(text), nil
	case "assistant":
		calls := make([]schema.ToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			calls = append(calls, schema.ToolCall{
				ID:   call.ID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				},
			})
		}
		if strings.TrimSpace(text) == "" && len(calls) == 0 {
			return nil, nil
		}
		return schema.AssistantMessage(text, calls), nil
	case "system", "developer":
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}
		return schema.SystemMessage(text), nil
	case "tool":
		if strings.TrimSpace(message.ToolCallID) == "" {
			return nil, fmt.Errorf("tool message is missing toolCallId")
		}
		return schema.ToolMessage(text, message.ToolCallID), nil
	default:
		return nil, fmt.Errorf("unsupported message role: %s", message.Role)
	}
}
