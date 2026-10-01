package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
)

type AgentActionRuntimeConfig struct {
	Runtime        *CommonAgentRuntime
	Core           dbport.Beginner
	CoreSchema     string
	Manifests      CommonAgentManifestReader
	Routes         PersonaModelRoutePolicyReader
	Contracts      *AgentModelPolicyRegistry
	Cell           *app.Cell
	TenantUUID     func(values.TenantId) uuid.UUID
	Now            func() time.Time
	Classification model.ClassificationLabel
	RetentionClass string
}

// AgentActionRuntimeAuthority derives every pin from the accepted inbox,
// current source/grant/installation owners and the run-owned protected output.
// Sponsored runs are refused until an independent human audience grant exists.
type AgentActionRuntimeAuthority struct{ cfg AgentActionRuntimeConfig }

func NewAgentActionRuntimeAuthority(cfg AgentActionRuntimeConfig) (*AgentActionRuntimeAuthority, error) {
	if cfg.Runtime == nil || cfg.Core == nil || cfg.CoreSchema == "" || cfg.Manifests == nil || cfg.Routes == nil || cfg.Contracts == nil || cfg.Cell == nil || cfg.Cell.Service == nil || cfg.Cell.Definitions == nil || cfg.TenantUUID == nil || cfg.Now == nil || !cfg.Classification.Valid() || cfg.RetentionClass == "" {
		return nil, ErrAgentActionApproval
	}
	return &AgentActionRuntimeAuthority{cfg: cfg}, nil
}

func canonicalAgentActionProposal(proposal AgentActionCompileRequest) AgentActionCompileRequest {
	if proposal.Request != nil {
		proposal.Request = proto.Clone(proposal.Request).(*intentsv1.CreateIntentRequest)
		proposal.Request.Initiator = nil
		proposal.Request.IdempotencyKey = "agent:validate"
	}
	return proposal
}

func agentActionProposalDigest(tenant, user string, proposal AgentActionCompileRequest) (string, error) {
	encoded, err := json.Marshal(struct {
		Tenant, Subject string
		Proposal        AgentActionCompileRequest
	}{tenant, user, canonicalAgentActionProposal(proposal)})
	if err != nil {
		return "", err
	}
	return taskModelDigest(append([]byte("hcm-agent-business-intent/v1\x00"), encoded...)), nil
}

func sameAgentActionProposal(a, b AgentActionCompileRequest) bool {
	a, b = canonicalAgentActionProposal(a), canonicalAgentActionProposal(b)
	return proto.Equal(a.Definition, b.Definition) && proto.Equal(a.Request, b.Request) && a.AgentRunID == b.AgentRunID && a.AgentVersionRef == b.AgentVersionRef && a.ModelDigest == b.ModelDigest && reflect.DeepEqual(a.Sources, b.Sources) && reflect.DeepEqual(a.Taint, b.Taint) && a.Uncertainty == b.Uncertainty
}

func (a *AgentActionRuntimeAuthority) validateRetained(ctx context.Context, record agentrun.Record, run runstate.Run, content []byte) (agentActionValidatedOutput, error) {
	var output agentActionValidatedOutput
	if a == nil || ctx == nil || strictAgentActionJSON(content, &output) != nil || record.Decision != agentrun.DecisionAccepted || agentrun.ValidateAdmissionRecord(record) != nil || commonAgentCheckExecutionIdentity(record, run) != nil || record.Authority.Principal.Mode != agentrun.ModeOnBehalfOf || record.Authority.Principal.InvokerID == "" || output.Kind != "business-intent-proposal/v1" || output.TenantID != run.TenantID || output.RunID != run.ID || output.ForUser != record.Authority.Principal.InvokerID || output.RequestDigest != record.RequestDigest || output.Proposal.AgentRunID != run.ID || output.Proposal.AgentVersionRef != run.AgentID+"@"+run.AgentVersion || !agentActionHasResultCheckpoint(run, output.ModelResultDigest) {
		return output, ErrAgentActionApproval
	}
	if err := a.cfg.Runtime.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return output, err
	}
	schema, route, err := a.currentActionRoute(ctx, record, run)
	if err != nil || output.Schema != schema || output.Proposal.ModelDigest != "sha256:"+route.Route.Pin.Primary.ProfileDigest || !reflect.DeepEqual(output.Proposal.Sources, []string{record.Request.Source.Ref, record.Request.Context.SnapshotID}) || !reflect.DeepEqual(output.Proposal.Taint, []string{"AGENT_DERIVED"}) {
		return output, ErrAgentActionApproval
	}
	if output.Proposal.Definition == nil || output.Proposal.Request == nil || output.Proposal.Request.Initiator != nil || output.Proposal.Request.OriginEventRef != nil || output.Proposal.Request.ParentIntentId != nil {
		return output, ErrAgentActionApproval
	}
	return output, nil
}

// ReadAgentActionProposal exposes only an exact OBO invoker's validated output.
// Its owner-scoped artifact authorization follows current independent grants.
func (a *AgentActionRuntimeAuthority) ReadAgentActionProposal(ctx context.Context, tenant, user, runID string) (AgentActionCompileRequest, error) {
	if a == nil || ctx == nil {
		return AgentActionCompileRequest{}, ErrAgentActionApproval
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant().String() != tenant || p.Subject() != user {
		return AgentActionCompileRequest{}, ErrAgentActionApproval
	}
	record, err := a.cfg.Runtime.GetAdmission(ctx, tenant, runID)
	if err != nil || record.Decision != agentrun.DecisionAccepted || record.Authority.Principal.Mode != agentrun.ModeOnBehalfOf || record.Authority.Principal.InvokerID != user {
		return AgentActionCompileRequest{}, ErrAgentActionApproval
	}
	if err := a.cfg.Runtime.Recheck(ctx, tenant, runID); err != nil {
		return AgentActionCompileRequest{}, err
	}
	run, err := a.cfg.Runtime.GetRun(ctx, tenant, runID)
	if err != nil || run.State == runstate.StateCancelled || run.State == runstate.StateFailed || run.State == runstate.StateExpired || run.State == runstate.StateNeedsRepair {
		return AgentActionCompileRequest{}, ErrAgentActionApproval
	}
	var outputRef CommonAgentOutput
	for _, checkpoint := range run.Checkpoints {
		if checkpoint.Phase == runstate.PhaseValidation {
			if outputRef.Ref != "" && outputRef != (CommonAgentOutput{Ref: checkpoint.Ref, Digest: checkpoint.Digest}) {
				return AgentActionCompileRequest{}, ErrAgentActionApproval
			}
			outputRef = CommonAgentOutput{Ref: checkpoint.Ref, Digest: checkpoint.Digest}
		}
	}
	id, ok := strings.CutPrefix(outputRef.Ref, "artifact:")
	if !ok || !artifacts.ValidContentID(id) || outputRef.Digest != "sha256:"+id {
		return AgentActionCompileRequest{}, ErrAgentActionApproval
	}
	tenantID := a.cfg.TenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return AgentActionCompileRequest{}, ErrAgentActionApproval
	}
	tx, err := a.cfg.Core.Begin(ctx)
	if err != nil {
		return AgentActionCompileRequest{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return AgentActionCompileRequest{}, err
	}
	content, _, err := artifacts.Retrieve(ctx, tx, artifacts.Schema(a.cfg.CoreSchema), tenantID, id, artifacts.RetrievalAuthorization{Purpose: record.Request.Purpose, AllowedClassifications: []model.ClassificationLabel{a.cfg.Classification}, Scope: artifacts.SubjectScope{AllowedOwnerRefs: []string{runID}}, RequestedBy: user, ExpiresAt: record.Request.Deadline}, a.cfg.Now().UTC())
	if err != nil {
		var denied artifacts.ErrRetrievalDenied
		if errors.As(err, &denied) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return AgentActionCompileRequest{}, commitErr
			}
		}
		return AgentActionCompileRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AgentActionCompileRequest{}, err
	}
	output, err := a.validateRetained(ctx, record, run, content)
	if err != nil {
		return AgentActionCompileRequest{}, err
	}
	return output.Proposal, nil
}

func (a *AgentActionRuntimeAuthority) VerifyAgentAction(ctx context.Context, pin AgentActionAuthorityPin) error {
	proposal, err := a.ReadAgentActionProposal(ctx, pin.TenantID, pin.ForUser, pin.RunID)
	if err != nil {
		return err
	}
	digest, err := agentActionProposalDigest(pin.TenantID, pin.ForUser, proposal)
	if err != nil || digest != pin.DraftDigest || proposal.AgentVersionRef != pin.AgentVersionRef || proposal.ModelDigest != pin.ModelDigest {
		return ErrAgentActionApproval
	}
	if pin.Proposal != nil && !sameAgentActionProposal(proposal, *pin.Proposal) {
		return ErrAgentActionApproval
	}
	if pin.IntentID != "" {
		current, err := a.cfg.Cell.Service.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: pin.IntentID})
		if err != nil {
			return err
		}
		instance := current.GetIntent()
		if instance.GetInitiator().GetPrincipalId() != pin.ForUser || !proto.Equal(instance.GetDefinition(), proposal.Request.Definition) || !proto.Equal(instance.GetRequest(), proposal.Request.Request) || !reflect.DeepEqual(instance.GetSubjects(), proposal.Request.Subjects) {
			return ErrAgentActionApproval
		}
		var origin agentActionOrigin
		raw, ok := strings.CutPrefix(instance.GetOriginEventRef(), "agent-run:")
		if !ok || json.Unmarshal([]byte(raw), &origin) != nil || origin.RunID != pin.RunID || origin.ProposalDigest != pin.DraftDigest {
			return ErrAgentActionApproval
		}
		if a.cfg.Cell.Journey == nil {
			return ErrAgentActionApproval
		}
		detail, err := a.cfg.Cell.Journey.Inspect(ctx, pin.IntentID)
		if err != nil {
			return err
		}
		if detail.Summary.ProposalRevisionID != pin.ProposalRevisionID || detail.Summary.MaterialDigest != pin.ProposalDigest {
			return ErrAgentActionApproval
		}
	}
	return nil
}
