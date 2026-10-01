package main

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// agentPromptMaxBytes mirrors internal/transport/agents.MaxPromptBytes; the
// wasm client cannot import the transport package, so a test on each side
// pins the same value. The server enforces it regardless.
const agentPromptMaxBytes = 8 << 10

var (
	errAgentSetting    = errors.New("agent setting was not saved")
	errAgentPromptGone = errors.New("agent task prompt is empty or too long")
	errAgentTask       = errors.New("agent task could not be started")
)

// agentServiceBinding is the browser adapter for hcmnext.agent.v1.AgentService.
// It rides the workspace gRPC tunnel every other page uses, so the page needs
// no extra network permission. The browser names no tenant, actor or user: the
// session bearer and the server decide who is acting.
type agentServiceBinding struct {
	cfg    journeyclient.Config
	client agentv1.AgentServiceClient
}

func newAgentServiceBinding(cfg journeyclient.Config, conn grpc.ClientConnInterface) *agentServiceBinding {
	binding := &agentServiceBinding{cfg: cfg}
	if conn != nil {
		binding.client = agentv1.NewAgentServiceClient(conn)
	}
	return binding
}

func (b *agentServiceBinding) rpcContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, journeyclient.AuthorizationHeader, journeyclient.BearerScheme+b.cfg.Bearer)
}

func newSetAgentsEnabledRequest(enabled bool) *agentv1.SetAgentsEnabledRequest {
	return &agentv1.SetAgentsEnabledRequest{Enabled: enabled}
}

// newStartAgentTaskRequest builds the start request from the composer text.
// It refuses text that is empty once trimmed, over the byte bound or not valid
// UTF-8, so the browser never sends what the server would refuse.
func newStartAgentTaskRequest(text string) (*agentv1.StartAgentTaskRequest, bool) {
	prompt := strings.TrimSpace(text)
	if prompt == "" || len(prompt) > agentPromptMaxBytes || !utf8.ValidString(prompt) {
		return nil, false
	}
	return &agentv1.StartAgentTaskRequest{Prompt: prompt}, true
}

func newStartAgentTaskModeRequest(text string, mode agentv1.AgentStartMode) (*agentv1.StartAgentTaskRequest, bool) {
	request, ok := newStartAgentTaskRequest(text)
	if !ok || mode == agentv1.AgentStartMode_AGENT_START_MODE_UNSPECIFIED {
		return nil, false
	}
	request.Mode = mode
	return request, true
}

// SetAgentsEnabled asks the server to turn agents on or off for the caller's
// tenant. Success needs the server to echo the value that was asked for.
func (b *agentServiceBinding) SetAgentsEnabled(ctx context.Context, enabled bool) error {
	if b == nil || b.client == nil {
		return errAgentSetting
	}
	response, err := b.client.SetAgentsEnabled(b.rpcContext(ctx), newSetAgentsEnabledRequest(enabled))
	if err != nil || response.GetEnabled() != enabled {
		return errAgentSetting
	}
	return nil
}

// StartTask starts one read-only task from the composer text and returns its
// id. Both composer buttons use it: neither can start a write.
func (b *agentServiceBinding) StartTask(ctx context.Context, text string) (string, error) {
	return b.StartTaskMode(ctx, text, agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER)
}

// StartTaskMode sends the explicit quick-answer or long-task policy choice.
func (b *agentServiceBinding) StartTaskMode(ctx context.Context, text string, mode agentv1.AgentStartMode) (string, error) {
	request, ok := newStartAgentTaskModeRequest(text, mode)
	if !ok {
		return "", errAgentPromptGone
	}
	if b == nil || b.client == nil {
		return "", errAgentTask
	}
	response, err := b.client.StartAgentTask(b.rpcContext(ctx), request)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(response.GetTaskId()) == "" {
		return "", errAgentTask
	}
	return response.GetTaskId(), nil
}

func (b *agentServiceBinding) ControlTask(ctx context.Context, taskID string, version uint64, action agentv1.AgentTaskAction) (string, error) {
	if b == nil || b.client == nil || strings.TrimSpace(taskID) == "" || version == 0 || action == agentv1.AgentTaskAction_AGENT_TASK_ACTION_UNSPECIFIED {
		return "", errAgentTask
	}
	response, err := b.client.ControlAgentTask(b.rpcContext(ctx), &agentv1.ControlAgentTaskRequest{TaskId: taskID, ExpectedVersion: version, Action: action})
	if err != nil {
		return "", err
	}
	if response == nil || strings.TrimSpace(response.GetTaskId()) == "" {
		return "", errAgentTask
	}
	return response.GetTaskId(), nil
}

// agentControlMessageKey selects the localized status message for a failed
// control. The page always announces a result and leaves retryable actions
// enabled after a refusal or conflict.
func agentControlMessageKey(err error) string {
	if err == nil {
		return "done"
	}
	switch status.Code(err) {
	case codes.Aborted:
		return "conflict"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "denied"
	case codes.FailedPrecondition:
		return "disabled"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "conflict"):
		return "conflict"
	case strings.Contains(message, "not authorized"), strings.Contains(message, "permission denied"):
		return "denied"
	case strings.Contains(message, "disabled"):
		return "disabled"
	default:
		return "failed"
	}
}

// agentStartMessageKey names which localized composer message (the
// data-msg-* attribute on #agents-composer-status) explains a failed start.
func agentStartMessageKey(err error) string {
	if errors.Is(err, errAgentPromptGone) {
		return "empty"
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return "empty"
	case codes.FailedPrecondition:
		return "disabled"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "denied"
	default:
		return "failed"
	}
}
