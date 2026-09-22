// Discharge drains a recorded compensation obligation (WF-REV-001).
//
// [Decide] leaves a COMPENSATION_REQUIRED instance in CANCELLING with the
// obligation (the published compensation references) carried on the decision
// row. Nothing about deciding compensates anything: Discharge reads that
// recorded obligation back and acts on it.
//
// For each obligated effect, in reverse of the recorded obligation order,
// Discharge asks the Compensator port to run and observe one compensation,
// then records the result durably on the effect's own node execution with the
// spec's SUCCEEDED -> COMPENSATED edge. The COMPENSATED transition carries
// the observed evidence reference, so a resumed driver can tell a finished
// compensation from a pending one without repeating it.
//
//   - every compensation observed compensated: timers and open work items are
//     closed exactly as [decider.cancel] closes them and the instance moves
//     CANCELLING -> CANCELLED;
//   - any compensation remaining (failed, refused, unobservable, malformed,
//     or scoped to a child instance, whose own discharge pass owns it): the
//     instance moves CANCELLING -> REPAIR_REQUIRED with the remaining effects
//     named in the decision's reasons and evidence, never back to CANCELLING.
//
// The whole pass runs inside the caller's transaction, like [Decide]: one
// call is all-or-nothing. Crash resume therefore relies on two properties:
// finished compensations stay finished because their COMPENSATED transition
// committed with an earlier call, and the Compensator itself must be
// idempotent per (obligation, effect) so a side effect applied before a crash
// is safe to re-present. WF-REV-002 binds this port to the compensate
// executor, whose operation keys provide that idempotency.
package cancellation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ErrNoObligation reports a CANCELLING instance with no recorded
// COMPENSATION_REQUIRED decision for its current state: there is nothing to
// discharge, and the driver invents no obligation.
var ErrNoObligation = errors.New("workflow cancellation: no compensation obligation recorded")

// EffectCompensated is the discharge evidence disposition of an effect whose
// compensation was observed compensated.
const EffectCompensated = "COMPENSATED"

// EffectRemaining is the discharge evidence disposition prefix of an effect
// whose compensation did not finish, followed by a short detail.
const EffectRemaining = "REMAINING"

// CompensationItem is one obligated effect parsed from the recorded
// obligation's "effectID=compensation" references.
type CompensationItem struct {
	// ObligationID is the COMPENSATION_REQUIRED decision the item comes from.
	ObligationID uuid.UUID
	// EffectID is the judged effect, "nodeID#attempt".
	EffectID string
	// NodeID and Attempt locate the effect's node execution.
	NodeID  string
	Attempt int
	// Compensation is the published compensation reference that releases it.
	Compensation string
}

// CompensationResult is the observed outcome of one compensation.
type CompensationResult struct {
	// Compensated reports the effect was observed compensated.
	Compensated bool
	// EvidenceRef binds the observation. Required when Compensated: an
	// unbound compensation is not durable evidence and is refused.
	EvidenceRef string
}

// Compensator runs one compensation and observes its result. It receives the
// caller's transaction for its own durable records and must be idempotent
// per (ObligationID, EffectID): a resumed discharge re-presents only effects
// without a recorded COMPENSATED transition, but a crash between the side
// effect and the commit may still present one twice.
type Compensator interface {
	Compensate(ctx context.Context, ex Executor, item CompensationItem) (CompensationResult, error)
}

// DischargeRequest asks for one discharge of a recorded obligation.
type DischargeRequest struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// ExpectedInstanceVersion fences the caller's view; zero accepts the
	// version read under the instance lock.
	ExpectedInstanceVersion int64
	Reason                  string
	RequestedBy             string
	RecordedAt              time.Time
}

// Discharge reads the instance's recorded compensation obligation, runs each
// compensation in reverse obligation order through comp, and moves the
// instance to CANCELLED when every compensation is observed, or to
// REPAIR_REQUIRED naming the remaining effects otherwise.
func Discharge(ctx context.Context, ex Executor, req DischargeRequest, comp Compensator) (ret0 Outcome, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.cancellation.discharge",
		observe.Attrs{observe.KeyTenant: req.TenantID.String()}, observe.Attrs{observe.KeyInstance: req.InstanceID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if ex == nil || comp == nil || req.TenantID == uuid.Nil || req.InstanceID == uuid.Nil ||
		strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.RequestedBy) == "" || req.RecordedAt.IsZero() ||
		req.ExpectedInstanceVersion < 0 {
		return Outcome{}, fmt.Errorf("%w: tenant, instance, compensator, reason, actor and RecordedAt are required", ErrInvalid)
	}
	inst, err := lockInstance(ctx, ex, req.TenantID, req.InstanceID)
	if err != nil {
		return Outcome{}, err
	}
	if req.ExpectedInstanceVersion > 0 && req.ExpectedInstanceVersion != inst.InstanceVersion {
		return Outcome{}, fmt.Errorf("%w: expected %d, stored %d", ErrStale, req.ExpectedInstanceVersion, inst.InstanceVersion)
	}
	// The obligation is the instance's latest decision, not the one pinned to
	// its current version: recording each compensation bumps the version, so
	// an exact-version lookup would orphan the obligation after the first
	// finished item (or any concurrent durable write).
	latest, statusAfter, found, err := latestDecision(ctx, ex, inst)
	if err != nil {
		return Outcome{}, err
	}
	if inst.RuntimeStatus != runtime.InstanceCancelling {
		// A terminal instance standing on its latest decision is a finished
		// discharge (or a clean cancel): return it instead of acting again.
		// Anything else has no obligation to drain.
		if inst.RuntimeStatus.Terminal() && found && statusAfter == inst.RuntimeStatus {
			latest.Replayed = true
			return latest, nil
		}
		return Outcome{}, fmt.Errorf("%w: instance %s is %s", ErrTerminal, inst.InstanceID, inst.RuntimeStatus)
	}
	if !found || latest.Decision != workflow.CompensationRequired {
		return Outcome{}, fmt.Errorf("%w: instance %s holds no compensation obligation", ErrNoObligation, inst.InstanceID)
	}
	obligation := latest
	d := discharger{req: req, comp: comp}
	return d.discharge(ctx, ex, inst, obligation)
}

type discharger struct {
	req  DischargeRequest
	comp Compensator
}

// discharge drains obligation's refs in reverse recorded order. The judge
// records the obligation in node-execution order, so the reverse unwinds the
// most recently committed effect first.
func (d discharger) discharge(ctx context.Context, ex Executor, inst runtime.Instance, obligation Outcome) (Outcome, error) {
	at := d.req.RecordedAt.UTC()
	items := parseObligation(obligation)
	version := inst.InstanceVersion
	var discharged []string
	var remaining []string
	var remainingRefs []string
	evidence := make([]workflow.EffectDisposition, 0, len(items))
	reasons := make([]Reason, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		detail, done, err := d.compensateOne(ctx, ex, inst, item, at, &version)
		if err != nil {
			return Outcome{}, err
		}
		if done {
			discharged = append(discharged, item.EffectID)
			evidence = append(evidence, workflow.EffectDisposition{ID: item.EffectID, Disposition: EffectCompensated})
		} else {
			remaining = append(remaining, item.EffectID)
			remainingRefs = append(remainingRefs, items[i].raw)
			evidence = append(evidence, workflow.EffectDisposition{ID: item.EffectID, Disposition: EffectRemaining + ":" + detail})
			reasons = append(reasons, Reason{Code: ReasonEffectCompensation, NodeID: item.NodeID, Ref: item.EffectID})
		}
	}
	out := Outcome{
		DecisionID: decisionID(inst), Evidence: workflow.CancellationOutcome{Phase: phaseOf(inst), Effects: evidence},
		Reasons: reasons, CompensationRefs: remainingRefs,
		StatusBefore: inst.RuntimeStatus, VersionBefore: inst.InstanceVersion, Instance: inst,
	}
	if len(remaining) > 0 {
		out.Decision = workflow.RepairRequired
	} else {
		out.Decision = workflow.Cancelled
		out.Reasons = nil
		for _, id := range discharged {
			out.Reasons = append(out.Reasons, Reason{Code: ReasonEffectCompensation, Ref: id})
		}
	}
	out.Evidence.Decision = out.Decision
	out.Evidence.Digest = dischargeDigest(inst, out)
	if len(out.CompensationRefs) == 0 {
		out.CompensationRefs = []string{}
	}
	var actErr error
	if out.Decision == workflow.Cancelled {
		out.Instance, actErr = d.cancelled(ctx, ex, inst, version, at)
	} else {
		out.Instance, actErr = d.repair(ctx, ex, inst, version, at)
	}
	if actErr != nil {
		return Outcome{}, actErr
	}
	scanReq := Request{TenantID: d.req.TenantID, InstanceID: d.req.InstanceID, Reason: d.req.Reason,
		RequestedBy: d.req.RequestedBy, RecordedAt: d.req.RecordedAt}
	if err := recordDecision(ctx, ex, out, inst.CompiledPlanHash, obligation.DecisionID, scanReq); err != nil {
		return Outcome{}, err
	}
	return out, nil
}

// obligationItem is a parsed reference with its raw text for the record.
type obligationItem struct {
	CompensationItem
	raw string
}

// parseObligation splits the recorded "effectID=compensation" references.
// Malformed and child-scoped entries are kept with empty coordinates: they
// can never discharge here and fail closed as remaining.
func parseObligation(obligation Outcome) []obligationItem {
	items := make([]obligationItem, 0, len(obligation.CompensationRefs))
	for _, raw := range obligation.CompensationRefs {
		item := obligationItem{raw: raw}
		item.ObligationID = obligation.DecisionID
		id, compensation, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(compensation) == "" {
			item.EffectID = raw
			items = append(items, item)
			continue
		}
		item.EffectID, item.Compensation = id, compensation
		if strings.Contains(id, "/") {
			items = append(items, item)
			continue
		}
		hash := strings.LastIndex(id, "#")
		attempt, err := strconv.Atoi(id[hash+1:])
		if hash < 0 || err != nil || attempt < 1 {
			items = append(items, item)
			continue
		}
		item.NodeID, item.Attempt = id[:hash], attempt
		items = append(items, item)
	}
	return items
}

// compensateOne runs one obligation item. It reports done with an empty
// detail when the compensation was observed and recorded, or not done with a
// short detail naming why the effect remains. Storage failures are the only
// errors: a failed compensation is a remaining effect, not a driver failure.
func (d discharger) compensateOne(ctx context.Context, ex Executor, inst runtime.Instance, item obligationItem, at time.Time, version *int64) (string, bool, error) {
	if item.NodeID == "" {
		if strings.Contains(item.EffectID, "/") {
			return "child effect is discharged by its own instance pass", false, nil
		}
		return "malformed obligation reference", false, nil
	}
	exec, err := (runtime.Store{}).LoadNodeExecution(ctx, ex, inst.TenantID, inst.InstanceID, item.NodeID, item.Attempt)
	if err != nil {
		return "effect execution is not durably recorded", false, nil
	}
	if exec.Status == runtime.NodeCompensated {
		// A finished compensation is never repeated: the resumed pass counts
		// the recorded COMPENSATED transition as this item's discharge.
		return "", true, nil
	}
	if exec.Status != runtime.NodeSucceeded {
		return fmt.Sprintf("effect execution is %s, not a settled success", exec.Status), false, nil
	}
	res, err := d.comp.Compensate(ctx, ex, item.CompensationItem)
	if err != nil {
		return fmt.Sprintf("compensation failed: %v", err), false, nil
	}
	if !res.Compensated {
		return "compensation was not applied", false, nil
	}
	if strings.TrimSpace(res.EvidenceRef) == "" {
		return "compensation reported no observation evidence", false, nil
	}
	_, next, err := (runtime.Store{}).RecordNodeTransition(ctx, ex, runtime.NodeTransition{
		TenantID: inst.TenantID, InstanceID: inst.InstanceID, NodeID: item.NodeID, Attempt: item.Attempt,
		ExpectedInstanceVersion: *version, Status: runtime.NodeCompensated,
		OutputArtifactRef: res.EvidenceRef, CompletedAt: &at,
	})
	if err != nil {
		return "", false, fmt.Errorf("workflow cancellation: record compensation of %s: %w", item.EffectID, err)
	}
	*version = next
	return "", true, nil
}

// cancelled closes timers and open work items exactly as a clean cancel does,
// then moves the instance CANCELLING -> CANCELLED with the same completion.
func (d discharger) cancelled(ctx context.Context, ex Executor, inst runtime.Instance, version int64, at time.Time) (runtime.Instance, error) {
	if err := cancelTimers(ctx, ex, inst, at); err != nil {
		return runtime.Instance{}, err
	}
	scanReq := Request{TenantID: d.req.TenantID, InstanceID: d.req.InstanceID, Reason: d.req.Reason,
		RequestedBy: d.req.RequestedBy, RecordedAt: d.req.RecordedAt}
	if err := cancelOpenWorkItems(ctx, ex, inst, scanReq, at); err != nil {
		return runtime.Instance{}, err
	}
	t := transitionOf(inst, runtime.InstanceCancelled)
	t.ExpectedVersion = version
	t.CurrentNodeIDs = nil
	t.CompletionDimensions = runtime.Dimensions{RequestState: "CANCELLED", ExecutionState: "NOT_PLANNED",
		BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}
	t.CompletedAt = &at
	return (runtime.Store{}).RecordInstanceState(ctx, ex, t)
}

// repair moves the instance CANCELLING -> REPAIR_REQUIRED with the same
// completion a repair verdict records, so a failed compensation lands where
// the policy already lands ambiguous effects, never back in CANCELLING.
func (d discharger) repair(ctx context.Context, ex Executor, inst runtime.Instance, version int64, at time.Time) (runtime.Instance, error) {
	t := transitionOf(inst, runtime.InstanceRepairRequired)
	t.ExpectedVersion = version
	t.CompletionDimensions = runtime.Dimensions{RequestState: "CANCELLED", ExecutionState: "UNKNOWN",
		BusinessState: "UNKNOWN", ConsistencyState: "REPAIR_REQUIRED", ObligationState: "NOT_APPLICABLE"}
	t.CompletedAt = &at
	return (runtime.Store{}).RecordInstanceState(ctx, ex, t)
}

// latestDecision returns the instance's most recently recorded decision.
// Discharge resolves its obligation (and its replay) through the latest row
// rather than the version-pinned one [loadCurrentDecision] reads, because
// every recorded compensation bumps the instance version.
func latestDecision(ctx context.Context, ex Executor, inst runtime.Instance) (Outcome, runtime.InstanceStatus, bool, error) {
	var (
		out          Outcome
		decision     string
		evidence     []byte
		reasons      []string
		compensation []string
		statusBefore string
		statusAfter  string
	)
	err := ex.QueryRow(ctx, `
		SELECT `+decisionColumns+`, status_after
		FROM workflow_cancellation_decision
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY instance_version_before DESC, decided_at DESC
		LIMIT 1`, inst.TenantID, inst.InstanceID).Scan(&out.DecisionID, &decision, &evidence, &reasons,
		&compensation, &statusBefore, &out.VersionBefore, &statusAfter)
	if errors.Is(err, dbport.ErrNoRows) {
		return Outcome{}, "", false, nil
	}
	if err != nil {
		return Outcome{}, "", false, fmt.Errorf("workflow cancellation: read latest decision of %s: %w", inst.InstanceID, err)
	}
	var doc evidenceDocument
	if err := json.Unmarshal(evidence, &doc); err != nil {
		return Outcome{}, "", false, fmt.Errorf("workflow cancellation: decode decision %s evidence: %w", out.DecisionID, err)
	}
	out.Decision = workflow.CancellationDecision(decision)
	out.Evidence, out.Children = doc.Outcome, doc.Children
	for _, r := range reasons {
		out.Reasons = append(out.Reasons, decodeReason(r))
	}
	out.CompensationRefs = compensation
	out.StatusBefore = runtime.InstanceStatus(statusBefore)
	out.Instance = inst
	return out, runtime.InstanceStatus(statusAfter), true, nil
}

func dischargeDigest(inst runtime.Instance, out Outcome) string {
	parts := []string{inst.TenantID.String(), inst.InstanceID.String(), out.Evidence.Phase, string(out.Decision)}
	for _, e := range out.Evidence.Effects {
		parts = append(parts, e.ID+"\x01"+e.Disposition)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
