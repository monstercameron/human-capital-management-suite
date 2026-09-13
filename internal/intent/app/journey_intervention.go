package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PROMOUX-013: governed edit, withdraw and cancel paths over an existing
// journey.
//
// EditProposal's own composition is not the one this todo started from -- it
// is the one a real PostgreSQL run of TestTodo_PROMOUX_013_Integration
// forced.
//
// promote_worker is declared with intent.ReleaseP1A and
// definitions.simulateOnlyChangeTransitions, whose RequestState edges
// (internal/intent/definitions/transitions.go's p1aChangeRequestEdges) reach
// SUPERSEDED only from SIMULATED, SUBMITTED or APPROVED -- never from DRAFT,
// matching the kernel's own declared profile
// (internal/intent/lifecycle/profile.go) exactly, not merely this
// definition's narrower one. A journey's stored intent never durably leaves
// DRAFT: Propose calls CreateIntent and then only ever *re-simulates*
// (package app's own doc: "SimulateIntent persists nothing at all"), and
// nothing in this release calls SubmitIntent for a journey. So
// [intent.SupersedeOriginal] -- correct, and exercised by
// TestTodo_EP_INTENT_003 against exactly the definitions that do reach
// SIMULATED/SUBMITTED/APPROVED -- is structurally unusable for a journey:
// every attempt refuses CodeFailedPrecondition/intent.illegal_transition,
// executed or not. Widening the kernel's own DRAFT edges to admit SUPERSEDED
// was ruled out here as cross-cutting kernel surgery affecting every intent
// type, not a journey-scoped fix, and out of this session's reach to verify
// safe.
//
// EditProposal therefore composes the two governed operations that *are*
// legal for a DRAFT (or later) journey intent and are already independently
// correct and tested: [IntentService.CancelIntent] (legal from DRAFT via
// p1aReadRequestEdges' DRAFT->CANCELLED edge, and already proven safe-point-
// aware for a mid-flight EXECUTING journey by
// TestTodo_PROMOUX_013_Integration/CancelDuringEligibleWait) stops the
// original, and -- only once that disposition reports the original
// genuinely and cleanly stopped -- [journeyEngine.Propose] mints the edited
// successor through the exact same admission, baseline resolution, ladder
// validation and promotion-guard path every ordinary proposal takes. A
// disposition other than CANCELLED (CANCELLATION_PENDING, TOO_LATE,
// REPAIR_REQUIRED) refuses the edit outright rather than minting a successor
// next to an original that was not actually stopped: "invalidate material
// approvals" is true here because the *original* carries them and the
// *original* is the one being durably cancelled -- the successor inherits
// nothing from it, including no approval.
//
// RequestIntervention is the same [IntentService.CancelIntent] call
// forwarded directly, exactly as [journeyEngine.Decide] forwards to
// ExecuteIntent. This file owns no lifecycle rule and no SQL: it only
// translates the journey port's request shape into the intent service's own
// governed RPCs and translates their answer back.

// Reason references this file owns for PreviewIntervention's stable,
// value-only disclosure. A caller who is not authorized to read the journey
// at all never reaches these -- Inspect (which every preview and
// intervention call goes through first) refuses them exactly as
// InspectJourney would.
const (
	reasonInterventionAlreadyTerminal  = "journey.intervention.unavailable.already_terminal"
	reasonInterventionAlreadyStarted   = "journey.intervention.unavailable.already_started"
	reasonInterventionNotYetStarted    = "journey.intervention.unavailable.not_yet_started"
	reasonInterventionAlreadyCommitted = "journey.intervention.unavailable.already_committed"
	// reasonEditNotCleanlyStoppable reports an edit refused because
	// cancelling the original -- the precondition for minting an edited
	// successor -- did not resolve to CANCELLED.
	reasonEditNotCleanlyStoppable = "journey.edit.original_not_cleanly_stoppable"
)

// journeyWorkerRefFromIntent reads the stored "worker_ref" field an original
// proposal's request payload carries -- the same key [journeyRequestPayload]
// writes and [journeySummaryFromProto] reads for display. EditProposal needs
// the raw reference (not the derived EntityRef) because it re-resolves the
// worker through [journeyEngine.locate] exactly as Propose does.
func journeyWorkerRefFromIntent(msg *intentsv1.IntentInstance) (string, error) {
	payload, err := decodeStruct(msg.GetRequest().GetProtobufWireBytes())
	if err != nil {
		return "", fmt.Errorf("app: journey: %w", err)
	}
	ref := optionalStr(payload, "worker_ref")
	if ref == "" {
		return "", fmt.Errorf("app: journey: the original proposal carries no worker_ref")
	}
	return ref, nil
}

// releasePromotionWindow closes PROMOUX-002's admission reservation for
// originalIntentID once its journey has been durably cancelled (disposition
// CANCELLED), freeing the worker+effective-date window for a fresh
// proposal. internal/data/promotionguard.Release's own doc names this
// precisely: "Nothing in this repository calls Release automatically yet" --
// PROMOUX-013 is that first caller, discovered because without it a
// cancelled-then-edited journey could never re-propose the same worker and
// effective date, which is the ordinary shape of a correction.
//
// It is best-effort exactly like [journeyEngine.confirmPromotionWindow]: the
// cancellation itself has already landed by the time this runs, so a
// release failure is not surfaced as this call's own failure. It costs
// availability (the freed window is not yet visible for reuse), never
// correctness: promotionguard.Admit's own exclusion is still enforced
// against whatever the row currently says.
func (e *journeyEngine) releasePromotionWindow(ctx context.Context, principal *trust.Principal, originalIntentID string) {
	if e.db == nil {
		return
	}
	intentID, err := uuid.Parse(originalIntentID)
	if err != nil {
		return
	}
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := e.svc.tenantUUID(principal.Tenant())
	if err := promotionguard.Release(ctx, tx, tenantID, intentID, e.now().UTC()); err != nil {
		return
	}
	_ = tx.Commit(ctx)
}

// loadPromotionJourney reads and type-checks one journey's stored intent,
// exactly as [journeyEngine.Execute] and [journeyEngine.Decide] each do
// before acting.
func (e *journeyEngine) loadPromotionJourney(ctx context.Context, intentID string) (*intentsv1.IntentInstance, error) {
	got, getErr := e.svc.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: intentID})
	if getErr != nil {
		return nil, journeyError(getErr)
	}
	if got.GetIntent().GetDefinition().GetIntentTypeId() != promotion.IntentType {
		return nil, fmt.Errorf("%w: %s is not a promotion journey", workspace.ErrJourneyUnknown, intentID)
	}
	return got.GetIntent(), nil
}

// ---------------------------------------------------------------------------
// EditProposal
// ---------------------------------------------------------------------------

// EditProposal implements [workspace.JourneyEngine]. See this file's own
// doc comment for why it is Cancel-then-repropose rather than Supersede.
func (e *journeyEngine) EditProposal(
	ctx context.Context, intentID string, expectedInstanceVersion uint64, idempotencyKey, reason string, in workspace.EditProposalInput,
) (workspace.JourneySummary, string, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneySummary{}, "", err
	}
	switch {
	case strings.TrimSpace(idempotencyKey) == "":
		return workspace.JourneySummary{}, "", journeyInputError("idempotency_key", "is required")
	case strings.TrimSpace(reason) == "":
		return workspace.JourneySummary{}, "", journeyInputError("reason", "is required to edit a proposal")
	case expectedInstanceVersion == 0:
		return workspace.JourneySummary{}, "", journeyInputError("expected_instance_version", "is required")
	}

	original, loadErr := e.loadPromotionJourney(ctx, intentID)
	if loadErr != nil {
		return workspace.JourneySummary{}, "", loadErr
	}
	workerRef, wrErr := journeyWorkerRefFromIntent(original)
	if wrErr != nil {
		return workspace.JourneySummary{}, "", wrErr
	}

	edited := workspace.ProposalInput{
		WorkerRef:        workerRef,
		TargetJobCode:    in.TargetJobCode,
		TargetGrade:      in.TargetGrade,
		TargetPositionID: in.TargetPositionID,
		ProposedBase:     in.ProposedBase,
		EffectiveDate:    in.EffectiveDate,
		BusinessReason:   in.BusinessReason,
	}
	if err := validateProposalInput(edited); err != nil {
		return workspace.JourneySummary{}, "", err
	}

	// Stop the original first. Nothing about the edited successor is minted
	// until this durably lands, so a refused or raced cancel writes nothing
	// (see internal/intent/app/lifecycle_endpoints.go's own PROMOUX-013
	// reordering note for CancelIntent's sibling SupersedeIntent, which
	// applies the identical principle).
	cancelled, cancelErr := e.svc.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
		IntentId:                intentID,
		IdempotencyKey:          idempotencyKey + ":stop-original",
		ReasonRef:               reason,
		ExpectedInstanceVersion: expectedInstanceVersion,
	})
	if cancelErr != nil {
		return workspace.JourneySummary{}, "", journeyError(cancelErr)
	}
	decisions := cancelled.GetIntent().GetCancellationDecisions()
	if len(decisions) == 0 {
		return workspace.JourneySummary{}, "", envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(fmt.Errorf("CancelIntent recorded no cancellation decision"))
	}
	disposition := intent.CancellationDisposition(decisions[len(decisions)-1].GetEffectDispositionRef())
	if disposition != intent.DispositionCancelled {
		return workspace.JourneySummary{}, "", envelope.New(envelope.CodeFailedPrecondition, reasonEditNotCleanlyStoppable,
			"a precondition for the operation is not met").
			WithViolation("intent_id",
				"the original could not be stopped cleanly right now ("+string(disposition)+"); the edit is refused rather than left partially applied",
				reasonEditNotCleanlyStoppable)
	}

	// PROMOUX-002's admission guard is keyed by (worker, effective date), not
	// by intent, so the original's still-ACTIVE reservation must be released
	// before Propose can admit a successor for the same window -- otherwise
	// an edit that keeps the same worker and effective date (the ordinary
	// shape of a correction) refuses itself as a duplicate promotion.
	e.releasePromotionWindow(ctx, principal, intentID)

	summary, proposeErr := e.Propose(ctx, edited)
	if proposeErr != nil {
		return workspace.JourneySummary{}, "", proposeErr
	}
	return summary, intentID, nil
}

// ---------------------------------------------------------------------------
// PreviewIntervention
// ---------------------------------------------------------------------------

// interventionUnavailableAtStage reports why kind is not offered at stage.
// The mapping is total: every non-eligible stage maps to exactly one of
// three reasons, each computed only from the stage itself -- never from any
// fact only an authorized viewer could know -- so the answer's shape cannot
// leak information a viewer who reached this far was not already entitled
// to (PROMOUX-001/PROMOUX-004's disclosure-by-value precedent applied here:
// the same stage always produces the same reason text, whoever is asking).
func interventionUnavailableAtStage(kind workspace.JourneyInterventionKind, stage workspace.JourneyStage) (string, bool) {
	terminal := map[workspace.JourneyStage]bool{
		workspace.JourneyStageCompleted: true, workspace.JourneyStageRejected: true,
		workspace.JourneyStageFailed: true, workspace.JourneyStageRecorded: true,
	}
	unstarted := map[workspace.JourneyStage]bool{
		workspace.JourneyStageProposed: true, workspace.JourneyStageBlocked: true,
	}
	committed := map[workspace.JourneyStage]bool{
		workspace.JourneyStageExecuted: true, workspace.JourneyStageObservingEffects: true,
	}
	if terminal[stage] {
		return reasonInterventionAlreadyTerminal, true
	}
	switch kind {
	case workspace.JourneyInterventionWithdraw:
		if !unstarted[stage] {
			return reasonInterventionAlreadyStarted, true
		}
	case workspace.JourneyInterventionCancel:
		if unstarted[stage] {
			return reasonInterventionNotYetStarted, true
		}
		if committed[stage] {
			return reasonInterventionAlreadyCommitted, true
		}
	}
	return "", false
}

// PreviewIntervention implements [workspace.JourneyEngine]. It runs the same
// authorized read [journeyEngine.Inspect] performs and derives availability
// purely from the returned durable stage: it opens no additional connection
// and evaluates no safe point of its own, because the safe point is only
// actually decided when [journeyEngine.RequestIntervention] runs the real
// CancelIntent gate.
func (e *journeyEngine) PreviewIntervention(
	ctx context.Context, intentID string, kind workspace.JourneyInterventionKind,
) (workspace.JourneyInterventionPreview, error) {
	if kind != workspace.JourneyInterventionWithdraw && kind != workspace.JourneyInterventionCancel {
		return workspace.JourneyInterventionPreview{}, journeyInputError("kind", "must be WITHDRAW or CANCEL")
	}
	detail, err := e.Inspect(ctx, intentID)
	if err != nil {
		return workspace.JourneyInterventionPreview{}, err
	}
	if reasonRef, unavailable := interventionUnavailableAtStage(kind, detail.Summary.Stage); unavailable {
		return workspace.JourneyInterventionPreview{Available: false, UnavailableReasonRef: reasonRef}, nil
	}
	switch kind {
	case workspace.JourneyInterventionWithdraw:
		return workspace.JourneyInterventionPreview{
			Available: true,
			ConsequenceSummary: "Withdrawing now cancels this proposal before any approval has been recorded. " +
				"No business effect has occurred.",
			LikelyOutcome:            workspace.InterventionApplied,
			CurrentGovernanceVersion: detail.Summary.GovernanceVersion,
		}, nil
	default: // JourneyInterventionCancel
		return workspace.JourneyInterventionPreview{
			Available: true,
			ConsequenceSummary: "Requesting cancellation now asks the engine to stop at its next safe point. " +
				"If the safe point has already passed, the promotion completes instead and cancellation is refused as too late.",
			LikelyOutcome:            workspace.InterventionPendingSafePoint,
			CurrentGovernanceVersion: detail.Summary.GovernanceVersion,
		}, nil
	}
}

// ---------------------------------------------------------------------------
// RequestIntervention
// ---------------------------------------------------------------------------

// interventionOutcomeFromDisposition projects one [intent.CancellationDisposition]
// onto the shared [workspace.JourneyInterventionOutcome] vocabulary. The
// mapping is total and exhaustive over CancelIntent's own four dispositions.
func interventionOutcomeFromDisposition(d intent.CancellationDisposition) workspace.JourneyInterventionOutcome {
	switch d {
	case intent.DispositionCancelled:
		return workspace.InterventionApplied
	case intent.DispositionCancellationPending:
		return workspace.InterventionPendingSafePoint
	case intent.DispositionTooLate:
		return workspace.InterventionTooLate
	case intent.DispositionRepairRequired:
		return workspace.InterventionRepairRequired
	default:
		return workspace.InterventionDenied
	}
}

// RequestIntervention implements [workspace.JourneyEngine]. Both
// [workspace.JourneyInterventionWithdraw] and
// [workspace.JourneyInterventionCancel] forward to the identical
// [IntentService.CancelIntent] call: the kind is UI framing (which
// affordance was offered and confirmed), never a second code path. The
// idempotency coordinator and the store's own optimistic
// instance-version compare-and-swap CancelIntent already runs through are
// what resolve a race with a concurrent approval, timer fire or terminal
// commit to exactly one durable outcome -- a losing caller's own request
// never mutates anything, because the compare-and-swap it would need
// refuses before any row is written.
func (e *journeyEngine) RequestIntervention(
	ctx context.Context, intentID string, req workspace.JourneyInterventionRequest,
) (workspace.JourneyInterventionResult, error) {
	principal, principalErr := journeyPrincipal(ctx)
	if principalErr != nil {
		return workspace.JourneyInterventionResult{}, principalErr
	}
	switch {
	case req.Kind != workspace.JourneyInterventionWithdraw && req.Kind != workspace.JourneyInterventionCancel:
		return workspace.JourneyInterventionResult{}, journeyInputError("kind", "must be WITHDRAW or CANCEL")
	case strings.TrimSpace(req.IdempotencyKey) == "":
		return workspace.JourneyInterventionResult{}, journeyInputError("idempotency_key", "is required")
	case strings.TrimSpace(req.Reason) == "":
		return workspace.JourneyInterventionResult{}, journeyInputError("reason", "is required")
	case req.ExpectedInstanceVersion == 0:
		return workspace.JourneyInterventionResult{}, journeyInputError("expected_instance_version", "is required")
	}
	if _, loadErr := e.loadPromotionJourney(ctx, intentID); loadErr != nil {
		return workspace.JourneyInterventionResult{}, loadErr
	}

	cancelled, cancelErr := e.svc.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
		IntentId:                intentID,
		IdempotencyKey:          req.IdempotencyKey,
		ReasonRef:               req.Reason,
		ExpectedInstanceVersion: req.ExpectedInstanceVersion,
	})
	if cancelErr != nil {
		return workspace.JourneyInterventionResult{}, journeyError(cancelErr)
	}

	summary, sumErr := journeySummaryFromProto(cancelled.GetIntent())
	if sumErr != nil {
		return workspace.JourneyInterventionResult{}, sumErr
	}
	decisions := cancelled.GetIntent().GetCancellationDecisions()
	var outcome workspace.JourneyInterventionOutcome
	var evidenceRef string
	if n := len(decisions); n > 0 {
		last := decisions[n-1]
		outcome = interventionOutcomeFromDisposition(intent.CancellationDisposition(last.GetEffectDispositionRef()))
		evidenceRef = last.GetCancellationDecisionId()
	}
	if outcome == workspace.InterventionApplied {
		// The window this journey claimed is freed as soon as it is
		// durably, cleanly stopped -- not only when it is about to be
		// edited (see [journeyEngine.releasePromotionWindow]).
		e.releasePromotionWindow(ctx, principal, intentID)
	}
	return workspace.JourneyInterventionResult{
		Journey: summary, Outcome: outcome, RetainedEvidenceRef: evidenceRef,
	}, nil
}
