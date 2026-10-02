package agents

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// DocumentStarter is the typed task-start extension. Tenant and subject still
// come only from the verified principal supplied by the transport.
type DocumentStarter interface {
	StartTaskWithDocuments(context.Context, *trust.Principal, string, agentclient.StartMode, []agentdocref.Reference) (agentclient.StartedTask, error)
}

// TaskSelectionStarter is the complete start port for document and persona
// selections. The verified principal remains the only source of user scope.
type TaskSelectionStarter interface {
	StartTaskWithSelection(context.Context, *trust.Principal, string, agentclient.StartMode, []agentdocref.Reference, string) (agentclient.StartedTask, error)
}

// TaskReader supplies owner-scoped durable projections for get and list.
type TaskReader interface {
	GetAgentTask(context.Context, *trust.Principal, string) (agentrun.AgentTask, error)
	ListAgentTasks(context.Context, *trust.Principal) ([]agentrun.AgentTask, error)
}

func documentReferences(in []*agentv1.AgentDocumentReference) ([]agentdocref.Reference, error) {
	refs := make([]agentdocref.Reference, 0, len(in))
	for _, item := range in {
		if item == nil {
			return nil, agentrun.ErrDocumentReferenceInvalid
		}
		mode := agentdocref.VersionMode("")
		switch item.GetVersionMode() {
		case agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_PINNED:
			mode = agentdocref.ModePinned
		case agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED:
			mode = agentdocref.ModeLatestPublished
		default:
			return nil, agentrun.ErrDocumentReferenceInvalid
		}
		refs = append(refs, agentdocref.Reference{DocumentID: item.GetDocumentId(), VersionMode: mode, PinnedVersion: item.GetPinnedVersion(), SectionAnchor: item.GetSectionAnchor(), Label: item.GetLabel()})
	}
	if agentdocref.Validate(refs, agentdocref.MaxRequestReferences) != nil {
		return nil, agentrun.ErrDocumentReferenceInvalid
	}
	return refs, nil
}

func projectDocumentReferences(refs []agentdocref.Reference) []*agentv1.AgentDocumentReference {
	out := make([]*agentv1.AgentDocumentReference, 0, len(refs))
	for _, ref := range refs {
		mode := agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_UNSPECIFIED
		switch ref.VersionMode {
		case agentdocref.ModePinned:
			mode = agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_PINNED
		case agentdocref.ModeLatestPublished:
			mode = agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED
		}
		out = append(out, &agentv1.AgentDocumentReference{DocumentId: ref.DocumentID, VersionMode: mode, PinnedVersion: ref.PinnedVersion, SectionAnchor: ref.SectionAnchor, Label: ref.Label})
	}
	return out
}

func projectAgentTask(task agentrun.AgentTask) *agentv1.AgentTaskProjection {
	omissions := make([]*agentv1.AgentDocumentOmission, 0, len(task.Plan.DocumentOmissions))
	for _, omission := range task.Plan.DocumentOmissions {
		omissions = append(omissions, &agentv1.AgentDocumentOmission{Label: omission.Label, Reason: omission.Reason})
	}
	failureSummary, retryable := taskFailurePresentation(task.FailureCode)
	steps := make([]*agentv1.AgentTaskStepProjection, 0, len(task.Plan.Steps))
	for _, step := range task.Plan.Steps {
		projected := &agentv1.AgentTaskStepProjection{Kind: taskStepKind(step.Type), Status: taskStepStatus(step.State)}
		if !step.StartedAt.IsZero() {
			projected.StartedAt = timestamppb.New(step.StartedAt)
		}
		if !step.FinishedAt.IsZero() {
			projected.FinishedAt = timestamppb.New(step.FinishedAt)
		}
		if step.State == agentrun.StepFailed {
			projected.FailureSummary = "This step could not be completed."
		}
		steps = append(steps, projected)
	}
	agent := agentrun.TaskAgentIdentity{ID: "general-agent", DisplayName: "General agent", Version: "hcm-agent-self-service/v1"}
	if task.Plan.AnsweringAgent != nil && task.Plan.AnsweringAgent.Validate() == nil {
		agent = *task.Plan.AnsweringAgent
	}
	usedDocuments, usageState := projectUsedDocumentReferences(task)
	projection := &agentv1.AgentTaskProjection{
		TaskId: task.ID, State: string(task.State), Version: task.Version, Prompt: task.Goal,
		DocumentReferences: projectDocumentReferences(task.Plan.DocumentReferences), DocumentOmissions: omissions,
		ResultPreview: oneLinePreview(task.Ledger.AnswerText, 160), FailureSummary: failureSummary, Retryable: retryable, Steps: steps,
		AnsweringAgentId: agent.ID, AnsweringAgentDisplayName: agent.DisplayName, AnsweringAgentVersion: agent.Version,
		UsedDocumentReferences: usedDocuments, DocumentUsageState: usageState,
	}
	if !task.CreatedAt.IsZero() {
		projection.CreatedAt = timestamppb.New(task.CreatedAt)
	}
	if !task.UpdatedAt.IsZero() {
		projection.UpdatedAt = timestamppb.New(task.UpdatedAt)
	}
	return projection
}

const agentDocumentUsageLedgerMarker = "agent-document-usage:v1"

// projectUsedDocumentReferences treats a sealed ledger marker as the
// compatibility boundary. Tasks completed before citation capture have no
// marker and remain UNKNOWN; a marked task with no cited references is NONE.
func projectUsedDocumentReferences(task agentrun.AgentTask) ([]*agentv1.AgentDocumentReference, agentv1.AgentDocumentUsageState) {
	if task.State != agentrun.StateCompleted {
		return nil, agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_UNKNOWN
	}
	for i := len(task.Ledger.Entries) - 1; i >= 0; i-- {
		entry := task.Ledger.Entries[i]
		if entry.Kind != "STEP_RESULT" || !slices.Contains(entry.SourceIDs, agentDocumentUsageLedgerMarker) {
			continue
		}
		cited := make(map[string]struct{}, len(entry.SourceIDs))
		for _, sourceID := range entry.SourceIDs {
			if id, ok := citedDocumentID(sourceID); ok {
				cited[id] = struct{}{}
			}
		}
		used := make([]agentdocref.Reference, 0, len(cited))
		for _, ref := range task.Plan.DocumentReferences {
			if _, ok := cited[ref.DocumentID]; ok {
				used = append(used, ref)
			}
		}
		if len(used) == 0 {
			return nil, agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_NONE
		}
		return projectDocumentReferences(used), agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_USED
	}
	return nil, agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_UNKNOWN
}

func citedDocumentID(sourceID string) (string, bool) {
	id, ok := strings.CutPrefix(strings.TrimSpace(sourceID), "document:")
	if !ok {
		return "", false
	}
	if before, _, found := strings.Cut(id, "/version:"); found {
		id = before
	}
	if before, _, found := strings.Cut(id, "#"); found {
		id = before
	}
	id = strings.TrimSpace(id)
	return id, id != ""
}

func oneLinePreview(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func taskFailurePresentation(code string) (string, bool) {
	switch code {
	case "TASK_EXPIRED":
		return "The task took too long to finish.", true
	case "USER_CANCELLED":
		return "The task was cancelled.", false
	case "LOST_WAKE":
		return "The task stopped while waiting for an update.", true
	case "STEP_FAILED":
		return "A step could not be completed.", true
	case "AUTHORITY_USER_INACTIVE", "AUTHORITY_GRANT_REVOKED", "AUTHORITY_GRANT_EXPIRED", "AUTHORITY_TENANT_DISABLED":
		return "The task stopped because access changed.", false
	case "AMBIGUOUS_EFFECT":
		return "The task needs review before it can continue.", false
	case "":
		return "", false
	default:
		return "The task could not be completed.", false
	}
}

func taskStepKind(kind agentrun.StepType) string {
	switch kind {
	case agentrun.StepRead:
		return "Read information"
	case agentrun.StepAnalyze:
		return "Prepare answer"
	case agentrun.StepDraft:
		return "Prepare draft"
	case agentrun.StepCommunicate:
		return "Send message"
	case agentrun.StepSubmit:
		return "Submit request"
	case agentrun.StepVerify:
		return "Verify result"
	case agentrun.StepAskUser:
		return "Ask for information"
	case agentrun.StepWait:
		return "Wait for an update"
	default:
		return "Work on request"
	}
}

func taskStepStatus(status agentrun.PlanStepState) string {
	switch status {
	case agentrun.StepPending:
		return "Not started"
	case agentrun.StepRunning:
		return "In progress"
	case agentrun.StepWaiting:
		return "Waiting"
	case agentrun.StepAwaitingApproval:
		return "Needs approval"
	case agentrun.StepCompleted:
		return "Completed"
	case agentrun.StepFailed:
		return "Failed"
	default:
		return "Status unavailable"
	}
}

func taskReadError(err error) error {
	if errors.Is(err, agentrun.ErrNotFound) {
		return refuse(envelope.CodeNotFound, "agents.task_not_found", "the task was not found")
	}
	return refuse(envelope.CodeUnavailable, "agents.tasks_unavailable", "agent tasks are unavailable")
}
