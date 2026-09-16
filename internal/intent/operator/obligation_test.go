package operator

import (
	"context"
	"testing"
	"time"
)

func obligationAt(key string, family Family, operatorID, approver string, at time.Time, ids ...string) Obligation {
	return Obligation{
		ID: ObligationID(testTenant, key), Tenant: testTenant, Kind: KindWorkflowRepair, Family: family,
		Scope: Scope{Resource: "workflow_instance", IDs: ids}, Operator: operatorID, Approver: approver,
		AuthorityKind: AuthorityBreakGlass, AuthorityRef: "bg-" + key, IdempotencyKey: key,
		RequestDigest: "sha256:" + key, TicketRef: "INC-39", Bypassed: []string{BypassJITAuthority},
		BypassReason: "region down", RecordedAt: at, DueAt: at.Add(ReviewWindow),
	}.Sealed()
}

// TestMemoryObligationsAreDurableIdempotentAndTenantScoped proves the
// in-process obligation store the gateway falls back to: recording is
// idempotent per obligation id, a store read back by a recomposed journal
// still holds the obligation, discharging removes it from the outstanding set
// exactly once, and one tenant never sees another's debts.
func TestMemoryObligationsAreDurableIdempotentAndTenantScoped(t *testing.T) {
	ctx := context.Background()
	j := NewMemoryJournal()
	first := obligationAt("key-a", FamilyRepair, "operator:ana", "approver:lead", testNow, "instance-1")
	later := obligationAt("key-b", FamilyOverride, "operator:ana", "approver:lead", testNow.Add(time.Hour), "instance-2")
	for _, o := range []Obligation{first, later, first} {
		if err := j.RecordObligation(ctx, o); err != nil {
			t.Fatalf("RecordObligation(%s): %v", o.ID, err)
		}
	}
	out, err := j.OutstandingObligations(ctx, testTenant)
	if err != nil || len(out) != 2 {
		t.Fatalf("outstanding = %+v, %v; want two obligations", out, err)
	}
	if !out[0].DueAt.Before(out[1].DueAt) {
		t.Errorf("outstanding is not ordered by due date: %s then %s", out[0].DueAt, out[1].DueAt)
	}

	// Another tenant's obligations are its own, and neither tenant sees the
	// other's.
	foreign := obligationAt("key-a", FamilyRepair, "operator:zed", "approver:kim", testNow, "instance-1")
	foreign.Tenant = "tenant-other"
	foreign = foreign.Sealed()
	if err := j.RecordObligation(ctx, foreign); err != nil {
		t.Fatalf("RecordObligation for another tenant: %v", err)
	}
	if got, err := j.OutstandingObligations(ctx, "tenant-other"); err != nil || len(got) != 1 || got[0].Operator != "operator:zed" {
		t.Fatalf("another tenant's outstanding = %+v, %v; want only its own", got, err)
	}
	if got, err := j.OutstandingObligations(ctx, testTenant); err != nil || len(got) != 2 {
		t.Fatalf("outstanding leaked across tenants: %+v, %v", got, err)
	}

	review := ObligationReview{Reviewer: "reviewer:sam", Outcome: ObligationJustified, Note: "checked", At: testNow.Add(time.Hour)}
	discharged, err := j.DischargeObligation(ctx, testTenant, first.ID, review)
	if err != nil || discharged.Review == nil || discharged.Verify() != nil {
		t.Fatalf("discharge = %+v, %v", discharged, err)
	}
	if _, err := j.DischargeObligation(ctx, testTenant, first.ID, review); CodeOf(err) != CodeObligationUnknown {
		t.Fatalf("second discharge = %v, want %s", err, CodeObligationUnknown)
	}
	if _, err := j.DischargeObligation(ctx, testTenant, "obligation:missing", review); CodeOf(err) != CodeObligationUnknown {
		t.Fatalf("discharge of an unknown obligation = %v, want %s", err, CodeObligationUnknown)
	}
	if out, err := j.OutstandingObligations(ctx, testTenant); err != nil || len(out) != 1 || out[0].ID != later.ID {
		t.Fatalf("outstanding after discharge = %+v, %v; want only %s", out, err, later.ID)
	}
}

// TestObligationValidationRefusesAnUnaccountableDebt proves an obligation that
// could not hold anyone to account is refused before it is stored.
func TestObligationValidationRefusesAnUnaccountableDebt(t *testing.T) {
	ctx := context.Background()
	good := obligationAt("key-v", FamilyRepair, "operator:ana", "approver:lead", testNow, "instance-1")
	for name, mutate := range map[string]func(*Obligation){
		"no id":        func(o *Obligation) { o.ID = "" },
		"no tenant":    func(o *Obligation) { o.Tenant = "" },
		"no key":       func(o *Obligation) { o.IdempotencyKey = "" },
		"no operator":  func(o *Obligation) { o.Operator = " " },
		"no approver":  func(o *Obligation) { o.Approver = "" },
		"no bypass":    func(o *Obligation) { o.Bypassed = nil },
		"no reason":    func(o *Obligation) { o.BypassReason = " " },
		"never due":    func(o *Obligation) { o.DueAt = o.RecordedAt },
		"due backward": func(o *Obligation) { o.DueAt = o.RecordedAt.Add(-time.Hour) },
		"no instant":   func(o *Obligation) { o.RecordedAt = time.Time{} },
	} {
		bad := good
		mutate(&bad)
		bad = bad.Sealed() // resealed, so only the rule refuses it, not the digest
		if err := NewMemoryJournal().RecordObligation(ctx, bad); err == nil {
			t.Errorf("%s: recorded", name)
		}
	}

	// A review needs a reviewer, a note, an instant and a declared outcome.
	for name, review := range map[string]ObligationReview{
		"no reviewer": {Outcome: ObligationJustified, Note: "n", At: testNow},
		"no note":     {Reviewer: "reviewer:sam", Outcome: ObligationJustified, At: testNow},
		"no instant":  {Reviewer: "reviewer:sam", Outcome: ObligationJustified, Note: "n"},
		"bad outcome": {Reviewer: "reviewer:sam", Outcome: "MAYBE", Note: "n", At: testNow},
		"is operator": {Reviewer: "OPERATOR:ANA", Outcome: ObligationJustified, Note: "n", At: testNow},
		"is approver": {Reviewer: "approver:lead", Outcome: ObligationJustified, Note: "n", At: testNow},
	} {
		if _, err := good.Discharged(review); err == nil {
			t.Errorf("%s: discharged", name)
		}
	}
}

// TestApproverOverScopeFindsOnlyOverlappingUndischargedObligations proves the
// durable half of the repair separation rule looks at exactly the right rows.
func TestApproverOverScopeFindsOnlyOverlappingUndischargedObligations(t *testing.T) {
	open := obligationAt("key-1", FamilyRepair, "operator:ana", "approver:lead", testNow, "instance-1", "instance-2")
	elsewhere := obligationAt("key-2", FamilyRepair, "operator:ana", "approver:kim", testNow, "instance-9")
	reviewed, err := open.Discharged(ObligationReview{Reviewer: "reviewer:sam", Outcome: ObligationJustified, Note: "n", At: testNow})
	if err != nil {
		t.Fatal(err)
	}
	otherResource := obligationAt("key-3", FamilyRepair, "operator:ana", "approver:lead", testNow, "instance-1")
	otherResource.Scope.Resource = "payroll_run"
	otherResource = otherResource.Sealed()
	all := []Obligation{open, elsewhere, otherResource}
	scope := Scope{Resource: "workflow_instance", IDs: []string{"instance-2"}}

	if got, ok := ApproverOverScope(all, "APPROVER:LEAD", scope); !ok || got.ID != open.ID {
		t.Errorf("ApproverOverScope = %+v, %v; want %s (case-insensitive match)", got, ok, open.ID)
	}
	if _, ok := ApproverOverScope(all, "approver:kim", scope); ok {
		t.Error("an approver of an unrelated scope was treated as a conflict")
	}
	if _, ok := ApproverOverScope(all, "operator:ana", scope); ok {
		t.Error("the operator of record was treated as its own approver")
	}
	if _, ok := ApproverOverScope(all, "  ", scope); ok {
		t.Error("a blank principal matched an approver")
	}
	if _, ok := ApproverOverScope([]Obligation{reviewed}, "approver:lead", scope); ok {
		t.Error("a discharged obligation still blocks the approver")
	}
	if _, ok := ApproverOverScope(all, "approver:lead", Scope{Resource: "payroll_run", IDs: []string{"instance-1"}}); !ok {
		t.Error("a same-resource overlap was missed")
	}
}

// TestSuspendedFamiliesReportsTheOldestOverdueObligationPerFamily proves the
// suspension set names one obligation per family and prefers the oldest, so a
// refusal cites the debt that has been outstanding longest.
func TestSuspendedFamiliesReportsTheOldestOverdueObligationPerFamily(t *testing.T) {
	oldest := obligationAt("key-old", FamilyRepair, "operator:ana", "approver:lead", testNow, "instance-1")
	newer := obligationAt("key-new", FamilyRepair, "operator:ana", "approver:lead", testNow.Add(time.Hour), "instance-2")
	override := obligationAt("key-ov", FamilyOverride, "operator:ana", "approver:lead", testNow, "instance-3")
	now := testNow.Add(ReviewWindow + 2*time.Hour)

	got := SuspendedFamilies([]Obligation{newer, oldest, override}, now)
	if len(got) != 2 {
		t.Fatalf("suspended families = %+v, want repair and override", got)
	}
	if got[FamilyRepair].ID != oldest.ID {
		t.Errorf("repair suspension cites %s, want the oldest overdue %s", got[FamilyRepair].ID, oldest.ID)
	}
	if got[FamilyOverride].ID != override.ID {
		t.Errorf("override suspension cites %s, want %s", got[FamilyOverride].ID, override.ID)
	}
	if none := SuspendedFamilies(nil, now); len(none) != 0 {
		t.Errorf("no obligations suspended %d families", len(none))
	}
}
