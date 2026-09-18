package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// acknowledgementEventSource is the origin the promotion acknowledgement
// intake relays: the HRIS employment record the operator verified the
// employee's signature against. The pinned plan allowlists it on the
// acknowledgement gate, and the intake refuses a wait that does not. The
// operator's own identity travels separately, bound into the signal payload
// as the attester, so origin (what was verified) and relay (who verified it)
// are never conflated.
const acknowledgementEventSource = "hcmnext.integrations.hris"

// attestedAckVerifier admits only server-constructed acknowledgement signals:
// the expected HRIS origin with no external signature. The journey builds the
// signal itself after authorizing the attester, so a present signature means
// webhook bytes reached the operator path by mistake, and any other source
// means the wrong intake.
type attestedAckVerifier struct{ source string }

func (v attestedAckVerifier) Verify(sig stepSignal.Signal) error {
	if sig.Source != v.source {
		return fmt.Errorf("app: acknowledgement source %q is not the attested %q", sig.Source, v.source)
	}
	if len(sig.Signature) != 0 {
		return fmt.Errorf("app: an attested acknowledgement carries no external signature")
	}
	return nil
}

// acknowledgementPayload is the JSON document the intake stores as the
// acknowledgement signal's payload. Every field is evidence: who verified,
// when, against what artifact, and what they stated.
type acknowledgementPayload struct {
	IntentID    string `json:"intent_id"`
	AttestedBy  string `json:"attested_by"`
	AttestedAt  string `json:"attested_at"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
	Note        string `json:"note,omitempty"`
}

// Acknowledge records one employee's verified promotion acknowledgement
// against a journey parked on its acknowledgement gate, receives the
// correlated signal into the durable signal store, resumes the driver from
// the matched receipt, and returns the journey at its resulting stage.
//
// Admission mirrors Decide's: the cell must admit the promotion type, the
// caller must be authenticated, and the journey's initiator may not attest
// their own promotion (the PROMOUX-015 separation rationale applies to
// attestations exactly as it does to approvals). The wait itself is the final
// guard: only an OPEN acknowledgement subscription is receivable, so a
// journey that never parked, already completed, or already settled its wait
// is refused before any write.
func (e *journeyEngine) Acknowledge(ctx context.Context, intentID string, ack workspace.Acknowledgement) (workspace.JourneyDetail, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	inst, _, ownedErr := e.svc.loadInstance(ctx, principal.Tenant().String(), intentID)
	if ownedErr != nil {
		return workspace.JourneyDetail{}, journeyError(ownedErr)
	}
	def, defErr := e.svc.defs.Resolve(inst.Definition)
	if defErr != nil {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %s", workspace.ErrJourneyUnknown, intentID)
	}
	if def.Ref.TypeID != promotion.IntentType {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %s is not a promotion journey",
			workspace.ErrJourneyUnknown, intentID)
	}
	switch inst.Lifecycle.Request {
	case lifecycle.RequestCancelled, lifecycle.RequestSuperseded, lifecycle.RequestRejected,
		lifecycle.RequestWithdrawn, lifecycle.RequestClosed:
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this journey's proposal has been %s and can no longer be acknowledged",
			workspace.ErrJourneyStage, string(inst.Lifecycle.Request))
	}
	if gateErr := e.svc.authorizeDecision(def); gateErr != nil {
		return workspace.JourneyDetail{}, journeyError(gateErr)
	}
	if inst.Initiator.PrincipalID == principal.Subject() {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: the initiator of a promotion may not attest its acknowledgement", workspace.ErrDenied)
	}
	resumer, ok := e.svc.executor.(SignalResumeExecutor)
	if e.svc.executor == nil || !ok {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this cell was composed with no signal-capable execution driver", workspace.ErrJourneyUnavailable)
	}
	now := e.now().UTC()

	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := e.svc.tenantUUID(principal.Tenant())
	sub, err := (signals.Store{}).OpenSubscriptionForCorrelation(ctx, tx, tenantID, promotionexec.NodeAcknowledgeRelease, intentID)
	if err != nil {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: no acknowledgement wait is open for this journey", workspace.ErrJourneyStage)
	}
	instance, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, sub.InstanceID)
	if err != nil {
		return workspace.JourneyDetail{}, journeyError(err)
	}
	if instance.RuntimeStatus != runtime.InstanceRunning && instance.RuntimeStatus != runtime.InstanceWaiting {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this journey's run is %s, not parked on its acknowledgement gate",
			workspace.ErrJourneyStage, instance.RuntimeStatus)
	}
	onFrontier := false
	for _, current := range instance.CurrentNodeIDs {
		if current == promotionexec.NodeAcknowledgeRelease {
			onFrontier = true
			break
		}
	}
	if !onFrontier {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: the acknowledgement gate is not on this journey's frontier", workspace.ErrJourneyStage)
	}
	hasSource := false
	for _, source := range sub.AcceptedSources {
		if source == acknowledgementEventSource {
			hasSource = true
			break
		}
	}
	if !hasSource {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this intake relays HRIS-recorded acknowledgements only", workspace.ErrJourneyStage)
	}
	payload, err := json.Marshal(acknowledgementPayload{
		IntentID: intentID, AttestedBy: principal.Subject(), AttestedAt: now.Format(time.RFC3339Nano),
		EvidenceRef: ack.EvidenceRef, Note: ack.Note,
	})
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	receipt, err := (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{
		Signal: stepSignal.Signal{
			// The signal store keys tenants by UUID, like the
			// subscription the driver opened: the raw tenant key is
			// not a UUID and would never match the parked wait.
			Tenant: values.TenantId(tenantID.String()), Source: acknowledgementEventSource,
			EventType: sub.EventType, SchemaRef: sub.ExpectedSchemaRef,
			CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue,
			IdempotencyKey: "journey:ack:" + intentID, Payload: payload,
			Taint:      workflow.TaintTainted,
			ReceivedAt: values.NewInstant(now),
		},
		ReceivedAt: now,
	}, attestedAckVerifier{source: acknowledgementEventSource})
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	accepted := false
	for _, disposition := range receipt.Dispositions {
		if disposition.SubscriptionID == sub.ID && disposition.Status == stepSignal.StatusAccepted {
			accepted = true
			break
		}
	}
	if !accepted {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: the acknowledgement wait did not accept this attestation", workspace.ErrJourneyStage)
	}
	start, intentInstance, intentRecord, err := e.svc.parkedExecutionStart(ctx, tx, principal.Tenant().String(), tenantID, instance, "acknowledgement")
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return workspace.JourneyDetail{}, err
	}
	result, resumeErr := resumer.ResumeSignal(ctx, ExecutionSignalResumeRequest{
		Start: start, InstanceID: sub.InstanceID,
		ExpectedInstanceVersion: instance.InstanceVersion,
		SignalID:                receipt.SignalID, SubscriptionID: sub.ID,
		RecordedAt: now,
	})
	if resumeErr != nil {
		return workspace.JourneyDetail{}, journeyError(executionError(resumeErr))
	}
	if outcomeErr := e.svc.consumeExecutionResult(ctx, intentInstance, def, intentRecord, result); outcomeErr != nil {
		return workspace.JourneyDetail{}, journeyError(outcomeErr)
	}
	return e.Inspect(ctx, intentID)
}
