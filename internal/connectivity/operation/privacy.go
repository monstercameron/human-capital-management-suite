package operation

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	privacydispatch "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/dispatch"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// ErrPrivacyGateRequired fails a governed dispatch closed when no DLP policy
// evaluator is supplied. Without a live policy there is no current DLP
// decision to bind, so the gate cannot clear anything.
var ErrPrivacyGateRequired = errors.New("operation: privacy dispatch policy is required")

// PrivacyDispatchRequest carries the DLP/transfer snapshot binding for one
// governed dispatch. Destination, purpose and payload always come from the
// journaled operation itself; the caller supplies only the dispatch-time
// observations the journal does not retain: the acting principal, the
// declared data classes and the payload inspection findings.
type PrivacyDispatchRequest struct {
	Lease       Lease
	Writer      Writer
	Binding     privacydispatch.Binding
	Current     privacydispatch.SnapshotSet
	Principal   string
	DataClasses []dlp.DataClass
	Inspection  dlp.Inspection
}

// PrivacyGrant is one resolved snapshot authority answer: the planned
// binding, the current signed set, the live DLP policy evaluator and the
// dispatch-time observations for the operation under admission.
type PrivacyGrant struct {
	Binding     privacydispatch.Binding
	Current     privacydispatch.SnapshotSet
	Policy      privacydispatch.DLPDecisioner
	Principal   string
	DataClasses []dlp.DataClass
	Inspection  dlp.Inspection
}

// StaticPrivacyAuthority serves one configured grant for every operation.
// Scope is enforced by the gate, not the authority: snapshots sealed for
// another destination, purpose or classification deny at admission with
// EGRESS_BLOCKED, exactly as a policy denial does.
type StaticPrivacyAuthority struct {
	grant PrivacyGrant
}

// NewStaticPrivacyAuthority fails fast when either snapshot set is
// malformed or the policy evaluator is missing. A revoked current set
// stays constructible: revocation is runtime state the gate evaluates per
// dispatch.
func NewStaticPrivacyAuthority(grant PrivacyGrant) (StaticPrivacyAuthority, error) {
	if grant.Policy == nil {
		return StaticPrivacyAuthority{}, ErrPrivacyGateRequired
	}
	if err := grant.Binding.Validate(); err != nil {
		return StaticPrivacyAuthority{}, err
	}
	if err := grant.Current.Validate(); err != nil {
		return StaticPrivacyAuthority{}, err
	}
	return StaticPrivacyAuthority{grant: grant}, nil
}

// ResolvePrivacyGrant serves the configured grant. The data-class slice is
// copied so concurrent admissions never share backing memory.
func (a StaticPrivacyAuthority) ResolvePrivacyGrant(_ context.Context, _ Operation) (PrivacyGrant, error) {
	grant := a.grant
	grant.DataClasses = append([]dlp.DataClass(nil), a.grant.DataClasses...)
	return grant, nil
}

// privacyClearanceSender satisfies privacydispatch.Sender for the
// pre-dispatch clearance check. The gate invokes it only after every signed
// snapshot and the live DLP evaluation pass; it retains the cleared payload
// digest input without performing any external effect. The provider call that
// follows sends those same journaled bytes under the same lease.
type privacyClearanceSender struct {
	payload []byte
}

func (s *privacyClearanceSender) Send(_ context.Context, payload []byte) error {
	s.payload = append([]byte(nil), payload...)
	return nil
}

// DispatchWithPrivacy resolves the current signed DLP/transfer policy
// snapshots through governance/privacy/dispatch before the provider writer
// is called. The gate evaluates the exact journaled payload bytes under the
// operation's own destination and purpose; any refusal returns EGRESS_BLOCKED
// with a durable journal event and zero provider calls, before the operation
// moves to SENDING. An allowed dispatch records PRIVACY_BOUND evidence and
// proceeds through the ordinary dispatch path.
func (j *MemoryJournal) DispatchWithPrivacy(ctx context.Context, req PrivacyDispatchRequest, policy privacydispatch.DLPDecisioner) (DispatchResult, error) {
	if err := contextError(ctx); err != nil {
		return DispatchResult{}, err
	}
	if req.Writer == nil {
		return DispatchResult{}, ErrProviderRequired
	}
	_, err := j.admitPrivacyDispatch(ctx, req.Lease, PrivacyGrant{
		Binding: req.Binding, Current: req.Current, Policy: policy,
		Principal: req.Principal, DataClasses: req.DataClasses, Inspection: req.Inspection,
	})
	if err != nil {
		return DispatchResult{}, err
	}
	return j.Dispatch(ctx, req.Lease, req.Writer)
}

// AdmitPrivacyDispatch evaluates one snapshot authority grant against the
// leased operation without performing any provider call: it is the admission
// half of DispatchWithPrivacy, exposed so the worker role can admit before
// its credentialed send. A denial records EGRESS_BLOCKED and returns the
// gate error; an allow records PRIVACY_BOUND and returns the operation,
// still LEASED. Admission is retry-safe: admitting an already-bound lease
// records another event and still succeeds.
func (j *MemoryJournal) AdmitPrivacyDispatch(ctx context.Context, lease Lease, grant PrivacyGrant) (Operation, error) {
	return j.admitPrivacyDispatch(ctx, lease, grant)
}

// admitPrivacyDispatch validates the fenced lease, resolves the grant
// through governance/privacy/dispatch against the exact journaled payload
// bytes, and records the durable evidence event for either outcome.
func (j *MemoryJournal) admitPrivacyDispatch(ctx context.Context, lease Lease, grant PrivacyGrant) (Operation, error) {
	if err := contextError(ctx); err != nil {
		return Operation{}, err
	}
	if grant.Policy == nil {
		return Operation{}, ErrPrivacyGateRequired
	}
	op, err := j.Get(ctx, lease.TenantID, lease.OperationID)
	if err != nil {
		return Operation{}, err
	}
	if op.State != StateLeased {
		return Operation{}, ErrLeaseRequired
	}
	if lease.Token == uuid.Nil || lease.FenceToken != op.FenceToken || lease.ExpiresAt.IsZero() {
		return Operation{}, fmt.Errorf("%w: privacy-bound lease no longer owns operation", ErrLeaseFenced)
	}
	cleared := &privacyClearanceSender{}
	_, gateErr := privacydispatch.Dispatch(ctx, grant.Binding, grant.Current, privacydispatch.Request{
		Principal:   grant.Principal,
		Destination: op.DestinationRef,
		Purpose:     op.Purpose,
		DataClasses: append([]dlp.DataClass(nil), grant.DataClasses...),
		Inspection:  grant.Inspection,
		Payload:     append([]byte(nil), op.MappedPayload...),
	}, grant.Policy, cleared)
	if gateErr != nil {
		if errors.Is(gateErr, privacydispatch.ErrEGRESSBlocked) {
			j.mu.Lock()
			current, ok := j.operations[op.OperationID]
			if !ok {
				j.mu.Unlock()
				return Operation{}, fmt.Errorf("%w: %s", ErrNotFound, op.OperationID)
			}
			j.appendJournalLocked(current, current.State, current.State, "EGRESS_BLOCKED", uuid.Nil, lease.FenceToken, "", grant.Binding.OperationDigest, j.now().UTC())
			j.mu.Unlock()
		}
		return Operation{}, gateErr
	}
	j.mu.Lock()
	current, ok := j.operations[op.OperationID]
	if !ok {
		j.mu.Unlock()
		return Operation{}, fmt.Errorf("%w: %s", ErrNotFound, op.OperationID)
	}
	if current.State != StateLeased || current.FenceToken != lease.FenceToken {
		j.mu.Unlock()
		return Operation{}, fmt.Errorf("%w: privacy-bound lease no longer owns operation", ErrLeaseFenced)
	}
	j.appendJournalLocked(current, current.State, current.State, "PRIVACY_BOUND", uuid.Nil, lease.FenceToken, "", grant.Binding.OperationDigest, j.now().UTC())
	j.mu.Unlock()
	return current, nil
}
