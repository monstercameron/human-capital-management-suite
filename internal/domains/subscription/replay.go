package subscription

import (
	"errors"
	"fmt"
	"strings"
)

// ErrReplayRejected identifies a replay refused at the planning boundary. It
// wraps every *ReplayRejection; the rejection names the offending field,
// state and event schema version.
var ErrReplayRejected = errors.New("subscription: replay rejected")

// ReplayRejectionCode is the stable contract code every replay refusal
// carries.
const ReplayRejectionCode = "SUB_006_REJECTED"

// ReplayRejection is the typed SUB_006_REJECTED error. Field names the
// offending input, State its offending value (digests and opaque references
// report only "mismatch" so refusals never echo them), and Version the event
// schema version the replay was attempted under.
type ReplayRejection struct {
	Code    string
	Field   string
	State   string
	Version string
}

func (e *ReplayRejection) Error() string {
	return fmt.Sprintf("subscription: %s field=%s state=%s version=%s", e.Code, e.Field, e.State, e.Version)
}

// Unwrap reports ErrReplayRejected for errors.Is.
func (e *ReplayRejection) Unwrap() error { return ErrReplayRejected }

func rejectReplay(field, state string, schemaVersion int) *ReplayRejection {
	return &ReplayRejection{Code: ReplayRejectionCode, Field: field, State: state, Version: fmt.Sprint(schemaVersion)}
}

// ReplayRequest proposes one redelivery of a recorded outbound request. The
// recorded request is resent as-is on its own journal lineage, so a replay
// can never fork a duplicate lineage or invent a new event. Candidate must
// equal the recorded envelope bit for bit; any difference is invented
// business history and is refused.
type ReplayRequest struct {
	Original     DeliveryRequest
	Candidate    CanonicalEnvelope
	Subscription EventSubscription
	Grant        ScopeGrant
	Epoch        uint64
	RequestedBy  string
}

// Replay is the authorized redelivery plan. Delivery is the recorded request
// on its original lineage key; Epoch is the operator-declared redelivery
// generation carried on the evidence, and Auth is the rechecked current
// authorization. Replays travel straight to the journal with fresh AuthZ
// evidence: autonomous retries stay bounded by the dispatcher policy, while
// each replay generation is human-attributed.
type Replay struct {
	Delivery       DeliveryRequest
	Epoch          uint64
	RequestedBy    string
	OriginalDigest string
	Auth           AuthorizationEvent
}

// Explain returns a redaction-safe replay summary.
func (r Replay) Explain() string {
	return fmt.Sprintf("subscription replay subscription=%s epoch=%d sequence=%d digest=%s delivery=%s requested_by=%s",
		r.Delivery.SubscriptionID, r.Epoch, r.Delivery.Envelope.Sequence, r.OriginalDigest, r.Delivery.IdempotencyKey, r.RequestedBy)
}

// PlanReplay authorizes one redelivery without appending a new domain event.
// It has no side effects: it neither journals, dead-letters nor calls a
// provider. The caller delivers Replay.Delivery through the journal as the
// lineage's next attempt.
func PlanReplay(request ReplayRequest) (Replay, error) {
	version := request.Candidate.SchemaVersion
	if strings.TrimSpace(request.RequestedBy) == "" || strings.TrimSpace(request.RequestedBy) != request.RequestedBy {
		return Replay{}, rejectReplay("requested_by", "missing", version)
	}
	if request.Epoch == 0 {
		return Replay{}, rejectReplay("epoch", "missing", version)
	}
	recorded, err := request.Original.validate()
	if err != nil {
		return Replay{}, rejectReplay("replay_request", "invalid", version)
	}
	if err := request.Candidate.Validate(); err != nil {
		return Replay{}, rejectReplay("replay_envelope", "invalid", version)
	}
	if field, state := inventedHistory(recorded.Envelope, request.Candidate); field != "" {
		return Replay{}, rejectReplay(field, state, version)
	}
	if err := request.Subscription.Verify(); err != nil {
		return Replay{}, rejectReplay("subscription", "invalid", version)
	}
	if request.Subscription.State != StateActive {
		return Replay{}, rejectReplay("subscription_state", string(request.Subscription.State), version)
	}
	if request.Subscription.SubscriptionID != recorded.SubscriptionID {
		return Replay{}, rejectReplay("subscription_id", recorded.SubscriptionID, version)
	}
	if request.Subscription.TenantScope != recorded.Envelope.Tenant {
		return Replay{}, rejectReplay("tenant_scope", recorded.Envelope.Tenant, version)
	}
	decision, err := Authorize(request.Subscription, request.Grant)
	if err != nil {
		return Replay{}, rejectReplay("authorization", authzState(err), version)
	}
	return Replay{Delivery: cloneDeliveryRequest(recorded), Epoch: request.Epoch, RequestedBy: request.RequestedBy,
		OriginalDigest: recorded.Envelope.Digest(), Auth: decision.Event(request.Grant)}, nil
}

// inventedHistory names the first identity field where a replay candidate
// differs from the recorded envelope. An empty field means the candidate is
// the recorded event.
func inventedHistory(recorded, candidate CanonicalEnvelope) (field, state string) {
	switch {
	case candidate.Sequence != recorded.Sequence:
		return "sequence", fmt.Sprint(candidate.Sequence)
	case candidate.Kind != recorded.Kind:
		return "event_kind", string(candidate.Kind)
	case candidate.SchemaVersion != recorded.SchemaVersion:
		return "schema_version", fmt.Sprint(candidate.SchemaVersion)
	case candidate.Tenant != recorded.Tenant:
		return "tenant_scope", candidate.Tenant
	case candidate.PayloadDigest != recorded.PayloadDigest:
		return "payload_digest", "mismatch"
	case candidate.ProvenanceRef != recorded.ProvenanceRef:
		return "provenance_ref", "mismatch"
	case !sameRefs(candidate.SubjectRefs, recorded.SubjectRefs):
		return "subject_refs", "mismatch"
	case !candidate.EffectiveAt.Equal(recorded.EffectiveAt):
		return "effective_at", "mismatch"
	case !candidate.KnownAt.Equal(recorded.KnownAt):
		return "known_at", "mismatch"
	default:
		return "", ""
	}
}

func sameRefs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func authzState(err error) string {
	message := err.Error()
	if message == "" {
		return "denied"
	}
	const marker = "subscription: authorization scope denied: "
	if strings.HasPrefix(message, marker) {
		return strings.TrimPrefix(message, marker)
	}
	return message
}
