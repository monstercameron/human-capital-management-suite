package timestore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_TCLOCK_007(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-tclock007"
	deadline := time.Now().Add(72 * time.Hour)

	p, err := s.RecordPunchPhoto(ctx, tenant, "receipt-1", "artifact-ref-1", "site-1", deadline)
	if err != nil || p.PunchReceiptID != "receipt-1" || p.ArtifactRef != "artifact-ref-1" || p.LegalHold {
		t.Fatalf("record photo: %v %+v", err, p)
	}

	view, err := s.RecordPhotoView(ctx, tenant, p.ID, "supervisor-1", "TIME_REVIEW")
	if err != nil || view.ViewerID != "supervisor-1" || view.ViewedAt.IsZero() {
		t.Fatalf("record view: %v %+v", err, view)
	}
	views, err := s.PhotoViews(ctx, tenant, p.ID, 10)
	if err != nil || len(views) != 1 {
		t.Fatalf("photo views: %v %+v", err, views)
	}

	held, err := s.SetPhotoLegalHold(ctx, tenant, p.ID, true)
	if err != nil || !held.LegalHold {
		t.Fatalf("set legal hold: %v %+v", err, held)
	}
	// A held photo is refused deletion even past its review window.
	if err := s.DeletePunchPhoto(ctx, tenant, p.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("delete held photo: %v", err)
	}
	unheld, err := s.SetPhotoLegalHold(ctx, tenant, p.ID, false)
	if err != nil || unheld.LegalHold {
		t.Fatalf("clear legal hold: %v %+v", err, unheld)
	}
	if err := s.DeletePunchPhoto(ctx, tenant, p.ID); err != nil {
		t.Fatalf("delete unheld photo: %v", err)
	}
	after, err := s.GetPunchPhoto(ctx, tenant, p.ID)
	if err != nil || after.DeletedAt == nil || after.ArtifactRef != "" {
		t.Fatalf("photo after delete: %v %+v", err, after)
	}
}

func TestTodo_TCLOCK_007_Golden(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-golden-photo"
	deadline := time.Now().Add(24 * time.Hour)
	p, err := s.RecordPunchPhoto(ctx, tenant, "receipt-golden", "artifact-golden", "site-golden", deadline)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := s.GetPunchPhoto(ctx, tenant, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PunchReceiptID != "receipt-golden" || got.ArtifactRef != "artifact-golden" || got.SiteID != "site-golden" || got.LegalHold {
		t.Fatalf("photo = %+v", got)
	}
}

func TestTodo_TCLOCK_007_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-a"
	p, err := s.RecordPunchPhoto(ctx, tenant, "receipt-1", "artifact-1", "site-1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := s.RecordPhotoView(ctx, tenant, p.ID, "supervisor-1", "TIME_REVIEW"); err != nil {
		t.Fatalf("view: %v", err)
	}
	// Cross-tenant isolation on the photo, its views, and hold/delete.
	if _, err := s.GetPunchPhoto(ctx, "tenant-b", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	views, err := s.PhotoViews(ctx, "tenant-b", p.ID, 10)
	if err != nil || len(views) != 0 {
		t.Fatalf("cross-tenant views leaked: %v %+v", err, views)
	}
	if _, err := s.RecordPhotoView(ctx, "tenant-b", p.ID, "intruder", "TIME_REVIEW"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant view record: %v", err)
	}
}

func TestTodo_TCLOCK_007_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-integration-photo"
	now := time.Now()

	overdue, err := s.RecordPunchPhoto(ctx, tenant, "receipt-overdue", "artifact-overdue", "site-1", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("record overdue: %v", err)
	}
	future, err := s.RecordPunchPhoto(ctx, tenant, "receipt-future", "artifact-future", "site-1", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("record future: %v", err)
	}
	held, err := s.RecordPunchPhoto(ctx, tenant, "receipt-held", "artifact-held", "site-1", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("record held: %v", err)
	}
	if _, err := s.SetPhotoLegalHold(ctx, tenant, held.ID, true); err != nil {
		t.Fatalf("hold: %v", err)
	}

	due, err := s.DueForPhotoDeletion(ctx, tenant, now, 100)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	got := map[string]bool{}
	for _, p := range due {
		got[p.ID] = true
	}
	if !got[overdue.ID] {
		t.Fatalf("overdue photo missing from sweep: %+v", due)
	}
	if got[future.ID] {
		t.Fatalf("future photo swept early: %+v", due)
	}
	if got[held.ID] {
		t.Fatalf("held photo swept despite legal hold: %+v", due)
	}

	for _, p := range due {
		if err := s.DeletePunchPhoto(ctx, tenant, p.ID); err != nil {
			t.Fatalf("delete swept photo %s: %v", p.ID, err)
		}
	}
	dueAgain, err := s.DueForPhotoDeletion(ctx, tenant, now, 100)
	if err != nil {
		t.Fatalf("due again: %v", err)
	}
	if len(dueAgain) != 0 {
		t.Fatalf("sweep left photos due: %+v", dueAgain)
	}
}
