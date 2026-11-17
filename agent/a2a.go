package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tk103331/oni3/config"
	a2aprotocol "github.com/tk103331/oni3/protocol/a2a/client"
	"github.com/tk103331/oni3/tools"
)

func init() {
	tools.SetRemoteAgentRunner(runRemoteAgent)
}

var (
	remoteClientsMu sync.Mutex
	remoteClients   = make(map[string]*a2aprotocol.Client)
)

type remoteAgentResult struct {
	Agent     string   `json:"agent"`
	State     string   `json:"state"`
	TaskID    string   `json:"task_id,omitempty"`
	ContextID string   `json:"context_id,omitempty"`
	Answer    string   `json:"answer,omitempty"`
	Message   string   `json:"message,omitempty"`
	Options   []string `json:"options,omitempty"`
}

func runRemoteAgent(ctx context.Context, spec tools.RemoteAgentSpec) (string, error) {
	cfg := config.GetConfig()
	if cfg == nil {
		return "", fmt.Errorf("global configuration not initialized")
	}
	remote, ok := cfg.A2AAgents[spec.Agent]
	if !ok {
		return "", fmt.Errorf("A2A agent is not configured: %s", spec.Agent)
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := remoteClient(runCtx, spec.Agent, remote)
	if err != nil {
		return remoteStop(ctx, runCtx, nil, "", spec, "", err)
	}

	taskID := strings.TrimSpace(spec.TaskID)
	contextID := strings.TrimSpace(spec.ContextID)
	prompt := strings.TrimSpace(spec.Prompt)

	if taskID != "" {
		current, err := client.GetTask(runCtx, taskID)
		if err != nil {
			return remoteStop(ctx, runCtx, client, taskID, spec, contextID, err)
		}
		if current.ContextID != "" {
			contextID = current.ContextID
		}
		state := a2aprotocol.NormalizeState(current.Status.State)
		switch {
		case a2aprotocol.IsInputRequired(state):
			if prompt == "" {
				return formatTaskResult(spec.Agent, client, current)
			}
			task, message, err := client.SendMessage(runCtx, prompt, contextID, taskID)
			if err != nil {
				return remoteStop(ctx, runCtx, client, taskID, spec, contextID, err)
			}
			return finishRemote(ctx, runCtx, spec, client, task, message, taskID, contextID)
		case a2aprotocol.IsTerminal(state):
			if prompt == "" {
				return formatTaskResult(spec.Agent, client, current)
			}
			task, message, err := client.SendMessage(runCtx, prompt, contextID, "")
			if err != nil {
				return remoteStop(ctx, runCtx, client, taskID, spec, contextID, err)
			}
			return finishRemote(ctx, runCtx, spec, client, task, message, taskID, contextID)
		case state == a2aprotocol.StateSubmitted, state == a2aprotocol.StateWorking:
			if prompt != "" {
				return "", fmt.Errorf("remote task %s is still running; omit prompt to poll it", taskID)
			}
			task, err := client.WaitTask(runCtx, taskID, spec.PollInterval)
			if err != nil {
				return remoteStop(ctx, runCtx, client, taskID, spec, contextID, err)
			}
			return formatTaskResult(spec.Agent, client, task)
		default:
			return "", fmt.Errorf("remote task %s has unsupported state %q", taskID, current.Status.State)
		}
	}

	if prompt == "" {
		return "", fmt.Errorf("prompt is required for a new remote task")
	}
	task, message, err := client.SendMessage(runCtx, prompt, contextID, "")
	if err != nil {
		return remoteStop(ctx, runCtx, client, "", spec, contextID, err)
	}
	return finishRemote(ctx, runCtx, spec, client, task, message, "", contextID)
}

func finishRemote(parent, runCtx context.Context, spec tools.RemoteAgentSpec, client *a2aprotocol.Client, task *a2aprotocol.Task, message *a2aprotocol.Message, taskID, contextID string) (string, error) {
	if message != nil {
		return formatMessageResult(spec.Agent, message)
	}
	if task == nil {
		return "", fmt.Errorf("remote A2A agent returned neither a task nor a message")
	}
	taskID = task.ID
	if task.ContextID != "" {
		contextID = task.ContextID
	}
	state := a2aprotocol.NormalizeState(task.Status.State)
	if !a2aprotocol.IsTerminal(state) && !a2aprotocol.IsInputRequired(state) {
		waited, err := client.WaitTask(runCtx, taskID, spec.PollInterval)
		if err != nil {
			return remoteStop(parent, runCtx, client, taskID, spec, contextID, err)
		}
		task = waited
	}
	return formatTaskResult(spec.Agent, client, task)
}

func formatTaskResult(agentName string, client *a2aprotocol.Client, task *a2aprotocol.Task) (string, error) {
	result := remoteAgentResult{
		Agent:     agentName,
		State:     a2aprotocol.NormalizeState(task.Status.State),
		TaskID:    task.ID,
		ContextID: task.ContextID,
	}
	switch {
	case a2aprotocol.IsInputRequired(result.State):
		result.Message = client.Question(task)
		result.Options = client.Options(task)
	case a2aprotocol.IsTerminal(result.State):
		if result.State == a2aprotocol.StateCompleted {
			result.Answer = client.Answer(task)
			if strings.TrimSpace(result.Answer) == "" {
				result.Message = "remote agent completed without text output"
			}
		} else {
			result.Message = client.Answer(task)
			if strings.TrimSpace(result.Message) == "" {
				result.Message = "remote task ended in state " + result.State
			}
		}
	default:
		result.Message = "remote task is still running"
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func formatMessageResult(agentName string, message *a2aprotocol.Message) (string, error) {
	result := remoteAgentResult{
		Agent:     agentName,
		State:     a2aprotocol.StateCompleted,
		TaskID:    message.TaskID,
		ContextID: message.ContextID,
		Answer:    strings.TrimSpace(message.Text()),
	}
	if result.Answer == "" {
		result.Message = "remote agent returned no text output"
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func remoteStop(parent, runCtx context.Context, client *a2aprotocol.Client, taskID string, spec tools.RemoteAgentSpec, contextID string, err error) (string, error) {
	if parent.Err() != nil {
		cancelRemoteTask(client, taskID)
		return "", parent.Err()
	}
	if runCtx.Err() == context.DeadlineExceeded {
		if taskID == "" {
			return "", fmt.Errorf("remote A2A call timed out after %s", spec.Timeout)
		}
		result := remoteAgentResult{
			Agent:     spec.Agent,
			State:     "timeout",
			TaskID:    taskID,
			ContextID: contextID,
			Message:   "remote task is still running; call again with task_id to poll it",
		}
		data, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return "", marshalErr
		}
		return string(data), nil
	}
	return "", err
}

func cancelRemoteTask(client *a2aprotocol.Client, taskID string) {
	if client == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = client.CancelTask(ctx, taskID)
}

func remoteClient(ctx context.Context, name string, remote config.A2AAgent) (*a2aprotocol.Client, error) {
	token := strings.TrimSpace(remote.Token)
	if token == "" && strings.TrimSpace(remote.TokenEnv) != "" {
		token = strings.TrimSpace(os.Getenv(remote.TokenEnv))
	}
	sum := sha256.Sum256([]byte(token))
	key := name + "|" + remote.CardURL + "|" + hex.EncodeToString(sum[:])

	remoteClientsMu.Lock()
	client := remoteClients[key]
	remoteClientsMu.Unlock()
	if client != nil {
		return client, nil
	}

	client, err := a2aprotocol.Discover(ctx, name, remote.CardURL, token, nil)
	if err != nil {
		return nil, err
	}
	remoteClientsMu.Lock()
	if existing := remoteClients[key]; existing != nil {
		client = existing
	} else {
		remoteClients[key] = client
	}
	remoteClientsMu.Unlock()
	return client, nil
}
