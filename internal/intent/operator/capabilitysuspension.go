package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TenantResolver reads the tenant one capability invocation belongs to from
// its context. A composition supplies it from whatever carries the tenant on
// that path (the authenticated principal, a request envelope); this package
// never reaches into another package's context key.
type TenantResolver func(ctx context.Context) (values.TenantId, bool)

// FixedTenant resolves every context to one tenant. It is what a single-tenant
// composition and a test wire.
func FixedTenant(tenant values.TenantId) TenantResolver {
	return func(context.Context) (values.TenantId, bool) { return tenant, tenant.Validate() == nil }
}

// CapabilitySuspensions turns outstanding bypass obligations into capability
// suspensions. It satisfies the capability gateway's suspension port
// structurally, so the capability layer keeps no dependency on the operator
// gateway: the gateway asks "may this capability run", and this answers "not
// while its authority family owes an overdue review".
//
// It fails closed. A store that cannot answer, or a context whose tenant the
// resolver cannot name, suspends the capability rather than admitting it: a
// governance control that fails open is not a control.
type CapabilitySuspensions struct {
	store  ObligationStore
	tenant TenantResolver
	clock  func() time.Time
}

// NewCapabilitySuspensions builds a suspension source over store. clock
// defaults to time.Now.
func NewCapabilitySuspensions(store ObligationStore, tenant TenantResolver, clock func() time.Time) (*CapabilitySuspensions, error) {
	if store == nil || tenant == nil {
		return nil, refuse(CodeInvalidRequest, "", "an obligation store and a tenant resolver are required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &CapabilitySuspensions{store: store, tenant: tenant, clock: clock}, nil
}

// SuspendedCapability reports whether the capability id may be invoked now.
// A capability outside the governed authority families ([FamilyForCapability])
// is never suspended by this source.
func (s *CapabilitySuspensions) SuspendedCapability(ctx context.Context, id string) (string, bool) {
	family, governed := FamilyForCapability(id)
	if !governed {
		return "", false
	}
	if s == nil || s.store == nil || s.tenant == nil {
		return "capability suspension cannot be evaluated: no obligation store is wired", true
	}
	tenant, ok := s.tenant(ctx)
	if !ok {
		return "capability suspension cannot be evaluated: the call names no tenant", true
	}
	outstanding, err := s.store.OutstandingObligations(ctx, tenant)
	if err != nil {
		return "capability suspension cannot be evaluated: " + err.Error(), true
	}
	now := s.clock().UTC()
	o, suspended := SuspendedFamilies(outstanding, now)[family]
	if !suspended {
		return "", false
	}
	return fmt.Sprintf("authority family %s is suspended: bypass obligation %s was due for review at %s",
		family, o.ID, o.DueAt.UTC().Format(time.RFC3339)), true
}
