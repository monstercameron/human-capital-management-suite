package workforce_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// withTimeTrackingFacts fills the three columns migrations/00380 added.
func withTimeTrackingFacts(row workforce.WorkerRow) workforce.WorkerRow {
	row.ExemptionStatus = string(timeprofile.NonExempt)
	row.TimeCaptureMode = string(timeprofile.CapturePunch)
	row.TimeProfileRef = "time-profile.hourly-punch/2026.1"
	return row
}

// TestTodo_WTIME_001_Integration proves migrations/00380's three columns
// survive the write and every read shape (Create, Get, List) against a real
// PostgreSQL instance, that an unasserted row reads back as genuinely NULL
// rather than empty-string padding, and that people.FieldExemptionStatus,
// people.FieldTimeCaptureMode and people.FieldTimeProfileRef are answered
// from them through the governed worker read.
func TestTodo_WTIME_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wtime-001")
	conn := appConn(t, db)
	row := withTimeTrackingFacts(newRow(tenant, "priya-nair"))

	var created, fetched workforce.WorkerRow
	var listed []workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		if created, err = (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
			return err
		}
		var found bool
		if fetched, found, err = (workforce.Store{}).Get(context.Background(), tx, tenant, "priya-nair"); err != nil {
			return err
		} else if !found {
			t.Fatal("the worker just created was not found")
		}
		listed, err = (workforce.Store{}).List(context.Background(), tx, tenant)
		return err
	})

	assertTimeTrackingFacts := func(t *testing.T, where string, got workforce.WorkerRow) {
		t.Helper()
		if got.ExemptionStatus != string(timeprofile.NonExempt) {
			t.Errorf("%s: exemption_status = %q, want %s", where, got.ExemptionStatus, timeprofile.NonExempt)
		}
		if got.TimeCaptureMode != string(timeprofile.CapturePunch) {
			t.Errorf("%s: time_capture_mode = %q, want %s", where, got.TimeCaptureMode, timeprofile.CapturePunch)
		}
		if got.TimeProfileRef != "time-profile.hourly-punch/2026.1" {
			t.Errorf("%s: time_profile_ref = %q, want the recorded reference", where, got.TimeProfileRef)
		}
	}
	assertTimeTrackingFacts(t, "Create", created)
	assertTimeTrackingFacts(t, "Get", fetched)
	if len(listed) != 1 {
		t.Fatalf("List returned %d rows, want 1", len(listed))
	}
	assertTimeTrackingFacts(t, "List", listed[0])

	fields := workforce.FieldValues(created)
	if got := fields[people.FieldExemptionStatus]; got != string(timeprofile.NonExempt) {
		t.Errorf("FieldExemptionStatus = %q, want %s", got, timeprofile.NonExempt)
	}
	if got := fields[people.FieldTimeCaptureMode]; got != string(timeprofile.CapturePunch) {
		t.Errorf("FieldTimeCaptureMode = %q, want %s", got, timeprofile.CapturePunch)
	}
	if got := fields[people.FieldTimeProfileRef]; got != "time-profile.hourly-punch/2026.1" {
		t.Errorf("FieldTimeProfileRef = %q, want the recorded reference", got)
	}
}

// TestTodo_WTIME_001_Integration_Unasserted proves a worker that asserts none
// of the three new facts reads back with genuinely NULL columns, the same
// distinction migrations/00316's own absence test draws: NULL is what keeps
// "nobody asserted this" distinguishable from an invented empty answer.
func TestTodo_WTIME_001_Integration_Unasserted(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wtime-001-absent")
	conn := appConn(t, db)

	var created workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (workforce.Store{}).Create(context.Background(), tx, newRow(tenant, "unstated-time"))
		return err
	})

	for _, tc := range []struct{ name, got string }{
		{"exemption_status", created.ExemptionStatus},
		{"time_capture_mode", created.TimeCaptureMode},
		{"time_profile_ref", created.TimeProfileRef},
	} {
		if tc.got != "" {
			t.Errorf("unasserted %s = %q, want empty", tc.name, tc.got)
		}
	}

	var nulls int
	dbRow := db.QueryRow(context.Background(), `
		SELECT count(*) FROM journey_worker
		WHERE tenant_id = $1
		  AND exemption_status IS NULL AND time_capture_mode IS NULL AND time_profile_ref IS NULL`,
		tenant)
	if err := dbRow.Scan(&nulls); err != nil {
		t.Fatalf("count NULL time tracking facts: %v", err)
	}
	if nulls != 1 {
		t.Fatalf("%d rows carry NULL time tracking facts, want 1", nulls)
	}

	// people.FieldExemptionStatus and its siblings are reported absent, not
	// as an empty string pretending to be an answer.
	fields := workforce.FieldValues(created)
	for _, field := range []people.FieldID{people.FieldExemptionStatus, people.FieldTimeCaptureMode, people.FieldTimeProfileRef} {
		if got, present := fields[field]; got != "" || !present {
			t.Errorf("unasserted %s = %q (present=%v), want an empty value the projection reports as absent", field, got, present)
		}
	}
}

// TestTodo_WTIME_001_Integration_CrossFieldRuleRejectedByStore proves the
// contractor exemption rule is enforced at the storage boundary too: Create
// calls Validate before it ever reaches the database, so a contradictory row
// never becomes an append-only fact.
func TestTodo_WTIME_001_Integration_CrossFieldRuleRejectedByStore(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wtime-001-store-reject")
	conn := appConn(t, db)

	row := newRow(tenant, "bad-contractor")
	row.WorkerType = "CONTRACTOR"
	row.ExemptionStatus = string(timeprofile.Exempt)

	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (workforce.Store{}).Create(context.Background(), tx, row)
		return err
	})
	if err == nil {
		t.Fatal("Create() accepted a contractor asserting EXEMPT, want a refusal")
	}
}
