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
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
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
	return newStartAgentTaskModeRequestWithDocuments(text, mode, nil)
}

func newStartAgentTaskModeRequestWithDocuments(text string, mode agentv1.AgentStartMode, references []agentdocref.Reference) (*agentv1.StartAgentTaskRequest, bool) {
	return newStartAgentTaskModeRequestWithSelection(text, mode, references, "")
}

func newStartAgentTaskModeRequestWithSelection(text string, mode agentv1.AgentStartMode, references []agentdocref.Reference, personaID string) (*agentv1.StartAgentTaskRequest, bool) {
	request, ok := newStartAgentTaskRequest(text)
	if !ok || mode == agentv1.AgentStartMode_AGENT_START_MODE_UNSPECIFIED || agentdocref.Validate(references, agentdocref.MaxRequestReferences) != nil || strings.TrimSpace(personaID) != personaID || len(personaID) > 512 || !utf8.ValidString(personaID) {
		return nil, false
	}
	for _, reference := range references {
		if reference.VersionMode != agentdocref.ModeLatestPublished || reference.PinnedVersion != 0 {
			return nil, false
		}
		request.DocumentReferences = append(request.DocumentReferences, &agentv1.AgentDocumentReference{
			DocumentId: reference.DocumentID, VersionMode: agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED,
			SectionAnchor: reference.SectionAnchor, Label: reference.Label,
		})
	}
	request.Mode = mode
	request.PersonaId = personaID
	return request, true
}

func projectAgentTask(task *agentv1.AgentTaskProjection) (productui.AgentTask, bool) {
	if task == nil || strings.TrimSpace(task.GetTaskId()) == "" {
		return productui.AgentTask{}, false
	}
	state := normalizeAgentTaskState(task.GetState())
	result := productui.AgentTask{ID: strings.TrimSpace(task.GetTaskId()), Version: task.GetVersion(), Title: strings.TrimSpace(task.GetPrompt()), Goal: strings.TrimSpace(task.GetPrompt()), State: state, ResultPreview: strings.TrimSpace(task.GetResultPreview())}
	result.FailureReason, result.Retryable = strings.TrimSpace(task.GetFailureSummary()), task.GetRetryable()
	result.AnsweringAgentID, result.AnsweringAgentDisplayName, result.AnsweringAgentVersion = strings.TrimSpace(task.GetAnsweringAgentId()), strings.TrimSpace(task.GetAnsweringAgentDisplayName()), strings.TrimSpace(task.GetAnsweringAgentVersion())
	if state == productui.AgentTaskCompleted {
		result.AnswerText = result.ResultPreview
	}
	if at := task.GetCreatedAt(); at != nil && at.IsValid() {
		result.CreatedAt = at.AsTime()
	}
	if at := task.GetUpdatedAt(); at != nil && at.IsValid() {
		result.UpdatedAt = at.AsTime()
	}
	for _, reference := range task.GetDocumentReferences() {
		if reference == nil || strings.TrimSpace(reference.GetDocumentId()) == "" {
			continue
		}
		result.Documents = append(result.Documents, productui.AgentTaskDocumentReference{DocumentID: strings.TrimSpace(reference.GetDocumentId()), Label: strings.TrimSpace(reference.GetLabel()), SectionAnchor: strings.TrimSpace(reference.GetSectionAnchor())})
	}
	for _, reference := range task.GetUsedDocumentReferences() {
		if reference == nil || strings.TrimSpace(reference.GetDocumentId()) == "" {
			continue
		}
		result.UsedDocuments = append(result.UsedDocuments, productui.AgentTaskDocumentReference{DocumentID: strings.TrimSpace(reference.GetDocumentId()), Label: strings.TrimSpace(reference.GetLabel()), SectionAnchor: strings.TrimSpace(reference.GetSectionAnchor())})
	}
	switch task.GetDocumentUsageState() {
	case agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_NONE:
		result.DocumentUsageState = productui.AgentDocumentUsageNone
	case agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_USED:
		result.DocumentUsageState = productui.AgentDocumentUsageUsed
	default:
		result.DocumentUsageState = productui.AgentDocumentUsageUnknown
	}
	for _, omission := range task.GetDocumentOmissions() {
		if omission != nil {
			result.DocumentOmissions = append(result.DocumentOmissions, productui.AgentTaskDocumentOmission{Label: strings.TrimSpace(omission.GetLabel()), Reason: strings.TrimSpace(omission.GetReason())})
		}
	}
	for _, step := range task.GetSteps() {
		if step == nil {
			continue
		}
		projected := productui.AgentTaskStep{Name: strings.TrimSpace(step.GetKind()), State: normalizeAgentStepState(step.GetStatus()), FailureReason: strings.TrimSpace(step.GetFailureSummary())}
		if at := step.GetStartedAt(); at != nil && at.IsValid() {
			projected.StartedAt = at.AsTime()
		}
		if at := step.GetFinishedAt(); at != nil && at.IsValid() {
			projected.FinishedAt = at.AsTime()
		}
		result.Steps = append(result.Steps, projected)
	}
	return result, true
}

func normalizeAgentStepState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "in progress", "working", "running":
		return "running"
	case "not started", "pending":
		return "pending"
	case "needs approval", "awaiting approval", "awaiting_approval":
		return "awaiting_approval"
	case "completed", "complete":
		return "completed"
	case "failed", "failure":
		return "failed"
	case "waiting":
		return "waiting"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
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
	task, err := b.StartTaskModeWithDocuments(ctx, text, mode, nil)
	return task.ID, err
}

func (b *agentServiceBinding) StartTaskModeWithDocuments(ctx context.Context, text string, mode agentv1.AgentStartMode, references []agentdocref.Reference) (productui.AgentTask, error) {
	return b.StartTaskModeWithSelection(ctx, text, mode, references, "")
}

func (b *agentServiceBinding) StartTaskModeWithSelection(ctx context.Context, text string, mode agentv1.AgentStartMode, references []agentdocref.Reference, personaID string) (productui.AgentTask, error) {
	request, ok := newStartAgentTaskModeRequestWithSelection(text, mode, references, personaID)
	if !ok {
		return productui.AgentTask{}, errAgentPromptGone
	}
	if b == nil || b.client == nil {
		return productui.AgentTask{}, errAgentTask
	}
	response, err := b.client.StartAgentTask(b.rpcContext(ctx), request)
	if err != nil {
		return productui.AgentTask{}, err
	}
	if response == nil {
		return productui.AgentTask{}, errAgentTask
	}
	if task, ok := projectAgentTask(response.GetTask()); ok {
		return task, nil
	}
	if strings.TrimSpace(response.GetTaskId()) == "" {
		return productui.AgentTask{}, errAgentTask
	}
	return productui.AgentTask{ID: strings.TrimSpace(response.GetTaskId()), Version: response.GetVersion(), State: normalizeAgentTaskState(response.GetState()), Title: strings.TrimSpace(text), Goal: strings.TrimSpace(text)}, nil
}

func (b *agentServiceBinding) ListTasks(ctx context.Context) ([]productui.AgentTask, error) {
	if b == nil || b.client == nil {
		return nil, errAgentTask
	}
	response, err := b.client.ListAgentTasks(b.rpcContext(ctx), &agentv1.ListAgentTasksRequest{})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errAgentTask
	}
	result := make([]productui.AgentTask, 0, len(response.GetTasks()))
	for _, task := range response.GetTasks() {
		if item, ok := projectAgentTask(task); ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (b *agentServiceBinding) GetTask(ctx context.Context, taskID string) (productui.AgentTask, error) {
	if b == nil || b.client == nil || strings.TrimSpace(taskID) == "" {
		return productui.AgentTask{}, errAgentTask
	}
	response, err := b.client.GetAgentTask(b.rpcContext(ctx), &agentv1.GetAgentTaskRequest{TaskId: strings.TrimSpace(taskID)})
	if err != nil {
		return productui.AgentTask{}, err
	}
	if response == nil {
		return productui.AgentTask{}, errAgentTask
	}
	task, ok := projectAgentTask(response.GetTask())
	if !ok || task.ID != strings.TrimSpace(taskID) {
		return productui.AgentTask{}, errAgentTask
	}
	return task, nil
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
