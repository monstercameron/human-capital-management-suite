package agenteval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type syntheticProvisionReaderProbe struct {
	record agentstore.SyntheticTenantProvisionRecord
	err    error
}

func (p syntheticProvisionReaderProbe) SyntheticTenantProvision(context.Context, uuid.UUID, string) (agentstore.SyntheticTenantProvisionRecord, error) {
	return p.record, p.err
}

type syntheticRuntimeResolverProbe struct {
	provision SyntheticTaskProvision
	err       error
	seen      agentstore.SyntheticTenantProvisionRecord
	tamper    bool
}

func (p *syntheticRuntimeResolverProbe) ComposeSyntheticTaskRuntime(_ context.Context, tenant values.TenantId, record agentstore.SyntheticTenantProvisionRecord) (SyntheticTaskProvision, error) {
	p.seen = record
	p.provision = validSyntheticProvision(tenant.String())
	p.provision.Marker = SyntheticTenantMarker{
		TenantID: tenant.String(), MarkerID: record.MarkerID, Purpose: record.Purpose,
		ProvisionID: record.ProvisionID, Status: record.Status, ExpiresAt: record.ExpiresAt,
	}
	p.provision.Isolation = SyntheticTaskIsolation{
		TenantID: tenant.String(), GrantStoreID: record.GrantStoreID, TaskStoreID: record.TaskStoreID,
		BudgetLedgerID: record.BudgetLedgerID, AuditStoreID: record.AuditStoreID,
		ToolOwnerID: record.ToolOwnerID, ToolOwnerProfile: record.ToolOwnerProfile,
	}
	if p.tamper {
		p.provision.Isolation.TaskStoreID = p.provision.Isolation.GrantStoreID
	}
	return p.provision, p.err
}

func TestPersistedSyntheticAuthorityLoadsMarkerAndPinsIsolatedRuntime(t *testing.T) {
	tenantUUID := uuid.New()
	tenant := values.TenantId("synthetic-promotion")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	record := agentstore.SyntheticTenantProvisionRecord{
		TenantID: tenantUUID, SuiteTenantID: tenant.String(), ProvisionID: "provision-7", MarkerID: "marker-7", Purpose: syntheticEvaluationPurpose,
		Status: "ACTIVE", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		GrantStoreID: "grant-store-7", TaskStoreID: "task-store-7", BudgetLedgerID: "budget-7",
		AuditStoreID: "audit-7", ToolOwnerID: "fixture-owner-7", ToolOwnerProfile: "fixture-only/v1",
	}
	runtime := &syntheticRuntimeResolverProbe{}
	reader := syntheticProvisionReaderProbe{record: record}
	authority, err := NewPersistedSyntheticTaskProvisionAuthority(reader, runtime, func(_ context.Context, got values.TenantId) (uuid.UUID, error) {
		if got != tenant {
			return uuid.Nil, ErrSyntheticProvisionUnavailable
		}
		return tenantUUID, nil
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.AuthorizeSyntheticTenant(context.Background(), tenant); err != nil {
		t.Fatalf("AuthorizeSyntheticTenant() error = %v", err)
	}
	provision, err := authority.ResolveSyntheticTaskProvision(context.Background(), tenant)
	if err != nil {
		t.Fatalf("ResolveSyntheticTaskProvision() error = %v", err)
	}
	if runtime.seen.ProvisionID != record.ProvisionID {
		t.Fatalf("runtime composer saw provision %q, want %q", runtime.seen.ProvisionID, record.ProvisionID)
	}
	if provision.Marker.MarkerID != record.MarkerID || provision.Marker.ProvisionID != record.ProvisionID || provision.Marker.TenantID != tenant.String() {
		t.Fatalf("resolved marker = %+v, want durable marker %+v", provision.Marker, record)
	}
	if provision.Isolation.GrantStoreID != record.GrantStoreID || provision.Isolation.TaskStoreID != record.TaskStoreID ||
		provision.Isolation.BudgetLedgerID != record.BudgetLedgerID || provision.Isolation.AuditStoreID != record.AuditStoreID ||
		provision.Isolation.ToolOwnerID != record.ToolOwnerID {
		t.Fatalf("resolved isolation = %+v, want durable binding IDs", provision.Isolation)
	}
}

func TestPersistedSyntheticAuthorityRejectsRuntimeBindingMismatch(t *testing.T) {
	tenantUUID := uuid.New()
	tenant := values.TenantId("synthetic-promotion")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	record := agentstore.SyntheticTenantProvisionRecord{
		TenantID: tenantUUID, SuiteTenantID: tenant.String(), ProvisionID: "provision-mismatch", MarkerID: "marker-mismatch",
		Purpose: syntheticEvaluationPurpose, Status: "ACTIVE", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		GrantStoreID: "grant-mismatch", TaskStoreID: "task-mismatch", BudgetLedgerID: "budget-mismatch",
		AuditStoreID: "audit-mismatch", ToolOwnerID: "owner-mismatch", ToolOwnerProfile: "fixture-only/v1",
	}
	runtime := &syntheticRuntimeResolverProbe{tamper: true}
	authority, err := NewPersistedSyntheticTaskProvisionAuthority(syntheticProvisionReaderProbe{record: record}, runtime,
		func(context.Context, values.TenantId) (uuid.UUID, error) { return tenantUUID, nil }, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.ResolveSyntheticTaskProvision(context.Background(), tenant); !errors.Is(err, ErrSyntheticProvisionUnavailable) {
		t.Fatalf("ResolveSyntheticTaskProvision() error = %v, want runtime binding denial", err)
	}
}

func TestPersistedSyntheticAuthorityFailsClosed(t *testing.T) {
	tenantUUID := uuid.New()
	tenant := values.TenantId("synthetic-onboarding")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	active := agentstore.SyntheticTenantProvisionRecord{
		TenantID: tenantUUID, SuiteTenantID: tenant.String(), ProvisionID: "provision-8", MarkerID: "marker-8", Purpose: syntheticEvaluationPurpose,
		Status: "ACTIVE", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		GrantStoreID: "grant-store-8", TaskStoreID: "task-store-8", BudgetLedgerID: "budget-8",
		AuditStoreID: "audit-8", ToolOwnerID: "fixture-owner-8", ToolOwnerProfile: "fixture-only/v1",
	}
	for _, tc := range []struct {
		name   string
		record agentstore.SyntheticTenantProvisionRecord
		err    error
	}{
		{name: "missing marker", err: agentstore.ErrSyntheticProvisionAbsent},
		{name: "revoked marker", record: func() agentstore.SyntheticTenantProvisionRecord {
			r := active
			r.Status = "REVOKED"
			r.RevokedAt = ptrTime(now)
			r.RevokeReason = "disabled"
			return r
		}()},
		{name: "expired marker", record: func() agentstore.SyntheticTenantProvisionRecord { r := active; r.ExpiresAt = now; return r }()},
		{name: "wrong tenant", record: func() agentstore.SyntheticTenantProvisionRecord {
			r := active
			r.SuiteTenantID = "synthetic-elsewhere"
			return r
		}()},
		{name: "storage error", err: errors.New("database offline")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &syntheticRuntimeResolverProbe{}
			authority, err := NewPersistedSyntheticTaskProvisionAuthority(syntheticProvisionReaderProbe{record: tc.record, err: tc.err}, runtime, func(_ context.Context, got values.TenantId) (uuid.UUID, error) {
				if got != tenant {
					return uuid.Nil, ErrSyntheticProvisionUnavailable
				}
				return tenantUUID, nil
			}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if err := authority.AuthorizeSyntheticTenant(context.Background(), tenant); !errors.Is(err, ErrSyntheticProvisionUnavailable) {
				t.Fatalf("AuthorizeSyntheticTenant() error = %v, want unavailable", err)
			}
			if _, err := authority.ResolveSyntheticTaskProvision(context.Background(), tenant); !errors.Is(err, ErrSyntheticProvisionUnavailable) {
				t.Fatalf("ResolveSyntheticTaskProvision() error = %v, want unavailable", err)
			}
			if runtime.seen.ProvisionID != "" {
				t.Fatal("runtime composition ran before the marker was trusted")
			}
		})
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
