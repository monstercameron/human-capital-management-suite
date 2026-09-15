package signals

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// WF-RUN-005's read side. Receive commits the receipt, the ACCEPTED
// disposition, the READY continuation and the ready-work row as one unit;
// what follows is how a resumer finds that unit again and proves, from the
// committed rows alone, which signal settled which waiting node attempt. The
// advancement consumes the continuation reference ("signal:<id>"), never the
// payload bytes.

// ErrNoMatchedReceipt reports that no ACCEPTED receipt exists for the
// requested signal and subscription (or node attempt).
var ErrNoMatchedReceipt = errors.New("signals: no matched signal receipt")

// ErrSubscriptionReplayMismatch reports a subscription insert that collided
// with a row that is not the replay of the same node attempt.
var ErrSubscriptionReplayMismatch = errors.New("signals: subscription collides with a different wait")

const (
	subscriptionOpen      = "OPEN"
	subscriptionSatisfied = "SATISFIED"
	continuationRefPrefix = "signal:"
)

// SubscriptionIDFor derives the durable subscription identity for one
// activation of one SIGNAL node, so a replayed advancement addresses the
// subscription it already made rather than opening a second wait.
func SubscriptionIDFor(tenantID, instanceID uuid.UUID, nodeID string, attempt int) uuid.UUID {
	return uuid.NewSHA1(signalNamespace, []byte("subscription\x00"+tenantID.String()+"\x00"+instanceID.String()+"\x00"+nodeID+"\x00"+strconv.Itoa(attempt)))
}

// ContinuationRef is the reference an ACCEPTED signal's continuation carries.
func ContinuationRef(signalID uuid.UUID) string { return continuationRefPrefix + signalID.String() }

// MatchedReceipt is one committed signal receipt together with the
// subscription it satisfied and the ACCEPTED disposition that scheduled its
// continuation. It deliberately carries the payload digest, not the payload.
type MatchedReceipt struct {
	TenantID          uuid.UUID
	SignalID          uuid.UUID
	SubscriptionID    uuid.UUID
	InstanceID        uuid.UUID
	NodeID            string
	NodeAttempt       int
	SubscriptionState string
	DispositionStatus stepSignal.Status
	ContinuationRef   string
	SchemaRef         string
	PayloadDigest     string
	AppliedAt         time.Time
	Causal            *runtimestate.CausalMetadata
}

// PendingMatch is a READY ready-work row whose node attempt a matched signal
// receipt settled, and that receipt.
type PendingMatch struct {
	Ready   runtimestate.ReadyWork
	Receipt MatchedReceipt
}

const matchedColumns = `
	r.tenant_id, r.signal_id, r.subscription_id, sub.instance_id, sub.node_id, sub.node_attempt,
	sub.subscription_state, d.status, COALESCE(d.continuation_ref, ''), s.schema_ref, s.payload_digest, r.applied_at,
	sub.correlation_id, sub.causation_id, sub.logical_operation_id, sub.attempt_id,
	sub.trace_id, sub.trace_span_id, sub.trace_flags, sub.trace_state, sub.trace_link_expires_at`

const matchedJoins = `
	FROM workflow_signal_receipt r
	JOIN workflow_signal_subscription sub
	  ON sub.tenant_id = r.tenant_id AND sub.subscription_id = r.subscription_id
	JOIN workflow_signal s
	  ON s.tenant_id = r.tenant_id AND s.signal_id = r.signal_id
	JOIN workflow_signal_disposition d
	  ON d.tenant_id = r.tenant_id AND d.signal_id = r.signal_id
	 AND d.subscription_id = r.subscription_id AND d.status = 'ACCEPTED'`

// SubscribeReplay records a subscription, treating a collision with the same
// deterministic subscription identity for the same node attempt as the replay
// it is. It reports whether the row already existed.
func (s Store) SubscribeReplay(ctx context.Context, ex Executor, in Subscription) (bool, error) {
	err := s.Subscribe(ctx, ex, in)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, runtimestate.ErrDuplicate) {
		return false, err
	}
	var instanceID uuid.UUID
	var nodeID, eventType string
	var attempt int
	scanErr := ex.QueryRow(ctx, `
		SELECT instance_id, node_id, node_attempt, event_type
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND subscription_id = $2`, in.TenantID, in.SubscriptionID).
		Scan(&instanceID, &nodeID, &attempt, &eventType)
	if errors.Is(scanErr, dbport.ErrNoRows) {
		return false, fmt.Errorf("%w: node %s on instance %s already waits under another subscription", ErrSubscriptionReplayMismatch, in.NodeID, in.InstanceID)
	}
	if scanErr != nil {
		return false, fmt.Errorf("signals: load colliding subscription: %w", scanErr)
	}
	if instanceID != in.InstanceID || nodeID != in.NodeID || attempt != in.NodeAttempt || eventType != in.EventType {
		return false, fmt.Errorf("%w: subscription %s belongs to %s/%s attempt %d", ErrSubscriptionReplayMismatch, in.SubscriptionID, instanceID, nodeID, attempt)
	}
	return true, nil
}

// LoadMatchedReceipt loads the ACCEPTED receipt for one signal against one
// subscription. It returns [ErrNoMatchedReceipt] when the signal never
// settled that subscription.
func (Store) LoadMatchedReceipt(ctx context.Context, ex Executor, tenantID, signalID, subscriptionID uuid.UUID) (MatchedReceipt, error) {
	if tenantID == uuid.Nil || signalID == uuid.Nil || subscriptionID == uuid.Nil {
		return MatchedReceipt{}, errors.New("signals: matched receipt lookup needs tenant, signal and subscription")
	}
	row := ex.QueryRow(ctx, `SELECT `+matchedColumns+matchedJoins+`
		WHERE r.tenant_id = $1 AND r.signal_id = $2 AND r.subscription_id = $3`, tenantID, signalID, subscriptionID)
	out, err := scanMatched(row.Scan)
	if errors.Is(err, dbport.ErrNoRows) {
		return MatchedReceipt{}, fmt.Errorf("%w: signal %s subscription %s", ErrNoMatchedReceipt, signalID, subscriptionID)
	}
	if err != nil {
		return MatchedReceipt{}, fmt.Errorf("signals: load matched receipt: %w", err)
	}
	return out, nil
}

// MatchedForNode loads the ACCEPTED receipt that settled one node attempt, and
// reports false when no signal did.
func (Store) MatchedForNode(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, nodeID string, attempt int) (MatchedReceipt, bool, error) {
	rows, err := ex.Query(ctx, `SELECT `+matchedColumns+matchedJoins+`
		WHERE r.tenant_id = $1 AND sub.instance_id = $2 AND sub.node_id = $3 AND sub.node_attempt = $4
		ORDER BY r.applied_at, r.signal_id
		LIMIT 1`, tenantID, instanceID, nodeID, attempt)
	if err != nil {
		return MatchedReceipt{}, false, fmt.Errorf("signals: find matched receipt for node: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return MatchedReceipt{}, false, fmt.Errorf("signals: iterate matched receipt for node: %w", err)
		}
		return MatchedReceipt{}, false, nil
	}
	out, err := scanMatched(rows.Scan)
	if err != nil {
		return MatchedReceipt{}, false, fmt.Errorf("signals: scan matched receipt for node: %w", err)
	}
	return out, true, nil
}

// PendingMatched returns READY, eligible ready work whose node attempt an
// ACCEPTED signal receipt settled, on an instance whose status admits
// advancement (a paused, quarantined, blocked or terminal instance's matched
// continuation stays READY and visible, never dispatched), row-locked (FOR UPDATE OF rw SKIP LOCKED)
// in the caller's transaction so a concurrent sweeper steps over it. Each
// ready-work row appears at most once, paired with the earliest receipt.
func (Store) PendingMatched(ctx context.Context, ex Executor, tenantID uuid.UUID, now time.Time, limit int) ([]PendingMatch, error) {
	if tenantID == uuid.Nil || limit < 1 {
		return nil, errors.New("signals: pending matched lookup needs a tenant and a positive limit")
	}
	rows, err := ex.Query(ctx, `
		SELECT rw.ready_work_id, rw.instance_id, rw.node_id, rw.attempt, rw.ready_state, rw.priority,
		       rw.eligible_at, rw.ready_version, rw.enqueued_at, `+matchedColumns+matchedJoins+`
		JOIN workflow_ready_work rw
		  ON rw.tenant_id = sub.tenant_id AND rw.instance_id = sub.instance_id
		 AND rw.node_id = sub.node_id AND rw.attempt = sub.node_attempt
		JOIN workflow_instance wi
		  ON wi.tenant_id = rw.tenant_id AND wi.instance_id = rw.instance_id
		WHERE rw.tenant_id = $1 AND rw.ready_state = 'READY' AND rw.eligible_at <= $2
		  AND sub.subscription_state = 'SATISFIED'
		  AND wi.runtime_status IN ('CREATED', 'RUNNING', 'WAITING')
		ORDER BY rw.eligible_at, rw.ready_work_id, r.applied_at, r.signal_id
		LIMIT $3
		FOR UPDATE OF rw SKIP LOCKED`, tenantID, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("signals: select pending matched continuations: %w", err)
	}
	defer rows.Close()
	var out []PendingMatch
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var ready runtimestate.ReadyWork
		var version int64
		receipt, err := scanMatched(func(dest ...any) error {
			head := []any{&ready.ReadyWorkID, &ready.InstanceID, &ready.NodeID, &ready.Attempt, &ready.State,
				&ready.Priority, &ready.EligibleAt, &version, &ready.EnqueuedAt}
			return rows.Scan(append(head, dest...)...)
		})
		if err != nil {
			return nil, fmt.Errorf("signals: scan pending matched continuation: %w", err)
		}
		if seen[ready.ReadyWorkID] {
			continue
		}
		seen[ready.ReadyWorkID] = true
		ready.TenantID = tenantID
		ready.Version = uint64(version)
		ready.EligibleAt, ready.EnqueuedAt = ready.EligibleAt.UTC(), ready.EnqueuedAt.UTC()
		out = append(out, PendingMatch{Ready: ready, Receipt: receipt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("signals: iterate pending matched continuations: %w", err)
	}
	return out, nil
}

func scanMatched(scan func(dest ...any) error) (MatchedReceipt, error) {
	var out MatchedReceipt
	var status string
	var correlation, causation, logical, attempt, traceID, spanID, traceState *string
	var traceFlags *int16
	var traceExpires *time.Time
	if err := scan(&out.TenantID, &out.SignalID, &out.SubscriptionID, &out.InstanceID, &out.NodeID, &out.NodeAttempt,
		&out.SubscriptionState, &status, &out.ContinuationRef, &out.SchemaRef, &out.PayloadDigest, &out.AppliedAt,
		&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &traceExpires); err != nil {
		return MatchedReceipt{}, err
	}
	out.DispositionStatus = stepSignal.Status(status)
	out.AppliedAt = out.AppliedAt.UTC()
	out.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, traceExpires)
	return out, nil
}

// Settled reports whether the receipt is the committed evidence a waiting
// node may advance on: an ACCEPTED disposition whose continuation is the
// reference to this exact signal, against a subscription it satisfied.
func (m MatchedReceipt) Settled() bool {
	return m.DispositionStatus == stepSignal.StatusAccepted && m.SubscriptionState == subscriptionSatisfied &&
		strings.HasPrefix(m.ContinuationRef, continuationRefPrefix) && m.ContinuationRef == ContinuationRef(m.SignalID)
}
