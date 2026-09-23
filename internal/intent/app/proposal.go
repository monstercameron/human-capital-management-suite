package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// planHorizon bounds how long a compiled plan may be considered current. A
// plan that never expires is a plan whose baseline can go stale unnoticed, so
// CompilePlan refuses one; this is the value this cell binds.
const planHorizon = 7 * 24 * time.Hour

// promotionStreamID is the stream a promotion's domain events would be
// appended to. P1A appends none - the plan is non-executable - but the plan
// still has to name the stream it would use, because "what would happen" is
// the product.
func promotionStreamID(subject values.EntityRef) string {
	return "people.worker." + subject.Id
}

// proposalFor turns a zero-effect promotion simulation into the immutable
// proposal specification an approval would later bind.
//
// Every planned write carries the per-field source-authority decision and the
// exact baseline revision it assumes; a simulation that cannot supply both
// produces no write rather than an unpinned one, and the kernel rejects the
// revision if this function ever gets that wrong.
func proposalFor(
	inst intent.Instance,
	def intent.Definition,
	call promotion.PreflightRequest,
	result promotion.SimulationResult,
	baseline intent.BaselineSnapshot,
	controls intent.ControlSnapshots,
	revision uint64,
	managerWorkerID string,
	contractDigest string,
) (intent.ProposalSpec, error) {
	if inst.RequestedEffectiveAt == nil {
		return intent.ProposalSpec{}, fmt.Errorf("app: %s carries no requested effective time", inst.IntentID)
	}
	effective, err := values.NewOpenInstantInterval(*inst.RequestedEffectiveAt)
	if err != nil {
		return intent.ProposalSpec{}, fmt.Errorf("app: effective interval: %w", err)
	}

	subject := call.Subject
	key, err := values.NewResourceKey(inst.Tenant, values.Kind("assignment"), "worker", subject.Id)
	if err != nil {
		return intent.ProposalSpec{}, fmt.Errorf("app: assignment resource key: %w", err)
	}
	watermark, ok := baseline.Revisions[subject.String()]
	if !ok || !watermark.IsSpecified() {
		return intent.ProposalSpec{}, fmt.Errorf("app: baseline pins no revision for %s", subject)
	}

	// The proposal's subject set is the instance's own: the proposal is about
	// exactly what the request was about, never a widened set the simulation
	// happened to touch.
	primary := intent.SubjectReference{}
	for _, s := range inst.Subjects {
		if resolvesAgainst(s, subject, call.Target) && primary.SubjectID == "" {
			primary = s
		}
	}
	if primary.SubjectID == "" {
		return intent.ProposalSpec{}, fmt.Errorf("app: no instance subject resolves to %s", subject)
	}

	spec := intent.ProposalSpec{
		IntentID:            inst.IntentID,
		Revision:            revision,
		Tenant:              inst.Tenant,
		OrganizationScopeID: inst.OrganizationScopeID,
		LegalEntityID:       inst.OrganizationScopeID,
		Subjects:            inst.Subjects,
		EffectiveTime:       effective,
		Purpose: intent.PurposeDecision{
			Purpose:        inst.Purpose,
			RecipientRef:   "recipient:" + inst.Initiator.PrincipalID,
			DestinationRef: "destination:internal",
			ResidencyRef:   "residency:" + inst.OrganizationScopeID,
		},
		Revalidation:     intent.RevalidationPlan{Rules: []string{def.RevalidationRule}},
		ControlSnapshots: controls,
		CreatedBy:        inst.Initiator,
		SourceBaselines: []intent.SourceBaseline{{
			StreamID:         promotionStreamID(subject),
			ExpectedRevision: watermark,
		}},
	}

	for _, change := range result.Projected.Changes {
		field := "assignment." + change.Field
		spec.CurrentState = append(spec.CurrentState, intent.StateAssertion{
			Subject: primary, ResourceKey: key, FieldPath: field, CanonicalText: change.Before,
		})
		spec.ProposedState = append(spec.ProposedState, intent.StateAssertion{
			Subject: primary, ResourceKey: key, FieldPath: field, CanonicalText: change.After,
		})
		if !change.Changed {
			continue
		}
		spec.Writes = append(spec.Writes, intent.PlannedWrite{
			Subject:                 primary,
			ResourceKey:             key,
			FieldPath:               field,
			CurrentCanonicalText:    change.Before,
			ProposedCanonicalText:   change.After,
			SourceAuthorityDecision: "authority.local_master/v1",
			ExpectedRevision:        watermark,
			Operation:               intent.WriteOperationUpdate,
			EffectiveInterval:       effective,
		})
	}

	if err := appendCommitMaterial(&spec, inst.Tenant, primary, subject, watermark, effective, result, managerWorkerID); err != nil {
		return intent.ProposalSpec{}, err
	}

	if def.ApprovalRequired {
		spec.RequiredApprovals = []intent.RequiredApproval{{
			RequirementID:        "req." + def.Ref.TypeID + "/v1",
			SeparationConstraint: "not_requester",
		}}
	}
	if result.Preflight.ResultDigest != "" {
		spec.Attachments = []intent.AttachmentRef{{
			ArtifactID:  "artifact:preflight:" + inst.IntentID,
			AlgorithmID: "sha256",
			Digest:      result.Preflight.ResultDigest,
		}}
	}
	// A position-bound simulation assembles a governed simulation contract
	// over the resolve-time sims; the minted proposal pins its digest
	// alongside the preflight so the approved revision -- the material the
	// terminal resolver builds the commit from -- executes against the
	// contract the simulation certified, not just the projection. Empty on
	// paths that assembled none.
	if digest := strings.TrimSpace(contractDigest); digest != "" {
		spec.Attachments = append(spec.Attachments, intent.AttachmentRef{
			ArtifactID:  "artifact:simcontract:" + inst.IntentID,
			AlgorithmID: "sha256",
			Digest:      digest,
		})
	}
	return spec, nil
}

// planFor compiles the non-executable transaction plan for a minted proposal
// revision. It names every participant, read, append and projection mutation
// the commit would perform, and no effect at all: promote_worker is
// ZERO_EFFECT in P1A, and CompilePlan refuses an effect on a zero-effect
// definition.
func planFor(
	inst intent.Instance,
	def intent.Definition,
	rev intent.ProposalRevision,
	call promotion.PreflightRequest,
	baseline intent.BaselineSnapshot,
	controls Controls,
	governanceReady bool,
) (intent.PlanInput, error) {
	subject := call.Subject
	key, err := values.NewResourceKey(inst.Tenant, values.Kind("assignment"), "worker", subject.Id)
	if err != nil {
		return intent.PlanInput{}, fmt.Errorf("app: assignment resource key: %w", err)
	}
	watermark := baseline.Revisions[subject.String()]
	if !watermark.IsSpecified() {
		return intent.PlanInput{}, fmt.Errorf("app: baseline pins no revision for %s", subject)
	}
	sequence, ok := watermark.Sequence()
	if !ok {
		return intent.PlanInput{}, fmt.Errorf("app: baseline revision for %s is not a sequence", subject)
	}

	stream := promotionStreamID(subject)
	decision := "PERMIT"
	if !governanceReady {
		decision = "DENY"
	}

	in := intent.PlanInput{
		Proposal:   rev,
		Definition: def,
		Mode:       inst.ExecutionMode,
		Governance: intent.GovernanceSnapshot{
			SnapshotDigest: controls.Snapshots.PolicyBundleDigest,
			AuthZDecision:  decision,
			LegalDecision:  "PERMIT",
			PolicyDecision: decision,
			RiskDecision:   "ACCEPT",
		},
		Conflict: intent.ConflictSnapshot{
			SnapshotDigest: controls.SourceAuthorityDigest,
			FenceToken:     watermark.String(),
			FootprintRef:   def.ConflictFootprintRule,
		},
		Participants: []intent.PlanParticipant{{
			ParticipantID: "participant:people",
			StreamID:      stream,
			StorageClass:  "LOCAL_POSTGRES",
			Local:         true,
		}},
		Reads: []intent.PlannedRead{{
			ResourceKey:      key,
			ExpectedRevision: watermark,
		}},
		Preconditions: []intent.CommitPrecondition{
			{Kind: "SOURCE_AUTHORITY", Ref: "authority.local_master/v1"},
			{Kind: "BASELINE_REVISION", Ref: watermark.String()},
		},
		IdempotencyRecordRef: "idempotency:" + inst.IdempotencyKey,
		RevalidationRuleRefs: []string{def.RevalidationRule},
		ExpiresAt:            values.NewInstant(inst.CreatedAt.Time().Add(planHorizon)),
	}

	if len(rev.Writes) > 0 {
		in.Appends = []intent.PlannedAppend{{
			StreamID:         stream,
			ExpectedSequence: sequence + 1,
			EventType:        "people.assignment_position_changed/v1",
			PayloadDigest:    rev.MaterialDigest.Digest,
		}}
		in.ProjectionMutations = []intent.ProjectionMutation{{
			ProjectionID: "projection.worker_state/v1",
			ResourceKey:  key,
			Operation:    "UPSERT",
		}}
	}
	for _, approval := range rev.RequiredApprovals {
		in.ApprovalRequirementIDs = append(in.ApprovalRequirementIDs, approval.RequirementID)
	}
	return in, nil
}
