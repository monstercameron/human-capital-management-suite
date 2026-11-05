// WF-RUN-039 audit closure (2026-09-24): the served cell capability
// gateway consults tenant-resolved bypass obligations, so an overdue
// obligation suspends the served capability.
//
// The suspension source reads the durable operator journal — the same rows
// the operator gateway's bypass receipts land in — and resolves the tenant
// from the trust principal the served invocation carries. Ungoverned
// capabilities never consult it: only the workflow authority families
// suspend. A cell composed without the execution database keeps today's
// unwired gateway, matching the precedent that a cell without that
// database has no operator doors at all.
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operatorjournal"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// suspensionTenant resolves the tenant one capability invocation belongs
// to from the trust principal the served path authenticated. No principal,
// or an unnameable tenant, resolves nothing — and the suspension source
// fails closed on an unnameable tenant, suspending rather than admitting.
func suspensionTenant(ctx context.Context) (values.TenantId, bool) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return "", false
	}
	tenant := principal.Tenant()
	if err := tenant.Validate(); err != nil {
		return "", false
	}
	return tenant, true
}

// suspensionsOverStore builds the capability suspension source over one
// obligation store. It is what the cell wires and what the served
// suspension proof exercises.
func suspensionsOverStore(store operator.ObligationStore, now func() time.Time) (*operator.CapabilitySuspensions, error) {
	return operator.NewCapabilitySuspensions(store, suspensionTenant, now)
}

// composeCapabilitySuspensions builds the served suspension source over
// the durable operator journal, or nil when the cell has no execution
// database and tenant mapping to read obligations from. A nil source
// keeps the gateway exactly as it is composed today.
func composeCapabilitySuspensions(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) *operator.CapabilitySuspensions {
	if db == nil || tenantUUID == nil {
		return nil
	}
	journal := &operatorjournal.Journal{DB: db, TenantIDs: operatorjournal.TenantIDs(func(tenant values.TenantId) (uuid.UUID, error) {
		id := tenantUUID(tenant)
		if id == uuid.Nil {
			return uuid.Nil, fmt.Errorf("app: tenant %s has no storage identity", tenant)
		}
		return id, nil
	})}
	src, err := suspensionsOverStore(journal, now)
	if err != nil {
		return nil
	}
	return src
}
