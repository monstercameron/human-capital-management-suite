package timestore

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestTodo_TCLOCK_007017_ClaimTombstoneFinalizeIsIdempotent(t *testing.T) {
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	when := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	p, err := s.RecordPunchPhoto(ctx, "tenant-017", "receipt-017", "artifact://photo-017", "site-017", when.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Conn.Exec(ctx, `INSERT INTO time_punch_photo_exception(tenant_id,photo_id,exception_ref) VALUES($1,$2,$3)`, "tenant-017", p.ID, "exception-017"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimPhotoDisposition(ctx, "tenant-017", p.ID, 1, "blocked-017", when); !errors.Is(err, ErrPhotoDispositionHeld) {
		t.Fatalf("held claim err=%v", err)
	}
	if _, err := db.Conn.Exec(ctx, `UPDATE time_punch_photo_exception SET resolved_at=now() WHERE tenant_id=$1 AND photo_id=$2 AND exception_ref=$3`, "tenant-017", p.ID, "exception-017"); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimPhotoDisposition(ctx, "tenant-017", p.ID, 1, "claim-017", when)
	if err != nil || claimed.State != "CLAIMED" || claimed.Revision != 2 {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	repeated, err := s.ClaimPhotoDisposition(ctx, "tenant-017", p.ID, 1, "claim-017", when)
	if err != nil || repeated.Revision != claimed.Revision {
		t.Fatalf("idempotent claim=%+v err=%v", repeated, err)
	}
	tombstone, err := s.TombstonePhotoDisposition(ctx, "tenant-017", p.ID, claimed.Revision, "tombstone-017")
	if err != nil || tombstone.State != "TOMBSTONED" {
		t.Fatalf("tombstone=%+v err=%v", tombstone, err)
	}
	deleted, err := s.FinalizePhotoDisposition(ctx, "tenant-017", p.ID, tombstone.Revision, "finalize-017")
	if err != nil || deleted.State != "DELETED" {
		t.Fatalf("finalize=%+v err=%v", deleted, err)
	}
	if got, err := s.GetPunchPhoto(ctx, "tenant-017", p.ID); err != nil || got.ArtifactRef != "" || got.DeletedAt == nil {
		t.Fatalf("photo after finalize=%+v err=%v", got, err)
	}
	if _, err := s.TombstonePhotoDisposition(ctx, "tenant-017", p.ID, tombstone.Revision, "tombstone-017"); !errors.Is(err, ErrPhotoDispositionConflict) {
		t.Fatalf("stale tombstone err=%v", err)
	}
}
