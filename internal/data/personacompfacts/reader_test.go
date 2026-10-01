package personacompfacts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENTP_023_CompensationNative_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID, foreignTenantID, workerID := uuid.New(), uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','Persona compensation','ACTIVE','2026-01-01')`, tenantID, "persona-comp-"+tenantID.String())
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','Foreign compensation','ACTIVE','2026-01-01')`, foreignTenantID, "foreign-comp-"+foreignTenantID.String())
	db.Exec(t, `INSERT INTO aggregate_entity (tenant_id,entity_id,kind,canonical_id) VALUES ($1,$2,'worker',$3)`, tenantID, workerID, "eid:v1:worker:"+workerID.String())
	db.Exec(t, `INSERT INTO aggregate_entity (tenant_id,entity_id,kind,canonical_id) VALUES ($1,$2,'worker',$3)`, foreignTenantID, workerID, "eid:v1:worker:"+workerID.String())
	worker := values.EntityRef{Tenant: "persona-comp", Kind: people.KindWorker, Id: workerID.String()}
	reader := Reader{DB: db.Conn, TenantUUID: func(tenant values.TenantId) uuid.UUID {
		if tenant == worker.Tenant {
			return tenantID
		}
		if tenant == "other-tenant" {
			return foreignTenantID
		}
		return uuid.Nil
	}}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recorded := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	packageID, baseID := uuid.New(), uuid.New()
	put := func(kind, amount, frequency string, entityID uuid.UUID, at time.Time) {
		t.Helper()
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			t.Fatal(err)
		}
		money, err := values.NewMoney(amount, "USD", 4, values.RoundingExactRequired)
		if err != nil {
			t.Fatal(err)
		}
		component, err := aggregates.NewCompensationComponent(tenantID, entityID, packageID, from, nil, at, kind, money, frequency)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (aggregates.CompensationStore{}).PutCompensationComponent(ctx, tx, component); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	pkg, err := aggregates.NewCompensationPackage(tenantID, packageID, workerID, nil, nil, from, nil, recorded, "USD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (aggregates.CompensationStore{}).PutCompensationPackage(ctx, tx, pkg); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	put("BASE_PAY", "96000.0000", "ANNUAL", baseID, recorded)
	put("BONUS_TARGET", "4800.0000", "ANNUAL", uuid.New(), recorded)
	put("ALLOWANCE", "200.0000", "MONTHLY", uuid.New(), recorded)
	asOf := values.NewInstant(recorded.Add(time.Hour))
	snapshot, err := reader.CompensationAt(ctx, worker, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Worker != worker || len(snapshot.Components) != 3 || !snapshot.Revision.IsSpecified() {
		t.Fatalf("incomplete persisted snapshot: %#v", snapshot)
	}
	amounts := map[string]string{}
	for _, component := range snapshot.Components {
		amounts[component.Kind] = component.Amount.Amount().String()
		contains, err := component.Effective.ContainsInstant(asOf)
		if err != nil || !contains || component.KnownAt.Instant().Time() != recorded || component.Provenance.EvidenceRef == "" || component.Authority.Validate() != nil {
			t.Fatalf("component lost source coordinates: %#v", component)
		}
	}
	if amounts["BASE_PAY"] != "96000.0000" || amounts["BONUS_TARGET"] != "4800.0000" || amounts["ALLOWANCE"] != "200.0000" {
		t.Fatalf("persisted amounts = %v", amounts)
	}
	// A correction is effective in January but first known in July. The June
	// read must keep the superseded June row; the July read sees the correction.
	put("BASE_PAY", "108000.0000", "ANNUAL", baseID, recorded.AddDate(0, 1, 0))
	historical, err := reader.CompensationAt(ctx, worker, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if string(historical.Revision.Canonical()) != string(snapshot.Revision.Canonical()) {
		t.Fatal("later knowledge changed the historical revision")
	}
	later, err := reader.CompensationAt(ctx, worker, values.NewInstant(recorded.AddDate(0, 1, 0).Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	var laterBase string
	for _, component := range later.Components {
		if component.Kind == "BASE_PAY" {
			laterBase = component.Amount.Amount().String()
		}
	}
	if laterBase != "108000.0000" {
		t.Fatalf("correction base = %s", laterBase)
	}
	if _, err := reader.CompensationAt(ctx, worker, values.NewInstant(recorded.Add(-time.Second))); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown package error = %v", err)
	}
	other := worker
	other.Tenant = "other-tenant"
	if _, err := reader.CompensationAt(ctx, other, asOf); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cross-tenant error = %v", err)
	}
	if _, err := (Reader{}).CompensationAt(ctx, worker, asOf); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured reader error = %v", err)
	}
}
