package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var (
	errMissingPunchProposalConfig = errors.New("missing punch proposal: incomplete composition")
	errMissingPunchProposalInput  = errors.New("missing punch proposal: invalid input")
	errMissingPunchProposalBind   = errors.New("missing punch proposal: binding mismatch")
)

// MissingPunchProposalCapture is server-owned input for one immutable proposal.
// The request values are copied from the validated workflow request; callers
// cannot provide a material digest or approval identity.
type MissingPunchProposalCapture struct {
	Request                 clockservice.MissingPunchWorkflowRequest
	Definition              intent.Definition
	Digester                intent.Digester
	IDs                     intent.IDSource
	Clock                   intent.Clock
	Tenant                  values.TenantId
	OrganizationScopeID     string
	LegalEntityID           string
	ControlSnapshots        intent.ControlSnapshots
	CreatedBy               intent.PrincipalReference
	RequiredApprovals       []intent.RequiredApproval
	Obligations             []intent.Obligation
	Purpose                 intent.PurposeDecision
	Revalidation            intent.RevalidationPlan
	SourceAuthorityDecision string
	IntentType              string
}

// CaptureMissingPunchProposal creates a canonical ProposalRevision from the
// six typed workflow inputs and the original workflow lineage. It uses
// intent.NewProposalRevision, so the returned material digest is minted by the
// existing intent canonicalization rather than assembled in this adapter.
func CaptureMissingPunchProposal(c MissingPunchProposalCapture) (intent.ProposalRevision, error) {
	r := c.Request
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.RequestID) == "" || strings.TrimSpace(r.SessionID) == "" ||
		strings.TrimSpace(r.OriginalObservationID) == "" || strings.TrimSpace(r.Reason) == "" ||
		strings.TrimSpace(r.OriginalWorkflowInstanceRef) == "" || r.ExpectedRevision == 0 ||
		r.ClaimedOutAt.IsZero() || r.At.IsZero() || c.Tenant == "" || c.Digester == nil || c.Clock == nil || strings.TrimSpace(c.SourceAuthorityDecision) == "" || strings.TrimSpace(c.IntentType) == "" {
		return intent.ProposalRevision{}, errMissingPunchProposalInput
	}
	if c.Tenant != values.TenantId(r.TenantID) || r.Action != "REQUEST" {
		return intent.ProposalRevision{}, errMissingPunchProposalBind
	}
	if err := uuid.Validate(r.OriginalWorkflowInstanceRef); err != nil {
		return intent.ProposalRevision{}, fmt.Errorf("%w: original workflow instance: %v", errMissingPunchProposalInput, err)
	}
	resource, err := values.NewResourceKey(c.Tenant, values.Kind("time_session"), r.SessionID)
	if err != nil {
		return intent.ProposalRevision{}, fmt.Errorf("%w: session resource: %v", errMissingPunchProposalInput, err)
	}
	baseline, err := values.NewSequenceRevision("time_session:"+r.SessionID, r.ExpectedRevision)
	if err != nil {
		return intent.ProposalRevision{}, fmt.Errorf("%w: session revision: %v", errMissingPunchProposalInput, err)
	}
	subject := intent.SubjectReference{Kind: "TIME_SESSION", SubjectID: r.SessionID, AuthorityDomain: "TIME"}
	proposalTime, err := values.NewOpenInstantInterval(values.NewInstant(r.ClaimedOutAt.UTC()))
	if err != nil {
		return intent.ProposalRevision{}, fmt.Errorf("%w: effective time: %v", errMissingPunchProposalInput, err)
	}
	intentID := missingPunchProposalIntentID(r)
	spec := intent.ProposalSpec{
		IntentID:            intentID.String(),
		Revision:            1,
		Tenant:              c.Tenant,
		OrganizationScopeID: c.OrganizationScopeID,
		LegalEntityID:       c.LegalEntityID,
		Subjects:            []intent.SubjectReference{subject},
		EffectiveTime:       proposalTime,
		CurrentState:        []intent.StateAssertion{{Subject: subject, ResourceKey: resource, FieldPath: "session_revision", CanonicalText: fmt.Sprint(r.ExpectedRevision)}},
		ProposedState:       []intent.StateAssertion{{Subject: subject, ResourceKey: resource, FieldPath: "missing_punch.clock_out", CanonicalText: r.ClaimedOutAt.UTC().Format(time.RFC3339Nano)}, {Subject: subject, ResourceKey: resource, FieldPath: "missing_punch.reason", CanonicalText: r.Reason}, {Subject: subject, ResourceKey: resource, FieldPath: "missing_punch.original_workflow_instance", CanonicalText: r.OriginalWorkflowInstanceRef}},
		Writes:              []intent.PlannedWrite{{Subject: subject, ResourceKey: resource, FieldPath: "missing_punch.clock_out", ProposedCanonicalText: r.ClaimedOutAt.UTC().Format(time.RFC3339Nano), SourceAuthorityDecision: c.SourceAuthorityDecision, ExpectedRevision: baseline}},
		RequiredApprovals:   append([]intent.RequiredApproval(nil), c.RequiredApprovals...),
		Obligations:         append([]intent.Obligation(nil), c.Obligations...),
		Purpose:             c.Purpose,
		Revalidation:        c.Revalidation,
		ControlSnapshots:    c.ControlSnapshots,
		CreatedBy:           c.CreatedBy,
	}
	if spec.OrganizationScopeID == "" || spec.LegalEntityID == "" {
		return intent.ProposalRevision{}, errMissingPunchProposalInput
	}
	return intent.NewProposalRevision(spec, c.Definition, c.Digester, c.IDs, c.Clock)
}

// MissingPunchProposalStore durably materializes and reloads complete
// proposals. It uses the core intent-control database; the time request store
// remains a separate database and is never joined to this transaction.
type MissingPunchProposalStore struct {
	DB         execute.Beginner
	Tenant     func(values.TenantId) (uuid.UUID, error)
	Digester   intent.Digester
	ProducedBy string
}

// BindMissingPunchProposalStart creates the server-side proposal source used
// when an approval continuation needs to carry the exact immutable revision.
// Facts are required ports: the runtime resolves approval and supersession
// from durable stores instead of trusting fields on a request.
func BindMissingPunchProposalStart(base runtime.StartRequest, p intent.ProposalRevision, facts runtime.ProposalFacts, approvals runtime.ApprovalFacts, resolveTenant func(values.TenantId) (uuid.UUID, error), intentType string) (runtime.StartRequest, error) {
	if p.ProposalRevisionID == "" || p.IntentID == "" || p.MaterialDigest.Digest == "" || p.Tenant == "" || facts == nil || approvals == nil || base.TenantID == uuid.Nil || resolveTenant == nil || strings.TrimSpace(intentType) == "" {
		return runtime.StartRequest{}, errMissingPunchProposalBind
	}
	tenantID, err := resolveTenant(p.Tenant)
	if err != nil || tenantID != base.TenantID {
		return runtime.StartRequest{}, errMissingPunchProposalBind
	}
	base.Proposal = runtime.ProposalBinding{Revision: p}
	base.Source = &runtime.StartSource{Kind: runtime.StartSourceProposal, IntentType: intentType, Proposal: &base.Proposal}
	base.ProposalFacts = facts
	base.ApprovalFacts = approvals
	return base, nil
}

// PersistMissingPunchProposal writes an immutable proposal revision and reads
// it back in the same transaction, proving the stored bytes and digest match.
func (s MissingPunchProposalStore) PersistMissingPunchProposal(ctx context.Context, p intent.ProposalRevision) error {
	if s.DB == nil || s.Tenant == nil || s.Digester == nil || strings.TrimSpace(s.ProducedBy) == "" {
		return errMissingPunchProposalConfig
	}
	tenant, err := s.Tenant(p.Tenant)
	if err != nil || tenant == uuid.Nil {
		return errMissingPunchProposalBind
	}
	intentID, err := uuid.Parse(p.IntentID)
	if err != nil || intentID == uuid.Nil {
		return fmt.Errorf("%w: intent id must be a UUID", errMissingPunchProposalInput)
	}
	payload, err := intentcontrol.EncodeFullProposal(p)
	if err != nil {
		return fmt.Errorf("encode proposal: %w", err)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	_, err = (intentcontrol.RevisionStore{}).Materialize(ctx, tx, intentcontrol.Revision{TenantID: tenant, IntentID: intentID, Revision: p.Revision, ProposalDigest: p.MaterialDigest.Digest, MaterialDigest: p.MaterialDigest.Digest, SchemaRef: critical.SchemaRefFullProposalSnapshot, Payload: payload, ProducedBy: s.ProducedBy, ProducedAt: p.CreatedAt.Time()})
	if err != nil {
		return err
	}
	loaded, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenant, intentID, p.Revision)
	if err != nil {
		return err
	}
	decoded, err := intentcontrol.DecodeFullProposal(loaded.Payload, proposalDigestVerifier{s.Digester})
	if err != nil || decoded.MaterialDigest.Digest != p.MaterialDigest.Digest || string(decoded.MaterialPayload().WireBytes) != string(p.MaterialPayload().WireBytes) {
		return errMissingPunchProposalBind
	}
	return tx.Commit(ctx)
}

// LoadMissingPunchProposal reloads the complete immutable proposal and asks
// the existing decoder to verify its canonical digest before returning it.
func (s MissingPunchProposalStore) LoadMissingPunchProposal(ctx context.Context, tenant values.TenantId, intentID uuid.UUID, revision uint64) (intent.ProposalRevision, error) {
	if s.DB == nil || s.Tenant == nil || s.Digester == nil || tenant == "" || intentID == uuid.Nil || revision == 0 {
		return intent.ProposalRevision{}, errMissingPunchProposalConfig
	}
	tenantID, err := s.Tenant(tenant)
	if err != nil || tenantID == uuid.Nil {
		return intent.ProposalRevision{}, errMissingPunchProposalBind
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return intent.ProposalRevision{}, err
	}
	row, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenantID, intentID, revision)
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	decoded, err := intentcontrol.DecodeFullProposal(row.Payload, proposalDigestVerifier{s.Digester})
	if err != nil || decoded.Tenant != tenant || decoded.IntentID != intentID.String() || decoded.Revision != revision || row.MaterialDigest != decoded.MaterialDigest.Digest {
		return intent.ProposalRevision{}, errMissingPunchProposalBind
	}
	return decoded, nil
}

type proposalDigestVerifier struct{ intent.Digester }

func (v proposalDigestVerifier) VerifyProposalDigest(p intent.ProposalRevision) error {
	ref, err := v.ProposalDigest(p)
	if err != nil {
		return err
	}
	if ref.Digest != p.MaterialDigest.Digest {
		return errMissingPunchProposalBind
	}
	return nil
}
