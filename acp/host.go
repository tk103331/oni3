package acp

import (
	"context"
	"fmt"
	"strings"

	"github.com/coder/acp-go-sdk"
	"github.com/tk103331/oni3/tools"
	"github.com/tk103331/oni3/tools/approval"
)

// acpHost bridges the interactive tools (ask_user, todo_list, tool approval)
// to the ACP client. Without it those tools fail with "interactive sessions
// only"; with it, Zed-style clients answer questions, show the task list as
// an agent plan, and approve dangerous tool calls through the client UI.
type acpHost struct {
	svc     *Service
	session *acpSession
}

var _ tools.Host = (*acpHost)(nil)

// AskUser forwards a question to the client. ACP has no dedicated
// question/request_outcome method yet, so the question surfaces as a
// permission-style choice via session/request_permission when the connection
// supports it, and the free-text answer comes back as the selected option's
// id payload. Clients that deny (or a cancelled turn) return "".
func (h *acpHost) AskUser(ctx context.Context, question string, options []string) (string, error) {
	if h.svc.conn == nil {
		return "", nil
	}
	// Reuse the permission request: options become selectable answers. The
	// "typed answer" case (no options) falls back to a single "provide
	// answer" prompt that most clients render as a question dialog.
	permOptions := make([]acp.PermissionOption, 0, len(options))
	for i, opt := range options {
		permOptions = append(permOptions, acp.PermissionOption{
			Kind:     acp.PermissionOptionKindAllowOnce,
			Name:     opt,
			OptionId: acp.PermissionOptionId(fmt.Sprintf("answer-%d", i)),
		})
	}
	if len(permOptions) == 0 {
		permOptions = []acp.PermissionOption{{
			Kind:     acp.PermissionOptionKindAllowOnce,
			Name:     "回答",
			OptionId: "answer-0",
		}}
	}
	resp, err := h.svc.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(h.session.id),
		Options:   permOptions,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "question",
			Title:      strPtr(question),
			Kind:       kindPtr(acp.ToolKindOther),
		},
	})
	if err != nil {
		// A cancelled turn or unsupported client is not a tool error: the
		// model gets an empty answer and decides how to continue.
		return "", nil
	}
	if resp.Outcome.Selected != nil {
		idx := optionIndex(resp.Outcome.Selected.OptionId)
		if idx >= 0 && idx < len(options) {
			return options[idx], nil
		}
		// The option carries the answer text itself when the client lets the
		// user type it (custom option names).
		return string(resp.Outcome.Selected.OptionId), nil
	}
	return "", nil // cancelled or rejected
}

// UpdateTodos maps the todo_list tool onto the ACP agent plan.
func (h *acpHost) UpdateTodos(ctx context.Context, items []tools.TodoItem) error {
	if h.svc.conn == nil {
		return nil
	}
	entries := make([]acp.PlanEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, acp.PlanEntry{
			Content:  item.Content,
			Priority: acp.PlanEntryPriorityMedium,
			Status:   planStatus(item.Status),
		})
	}
	if len(entries) == 0 {
		return nil
	}
	h.svc.sendUpdate(ctx, h.session.id, acp.UpdatePlan(entries...))
	return nil
}

// ApproveTool asks the client whether a dangerous tool call may run.
// session/request_permission is the ACP-native way to do this; a denied or
// cancelled request reports false and the tool returns its "denied" result.
func (h *acpHost) ApproveTool(ctx context.Context, req approval.Request) (bool, error) {
	if h.svc.conn == nil {
		// No client to ask: keep the tool usable (the process sandbox rules
		// still apply), matching the non-interactive behavior.
		return true, nil
	}
	title := req.Tool
	if summary := strings.TrimSpace(req.Summary); summary != "" {
		title = req.Tool + " " + summary
	} else if detail := strings.TrimSpace(req.Detail); detail != "" {
		title = req.Tool + " " + detail
	}
	resp, err := h.svc.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(h.session.id),
		Options: []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowOnce, Name: "允许", OptionId: "allow-once"},
			{Kind: acp.PermissionOptionKindAllowAlways, Name: "总是允许", OptionId: "allow-always"},
			{Kind: acp.PermissionOptionKindRejectOnce, Name: "拒绝", OptionId: "reject-once"},
			{Kind: acp.PermissionOptionKindRejectAlways, Name: "总是拒绝", OptionId: "reject-always"},
		},
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "approval",
			Title:      strPtr(title),
			Kind:       kindPtr(acp.ToolKindExecute),
		},
	})
	if err != nil {
		// Connection error during a permission check is not the tool's
		// business: treat it as unanswered and deny, so a half-dead client
		// cannot accidentally authorize anything.
		return false, nil
	}
	selected := resp.Outcome.Selected != nil &&
		(strings.HasPrefix(string(resp.Outcome.Selected.OptionId), "allow"))
	return selected, nil
}

// planStatus converts a todo status to a plan entry status.
func planStatus(status string) acp.PlanEntryStatus {
	switch status {
	case tools.TodoInProgress:
		return acp.PlanEntryStatusInProgress
	case tools.TodoCompleted:
		return acp.PlanEntryStatusCompleted
	default:
		return acp.PlanEntryStatusPending
	}
}

// optionIndex extracts the numeric suffix of an answer-N option id.
func optionIndex(id acp.PermissionOptionId) int {
	var n int
	if _, err := fmt.Sscanf(string(id), "answer-%d", &n); err != nil {
		return -1
	}
	return n
}

func strPtr(s string) *string { return &s }

func kindPtr(k acp.ToolKind) *acp.ToolKind { return &k }
