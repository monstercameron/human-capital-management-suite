package industrypack

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
)

const (
	EntitlementRejectionCode   = "PACK_008_REJECTED"
	EntitlementSKUAbsent       = "SKU_ABSENT"
	EntitlementSuspended       = "SUSPENDED"
	EntitlementExpired         = "EXPIRED"
	EntitlementNotYetEffective = "NOT_YET_EFFECTIVE"
	EntitlementSnapshotAbsent  = "SNAPSHOT_ABSENT"
)

var ErrEntitlementRejected = errors.New("industrypack: commercial entitlement rejected")

// ContractRevisionRef identifies the tenant's current commercial revision.
// A trusted application resolver supplies this reference from tenant state.
type ContractRevisionRef struct {
	ContractID string
	Revision   uint64
}

// ContractRevisionResolver selects the revision bound to a tenant in the
// application's authoritative commercial relationship.
type ContractRevisionResolver interface {
	ResolveContractRevision(context.Context, string) (ContractRevisionRef, error)
}

type ContractRevisionResolverFunc func(context.Context, string) (ContractRevisionRef, error)

func (f ContractRevisionResolverFunc) ResolveContractRevision(ctx context.Context, tenantID string) (ContractRevisionRef, error) {
	return f(ctx, tenantID)
}

// EntitlementClock is supplied by the trusted application boundary.
type EntitlementClock interface{ Now() time.Time }

type EntitlementClockFunc func() time.Time

func (f EntitlementClockFunc) Now() time.Time { return f() }

// IndustryEntitlementBinding pins the commercial decision accepted for an
// industry pack operation.
type IndustryEntitlementBinding struct {
	Decision    commercial.EntitlementDecision `json:"decision"`
	Capability  string                         `json:"capability"`
	EvaluatedAt time.Time                      `json:"evaluated_at"`
}

func (b IndustryEntitlementBinding) Fingerprint() string { return b.Decision.Fingerprint }

type EntitlementRejection struct {
	Code       string
	State      string
	Capability string
}

func (e *EntitlementRejection) Error() string {
	return fmt.Sprintf("%s: industry pack entitlement %s", e.Code, e.State)
}

func (e *EntitlementRejection) Unwrap() error { return ErrEntitlementRejected }

// IndustryEntitlementCapability maps an industry to its sold commercial SKU.
func IndustryEntitlementCapability(industry Industry) string {
	return "hcmnext.industrypack." + strings.ToLower(string(industry)) + "/v1"
}

// IndustryEntitlementAuthority resolves the tenant's frozen commercial
// revision at an application boundary and supplies trusted time to admission.
type IndustryEntitlementAuthority struct {
	store          commercial.ContractRevisionStore
	revisions      ContractRevisionResolver
	clock          EntitlementClock
	maxEnvelopeAge time.Duration
}

// NewIndustryEntitlementAuthority composes the durable snapshot store, the
// trusted tenant-to-revision mapping, and the application clock. A zero maxAge
// disables envelope-age enforcement only when selected by trusted composition.
func NewIndustryEntitlementAuthority(store commercial.ContractRevisionStore, revisions ContractRevisionResolver, clock EntitlementClock, maxAge time.Duration) (*IndustryEntitlementAuthority, error) {
	if store == nil || revisions == nil || clock == nil || maxAge < 0 {
		return nil, errors.New("industrypack: entitlement authority dependencies are required")
	}
	return &IndustryEntitlementAuthority{store: store, revisions: revisions, clock: clock, maxEnvelopeAge: maxAge}, nil
}

func (a *IndustryEntitlementAuthority) now() (time.Time, error) {
	if a == nil || a.clock == nil {
		return time.Time{}, &EntitlementRejection{Code: EntitlementRejectionCode, State: EntitlementSnapshotAbsent}
	}
	now := a.clock.Now().UTC()
	if now.IsZero() {
		return time.Time{}, &EntitlementRejection{Code: EntitlementRejectionCode, State: EntitlementSnapshotAbsent}
	}
	return now, nil
}

func (a *IndustryEntitlementAuthority) admit(ctx context.Context, tenantID string, industry Industry) (IndustryEntitlementBinding, error) {
	if a == nil || a.store == nil || a.revisions == nil || strings.TrimSpace(tenantID) == "" || !industry.Valid() {
		return IndustryEntitlementBinding{}, &EntitlementRejection{Code: EntitlementRejectionCode, State: EntitlementSnapshotAbsent}
	}
	ref, err := a.revisions.ResolveContractRevision(ctx, tenantID)
	if err != nil || strings.TrimSpace(ref.ContractID) == "" || ref.Revision == 0 {
		return IndustryEntitlementBinding{}, &EntitlementRejection{Code: EntitlementRejectionCode, State: EntitlementSnapshotAbsent}
	}
	snapshot, err := a.store.GetEntitlementSnapshot(ctx, tenantID, ref.ContractID, ref.Revision)
	if err != nil || snapshot.Validate() != nil || snapshot.TenantID() != tenantID || snapshot.ContractID() != ref.ContractID || snapshot.Revision() != ref.Revision {
		return IndustryEntitlementBinding{}, &EntitlementRejection{Code: EntitlementRejectionCode, State: EntitlementSnapshotAbsent}
	}
	now, err := a.now()
	if err != nil {
		return IndustryEntitlementBinding{}, err
	}
	capability := IndustryEntitlementCapability(industry)
	decision := snapshot.Resolve(commercial.EntitlementRequest{TenantID: tenantID, Capability: capability, At: now})
	if decision.Allowed() {
		return IndustryEntitlementBinding{Decision: decision, Capability: capability, EvaluatedAt: now}, nil
	}
	state := EntitlementSKUAbsent
	switch decision.Code {
	case commercial.CodeContractSuspended, commercial.CodeContractRevoked:
		state = EntitlementSuspended
	case commercial.CodeContractExpired:
		state = EntitlementExpired
	case commercial.CodeContractNotYetEffective:
		state = EntitlementNotYetEffective
	}
	return IndustryEntitlementBinding{Decision: decision, Capability: capability, EvaluatedAt: now}, &EntitlementRejection{Code: EntitlementRejectionCode, State: state, Capability: capability}
}
