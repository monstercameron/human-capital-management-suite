package workorderbilling

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func dec(t *testing.T, s string, scale int32) values.Decimal {
	t.Helper()
	d, e := values.NewDecimal(s, scale, values.RoundingExactRequired)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func money(t *testing.T, s string) values.Money {
	t.Helper()
	m, e := values.NewMoney(s, "USD", 2, values.RoundingExactRequired)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func policy(t *testing.T) PricingPolicy {
	return PricingPolicy{ID: "cust-contract", Version: "v3", Currency: "USD", Mode: PricingUnitPrice, AmountScale: 2, Rounding: values.RoundingExactRequired, UnitRates: map[string]values.Decimal{"EACH": dec(t, "12.50", 2)}}
}
func accepted() Source {
	return Source{ID: "progress-17", Revision: "r4", WorkOrderID: "wo-1", Kind: SourceAcceptedQuantity, Accepted: true, Quantity: decTest("3.00", 2), Unit: "EACH"}
}
func decTest(s string, scale int32) values.Decimal {
	d, _ := values.NewDecimal(s, scale, values.RoundingExactRequired)
	return d
}

func TestBuildAndTransitionReplayRetainsAccountingTrace(t *testing.T) {
	d, err := Build(BuildRequest{ID: "bill-1", WorkOrderID: "wo-1", Policy: policy(t), Sources: []Source{accepted()}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Total.String() != "37.50 USD" || len(d.Lines) != 1 || d.Lines[0].SourceRevision != "r4" || d.Digest == "" {
		t.Fatalf("untraceable amount: %#v", d)
	}
	originalDigest := d.Digest
	d, err = d.Transition("approve", "finance-1", "2026-09-25T12:00:00Z", "approve-1", "")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := d.Transition("approve", "finance-1", "2026-09-25T12:00:00Z", "approve-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed.Events) != 1 || replayed.Digest != originalDigest {
		t.Fatal("replay duplicated event or changed calculation")
	}
	if _, err = d.Transition("issue", "finance-1", "2026-09-25T12:01:00Z", "approve-1", ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("idempotency key collision accepted: %v", err)
	}
	d, err = d.Transition("issue", "finance-1", "2026-09-25T12:02:00Z", "issue-1", "external-invoice-ref")
	if err != nil {
		t.Fatal(err)
	}
	d, err = d.Transition("reconcile", "finance-1", "2026-09-25T12:03:00Z", "reconcile-1", "erp-batch-88")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != Reconciled || d.Lines[0].SourceID != "progress-17" || d.PolicyVersion != "v3" {
		t.Fatalf("lost accounting trace: %#v", d)
	}
}

func TestRejectsUnacceptedOrUnapprovedSource(t *testing.T) {
	req := BuildRequest{ID: "bill-1", WorkOrderID: "wo-1", Policy: policy(t), Sources: []Source{accepted()}}
	req.Sources[0].Accepted = false
	if _, err := Build(req); !errors.Is(err, ErrRejected) {
		t.Fatalf("unaccepted progress billed: %v", err)
	}
	p := policy(t)
	p.Mode = PricingTimeMaterial
	p.TMMultipliers = map[string]values.Decimal{"LABOR": dec(t, "1.25", 2)}
	req = BuildRequest{ID: "bill-2", WorkOrderID: "wo-1", Policy: p, Sources: []Source{{ID: "cost-1", Revision: "r1", WorkOrderID: "wo-1", Kind: SourceApprovedTime, Approved: false, Category: "LABOR", Amount: money(t, "80.00")}}}
	if _, err := Build(req); !errors.Is(err, ErrRejected) {
		t.Fatalf("unapproved time billed: %v", err)
	}
}

func TestFixedMilestoneAndCorrection(t *testing.T) {
	p := policy(t)
	p.Mode = PricingFixedMilestone
	p.MilestoneAmounts = map[string]values.Money{"install": money(t, "250.00")}
	d, err := Build(BuildRequest{ID: "bill-1", WorkOrderID: "wo-1", Policy: p, Sources: []Source{{ID: "ms-1", Revision: "r2", WorkOrderID: "wo-1", Kind: SourceAcceptedMilestone, Accepted: true, MilestoneID: "install"}}})
	if err != nil {
		t.Fatal(err)
	}
	d, err = d.Transition("approve", "finance", "t1", "a1", "")
	if err != nil {
		t.Fatal(err)
	}
	d, err = d.Transition("issue", "finance", "t2", "i1", "invoice-9")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := Build(BuildRequest{ID: "bill-2", WorkOrderID: "wo-1", Policy: p, Sources: []Source{{ID: "ms-1", Revision: "r3", WorkOrderID: "wo-1", Kind: SourceAcceptedMilestone, Accepted: true, MilestoneID: "install"}}})
	if err != nil {
		t.Fatal(err)
	}
	corr, err := Correct(d, replacement, "finance", "t3", "c1", "accepted quantity revised")
	if err != nil {
		t.Fatal(err)
	}
	if corr.CorrectionOf != "bill-1" || corr.Lines[0].SourceRevision != "r3" || len(d.Events) != 2 {
		t.Fatalf("correction damaged original or failed trace: %#v", corr)
	}
}

func TestDigestCoversLineContentAndCommandsRejectTampering(t *testing.T) {
	original, err := Build(BuildRequest{ID: "bill-1", WorkOrderID: "wo-1", Policy: policy(t), Sources: []Source{accepted()}, DescriptionBySource: map[string]string{"progress-17": "Install stringers"}})
	if err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Lines = append([]Line(nil), original.Lines...)
	changed.Lines[0].Description = "Install unapproved extras"
	if digestDraft(changed) == original.Digest {
		t.Fatal("description change did not change content digest")
	}
	if _, err := changed.Transition("approve", "finance", "t1", "approve-1", ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("tampered line approved: %v", err)
	}

	issued, err := original.Transition("approve", "finance", "t1", "approve-1", "")
	if err != nil {
		t.Fatal(err)
	}
	issued, err = issued.Transition("issue", "finance", "t2", "issue-1", "invoice-1")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := Build(BuildRequest{ID: "bill-2", WorkOrderID: "wo-1", Policy: policy(t), Sources: []Source{accepted()}})
	if err != nil {
		t.Fatal(err)
	}
	forged := replacement
	forged.Lines = append([]Line(nil), replacement.Lines...)
	forged.Lines[0].Amount = money(t, "999.99")
	if _, err := Correct(issued, forged, "finance", "t3", "correct-1", "correction"); !errors.Is(err, ErrRejected) {
		t.Fatalf("forged correction accepted: %v", err)
	}
}

func TestBillingDraftJSONRoundTripAllPricingModes(t *testing.T) {
	unit := BuildRequest{ID: "u", WorkOrderID: "wo-1", Policy: policy(t), Sources: []Source{accepted()}}
	tmPolicy := policy(t)
	tmPolicy.ID, tmPolicy.Mode, tmPolicy.TMMultipliers = "tm", PricingTimeMaterial, map[string]values.Decimal{"LABOR": dec(t, "1.25", 2)}
	tm := BuildRequest{ID: "tm", WorkOrderID: "wo-1", Policy: tmPolicy, Sources: []Source{{ID: "cost", Revision: "r1", WorkOrderID: "wo-1", Kind: SourceApprovedTime, Approved: true, Category: "LABOR", Amount: money(t, "80.00")}}}
	fixedPolicy := policy(t)
	fixedPolicy.ID, fixedPolicy.Mode, fixedPolicy.MilestoneAmounts = "fixed", PricingFixedMilestone, map[string]values.Money{"install": money(t, "250.00")}
	fixed := BuildRequest{ID: "fixed", WorkOrderID: "wo-1", Policy: fixedPolicy, Sources: []Source{{ID: "milestone", Revision: "r1", WorkOrderID: "wo-1", Kind: SourceAcceptedMilestone, Accepted: true, MilestoneID: "install"}}}
	for _, req := range []BuildRequest{unit, tm, fixed} {
		draft, err := Build(req)
		if err != nil {
			t.Fatalf("Build(%s): %v", req.ID, err)
		}
		payload, err := json.Marshal(draft)
		if err != nil {
			t.Fatalf("Marshal(%s): %v", req.ID, err)
		}
		var restored BillingDraft
		if err := json.Unmarshal(payload, &restored); err != nil {
			t.Fatalf("Unmarshal(%s): %v", req.ID, err)
		}
		if err := restored.Validate(); err != nil {
			t.Fatalf("Validate(%s) after JSON round-trip: %v", req.ID, err)
		}
		if restored.Digest != draft.Digest || restored.Total.String() != draft.Total.String() {
			t.Fatalf("round-trip changed billing %s", req.ID)
		}
	}
}
