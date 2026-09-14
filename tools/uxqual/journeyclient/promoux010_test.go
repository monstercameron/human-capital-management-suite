package journeyclient

import (
	"testing"
)

// TestTodo_PROMOUX_010_Regression pins the specific caller-side defect the
// second PROMOUX-010 pass found and fixed: tools/uxqual/render/journey's
// reviewSurface (the shared, contained confirmation component) was fully
// built and its own package's tests were green, but ProposalForm.Confirmation
// -- the field that gates whether Start's own final action renders through
// that surface at all -- was set by nobody in this package, so the running
// server's proposal form kept submitting from a bare button in a plain
// jn-formfoot div: no <details>, no Cancel, no Escape target, identical to
// the pre-fix RED. actionConfirmation already fed Action.Confirmation for
// Approve and Reject (see actions() above); this is the same wiring for
// Start, done through the same headline/formatAmount/formatDate/workerName
// helpers rather than a parallel fact-builder.
//
// This lives here, not only in tools/uxqual/render/journey, because that is
// exactly the gap that let the defect through: a render-package test that
// hand-builds a journey.ProposalForm with Confirmation already set proves
// the surface renders correctly once fed, but proves nothing about whether
// the real caller ever feeds it. Only calling the actual ProposalForm and
// FocusedProposalForm functions catches a caller that silently stops
// wiring it.
func TestTodo_PROMOUX_010_Regression(t *testing.T) {
	t.Run("ProposalForm sets Confirmation and ConfirmationNote once worker, placement, pay and date are known", func(t *testing.T) {
		values := map[string]string{
			FieldWorker: testCreatedRef, FieldJobCode: "OPS-HRBP3", FieldGrade: "P3",
			FieldBase: "78000", FieldEffective: "2026-12-01",
		}
		form := ProposalForm(values, testWorkers(), "", testWorkforceOptions())
		if len(form.Confirmation) == 0 {
			t.Fatal("ProposalForm set no Confirmation at all -- Start's final action would submit with no review, reproducing the original RED")
		}
		got := map[string]string{}
		for _, f := range form.Confirmation {
			got[f.Label] = f.Value
		}
		want := map[string]string{
			"Employee":       "Rosa",
			"Placement":      "OPS-HRBP2 · P2 → OPS-HRBP3 · P3",
			"Base pay":       "USD 78,000.00",
			"Effective date": "1 Dec 2026",
		}
		for label, value := range want {
			if got[label] != value {
				t.Errorf("Confirmation[%q] = %q, want %q (full: %+v)", label, got[label], value, form.Confirmation)
			}
		}
		if form.ConfirmationNote == "" {
			t.Error("ProposalForm set no ConfirmationNote; the review surface would show no consequence line")
		}
	})

	t.Run("FocusedProposalForm refines Confirmation with the pinned worker's own name", func(t *testing.T) {
		worker := testWorkers()[0] // testCreatedRef, preferred name "Rosa"
		form := focusedProposalForm(
			map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "78000"},
			testCreatedRef, testWorkforceOptions(), worker,
		)
		if len(form.Confirmation) == 0 {
			t.Fatal("FocusedProposalForm set no Confirmation -- the person-scoped Start route (the one the todo's live evidence measured) would submit with no review")
		}
		if form.Confirmation[0].Label != "Employee" || form.Confirmation[0].Value != "Rosa" {
			t.Fatalf("FocusedProposalForm's Confirmation does not name the pinned worker: %+v", form.Confirmation)
		}
	})

	t.Run("an unresolved worker degrades to the generic Employee fact rather than an empty, unreviewable form", func(t *testing.T) {
		form := ProposalForm(map[string]string{FieldEffective: "2026-12-01"}, nil, "")
		if len(form.Confirmation) == 0 {
			t.Fatal("no worker resolved must still leave a reviewable Confirmation (at least the effective date), not an empty one that would skip the review surface entirely")
		}
		if form.Confirmation[0].Label != "Employee" || form.Confirmation[0].Value != "Employee" {
			t.Fatalf("unresolved-worker fact = %+v, want the generic Employee fallback", form.Confirmation[0])
		}
	})
}
