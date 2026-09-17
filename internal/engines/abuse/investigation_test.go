package abuse

import (
	"errors"
	"testing"
	"time"
)

func abuseFinding() RiskFinding {
	return RiskFinding{
		DetectorID: "detector:privileged-burst", DetectorSemver: "v2", DetectorDigest: "sha256:detector",
		SignalID: "signal:off-hours-access", Kind: SignalKindBulkExport, Category: FindingBulkExport,
		Severity: SeverityHigh,
		Evidence: RiskEvidence{
			Scope: FindingScopePrincipal, Principal: "subject:operator-1", Tenant: "acme",
			WindowStart: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			WindowEnd:   time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
			InputIDs:    []string{"event:1", "event:2"}, EventCount: 2, Volume: 2,
		},
	}
}

func openInvestigation(t *testing.T) Investigation {
	t.Helper()
	inv, err := OpenInvestigation(OpenRequest{
		ID: "inv-1", Tenant: "acme", Compartment: "compartment:trust-safety",
		Finding:      abuseFinding(),
		Investigator: "investigator:case-9",
		EvidenceRefs: []string{"evidence:events-1"},
		OpenedAt:     time.Date(2026, 6, 15, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("OpenInvestigation: %v", err)
	}
	return inv
}

// TestTodo_ABUSE_006 is the primary ABUSE-006 contract test: the finding
// lifecycle preserves compartment, evidence, investigator separation of
// duties and an explicit substantiated/unsubstantiated/inconclusive result
// with correction — while no alert ever equals guilt.
func TestTodo_ABUSE_006(t *testing.T) {
	t.Run("full lifecycle preserves evidence and compartment", func(t *testing.T) {
		inv := openInvestigation(t)
		if inv.State != InvestigationOpen {
			t.Fatalf("state = %v, want OPEN", inv.State)
		}
		inv, err := inv.Begin("investigator:case-9")
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		inv, err = inv.Transfer("compartment:legal", "legal privilege applies", "investigator:case-9")
		if err != nil {
			t.Fatalf("Transfer: %v", err)
		}
		if inv.Compartment != "compartment:legal" {
			t.Fatalf("transfer must move the compartment: %+v", inv)
		}
		if len(inv.EvidenceRefs) != 1 || inv.EvidenceRefs[0] != "evidence:events-1" {
			t.Fatalf("transfer must preserve evidence: %+v", inv.EvidenceRefs)
		}
		inv, err = inv.Disposition(DispositionRequest{
			Outcome: OutcomeSubstantiated, Reason: "off-hours bulk read confirmed against bastion logs",
			DecidedBy: "reviewer:lead-2", DecidedAt: time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("Disposition: %v", err)
		}
		if inv.State != InvestigationSubstantiated || inv.Result == nil {
			t.Fatalf("disposition must close with a result: %+v", inv)
		}
		inv, err = inv.Correct("bastion log reference amended", "reviewer:lead-2")
		if err != nil {
			t.Fatalf("Correct: %v", err)
		}
		if len(inv.Corrections) != 1 || inv.Result.Outcome != OutcomeSubstantiated {
			t.Fatalf("correction must append without rewriting the result: %+v", inv)
		}
		if inv.Digest == "" {
			t.Fatal("investigation must carry a digest")
		}
	})

	t.Run("investigator separation of duties is enforced", func(t *testing.T) {
		// The subject of a finding can never investigate it.
		_, err := OpenInvestigation(OpenRequest{
			ID: "inv-sod", Tenant: "acme", Compartment: "compartment:trust-safety",
			Finding: abuseFinding(), Investigator: "subject:operator-1",
			EvidenceRefs: []string{"evidence:1"}, OpenedAt: time.Now().UTC(),
		})
		if !errors.Is(err, ErrInvestigationRejected) {
			t.Fatalf("subject-as-investigator must be rejected, got %v", err)
		}
		// Four eyes: the investigator cannot disposition their own case.
		inv := openInvestigation(t)
		inv, err = inv.Begin("investigator:case-9")
		if err != nil {
			t.Fatal(err)
		}
		_, err = inv.Disposition(DispositionRequest{
			Outcome: OutcomeUnsubstantiated, Reason: "routine", DecidedBy: "investigator:case-9",
		})
		if !errors.Is(err, ErrInvestigationRejected) {
			t.Fatalf("self-disposition must be rejected, got %v", err)
		}
	})

	t.Run("no alert equals guilt", func(t *testing.T) {
		inv := openInvestigation(t)
		var err error
		inv, err = inv.Begin("investigator:case-9")
		if err != nil {
			t.Fatal(err)
		}
		inv, err = inv.Disposition(DispositionRequest{
			Outcome: OutcomeSubstantiated, Reason: "confirmed", DecidedBy: "reviewer:lead-2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !inv.RequiresHumanDecision() {
			t.Fatal("even a substantiated finding must require a separate human consequence decision")
		}
		if inv.Consequence != "" {
			t.Fatalf("investigation must never carry an automatic consequence: %q", inv.Consequence)
		}
	})

	t.Run("lifecycle order and closed outcomes are enforced", func(t *testing.T) {
		inv := openInvestigation(t)
		_, err := inv.Disposition(DispositionRequest{Outcome: OutcomeSubstantiated, Reason: "x", DecidedBy: "reviewer:lead-2"})
		if !errors.Is(err, ErrInvestigationRejected) {
			t.Fatalf("disposition before investigation must be rejected, got %v", err)
		}
		inv, err = inv.Begin("investigator:case-9")
		if err != nil {
			t.Fatal(err)
		}
		_, err = inv.Disposition(DispositionRequest{Outcome: "GUILTY", Reason: "x", DecidedBy: "reviewer:lead-2"})
		if !errors.Is(err, ErrInvestigationRejected) {
			t.Fatalf("undeclared outcome must be rejected, got %v", err)
		}
		closed, err := inv.Disposition(DispositionRequest{Outcome: OutcomeInconclusive, Reason: "logs aged out", DecidedBy: "reviewer:lead-2"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := closed.Correct("late evidence", "reviewer:lead-2"); err != nil {
			t.Fatalf("closed cases must still accept corrections: %v", err)
		}
		if _, err := closed.Begin("investigator:case-9"); !errors.Is(err, ErrInvestigationRejected) {
			t.Fatalf("closed cases must not reopen silently, got %v", err)
		}
	})
}

// TestTodo_ABUSE_006_Security proves compartment isolation and evidence
// integrity: transfers keep the trail, foreign compartments cannot dispose,
// and tampered evidence bindings fail.
func TestTodo_ABUSE_006_Security(t *testing.T) {
	inv := openInvestigation(t)
	var err error
	inv, err = inv.Begin("investigator:case-9")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := inv.Transfer("compartment:legal", "privilege", "investigator:case-9")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved.Trail) == 0 || moved.Trail[0].From != "compartment:trust-safety" {
		t.Fatalf("transfer must record the prior compartment: %+v", moved.Trail)
	}
	if _, err := moved.Transfer("", "no reason", "investigator:case-9"); !errors.Is(err, ErrInvestigationRejected) {
		t.Fatalf("empty compartment transfer must be rejected, got %v", err)
	}
	tampered := moved
	tampered.EvidenceRefs = []string{"evidence:forged"}
	if tampered.Digest != moved.Digest {
		t.Fatal("test setup error: digest must be a value, not recomputed silently")
	}
	if err := tampered.VerifyDigest(); err == nil {
		t.Fatal("evidence tampering must break digest verification")
	}
	if err := moved.VerifyDigest(); err != nil {
		t.Fatalf("untampered investigation must verify: %v", err)
	}
}

// TestTodo_ABUSE_006_Mutation kills the control-removal mutants: dropping
// subject/investigator separation, the four-eyes rule, or the closed outcome
// vocabulary must each fail.
func TestTodo_ABUSE_006_Mutation(t *testing.T) {
	if _, err := OpenInvestigation(OpenRequest{
		ID: "m1", Tenant: "acme", Compartment: "c",
		Finding: abuseFinding(), Investigator: "subject:operator-1",
		EvidenceRefs: []string{"e"}, OpenedAt: time.Now().UTC(),
	}); !errors.Is(err, ErrInvestigationRejected) {
		t.Fatal("subject-separation mutant survived")
	}
	inv := openInvestigation(t)
	var err error
	inv, err = inv.Begin("investigator:case-9")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inv.Disposition(DispositionRequest{
		Outcome: "AUTO_GUILTY", Reason: "x", DecidedBy: "reviewer:lead-2",
	}); !errors.Is(err, ErrInvestigationRejected) {
		t.Fatal("outcome-vocabulary mutant survived")
	}
	if _, err := inv.Disposition(DispositionRequest{
		Outcome: OutcomeSubstantiated, Reason: "x", DecidedBy: "investigator:case-9",
	}); !errors.Is(err, ErrInvestigationRejected) {
		t.Fatal("four-eyes mutant survived")
	}
}
