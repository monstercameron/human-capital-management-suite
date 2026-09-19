package signals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OpenSubscription is the intake half of a parked SIGNAL wait: the durable
// subscription row a correlated event must satisfy. It carries everything the
// intake needs to build the signal from the wait itself (event, correlation,
// schema, accepted sources, close window), so the intake never re-derives
// what the driver already pinned when the node parked.
type OpenSubscription struct {
	ID                                          uuid.UUID
	InstanceID                                  uuid.UUID
	NodeID                                      string
	NodeAttempt                                 int
	EventType, CorrelationKey, CorrelationValue string
	ExpectedSchemaRef                           string
	AcceptedSources                             []string
	Ordering                                    string
	ClosesAt                                    *time.Time
	Version                                     uint64
}

// ErrNoOpenSubscription reports an instance and node with no OPEN signal
// subscription: either the wait was never reached, or an earlier signal
// already settled it.
var ErrNoOpenSubscription = fmt.Errorf("signals: no open signal subscription")

type openSubscriptionRow interface {
	Scan(dest ...any) error
}

func scanOpenSubscription(row openSubscriptionRow, instance uuid.UUID, nodeID string) (OpenSubscription, error) {
	var out OpenSubscription
	var sources []byte
	var closesAt *time.Time
	if err := row.Scan(&out.ID, &out.InstanceID, &out.NodeID, &out.NodeAttempt,
		&out.EventType, &out.CorrelationKey, &out.CorrelationValue, &out.ExpectedSchemaRef,
		&sources, &out.Ordering, &closesAt, &out.Version); err != nil {
		return OpenSubscription{}, fmt.Errorf("%w for instance %s node %s: %v", ErrNoOpenSubscription, instance, nodeID, err)
	}
	out.ClosesAt = closesAt
	if len(sources) > 0 {
		if err := json.Unmarshal(sources, &out.AcceptedSources); err != nil {
			return OpenSubscription{}, fmt.Errorf("signals: decode accepted sources: %w", err)
		}
	}
	return out, nil
}

// OpenSubscriptionForNode loads the newest OPEN signal subscription for one
// instance node, locking it for the caller's transaction so a concurrent
// intake cannot receive against the same wait twice. Only the OPEN state
// qualifies: a satisfied, expired or cancelled wait is a stage refusal, not
// an intake.
func (s Store) OpenSubscriptionForNode(ctx context.Context, ex Executor, tenant, instance uuid.UUID, nodeID string) (OpenSubscription, error) {
	row := ex.QueryRow(ctx, `
		SELECT subscription_id, instance_id, node_id, node_attempt, event_type,
		       correlation_key, correlation_value, expected_schema_ref,
		       accepted_sources, ordering_expectation, expires_at, subscription_version
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND subscription_state = 'OPEN'
		ORDER BY created_at DESC, subscription_id DESC
		LIMIT 1
		FOR UPDATE`, tenant, instance, nodeID)
	return scanOpenSubscription(row, instance, nodeID)
}

// OpenSubscriptionForCorrelation loads the newest OPEN signal subscription
// for one node by its correlation value, locking it for the caller's
// transaction. The journey intake knows the intent, not the runtime instance
// id; intent-scoped correlation values name exactly one wait, so the row also
// resolves which instance is parked.
func (s Store) OpenSubscriptionForCorrelation(ctx context.Context, ex Executor, tenant uuid.UUID, nodeID, correlationValue string) (OpenSubscription, error) {
	row := ex.QueryRow(ctx, `
		SELECT subscription_id, instance_id, node_id, node_attempt, event_type,
		       correlation_key, correlation_value, expected_schema_ref,
		       accepted_sources, ordering_expectation, expires_at, subscription_version
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND node_id = $2 AND correlation_value = $3 AND subscription_state = 'OPEN'
		ORDER BY created_at DESC, subscription_id DESC
		LIMIT 1
		FOR UPDATE`, tenant, nodeID, correlationValue)
	return scanOpenSubscription(row, uuid.Nil, nodeID)
}
