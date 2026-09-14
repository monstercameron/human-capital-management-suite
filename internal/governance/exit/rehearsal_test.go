package exit

import (
	"testing"
	"time"
)

// TENANT-004 RED: pilot exit rehearsal contract before rehearsal.go exists.
func TestTodo_TENANT_004(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	freshExit := func() Request {
		r := validRequest()
		r.At = at
		for i := range r.Copies {
			r.Copies[i].FreshAt = at.Add(-time.Hour)
		}
		for i := range r.Revocations {
			r.Revocations[i].At = at
		}
		return r
	}
	base := freshExit()
	steps := []RehearsalStep{
		{ID: "export", Title: "Produce and verify the tenant export", Observed: true, EvidenceRef: "export-1"},
		{ID: "shutdown", Title: "Shut down connectors and tenant", Observed: true, EvidenceRef: "shutdown-1"},
		{ID: "revoke", Title: "Revoke live authorities", Observed: true, EvidenceRef: "r-provider"},
		{ID: "restore-plan", Title: "Record the restore re-delete plan", Observed: true, EvidenceRef: "rd-1"},
	}
	rec, err := Rehearse(RehearsalRequest{
		RehearsalID: "rehearsal-1", Tenant: "tenant-1", RequestedBy: "operator-1",
		At: at, Exit: base, Steps: steps,
	})
	if err != nil {
		t.Fatalf("Rehearse: %v", err)
	}
	if rec.Status != StatusCertifiable {
		t.Fatalf("status=%q blockers=%+v, want CERTIFIABLE", rec.Status, rec.Blockers)
	}
	if rec.Export.ID == "" || len(rec.Revocations) == 0 || len(rec.RestorePlan) == 0 {
		t.Fatalf("receipt lacks export, revocations or restore plan: %+v", rec)
	}

	// RED: certification that ignores a backup copy or an uninventoried
	// hold is not certifiable.
	partial := freshExit()
	kept := make([]Copy, 0, len(partial.Copies))
	for _, c := range partial.Copies {
		if c.Category != CategoryBackup {
			kept = append(kept, c)
		}
	}
	partial.Copies = kept
	partial.HoldExceptions = []HoldException{{ID: "hold-ghost", CopyID: "copy-GHOST", Authority: "legal-1", Reason: "litigation hold"}}
	rec, err = Rehearse(RehearsalRequest{
		RehearsalID: "rehearsal-2", Tenant: "tenant-1", RequestedBy: "operator-1",
		At: at, Exit: partial, Steps: steps,
	})
	if err != nil {
		t.Fatalf("Rehearse(partial): %v", err)
	}
	if rec.Status != StatusBlocked {
		t.Fatal("rehearsal ignoring backup inventory and a hold certified, want BLOCKED")
	}
	if !hasBlocker(rec.Blockers, "COPY_CATEGORY_UNKNOWN") || !hasBlocker(rec.Blockers, "HOLD_COPY_UNKNOWN") {
		t.Fatalf("blockers=%+v, want backup-category and ghost-hold findings", rec.Blockers)
	}

	// RED: an unobserved rehearsal step blocks certification too.
	unobserved := append([]RehearsalStep(nil), steps...)
	unobserved[1].Observed = false
	rec, err = Rehearse(RehearsalRequest{
		RehearsalID: "rehearsal-3", Tenant: "tenant-1", RequestedBy: "operator-1",
		At: at, Exit: base, Steps: unobserved,
	})
	if err != nil {
		t.Fatalf("Rehearse(unobserved): %v", err)
	}
	if rec.Status != StatusBlocked {
		t.Fatal("rehearsal with an unobserved step certified, want BLOCKED")
	}
}
