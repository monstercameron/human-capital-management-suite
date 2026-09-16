package committedfacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/committedfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var at = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

func seedTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'committedfacts test', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID, "committedfacts-"+tenantID.String()[:8])
	return tenantID
}

func inTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// projected is the journey_worker row and its aggregate projection.
func projected(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) workforce.WorkerRow {
	t.Helper()
	recorded := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	row := workforce.WorkerRow{
		TenantID: tenantID, WorkerID: uuid.New(), WorkerKey: "cf-001-noa-quinn",
		LegalName: "Noa Quinn", PreferredName: "Noa", WorkerNumber: "W-CF-1",
		WorkerType: "employee", LifecycleStatus: "active", EmploymentID: "emp_cf1", AssignmentID: "asg_cf1",
		JobCode: "OPS-HRBP2", JobTitle: "People Partner", Grade: "P2", OrgUnit: "people-ops",
		PositionID: "POS-CF-1", Location: "Boston, MA", PayZone: "US-EAST", FTE: "1.0000",
		HireDate: "2021-04-05", EffectiveFrom: "2021-04-05", BasePay: "90000.00", Currency: "USD",
		PayBasis: "ANNUAL_SALARY", BonusTarget: "0.0500", ManagerRelationshipRef: "rel_mgr_cf", RevisionStream: "people.worker.cf", RevisionSequence: 1,
		KnownAt: recorded, RecordedAt: recorded, CreatedBy: "test", Source: workforce.SourceCreated,
	}
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
			return err
		}
		_, err := demoworkforce.ProjectWorker(context.Background(), tx, row, "Committed Facts Test Entity")
		return err
	})
	return row
}

// TestCurrentPlacementReadsTheCommittedAssignmentAndPay proves the placement
// comes from the aggregates, follows a committed successor assignment and pay,
// and reports absence for a worker with no projection.
func TestCurrentPlacementReadsTheCommittedAssignmentAndPay(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	ctx := context.Background()
	row := projected(t, db, tenantID)
	ids := demoworkforce.AggregateIDsFor(row)

	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		placement, found, err := committedfacts.CurrentPlacement(ctx, tx, tenantID, row.WorkerID, at)
		if err != nil || !found {
			t.Fatalf("CurrentPlacement = %+v, %t, %v", placement, found, err)
		}
		if placement.JobCode != "OPS-HRBP2" || placement.Grade != "P2" || placement.OrgUnit != "people-ops" ||
			placement.PositionCode != "POS-CF-1" || placement.BasePay != "90000.00" || placement.Currency != "USD" {
			t.Fatalf("placement = %+v", placement)
		}
		missing, found, err := committedfacts.CurrentPlacement(ctx, tx, tenantID, uuid.New(), at)
		if err != nil || found {
			t.Fatalf("placement for an unprojected worker = %+v, %t, %v", missing, found, err)
		}
		return nil
	})

	// Commit the promotion's successor facts, exactly as the promotion commit
	// writer appends them.
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		promoted, err := aggregates.NewAssignment(tenantID, ids.Assignment, ids.Employment, true, at, nil, time.Now().UTC(),
			"OPS-HRBP3", "P3", nil, nil, "Boston, MA", "US-EAST", "1.0000", "")
		if err != nil {
			return err
		}
		if _, err := (aggregates.PeopleStore{}).PutAssignment(ctx, tx, promoted); err != nil {
			return err
		}
		pay, err := values.NewMoney("98000.00", "USD", 2, values.RoundingExactRequired)
		if err != nil {
			return err
		}
		component, err := aggregates.NewCompensationComponent(tenantID, ids.BasePay, ids.Package, at, nil, time.Now().UTC(), "BASE_PAY", pay, "ANNUAL")
		if err != nil {
			return err
		}
		_, err = (aggregates.CompensationStore{}).PutCompensationComponent(ctx, tx, component)
		return err
	})
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		placement, found, err := committedfacts.CurrentPlacement(ctx, tx, tenantID, row.WorkerID, at)
		if err != nil || !found {
			t.Fatalf("CurrentPlacement after the commit = %+v, %t, %v", placement, found, err)
		}
		if placement.JobCode != "OPS-HRBP3" || placement.Grade != "P3" || placement.BasePay != "98000.00" {
			t.Fatalf("committed placement = %+v, want the promoted job, grade and pay", placement)
		}
		return nil
	})
}

// baseFacts is a people.WorkerFacts answering fixed recorded values.
type baseFacts struct {
	exists bool
	values map[people.FieldID]string
	err    error
}

func (b baseFacts) WorkerFactsAt(_ context.Context, q people.FactQuery) (people.FactSet, error) {
	if b.err != nil {
		return people.FactSet{}, b.err
	}
	if !b.exists {
		return people.FactSet{Worker: q.Worker}, nil
	}
	revision, err := values.NewSequenceRevision("people.worker.base", 1)
	if err != nil {
		return people.FactSet{}, err
	}
	facts := make([]people.Fact, 0, len(q.Fields))
	for _, field := range q.Fields {
		value := values.Value(b.values[field])
		if field == people.FieldLocation {
			value = values.NewPresence[string](values.PresenceRedacted, "withheld")
		}
		facts = append(facts, people.Fact{
			Field: field, Value: value, Revision: revision,
			Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "test", PolicyRef: "test/1"},
			Provenance: evidence.Provenance{Source: "test", EvidenceRef: "ev:test"},
		})
	}
	return people.FactSet{Worker: q.Worker, Exists: true, Facts: facts, Watermark: revision}, nil
}

// TestOverlayReportsTheCommittedPlacementOverTheRecordedOne proves the
// overlay replaces the recorded placement fields with the committed ones,
// leaves a withheld field withheld, keeps the delegate's watermark, and
// passes an absent worker and a failing delegate straight through.
func TestOverlayReportsTheCommittedPlacementOverTheRecordedOne(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	ctx := context.Background()
	row := projected(t, db, tenantID)
	tenantKey := values.TenantId("committedfacts-tenant")
	reader := committedfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}

	base := baseFacts{exists: true, values: map[people.FieldID]string{
		people.FieldJobCode: "STALE-JOB", people.FieldGrade: "P1", people.FieldOrgUnit: "stale-unit",
		people.FieldPayZone: "US-WEST", people.FieldLocation: "Nowhere",
	}}
	overlay := committedfacts.Overlay{Base: base, Reader: reader}
	worker := values.EntityRef{Tenant: tenantKey, Kind: people.KindWorker, Id: row.WorkerID.String()}
	effective, err := values.ParseLocalDate("2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		t.Fatal(err)
	}
	query := people.FactQuery{
		Tenant: tenantKey, Worker: worker, AsOf: people.AsOf{EffectiveOn: effective, KnownAt: known},
		Fields: []people.FieldID{people.FieldJobCode, people.FieldGrade, people.FieldOrgUnit, people.FieldPayZone, people.FieldLocation},
	}
	set, err := overlay.WorkerFactsAt(ctx, query)
	if err != nil || !set.Exists {
		t.Fatalf("WorkerFactsAt = %+v, %v", set, err)
	}
	got := map[people.FieldID]string{}
	for _, fact := range set.Facts {
		if value, disclosed := fact.Value.Get(); disclosed {
			got[fact.Field] = value
		} else if fact.Field != people.FieldLocation {
			t.Fatalf("field %s was not disclosed", fact.Field)
		}
	}
	if got[people.FieldJobCode] != "OPS-HRBP2" || got[people.FieldGrade] != "P2" || got[people.FieldOrgUnit] != "people-ops" || got[people.FieldPayZone] != "US-EAST" {
		t.Fatalf("overlaid facts = %v, want the recorded aggregate placement", got)
	}
	if _, disclosed := got[people.FieldLocation]; disclosed {
		t.Fatal("the overlay disclosed a field the delegate withheld")
	}
	if !set.Watermark.IsSpecified() {
		t.Fatal("the overlay dropped the delegate's watermark")
	}

	absent, err := committedfacts.Overlay{Base: baseFacts{}, Reader: reader}.WorkerFactsAt(ctx, query)
	if err != nil || absent.Exists {
		t.Fatalf("absent worker = %+v, %v", absent, err)
	}
	if _, err := (committedfacts.Overlay{Reader: reader}).WorkerFactsAt(ctx, query); err == nil {
		t.Fatal("an overlay with no delegate answered")
	}
	unprojected := query
	unprojected.Worker.Id = uuid.NewString()
	plain, err := overlay.WorkerFactsAt(ctx, unprojected)
	if err != nil {
		t.Fatalf("unprojected worker: %v", err)
	}
	for _, fact := range plain.Facts {
		if fact.Field == people.FieldJobCode {
			if value, _ := fact.Value.Get(); value != "STALE-JOB" {
				t.Fatalf("unprojected worker's job code = %q, want the delegate's own answer", value)
			}
		}
	}
	// A reader with no database overlays nothing rather than refusing.
	if _, _, err := (committedfacts.Reader{}).PlacementAt(ctx, tenantKey, row.WorkerID.String(), at); err != nil {
		t.Fatalf("PlacementAt with no database: %v", err)
	}
}
