package positionfacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// seedOccupancy puts one worker into a position for the whole seeded window,
// so a caller reading the directory at the test coordinate sees the position
// as taken.
func seedOccupancy(t *testing.T, db *pgtest.DB, tenantID, positionEntityID uuid.UUID, allocation string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}

	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recorded := time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)
	// position_occupancy.worker_ref foreign-keys to aggregate_entity, so the
	// worker this row names has to be a registered entity. Only the identity
	// matters here; nothing in the directory read looks at a worker record.
	workerID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO aggregate_entity (tenant_id, entity_id, kind, canonical_id)
		VALUES ($1, $2, 'worker', $3)`, tenantID, workerID, "worker-"+workerID.String()); err != nil {
		t.Fatalf("register worker entity: %v", err)
	}
	occupancy, err := aggregates.NewPositionOccupancy(tenantID, uuid.New(), positionEntityID, nil, &workerID,
		from, nil, recorded, allocation, true)
	if err != nil {
		t.Fatalf("NewPositionOccupancy: %v", err)
	}
	store := aggregates.OrganizationStore{}
	if _, err := store.PutPositionOccupancy(ctx, tx, occupancy); err != nil {
		t.Fatalf("PutPositionOccupancy: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return workerID
}

func directoryAt(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, tenant values.TenantId) []positionfacts.DirectoryRow {
	t.Helper()
	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	rows, err := reader.Directory(context.Background(), tenant, readerAsOf(t))
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}
	return rows
}

func directoryRowFor(rows []positionfacts.DirectoryRow, id uuid.UUID) (positionfacts.DirectoryRow, bool) {
	for _, row := range rows {
		if row.Position.Id == id.String() {
			return row, true
		}
	}
	return positionfacts.DirectoryRow{}, false
}

// TestDirectoryListsSeededPositionsWithTheirLabels proves the directory reads
// real rows and resolves each one's job title, organization name and codes at
// the same coordinate the revision reader uses -- so a caller narrowing a
// list by JobCode narrows it by exactly what CheckCompatibility compares.
func TestDirectoryListsSeededPositionsWithTheirLabels(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-directory")
	tenant := values.TenantId("positionfacts-directory")
	first := seededPosition(t, db, tenantID, "ENG-MGR", "ENGINEERING")
	second := seededPosition(t, db, tenantID, "CARE-COORD", "CARE")

	rows := directoryAt(t, db, tenantID, tenant)
	if len(rows) != 2 {
		t.Fatalf("the directory lists %d positions, want the 2 seeded ones", len(rows))
	}

	row, found := directoryRowFor(rows, first)
	if !found {
		t.Fatalf("the seeded ENG-MGR position is absent from the directory")
	}
	if row.JobCode != "ENG-MGR" || row.OrgUnit != "ENGINEERING" {
		t.Fatalf("codes = %q/%q, want ENG-MGR/ENGINEERING", row.JobCode, row.OrgUnit)
	}
	if row.Title != "Engineering Manager" || row.Organization != "Engineering" {
		t.Fatalf("labels = %q/%q, want the job title and organization unit name", row.Title, row.Organization)
	}
	if row.Location != "Remote" {
		t.Fatalf("Location = %q, want the position's own recorded location", row.Location)
	}
	if row.Position.Tenant != tenant || row.Position.Kind != position.KindPosition {
		t.Fatalf("the row's reference is %+v, want a position in the requested tenant", row.Position)
	}
	if len(row.Occupants) != 0 {
		t.Fatalf("an unoccupied position reports %d occupants", len(row.Occupants))
	}

	if _, found := directoryRowFor(rows, second); !found {
		t.Fatalf("the seeded CARE-COORD position is absent from the directory")
	}
}

// TestDirectoryAttachesOccupancyToItsOwnPosition is the property the whole
// vacancy projection rests on: an occupancy row lands on the position it
// names and on no other. position.Occupant carries no position of its own,
// so a reader that pooled them would make every position look as full as the
// busiest one.
func TestDirectoryAttachesOccupancyToItsOwnPosition(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-occupancy")
	tenant := values.TenantId("positionfacts-occupancy")
	taken := seededPosition(t, db, tenantID, "ENG-MGR", "ENGINEERING")
	open := seededPosition(t, db, tenantID, "CARE-COORD", "CARE")
	worker := seedOccupancy(t, db, tenantID, taken, "1.0000")

	rows := directoryAt(t, db, tenantID, tenant)

	occupied, _ := directoryRowFor(rows, taken)
	if len(occupied.Occupants) != 1 {
		t.Fatalf("the occupied position reports %d occupants, want 1", len(occupied.Occupants))
	}
	if occupied.Occupants[0].Worker.Id != worker.String() {
		t.Fatalf("the occupant is %q, want the seeded worker %q", occupied.Occupants[0].Worker.Id, worker)
	}
	if occupied.Occupants[0].Worker.Kind != position.KindWorker {
		t.Fatalf("the occupant is a %q, want a worker", occupied.Occupants[0].Worker.Kind)
	}
	if !occupied.Occupants[0].Exclusive {
		t.Fatalf("a primary occupancy was not read as exclusive")
	}
	if occupied.Occupants[0].FTE.Sign() <= 0 {
		t.Fatalf("the occupant takes up no capacity at all: %v", occupied.Occupants[0].FTE)
	}
	if err := occupied.Occupants[0].Validate(); err != nil {
		t.Fatalf("the occupant is not well formed for CalculateCapacity: %v", err)
	}

	vacant, _ := directoryRowFor(rows, open)
	if len(vacant.Occupants) != 0 {
		t.Fatalf("the other position picked up %d occupants that are not its own", len(vacant.Occupants))
	}
}

// TestDirectoryRefusesWhatItCannotAnswer keeps the failure modes honest: an
// unconfigured reader is an error, an unmapped tenant is an empty answer,
// and a malformed request never reaches the database.
func TestDirectoryRefusesWhatItCannotAnswer(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-refusal")
	tenant := values.TenantId("positionfacts-refusal")
	seededPosition(t, db, tenantID, "ENG-MGR", "ENGINEERING")
	ctx := context.Background()

	if _, err := (positionfacts.Reader{}).Directory(ctx, tenant, readerAsOf(t)); err == nil {
		t.Fatalf("a reader with no database answered a directory read")
	}

	unmapped := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.Nil }}
	rows, err := unmapped.Directory(ctx, tenant, readerAsOf(t))
	if err != nil {
		t.Fatalf("an unmapped tenant is an answer, not a fault: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("an unmapped tenant disclosed %d positions", len(rows))
	}

	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	if _, err := reader.Directory(ctx, values.TenantId(""), readerAsOf(t)); err == nil {
		t.Fatalf("an empty tenant was accepted")
	}
	if _, err := reader.Directory(ctx, tenant, position.AsOf{}); err == nil {
		t.Fatalf("a zero coordinate was accepted")
	}
}

// TestDirectoryIsScopedToOneTenant proves the read is tenant-scoped in fact,
// not only by convention: the reference the picker later issues decodes only
// inside the tenant it was read for, and that is the boundary the vacancy
// projection relies on in place of a per-position authorization hook.
func TestDirectoryIsScopedToOneTenant(t *testing.T) {
	db := pgtest.New(t)
	mine := seedReaderTenant(t, db, "positionfacts-mine")
	theirs := seedReaderTenant(t, db, "positionfacts-theirs")
	seededPosition(t, db, mine, "ENG-MGR", "ENGINEERING")
	seededPosition(t, db, theirs, "FIN-DIR", "FINANCE")
	seededPosition(t, db, theirs, "CARE-COORD", "CARE")

	rows := directoryAt(t, db, mine, values.TenantId("positionfacts-mine"))
	if len(rows) != 1 {
		t.Fatalf("the directory lists %d positions, want only this tenant's 1", len(rows))
	}
	if rows[0].JobCode != "ENG-MGR" {
		t.Fatalf("another tenant's position leaked: %q", rows[0].JobCode)
	}
}
