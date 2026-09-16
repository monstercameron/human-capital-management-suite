package operatorjournal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/operatorjournal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// bypassReceipt is the pending receipt a bypassing action records before its
// obligation; operator_bypass_obligation references it.
func bypassReceipt(tenant values.TenantId, key string, ids ...string) operator.Receipt {
	r := pending(tenant, key, "sha256:"+key)
	r.Kind = operator.KindWorkflowRepair
	r.Scope = operator.Scope{Resource: "workflow_instance", IDs: ids}
	r.Bypassed = []string{operator.BypassJITAuthority, operator.BypassDualControl}
	r.BypassReason, r.ReviewRequired = "primary region down", true
	return r
}

func obligation(tenant values.TenantId, key string, recordedAt time.Time, ids ...string) operator.Obligation {
	return operator.ObligationFor(bypassReceipt(tenant, key, ids...), "approver:lead", recordedAt)
}

// TestTodo_WF_RUN_039_Recovery proves bypass obligations survive a restart and
// that recovery runs through the review, not around it: an obligation recorded
// before a restart is still outstanding after one and still suspends its
// authority family; recording the same obligation twice is one obligation; a
// distinct reviewer's discharge is recorded exactly once and removes it from
// the outstanding set; a second discharge changes nothing; and tenants never
// see each other's debts.
func TestTodo_WF_RUN_039_Recovery(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	ids := map[values.TenantId]uuid.UUID{
		"acme":   seedTenant(t, db, "opj-obl-acme"),
		"globex": seedTenant(t, db, "opj-obl-globex"),
	}
	j := journal(db, ids)

	// The receipt is written first: an obligation belongs to a recorded action.
	for _, tenant := range []values.TenantId{"acme", "globex"} {
		if _, _, err := j.Begin(ctx, bypassReceipt(tenant, "bypass-1")); err != nil {
			t.Fatalf("Begin(%s): %v", tenant, err)
		}
	}
	acme := obligation("acme", "bypass-1", at, "instance-1")
	globex := obligation("globex", "bypass-1", at, "instance-1")
	for _, o := range []operator.Obligation{acme, acme, globex} {
		if err := j.RecordObligation(ctx, o); err != nil {
			t.Fatalf("RecordObligation(%s/%s): %v", o.Tenant, o.ID, err)
		}
	}

	// An obligation for an action that was never journaled has nothing to be
	// accountable for.
	orphan := obligation("acme", "never-journaled", at, "instance-9")
	if err := j.RecordObligation(ctx, orphan); err == nil {
		t.Error("recorded an obligation with no operator control receipt")
	}
	// Neither does a tampered one.
	tampered := acme
	tampered.Approver = "operator:ana"
	if err := j.RecordObligation(ctx, tampered); err == nil {
		t.Error("recorded a tampered obligation")
	}

	// A restart is a journal composed over a fresh connection.
	restarted := &operatorjournal.Journal{DB: db.NewConn(t), TenantIDs: j.TenantIDs}
	out, err := restarted.OutstandingObligations(ctx, "acme")
	if err != nil || len(out) != 1 {
		t.Fatalf("outstanding after restart = %+v, %v; want exactly one obligation", out, err)
	}
	got := out[0]
	switch {
	case got.ID != acme.ID || got.Verify() != nil:
		t.Fatalf("restored obligation = %+v (verify %v)", got, got.Verify())
	case got.Operator != acme.Operator || got.Approver != "approver:lead" || got.Family != operator.FamilyRepair:
		t.Errorf("restored accountability = %+v", got)
	case !got.DueAt.Equal(at.Add(operator.ReviewWindow)) || !got.Overdue(at.Add(operator.ReviewWindow)):
		t.Errorf("restored due %s; overdue at due = %v", got.DueAt, got.Overdue(at.Add(operator.ReviewWindow)))
	}
	if suspended := operator.SuspendedFamilies(out, at.Add(operator.ReviewWindow)); suspended[operator.FamilyRepair].ID != acme.ID {
		t.Errorf("a restored overdue obligation does not suspend its family: %+v", suspended)
	}
	if other, err := restarted.OutstandingObligations(ctx, "globex"); err != nil || len(other) != 1 || other[0].ID != globex.ID {
		t.Fatalf("globex outstanding = %+v, %v; want only its own obligation", other, err)
	}

	// The reviewer rules are enforced against the stored row, not the caller's.
	reviewAt := at.Add(operator.ReviewWindow + time.Hour)
	for name, reviewer := range map[string]string{"operator": acme.Operator, "approver": "approver:lead"} {
		_, err := restarted.DischargeObligation(ctx, "acme", acme.ID, operator.ObligationReview{
			Reviewer: reviewer, Outcome: operator.ObligationJustified, Note: "self-signed", At: reviewAt})
		if operator.CodeOf(err) != operator.CodeObligationReviewer {
			t.Errorf("discharge by the %s = %v, want %s", name, err, operator.CodeObligationReviewer)
		}
	}

	review := operator.ObligationReview{Reviewer: "reviewer:sam", Outcome: operator.ObligationViolation, Note: "bypass was not warranted", At: reviewAt}
	discharged, err := restarted.DischargeObligation(ctx, "acme", acme.ID, review)
	if err != nil || discharged.Review == nil || discharged.Review.Outcome != operator.ObligationViolation || discharged.Verify() != nil {
		t.Fatalf("discharge = %+v, %v", discharged, err)
	}
	if _, err := restarted.DischargeObligation(ctx, "acme", acme.ID, review); !errors.Is(err, operatorjournal.ErrNoOutstandingObligation) {
		t.Fatalf("second discharge = %v, want %v", err, operatorjournal.ErrNoOutstandingObligation)
	}
	if _, err := restarted.DischargeObligation(ctx, "acme", "obligation:missing", review); !errors.Is(err, operatorjournal.ErrNoOutstandingObligation) {
		t.Fatalf("discharge of an unknown obligation = %v", err)
	}
	// A discharged obligation is permanent evidence: the row still exists but
	// is no longer outstanding, and it no longer suspends anything.
	afterReview := &operatorjournal.Journal{DB: db.NewConn(t), TenantIDs: j.TenantIDs}
	if left, err := afterReview.OutstandingObligations(ctx, "acme"); err != nil || len(left) != 0 {
		t.Fatalf("outstanding after the review = %+v, %v; want none", left, err)
	}
	var rows int
	db.Conn.QueryRow(ctx, `SELECT count(*) FROM operator_bypass_obligation WHERE obligation_id = $1`, acme.ID).Scan(&rows)
	if rows != 1 {
		t.Errorf("discharged obligation rows = %d, want the row kept as evidence", rows)
	}

	// Malformed input is refused before it reaches the database.
	for name, bad := range map[string]func() error{
		"no database": func() error { return (&operatorjournal.Journal{}).RecordObligation(ctx, globex) },
		"unsealed": func() error {
			u := globex
			u.Digest = ""
			return j.RecordObligation(ctx, u)
		},
		"never due": func() error {
			u := globex
			u.DueAt = u.RecordedAt
			return j.RecordObligation(ctx, u.Sealed())
		},
		"no approver": func() error {
			u := globex
			u.Approver = ""
			return j.RecordObligation(ctx, u.Sealed())
		},
		"unknown tenant": func() error {
			_, err := j.OutstandingObligations(ctx, "initech")
			return err
		},
	} {
		if err := bad(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
