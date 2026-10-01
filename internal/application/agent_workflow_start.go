package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var ErrAgentWorkflowStartDenied = errors.New("agent workflow: start is outside current admitted authority")

type AgentWorkflowIntentOwner interface {
	ResolveAgentWorkflowTarget(context.Context, *intentsv1.ExecuteIntentRequest) (app.AgentWorkflowTarget, error)
	ExecuteIntent(context.Context, *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error)
	AuthorizeAgentWorkflowObservation(context.Context, string, string) error
}

type AgentWorkflowStartRequest struct {
	RunID  string
	Intent *intentsv1.ExecuteIntentRequest
	Skill  agentskills.SkillPin
	StepID string
}

type AgentGovernedWorkflowService struct {
	Evidence      AgentWorkflowEvidence
	Current       runstate.AdmissionRechecker
	Agents        *agentstore.Store
	ResolveTenant func(string) uuid.UUID
	Gate          *agentgate.Gate
	Context       AgentDiscoveryContext
	Intents       AgentWorkflowIntentOwner
	Controls      app.WorkflowControlReader
	Versions      version.Store
	Now           func() time.Time
}

// RequestStart uses only the existing admitted exact intent path. The first
// policy admits one agent-to-workflow edge and no nested agent invocation.
func (s AgentGovernedWorkflowService) RequestStart(ctx context.Context, request AgentWorkflowStartRequest) (*intentsv1.ExecutionReceipt, error) {
	_, target, err := s.authorize(ctx, request)
	if err != nil {
		return nil, err
	}
	response, err := s.Intents.ExecuteIntent(ctx, request.Intent)
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetExecution() == nil {
		return nil, ErrAgentWorkflowStartDenied
	}
	receipt := response.GetExecution()
	id, err := uuid.Parse(receipt.GetInstanceId())
	if err != nil || id == uuid.Nil {
		return nil, ErrAgentWorkflowStartDenied
	}
	// The workflow owner's durable projection confirms the returned identity;
	// a submitted intent or a chat post never becomes an execution receipt.
	principal, _ := trust.FromContext(ctx)
	control, err := s.Controls.ReadWorkflowControlRecord(ctx, principal.Tenant(), id)
	if err != nil {
		return nil, err
	}
	if control.Instance.WorkflowID != target.Selection.WorkflowID || control.Instance.CompiledPlanHash != target.Selection.Plan.Digest() ||
		control.Instance.CorrelationID != target.CorrelationID {
		return nil, ErrAgentWorkflowStartDenied
	}
	return receipt, nil
}

// Inspect repeats current admission, skill and intent authorization before
// reporting the workflow owner's observed state for the exact bound intent.
func (s AgentGovernedWorkflowService) Inspect(ctx context.Context, request AgentWorkflowStartRequest, instanceID uuid.UUID) (app.WorkflowControlRecord, error) {
	record, _, err := s.authorizeCurrent(ctx, request, "workflows.read_status")
	if err != nil {
		return app.WorkflowControlRecord{}, err
	}
	if s.Versions == nil || instanceID == uuid.Nil {
		return app.WorkflowControlRecord{}, ErrAgentWorkflowStartDenied
	}
	principal, _ := trust.FromContext(ctx)
	control, err := s.Controls.ReadWorkflowControlRecord(ctx, principal.Tenant(), instanceID)
	if err != nil {
		return app.WorkflowControlRecord{}, err
	}
	if err = s.Intents.AuthorizeAgentWorkflowObservation(ctx, request.Intent.GetIntentId(), control.Instance.CorrelationID); err != nil {
		return app.WorkflowControlRecord{}, err
	}
	published, err := version.Resolve(s.Versions, control.Instance.WorkflowID, version.Pin{CompiledPlanDigest: control.Instance.CompiledPlanHash})
	if err != nil {
		return app.WorkflowControlRecord{}, err
	}
	plan, err := workflow.DecodeCanonicalPlan(published.CanonicalPlanBytes)
	if err != nil {
		return app.WorkflowControlRecord{}, err
	}
	if plan.Digest() != control.Instance.CompiledPlanHash || plan.Version != control.Instance.WorkflowVersion {
		return app.WorkflowControlRecord{}, ErrAgentWorkflowStartDenied
	}
	if err = s.checkEdge(ctx, record, control.Instance.WorkflowID, plan); err != nil {
		return app.WorkflowControlRecord{}, err
	}
	return control, nil
}

func (s AgentGovernedWorkflowService) authorize(ctx context.Context, request AgentWorkflowStartRequest) (agentrun.Record, app.AgentWorkflowTarget, error) {
	record, _, err := s.authorizeCurrent(ctx, request, "workflows.request_start")
	if err != nil {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, err
	}
	target, err := s.Intents.ResolveAgentWorkflowTarget(ctx, request.Intent)
	if err != nil {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, err
	}
	if err = s.checkEdge(ctx, record, target.Selection.WorkflowID, target.Selection.Plan); err != nil {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, err
	}
	return record, target, nil
}

func (s AgentGovernedWorkflowService) authorizeCurrent(ctx context.Context, request AgentWorkflowStartRequest, capabilityID string) (agentrun.Record, app.AgentWorkflowTarget, error) {
	denied := func() (agentrun.Record, app.AgentWorkflowTarget, error) {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, ErrAgentWorkflowStartDenied
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || s.Evidence == nil || s.Current == nil ||
		s.Agents == nil || s.ResolveTenant == nil || s.Gate == nil || s.Context == nil || s.Intents == nil || s.Controls == nil || s.Now == nil ||
		request.Intent == nil || request.Intent.GetIntentId() == "" || request.Intent.GetIdempotencyKey() == "" || request.RunID == "" {
		return denied()
	}
	record, err := s.Evidence.GetAdmission(ctx, principal.Tenant().String(), request.RunID)
	if err != nil {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, err
	}
	if agentrun.ValidateAdmissionRecord(record) != nil || record.Decision != agentrun.DecisionAccepted || record.Request.Principal.Mode != agentrun.ModeOnBehalfOf ||
		record.Request.Principal.InvokerID != principal.Subject() || record.Request.Source.TenantID != principal.Tenant().String() || record.Request.Source.Kind == agentrun.SourceWorkflow {
		return denied()
	}
	if err = s.Current.Recheck(ctx, record.Request.Source.TenantID, record.ID); err != nil {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, err
	}
	user, subjects, fields, err := s.Context.Resolve(ctx, principal, record.Request.Purpose)
	if err != nil {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, err
	}
	if user.Principal == nil || user.Principal.Fingerprint() != principal.Fingerprint() || user.Roles == nil {
		return denied()
	}
	call := agentgate.CallRequest{User: user, Subjects: subjects, Fields: fields, Skill: request.Skill, Purpose: record.Request.Purpose, At: s.Now().UTC(),
		Actor: agentgate.AgentActor{RunID: record.ID, AgentVersion: record.Authority.Agent.Version, InstallationID: record.Authority.InstallationID, StepID: request.StepID}}
	decision, err := s.Gate.Authorize(ctx, call)
	if err != nil {
		return agentrun.Record{}, app.AgentWorkflowTarget{}, err
	}
	allowed := false
	for _, cap := range decision.Capabilities {
		if cap.Allowed && cap.Capability.ID == capabilityID && cap.Capability.Version == 1 {
			allowed = true
		}
	}
	if !allowed {
		return denied()
	}
	return record, app.AgentWorkflowTarget{}, nil
}

func (s AgentGovernedWorkflowService) checkEdge(ctx context.Context, record agentrun.Record, workflowID string, plan *workflow.CompiledWorkflow) error {
	if plan == nil || workflowID == "" {
		return ErrAgentWorkflowStartDenied
	}
	// Cause-depth is one in this release: a started workflow cannot invoke an
	// agent. This closed edge policy cannot be bypassed by changing CauseID.
	for _, node := range plan.Nodes {
		if node.Capability != nil && node.Capability.ID == "agents.invoke" {
			return ErrAgentWorkflowStartDenied
		}
	}
	version, err := strconv.ParseUint(record.Authority.Agent.Version, 10, 64)
	if err != nil || version == 0 {
		return ErrAgentWorkflowStartDenied
	}
	manifest, err := s.Agents.ManifestVersion(ctx, s.ResolveTenant(record.Request.Source.TenantID), record.Authority.Agent.AgentID, version)
	if err != nil {
		return err
	}
	if !agentWorkflowEdgeAllowed(manifest, record.Authority.Agent.Digest, workflowID, plan) {
		return ErrAgentWorkflowStartDenied
	}
	return nil
}

func agentWorkflowEdgeAllowed(manifest agentmanifest.Manifest, digest, workflowID string, plan *workflow.CompiledWorkflow) bool {
	actual, err := manifest.Digest()
	if err != nil || actual != digest || plan == nil {
		return false
	}
	for _, edge := range manifest.ToolCeiling {
		// Workflow pins use the bare SHA-256 value; manifest references use the
		// algorithm-qualified form of the same content digest.
		planDigest := plan.Digest()
		if !strings.HasPrefix(planDigest, "sha256:") {
			planDigest = "sha256:" + planDigest
		}
		if edge.ID == "workflows.request_start:"+workflowID && edge.Version == uint64(plan.Version) && edge.Digest == planDigest {
			return true
		}
	}
	return false
}
