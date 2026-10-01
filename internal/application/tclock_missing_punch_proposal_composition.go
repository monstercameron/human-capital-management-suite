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
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// MissingPunchProposalBinderConfig contains only server-owned proposal
// capture, persistence, and durable-fact dependencies. In particular, there
// is no default authority or approval decision in this composition.
type MissingPunchProposalBinderConfig struct {
	Store                   MissingPunchProposalStore
	Definition              intent.Definition
	Digester                intent.Digester
	IDs                     intent.IDSource
	Clock                   intent.Clock
	Tenant                  values.TenantId
	ResolveTenant           func(values.TenantId) (uuid.UUID, error)
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
	ProposalFacts           runtime.ProposalFacts
	ApprovalFacts           runtime.ApprovalFacts
	Capture                 func(context.Context, clockservice.MissingPunchWorkflowRequest) (MissingPunchProposalCapture, error)
}

// NewMissingPunchProposalStartBinder returns the concrete StartBinder used by
// the missing-punch engine. It reloads the deterministic proposal on retries;
// only the first request mints and persists a ProposalRevision.
func NewMissingPunchProposalStartBinder(c MissingPunchProposalBinderConfig) (func(context.Context, clockservice.MissingPunchWorkflowRequest, runtime.StartRequest) (runtime.StartRequest, error), error) {
	if c.Store.DB == nil || c.Digester == nil || c.ResolveTenant == nil || c.Tenant == "" || strings.TrimSpace(c.IntentType) == "" || c.ProposalFacts == nil || c.ApprovalFacts == nil || c.Capture == nil {
		return nil, errMissingPunchProposalConfig
	}
	store := c.Store
	store.Digester = c.Digester
	store.Tenant = func(t values.TenantId) (uuid.UUID, error) { return c.ResolveTenant(t) }
	return func(ctx context.Context, req clockservice.MissingPunchWorkflowRequest, base runtime.StartRequest) (runtime.StartRequest, error) {
		if req.RequestID == "" || req.TenantID != string(c.Tenant) {
			return runtime.StartRequest{}, errMissingPunchProposalBind
		}
		intentID := missingPunchProposalIntentID(req)
		proposal, err := store.LoadMissingPunchProposal(ctx, c.Tenant, intentID, 1)
		if err != nil {
			if !errors.Is(err, intentcontrol.ErrNotFound) {
				return runtime.StartRequest{}, fmt.Errorf("load missing punch proposal: %w", err)
			}
			capture, captureErr := c.Capture(ctx, req)
			if captureErr != nil {
				return runtime.StartRequest{}, captureErr
			}
			if capture.Request.RequestID != req.RequestID || capture.Request.TenantID != req.TenantID {
				return runtime.StartRequest{}, errMissingPunchProposalBind
			}
			proposal, err = CaptureMissingPunchProposal(capture)
			if err != nil {
				return runtime.StartRequest{}, err
			}
			if proposal.IntentID != intentID.String() {
				return runtime.StartRequest{}, errMissingPunchProposalBind
			}
			if err := store.PersistMissingPunchProposal(ctx, proposal); err != nil {
				return runtime.StartRequest{}, err
			}
		}
		if proposal.IntentID != intentID.String() || proposal.Revision != 1 {
			return runtime.StartRequest{}, errMissingPunchProposalBind
		}
		if err := validateMissingPunchProposalReplay(proposal, req); err != nil {
			return runtime.StartRequest{}, err
		}
		return BindMissingPunchProposalStart(base, proposal, c.ProposalFacts, c.ApprovalFacts, c.ResolveTenant, c.IntentType)
	}, nil
}

func validateMissingPunchProposalReplay(p intent.ProposalRevision, req clockservice.MissingPunchWorkflowRequest) error {
	if len(p.Subjects) != 1 || p.Subjects[0].SubjectID != req.SessionID || len(p.CurrentState) != 1 || p.CurrentState[0].CanonicalText != fmt.Sprint(req.ExpectedRevision) || len(p.ProposedState) != 3 {
		return errMissingPunchProposalBind
	}
	want := map[string]string{"missing_punch.clock_out": req.ClaimedOutAt.UTC().Format(time.RFC3339Nano), "missing_punch.reason": req.Reason, "missing_punch.original_workflow_instance": req.OriginalWorkflowInstanceRef}
	for _, state := range p.ProposedState {
		if want[state.FieldPath] != state.CanonicalText {
			return errMissingPunchProposalBind
		}
	}
	return nil
}

func missingPunchProposalIntentID(req clockservice.MissingPunchWorkflowRequest) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(req.TenantID+"\x00"+clockservice.MissingPunchWorkflowID+"\x00"+req.RequestID))
}
