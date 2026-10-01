package timestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestSelfProjectionLockLoadAndCASBump(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-self", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO time_self_clock_projection (tenant_id, worker_ref, assignment_ref) VALUES ('tenant-self','worker-1','assignment-1')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var got SelfProjection
	if err := s.RunTenantTx(ctx, "tenant-self", func(tx dbport.Tx) error {
		var err error
		got, err = LoadSelfProjection(ctx, tx, "tenant-self", "worker-1", "assignment-1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.StatusCode != "CLOCKED_OUT" {
		t.Fatalf("projection=%+v", got)
	}
	if err := s.RunTenantTx(ctx, "tenant-self", func(tx dbport.Tx) error {
		got, err := LoadSelfProjection(ctx, tx, "tenant-self", "worker-1", "assignment-1")
		if err != nil {
			return err
		}
		next, err := BumpSelfProjection(ctx, tx, "tenant-self", "worker-1", "assignment-1", got.Revision, "CLOCKED_IN", time.Now().UTC())
		if err != nil || next != 2 {
			t.Fatalf("next=%d err=%v", next, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-self", func(tx dbport.Tx) error {
		_, err := BumpSelfProjection(ctx, tx, "tenant-self", "worker-1", "assignment-1", 1, "CLOCKED_OUT", time.Time{})
		if err != ErrRevisionConflict {
			t.Fatalf("stale err=%v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-other", func(tx dbport.Tx) error {
		_, err := LoadSelfProjection(ctx, tx, "tenant-self", "worker-1", "assignment-1")
		if err == nil {
			t.Fatal("cross-tenant projection read succeeded")
		}
		if err := EnsureSelfClockProjection(ctx, tx, "tenant-self", "forged-worker", "assignment-1"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("foreign initialization err=%v", err)
		}
		if _, err := BumpSelfProjection(ctx, tx, "tenant-self", "worker-1", "assignment-1", 2, "CLOCKED_OUT", time.Time{}); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("foreign bump err=%v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSelfClockProjectionDerivesExistingPunchAndPreservesRevision(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		return EnsureSelfClockProjection(ctx, tx, "tenant-human", "worker-human", "assignment-human")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitPunchEffect(ctx, "tenant-human", humanPunchEntry("obs-projection", "digest-projection")); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3`, "tenant-human", "worker-human", "assignment-human"); err != nil {
			return err
		}
		return EnsureSelfClockProjection(ctx, tx, "tenant-human", "worker-human", "assignment-human")
	}); err != nil {
		t.Fatal(err)
	}
	var got SelfProjection
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		var err error
		got, err = LoadSelfProjection(ctx, tx, "tenant-human", "worker-human", "assignment-human")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.StatusCode != "CLOCKED_IN" || !got.LastEventAt.Equal(time.Unix(200, 0).UTC()) {
		t.Fatalf("derived projection=%+v", got)
	}
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		if _, err := BumpSelfProjection(ctx, tx, "tenant-human", "worker-human", "assignment-human", 1, "CLOCKED_IN", got.LastEventAt); err != nil {
			return err
		}
		return EnsureSelfClockProjection(ctx, tx, "tenant-human", "worker-human", "assignment-human")
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		got, err := LoadSelfProjection(ctx, tx, "tenant-human", "worker-human", "assignment-human")
		if err != nil {
			return err
		}
		if got.Revision != 2 {
			t.Fatalf("repeated ensure overwrote revision: %d", got.Revision)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSelfClockProjectionDerivesOnBreakAndRejectsUnknownStatus(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant, worker, assignment := "tenant-projection-break", "worker-break", "assignment-break"
	opened := time.Now().UTC().Truncate(time.Second)
	session, event := newOpenSession("session-projection-break", tenant, worker, assignment, opened)
	if _, err := s.OpenSession(ctx, tenant, session, event); err != nil {
		t.Fatal(err)
	}
	breakEvent := EventRow{SessionID: session.ID, Kind: "BREAK_START", ActorRef: worker, IdempotencyKey: "break-projection", Digest: "sha256:break-projection"}
	if _, err := s.ApplySessionTransition(ctx, tenant, session.ID, 1, SessionRow{ID: session.ID, TenantID: tenant, Status: "ON_BREAK", Source: session.Source}, []EventRow{breakEvent}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return EnsureSelfClockProjection(ctx, tx, tenant, worker, assignment)
	}); err != nil {
		t.Fatal(err)
	}
	var got SelfProjection
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		got, err = LoadSelfProjection(ctx, tx, tenant, worker, assignment)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got.StatusCode != "ON_BREAK" {
		t.Fatalf("projection status=%q", got.StatusCode)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE time_session SET status='UNKNOWN' WHERE tenant_id=$1 AND id=$2`, tenant, session.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3`, tenant, worker, assignment)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return EnsureSelfClockProjection(ctx, tx, tenant, worker, assignment)
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown session status error=%v", err)
	}
}

func TestEnsureSelfClockProjectionCurrentShiftWinsOverHistoricalCorrection(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant, worker, assignment := "tenant-history", "worker-history", "assignment-history"
	opened := time.Unix(200, 0).UTC()
	prior, priorEvent := newOpenSession("session-prior", tenant, worker, assignment, opened)
	if _, err := s.OpenSession(ctx, tenant, prior, priorEvent); err != nil {
		t.Fatal(err)
	}
	closeEvent := EventRow{SessionID: prior.ID, Kind: "CLOSED", ActorRef: worker, IdempotencyKey: "close-history", Digest: "sha256:close-history"}
	closed := prior
	closed.Status, closed.ClosedAt = "CLOSED", opened.Add(time.Hour)
	if _, err := s.ApplySessionTransition(ctx, tenant, prior.ID, 1, closed, []EventRow{closeEvent}); err != nil {
		t.Fatal(err)
	}
	current, currentEvent := newOpenSession("session-current", tenant, worker, assignment, opened.Add(24*time.Hour))
	if _, err := s.OpenSession(ctx, tenant, current, currentEvent); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE time_session SET updated_at='2099-01-01'::timestamptz WHERE tenant_id=$1 AND id=$2`, tenant, prior.ID); err != nil {
			return err
		}
		return EnsureSelfClockProjection(ctx, tx, tenant, worker, assignment)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		got, err := LoadSelfProjection(ctx, tx, tenant, worker, assignment)
		if err != nil {
			return err
		}
		if got.StatusCode != "CLOCKED_IN" || got.Revision != 1 {
			t.Fatalf("historical correction hid current shift: %+v", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
