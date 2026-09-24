package balancestore_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/balancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/cycle"
)

func TestTodo_REV_039_01(t *testing.T) {
	db, tenant := newRestatementDB(t)
	store := balancestore.New()
	firstRequest := restatementFixtureFor(tenant, "restatement-1")
	first, err := cycle.RestatePriorCycle(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := restatementFixtureFor(tenant, "restatement-2")
	secondRequest.Revision++
	secondRequest.Prior.CloseResultDigest = "sha256:" + strings.Repeat("7", 64)
	secondRecord, err := cycle.RestatePriorCycle(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	var appended balancestore.RestatementRecord
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		appended, err = store.AppendRestatement(context.Background(), tx, tenant, first)
		return err
	})
	if appended.RowID == uuid.Nil || appended.Restatement.Digest != first.Digest || appended.RecordedAt.IsZero() {
		t.Fatalf("appended record = %+v", appended)
	}
	var replay balancestore.RestatementRecord
	withTenant(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		replay, err = store.AppendRestatement(context.Background(), tx, tenant, first)
		return err
	})
	if !replay.Replay || replay.RowID != appended.RowID || replay.Restatement.Digest != appended.Restatement.Digest {
		t.Fatalf("replay = %+v, original = %+v", replay, appended)
	}
	err = withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := store.AppendRestatement(context.Background(), tx, tenant, secondRecord)
		return err
	})
	if !errors.Is(err, balancestore.ErrRestatementConflict) || balancestore.CodeOf(err) != balancestore.CodeRestatementConflict {
		t.Fatalf("second append = %v", err)
	}
}

func TestTodo_REV_039_01_Race(t *testing.T) {
	db, tenant := newRestatementDB(t)
	store := balancestore.New()
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			conn := db.NewConn(t)
			if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
				results <- err
				return
			}
			request := restatementFixtureFor(tenant, []string{"race-a", "race-b"}[index])
			request.Revision = uint64(index + 1)
			restatement, err := cycle.RestatePriorCycle(request)
			if err != nil {
				results <- err
				return
			}
			<-start
			results <- withTenantErr(conn, tenant, func(tx dbport.Tx) error {
				_, err := store.AppendRestatement(context.Background(), tx, tenant, restatement)
				return err
			})
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, balancestore.ErrRestatementConflict):
			conflicts++
		default:
			t.Fatalf("concurrent append = %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d, want one each", wins, conflicts)
	}
}

func TestTodo_REV_039_01_Fault(t *testing.T) {
	db, tenant := newRestatementDB(t)
	store := balancestore.New()
	conn := appConn(t, db)
	request := restatementFixtureFor(tenant, "restatement-rolled-back")
	restated, err := cycle.RestatePriorCycle(request)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("simulate transaction abort")
	err = withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.AppendRestatement(context.Background(), tx, tenant, restated); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("aborted transaction = %v", err)
	}
	if err := withTenantErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := store.LoadRestatement(context.Background(), tx, tenant, request.RestatementID)
		return err
	}); !errors.Is(err, balancestore.ErrRestatementNotFound) {
		t.Fatalf("rolled back record load = %v", err)
	}
}

func TestTodo_REV_039_01_Recovery(t *testing.T) {
	db, tenant := newRestatementDB(t)
	store := balancestore.New()
	request := restatementFixtureFor(tenant, "restatement-recovered")
	computed, err := cycle.RestatePriorCycle(request)
	if err != nil {
		t.Fatal(err)
	}
	writer := appConn(t, db)
	withTenant(t, writer, tenant, func(tx dbport.Tx) error {
		_, err := store.AppendRestatement(context.Background(), tx, tenant, computed)
		return err
	})

	// A new connection models recovery after the writer process has gone away.
	reader := appConn(t, db)
	var recovered balancestore.RestatementRecord
	withTenant(t, reader, tenant, func(tx dbport.Tx) error {
		var err error
		recovered, err = store.LoadRestatement(context.Background(), tx, tenant, request.RestatementID)
		return err
	})
	if recovered.Restatement.Digest != computed.Digest || recovered.Restatement.Prior != computed.Prior || recovered.Restatement.Outcome != computed.Outcome {
		t.Fatalf("recovered=%+v computed=%+v", recovered.Restatement, computed)
	}
	got, err := recovered.Restatement.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	want, err := computed.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("recovered canonical body differs\ngot:  %s\nwant: %s", got, want)
	}
}

func newRestatementDB(t *testing.T) (*pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 329); err != nil {
		t.Fatalf("apply migrations through 00329: %v", err)
	}
	tenant := insertTenant(t, db, "cycle-restatement-"+uuid.NewString())
	return db, tenant
}

func restatementFixtureFor(tenant uuid.UUID, id string) cycle.RestatementRequest {
	return cycle.RestatementRequest{
		RestatementID: id,
		Revision:      1,
		CorrectionAt:  time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
		Prior: cycle.PriorCycleClose{
			TenantID: tenant.String(), CycleID: "cycle-1",
			CycleRevisionDigest: "sha256:" + strings.Repeat("a", 64),
			CloseResultDigest:   "sha256:" + strings.Repeat("b", 64),
			CloseEvidenceRef:    "ledger:close/4",
			ClosedAt:            time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
			Sequence:            4,
		},
		AffectedPopulationManifestDigest: "sha256:" + strings.Repeat("e", 64),
		AffectedResultManifestDigest:     "sha256:" + strings.Repeat("f", 64),
		Impacts:                          []cycle.RestatementImpact{{ResultID: "result-1", PopulationID: "population-1", PriorResultDigest: "sha256:" + strings.Repeat("c", 64), CorrectedResultDigest: "sha256:" + strings.Repeat("d", 64)}},
		Approvals:                        []cycle.RestatementApproval{{ApprovalID: "approval-1", DecisionDigest: "sha256:" + strings.Repeat("9", 64), DecisionEvidenceRef: "ledger:approval/1", ApprovedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)}},
		Compensations:                    []cycle.RestatementCompensation{{CompensationID: "comp-1", Required: true, Planned: false}},
		Outcome:                          cycle.ReconciliationPendingCompensation,
	}
}
