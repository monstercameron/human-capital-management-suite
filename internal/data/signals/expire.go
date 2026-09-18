package signals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The expiry side of the durable signal path. Receive refuses a signal that
// arrives at or after the subscription's close time, but refusal alone leaves
// the parked run waiting on a wait that can never be satisfied: the
// acknowledgement gate's TIMED_OUT edge would stay declared and compiled but
// untraversed forever. ExpireDue is the sweeper that closes that loop. It
// marks each due OPEN subscription EXPIRED and, in the same transaction,
// records the TIMED_OUT continuation and enqueues its ready work, exactly as
// Receive commits receipt, disposition, continuation and ready work as one
// unit for an ACCEPTED signal. The dispatcher then resumes the parked SIGNAL
// node with the TIMED_OUT outcome through the same claim/settle machinery,
// so an expired wait reaches its declared edge exactly once.

// subscriptionExpired is the terminal state of a wait whose close time passed
// with no accepted signal. The schema's state check already admits it; only
// an expired row stops being receivable (see OpenSubscriptionForNode) while
// staying inspectable.
const subscriptionExpired = "EXPIRED"

// expiryRefPrefix namespaces timeout continuation references apart from
// signal receipts: a timeout carries no signal, so its continuation refers to
// the expired subscription, never to signal bytes that do not exist.
const expiryRefPrefix = "signal-timeout:"

// ExpiryContinuationRef is the reference an expired wait's continuation
// carries. The advancement records it as the SIGNAL node's output, exactly as
// it records "signal:<id>" for a matched receipt.
func ExpiryContinuationRef(subscriptionID uuid.UUID) string {
	return expiryRefPrefix + subscriptionID.String()
}

// ExpiredSubscription is one wait the sweeper closed: the identities a
// timeout resume advances from, never any signal payload.
type ExpiredSubscription struct {
	TenantID       uuid.UUID
	SubscriptionID uuid.UUID
	InstanceID     uuid.UUID
	NodeID         string
	NodeAttempt    int
	ExpiresAt      time.Time
	Version        uint64
	Causal         *runtimestate.CausalMetadata
}

// ErrNoExpiredSubscription reports an expiry lookup that names no EXPIRED
// wait: either the subscription never existed, is still OPEN, or already
// settled another way.
var ErrNoExpiredSubscription = errors.New("signals: no expired signal subscription")

// PendingExpiry is a READY ready-work row whose node attempt an expired wait
// settled, and that wait.
type PendingExpiry struct {
	Ready        runtimestate.ReadyWork
	Subscription ExpiredSubscription
}

// ExpireDue marks every OPEN subscription whose close time is at or before
// now EXPIRED, and commits each wait's TIMED_OUT continuation and ready-work
// enqueue in the same transaction. The update is conditional on the wait
// still being OPEN, so a signal accepted in a concurrent transaction wins
// the race: Receive holds the subscription row lock while it decides, and a
// wait it satisfied is no longer OPEN when this update lands. A second call
// finds nothing to do, which makes a retried sweep a no-op rather than a
// second wakeup.
func (s Store) ExpireDue(ctx context.Context, ex Executor, tenantID uuid.UUID, now time.Time, limit int) ([]ExpiredSubscription, error) {
	if tenantID == uuid.Nil || limit < 1 {
		return nil, errors.New("signals: expiry sweep needs a tenant and a positive limit")
	}
	at := now.UTC()
	rows, err := ex.Query(ctx, `
		SELECT subscription_id, instance_id, node_id, node_attempt, expires_at, subscription_version,
		       correlation_id, causation_id, logical_operation_id, attempt_id,
		       trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND subscription_state = 'OPEN'
		  AND expires_at IS NOT NULL AND expires_at <= $2
		ORDER BY expires_at, subscription_id
		LIMIT $3
		FOR UPDATE`, tenantID, at, limit)
	if err != nil {
		return nil, fmt.Errorf("signals: select due subscriptions: %w", err)
	}
	var due []ExpiredSubscription
	for rows.Next() {
		var sub ExpiredSubscription
		var correlation, causation, logical, attempt, traceID, spanID, traceState *string
		var traceFlags *int16
		var traceExpires *time.Time
		if err := rows.Scan(&sub.SubscriptionID, &sub.InstanceID, &sub.NodeID, &sub.NodeAttempt,
			&sub.ExpiresAt, &sub.Version,
			&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &traceExpires); err != nil {
			rows.Close()
			return nil, fmt.Errorf("signals: scan due subscription: %w", err)
		}
		sub.TenantID = tenantID
		sub.ExpiresAt = sub.ExpiresAt.UTC()
		sub.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, traceExpires)
		due = append(due, sub)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("signals: iterate due subscriptions: %w", err)
	}
	rows.Close()
	// The select's rows are drained and closed before any write: the sweep
	// runs on one connection, which cannot interleave a second statement
	// with an open result set.
	var out []ExpiredSubscription
	for _, sub := range due {
		affected, err := ex.Exec(ctx, `
			UPDATE workflow_signal_subscription
			SET subscription_state = 'EXPIRED', closed_at = $3, subscription_version = subscription_version + 1
			WHERE tenant_id = $1 AND subscription_id = $2 AND subscription_state = 'OPEN'`, tenantID, sub.SubscriptionID, at)
		if err != nil {
			return nil, fmt.Errorf("signals: expire subscription %s: %w", sub.SubscriptionID, err)
		}
		if affected == 0 {
			// A concurrent Receive satisfied the wait between the select
			// and this update; its ACCEPTED path owns the wakeup.
			continue
		}
		if err := applyExpired(ctx, ex, tenantID, sub, at); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, nil
}

// applyExpired records the TIMED_OUT continuation and enqueues its ready
// work. It mirrors applyAccepted: the continuation reference names the
// expired subscription, and the ready-work identity derives from the same
// node-attempt tuple, so one attempt stays one unit of work however the wait
// settled. An accept and an expiry can never both enqueue: each proceeds only
// from the state its own transaction set.
func applyExpired(ctx context.Context, ex Executor, tenantID uuid.UUID, sub ExpiredSubscription, at time.Time) error {
	cont := runtime.ContinuationRecord{TenantID: tenantID, InstanceID: sub.InstanceID,
		SourceNodeID: sub.NodeID, SourceAttempt: sub.NodeAttempt, TargetNodeID: sub.NodeID,
		Kind: frontier.IntentReady, RouteKey: "TIMED_OUT", Ref: ExpiryContinuationRef(sub.SubscriptionID),
		RecordedAt: at, Causal: runtimeCausal(normalizeCausalAt(sub.Causal, at))}
	if err := (runtime.ContinuationStore{}).MarkReady(ctx, ex, cont); err != nil {
		return fmt.Errorf("signals: record expiry continuation: %w", err)
	}
	readyID := uuid.NewSHA1(signalNamespace, []byte("ready\\x00"+tenantID.String()+"\\x00"+sub.InstanceID.String()+"\\x00"+sub.NodeID+"\\x00"+fmt.Sprint(sub.NodeAttempt)))
	err := (runtimestate.ReadyWorkStore{}).Enqueue(ctx, ex, runtimestate.ReadyWork{TenantID: tenantID, ReadyWorkID: readyID,
		InstanceID: sub.InstanceID, NodeID: sub.NodeID, Attempt: sub.NodeAttempt, State: runtimestate.ReadyReady,
		EligibleAt: at, EnqueuedAt: at, Causal: normalizeCausalAt(sub.Causal, at)})
	if errors.Is(err, runtimestate.ErrDuplicate) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("signals: enqueue expiry ready work: %w", err)
	}
	return nil
}

// LoadExpiredSubscription loads one EXPIRED wait for a timeout resume to
// advance from. Anything but an EXPIRED row is [ErrNoExpiredSubscription]: an
// OPEN wait has a live intake, and a satisfied or cancelled one already
// settled another way.
func (s Store) LoadExpiredSubscription(ctx context.Context, ex Executor, tenantID, subscriptionID uuid.UUID) (ExpiredSubscription, error) {
	if tenantID == uuid.Nil || subscriptionID == uuid.Nil {
		return ExpiredSubscription{}, errors.New("signals: expired subscription lookup needs tenant and subscription")
	}
	var out ExpiredSubscription
	var state string
	var correlation, causation, logical, attempt, traceID, spanID, traceState *string
	var traceFlags *int16
	var traceExpires *time.Time
	err := ex.QueryRow(ctx, `
		SELECT subscription_id, instance_id, node_id, node_attempt, expires_at, subscription_version, subscription_state,
		       correlation_id, causation_id, logical_operation_id, attempt_id,
		       trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND subscription_id = $2`, tenantID, subscriptionID).
		Scan(&out.SubscriptionID, &out.InstanceID, &out.NodeID, &out.NodeAttempt,
			&out.ExpiresAt, &out.Version, &state,
			&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &traceExpires)
	if err != nil {
		return ExpiredSubscription{}, fmt.Errorf("%w: subscription %s: %v", ErrNoExpiredSubscription, subscriptionID, err)
	}
	if state != subscriptionExpired {
		return ExpiredSubscription{}, fmt.Errorf("%w: subscription %s is %s", ErrNoExpiredSubscription, subscriptionID, state)
	}
	out.TenantID = tenantID
	out.ExpiresAt = out.ExpiresAt.UTC()
	out.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, traceExpires)
	return out, nil
}

// ExpiredForNode loads the EXPIRED wait that settled one node attempt, and
// reports false when no expiry did. A dispatcher that reached a ready-work
// row with no matched receipt uses it to tell a timed-out wait (resume as
// TIMED_OUT) from a row that is simply not signal work (pass to the next
// dispatcher).
func (s Store) ExpiredForNode(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, nodeID string, attempt int) (ExpiredSubscription, bool, error) {
	var out ExpiredSubscription
	var state string
	var expiresAt *time.Time
	var version uint64
	var correlation, causation, logical, att, traceID, spanID, traceState *string
	var traceFlags *int16
	var traceExpires *time.Time
	err := ex.QueryRow(ctx, `
		SELECT subscription_id, instance_id, node_id, node_attempt, expires_at, subscription_version, subscription_state,
		       correlation_id, causation_id, logical_operation_id, attempt_id,
		       trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND node_attempt = $4
		ORDER BY created_at DESC, subscription_id DESC
		LIMIT 1`, tenantID, instanceID, nodeID, attempt).
		Scan(&out.SubscriptionID, &out.InstanceID, &out.NodeID, &out.NodeAttempt,
			&expiresAt, &version, &state,
			&correlation, &causation, &logical, &att, &traceID, &spanID, &traceFlags, &traceState, &traceExpires)
	if errors.Is(err, dbport.ErrNoRows) {
		return ExpiredSubscription{}, false, nil
	}
	if err != nil {
		return ExpiredSubscription{}, false, fmt.Errorf("signals: load subscription for node: %w", err)
	}
	if state != subscriptionExpired {
		return ExpiredSubscription{}, false, nil
	}
	out.TenantID = tenantID
	if expiresAt != nil {
		out.ExpiresAt = expiresAt.UTC()
	}
	out.Version = version
	out.Causal = causalFromPointers(correlation, causation, logical, att, traceID, spanID, traceFlags, traceState, traceExpires)
	return out, true, nil
}

// PendingExpired returns READY, eligible ready work whose node attempt an
// EXPIRED wait settled, on an instance whose status admits advancement (a
// paused, quarantined, blocked or terminal instance's expiry continuation
// stays READY and visible, never dispatched), row-locked (FOR UPDATE OF rw
// SKIP LOCKED) in the caller's transaction so a concurrent sweeper steps over
// it. It is PendingMatched's counterpart for waits that closed with no
// signal: the join is against the subscription row alone, because an expiry
// has no receipt.
func (s Store) PendingExpired(ctx context.Context, ex Executor, tenantID uuid.UUID, now time.Time, limit int) ([]PendingExpiry, error) {
	if tenantID == uuid.Nil || limit < 1 {
		return nil, errors.New("signals: pending expiry lookup needs a tenant and a positive limit")
	}
	rows, err := ex.Query(ctx, `
		SELECT rw.ready_work_id, rw.instance_id, rw.node_id, rw.attempt, rw.ready_state, rw.priority,
		       rw.eligible_at, rw.ready_version, rw.enqueued_at,
		       sub.subscription_id, sub.instance_id, sub.node_id, sub.node_attempt, sub.expires_at, sub.subscription_version,
		       sub.correlation_id, sub.causation_id, sub.logical_operation_id, sub.attempt_id,
		       sub.trace_id, sub.trace_span_id, sub.trace_flags, sub.trace_state, sub.trace_link_expires_at
		FROM workflow_ready_work rw
		JOIN workflow_signal_subscription sub
		  ON sub.tenant_id = rw.tenant_id AND sub.instance_id = rw.instance_id
		 AND sub.node_id = rw.node_id AND sub.node_attempt = rw.attempt
		JOIN workflow_instance wi
		  ON wi.tenant_id = rw.tenant_id AND wi.instance_id = rw.instance_id
		WHERE rw.tenant_id = $1 AND rw.ready_state = 'READY' AND rw.eligible_at <= $2
		  AND sub.subscription_state = 'EXPIRED'
		  AND wi.runtime_status IN ('CREATED', 'RUNNING', 'WAITING')
		ORDER BY rw.eligible_at, rw.ready_work_id
		LIMIT $3
		FOR UPDATE OF rw SKIP LOCKED`, tenantID, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("signals: select pending expiries: %w", err)
	}
	defer rows.Close()
	var out []PendingExpiry
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var pending PendingExpiry
		var version int64
		var correlation, causation, logical, attempt, traceID, spanID, traceState *string
		var traceFlags *int16
		var traceExpires *time.Time
		ready := &pending.Ready
		sub := &pending.Subscription
		if err := rows.Scan(&ready.ReadyWorkID, &ready.InstanceID, &ready.NodeID, &ready.Attempt, &ready.State,
			&ready.Priority, &ready.EligibleAt, &version, &ready.EnqueuedAt,
			&sub.SubscriptionID, &sub.InstanceID, &sub.NodeID, &sub.NodeAttempt, &sub.ExpiresAt, &sub.Version,
			&correlation, &causation, &logical, &attempt, &traceID, &spanID, &traceFlags, &traceState, &traceExpires); err != nil {
			return nil, fmt.Errorf("signals: scan pending expiry: %w", err)
		}
		if seen[ready.ReadyWorkID] {
			continue
		}
		seen[ready.ReadyWorkID] = true
		ready.TenantID = tenantID
		ready.Version = uint64(version)
		ready.EligibleAt, ready.EnqueuedAt = ready.EligibleAt.UTC(), ready.EnqueuedAt.UTC()
		sub.TenantID = tenantID
		sub.ExpiresAt = sub.ExpiresAt.UTC()
		sub.Causal = causalFromPointers(correlation, causation, logical, attempt, traceID, spanID, traceFlags, traceState, traceExpires)
		out = append(out, pending)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("signals: iterate pending expiries: %w", err)
	}
	return out, nil
}
