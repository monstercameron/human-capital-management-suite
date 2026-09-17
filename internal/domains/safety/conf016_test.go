package safety

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// caseChainDigest folds every sealed case digest into one value a
// reconstructor can compare. It is test-only: CONF-016 proves the existing
// safety mechanics, it adds no new domain type.
func caseChainDigest(digests []string) string {
	ordered := append([]string(nil), digests...)
	sort.Strings(ordered)
	sum := sha256.Sum256([]byte(strings.Join(ordered, "\x01")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func conf016Instant(day int) values.Instant {
	return values.NewInstant(time.Date(2026, 9, day, 12, 0, 0, 0, time.UTC))
}

// conf016Case builds the full workers-compensation lifecycle in one
// MemoryStore: incident, medical injury, reportability revision, claim,
// payment chain with reversal, restriction clearance, corrective action,
// filing amendment and closing reconciliation. Every revision is stored,
// so lineage is proven by retrieval, never by assertion alone.
type conf016Case struct {
	store          *MemoryStore
	incident       IncidentRevision
	injury         InjuryRevision
	reportability1 ReportabilityDeterminationRevision
	reportability2 ReportabilityDeterminationRevision
	claim          ClaimRevision
	payment1       WorkersCompPaymentRevision
	payment2       WorkersCompPaymentRevision
	payment3       WorkersCompPaymentRevision
	clearance      RestrictionClearanceRevision
	action         CorrectiveActionRevision
	filing1        FilingRevision
	filing2        FilingRevision
	filing3        FilingRevision
	reconciliation SafetyReconciliationRevision
}

func buildConf016Case(t *testing.T) *conf016Case {
	t.Helper()
	c := &conf016Case{store: NewMemoryStore()}
	var err error
	c.incident, err = NewIncidentRevision(IncidentRevision{ID: "inc-016", CaseRef: "case-016", CompartmentRef: "operational", Revision: 1, IncidentAt: conf016Instant(5), Kind: IncidentInjury, WorkerRef: "worker-016", ReporterRef: "reporter-016", LocationRef: "site-016", Description: "fall from platform", Status: IncidentOpen})
	if err != nil {
		t.Fatalf("incident: %v", err)
	}
	if err := c.store.SaveIncident(c.incident); err != nil {
		t.Fatalf("save incident: %v", err)
	}
	c.injury, err = NewInjuryRevision(InjuryRevision{ID: "inj-016", CaseRef: "case-016", CompartmentRef: "medical", Revision: 1, IncidentRef: c.incident.CanonicalDigest, WorkerRef: "worker-016", Kind: InjuryPhysical, MedicalEvidenceRef: "med-ev-016", Severity: "restricted"})
	if err != nil {
		t.Fatalf("injury: %v", err)
	}
	if err := c.store.SaveInjury(c.injury); err != nil {
		t.Fatalf("save injury: %v", err)
	}
	c.reportability1, err = DetermineReportability(c.incident, OSHARecordable, "rep-016")
	if err != nil {
		t.Fatalf("reportability rev1: %v", err)
	}
	if err := c.store.SaveReportability(c.reportability1); err != nil {
		t.Fatalf("save reportability rev1: %v", err)
	}
	// New clinical evidence upgrades severity: a successor revision with
	// the exact parent digest, never a rewrite of rev1.
	c.reportability2, err = NewReportabilityDeterminationRevision(ReportabilityDeterminationRevision{ID: "rep-016", CaseRef: "case-016", CompartmentRef: "regulatory", Revision: 2, ParentRevision: 1, ParentDigest: c.reportability1.CanonicalDigest, IncidentRef: c.incident.ID, IncidentAt: c.incident.IncidentAt, Class: OSHAReportableSevere, Clock: ClockSevere24Hours, RuleCitation: "29 CFR 1904.39", Deadline: values.NewInstant(c.incident.IncidentAt.Time().Add(24 * time.Hour)), Rationale: "clinical evidence upgraded severity beyond recordable"})
	if err != nil {
		t.Fatalf("reportability rev2: %v", err)
	}
	if err := c.store.SaveReportability(c.reportability2); err != nil {
		t.Fatalf("save reportability rev2: %v", err)
	}
	c.claim, err = NewClaimRevision(ClaimRevision{ID: "claim-016", CaseRef: "case-016", CompartmentRef: "claims", Revision: 1, IncidentRef: c.incident.CanonicalDigest, WorkerRef: "worker-016", ClaimRef: "carrier-claim-016", AuthorityRef: "wc-board-016", Status: ClaimSubmitted})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := c.store.SaveClaim(c.claim); err != nil {
		t.Fatalf("save claim: %v", err)
	}
	c.payment1, err = NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "pay-016", CaseRef: "case-016", CompartmentRef: "claims", Revision: 1, ClaimRef: c.claim.CanonicalDigest, WorkerRef: "worker-016", AmountMinor: 125000, Currency: "USD", Status: PaymentObserved, ObservationRef: "obs-pay-016-1"})
	if err != nil {
		t.Fatalf("payment rev1: %v", err)
	}
	if err := c.store.SaveWorkersCompPayment(c.payment1); err != nil {
		t.Fatalf("save payment rev1: %v", err)
	}
	c.payment2, err = NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "pay-016", CaseRef: "case-016", CompartmentRef: "claims", Revision: 2, ParentRevision: 1, ParentDigest: c.payment1.CanonicalDigest, ClaimRef: c.claim.CanonicalDigest, WorkerRef: "worker-016", AmountMinor: 125000, Currency: "USD", Status: PaymentSettled, ObservationRef: "obs-pay-016-2"})
	if err != nil {
		t.Fatalf("payment rev2: %v", err)
	}
	if err := c.store.SaveWorkersCompPayment(c.payment2); err != nil {
		t.Fatalf("save payment rev2: %v", err)
	}
	// Reversal is an append-only successor: the settled amount is never
	// rewritten, only superseded with an idempotency reference.
	c.payment3, err = NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "pay-016", CaseRef: "case-016", CompartmentRef: "claims", Revision: 3, ParentRevision: 2, ParentDigest: c.payment2.CanonicalDigest, ClaimRef: c.claim.CanonicalDigest, WorkerRef: "worker-016", AmountMinor: 125000, Currency: "USD", Status: PaymentReversed, ObservationRef: "obs-pay-016-3", ReversalRef: "reversal-016"})
	if err != nil {
		t.Fatalf("payment rev3: %v", err)
	}
	if err := c.store.SaveWorkersCompPayment(c.payment3); err != nil {
		t.Fatalf("save payment rev3: %v", err)
	}
	c.clearance, err = NewRestrictionClearanceRevision(RestrictionClearanceRevision{ID: "clr-016", CaseRef: "case-016", CompartmentRef: "medical", Revision: 1, RestrictionRef: "restr-016", WorkerRef: "worker-016", EvidenceRef: "med-ev-016", ObservationRef: "obs-clr-016", AuthorityRef: "med-board-016", EvidenceDigest: "sha256:evidence-016"})
	if err != nil {
		t.Fatalf("clearance: %v", err)
	}
	if err := c.store.SaveRestrictionClearance(c.clearance); err != nil {
		t.Fatalf("save clearance: %v", err)
	}
	c.action, err = NewCorrectiveActionRevision(CorrectiveActionRevision{ID: "act-016", CaseRef: "case-016", CompartmentRef: "operational", Revision: 1, IncidentRef: c.incident.CanonicalDigest, OwnerRef: "owner-016", DueRule: "verify-before-close", Action: "install guardrail", Status: CorrectiveActionClosed, VerificationEvidenceRef: "verify-016"})
	if err != nil {
		t.Fatalf("corrective action: %v", err)
	}
	if err := c.store.SaveCorrectiveAction(c.action); err != nil {
		t.Fatalf("save corrective action: %v", err)
	}
	c.filing1, err = NewFilingRevision(FilingRevision{ID: "file-016", CaseRef: "case-016", CompartmentRef: "regulatory", Revision: 1, IncidentRef: c.incident.CanonicalDigest, AuthorityRef: "wc-board-016", ProviderRef: "carrier-016", SubmissionRef: "sub-016-1", SignerRef: "signer-016", SignatureRef: "sig-016-1", Status: FilingSubmitted})
	if err != nil {
		t.Fatalf("filing rev1: %v", err)
	}
	if err := c.store.SaveFiling(c.filing1); err != nil {
		t.Fatalf("save filing rev1: %v", err)
	}
	c.filing2, err = NewFilingRevision(FilingRevision{ID: "file-016", CaseRef: "case-016", CompartmentRef: "regulatory", Revision: 2, ParentRevision: 1, ParentDigest: c.filing1.CanonicalDigest, IncidentRef: c.incident.CanonicalDigest, AuthorityRef: "wc-board-016", ProviderRef: "carrier-016", SubmissionRef: "sub-016-2", SignerRef: "signer-016", SignatureRef: "sig-016-2", Status: FilingRejected, ObservationRef: "rej-016", ObservedAt: conf016Instant(6)})
	if err != nil {
		t.Fatalf("filing rev2: %v", err)
	}
	if err := c.store.SaveFiling(c.filing2); err != nil {
		t.Fatalf("save filing rev2: %v", err)
	}
	c.filing3, err = ResubmitFiling(c.filing2, "sub-016-3", "sig-016-3", "amend-016", conf016Instant(7))
	if err != nil {
		t.Fatalf("filing amendment: %v", err)
	}
	if err := c.store.SaveFiling(c.filing3); err != nil {
		t.Fatalf("save filing rev3: %v", err)
	}
	c.reconciliation, err = NewSafetyReconciliationRevision(SafetyReconciliationRevision{ID: "recon-016", CaseRef: "case-016", CompartmentRef: "regulatory", Revision: 1, IncidentRef: c.incident.CanonicalDigest, SourceRevisionDigest: c.incident.CanonicalDigest, ObservationRef: "recon-obs-016", ObservedAt: conf016Instant(7), AmendedFilingRef: c.filing3.CanonicalDigest, Status: ReconciliationOpen})
	if err != nil {
		t.Fatalf("reconciliation: %v", err)
	}
	if err := c.store.SaveSafetyReconciliation(c.reconciliation); err != nil {
		t.Fatalf("save reconciliation: %v", err)
	}
	return c
}

// TestTodo_CONF_016 is the PRIMARY CONF-016 contract test: protected
// evidence, reportability revision, claim/payment/reversal, corrective
// action and filing amendment preserve lineage and reconciliation across
// the safety, injury and workers-compensation compartments.
func TestTodo_CONF_016(t *testing.T) {
	c := buildConf016Case(t)

	t.Run("GREEN: protected evidence never leaks across compartments", func(t *testing.T) {
		if strings.Contains(c.injury.Explain(), "med-ev-016") {
			t.Fatalf("medical evidence leaked into explanation: %q", c.injury.Explain())
		}
		if strings.Contains(c.incident.Explain(), "worker-016") || strings.Contains(c.incident.Explain(), "reporter-016") {
			t.Fatalf("worker identity leaked into operational explanation: %q", c.incident.Explain())
		}
	})

	t.Run("GREEN: reportability revision preserves lineage", func(t *testing.T) {
		rev1, ok := c.store.GetReportability("rep-016", 1)
		if !ok {
			t.Fatal("original reportability revision was overwritten")
		}
		rev2, ok := c.store.GetReportability("rep-016", 2)
		if !ok {
			t.Fatal("reportability successor is missing")
		}
		if rev2.ParentDigest != rev1.CanonicalDigest {
			t.Fatal("reportability successor does not name its parent digest")
		}
		if rev1.Class == rev2.Class {
			t.Fatal("revision changed no reportability class")
		}
	})

	t.Run("GREEN: claim, payment and reversal reconcile without duplication", func(t *testing.T) {
		// Identical replay is idempotent.
		if err := c.store.SaveClaim(c.claim); err != nil {
			t.Fatalf("identical claim replay must be idempotent, got %v", err)
		}
		if err := c.store.SaveWorkersCompPayment(c.payment2); err != nil {
			t.Fatalf("identical payment replay must be idempotent, got %v", err)
		}
		// The settled amount survives its reversal untouched.
		settled, ok := c.store.GetWorkersCompPayment("pay-016", 2)
		if !ok || settled.AmountMinor != 125000 || settled.Status != PaymentSettled {
			t.Fatalf("reversal rewrote the settled revision: %+v", settled)
		}
		reversed, ok := c.store.GetWorkersCompPayment("pay-016", 3)
		if !ok || reversed.Status != PaymentReversed || reversed.ReversalRef == "" {
			t.Fatalf("reversal successor is not bound: %+v", reversed)
		}
	})

	t.Run("RED: duplicates, authority-free clearance and unverified closure fail", func(t *testing.T) {
		dup := c.claim
		dup.Status = ClaimAccepted
		dup.CanonicalDigest = ""
		resealed, err := NewClaimRevision(dup)
		if err != nil {
			t.Fatalf("reseal: %v", err)
		}
		if err := c.store.SaveClaim(resealed); !errors.Is(err, ErrRefused) {
			t.Fatalf("duplicate claim under one key must be refused, got %v", err)
		}
		forgedPay, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "pay-016", CaseRef: "case-016", CompartmentRef: "claims", Revision: 2, ParentRevision: 1, ParentDigest: c.payment1.CanonicalDigest, ClaimRef: c.claim.CanonicalDigest, WorkerRef: "worker-016", AmountMinor: 999999, Currency: "USD", Status: PaymentSettled, ObservationRef: "obs-forged"})
		if err != nil {
			t.Fatalf("reseal: %v", err)
		}
		if err := c.store.SaveWorkersCompPayment(forgedPay); !errors.Is(err, ErrRefused) {
			t.Fatalf("duplicate payment under one key must be refused, got %v", err)
		}
		if _, err := NewRestrictionClearanceRevision(RestrictionClearanceRevision{ID: "clr-x", CaseRef: "case-016", CompartmentRef: "medical", Revision: 1, RestrictionRef: "restr-016", WorkerRef: "worker-016", EvidenceRef: "med-ev-016", ObservationRef: "obs-x", EvidenceDigest: "sha256:x"}); !errors.Is(err, ErrInvalidRevision) {
			t.Fatalf("authority-free clearance must be refused, got %v", err)
		}
		if _, err := NewCorrectiveActionRevision(CorrectiveActionRevision{ID: "act-x", CaseRef: "case-016", CompartmentRef: "operational", Revision: 1, IncidentRef: c.incident.CanonicalDigest, OwnerRef: "owner-016", DueRule: "verify-before-close", Action: "x", Status: CorrectiveActionClosed}); err == nil {
			t.Fatal("unverified closure must be refused")
		}
	})

	t.Run("GREEN: filing amendment and reconciliation close the lineage", func(t *testing.T) {
		if c.filing3.ParentDigest != c.filing2.CanonicalDigest || c.filing3.Status != FilingSubmitted {
			t.Fatal("amendment does not chain to the rejected revision")
		}
		if c.reconciliation.AmendedFilingRef != c.filing3.CanonicalDigest {
			t.Fatal("reconciliation does not cite the amended filing")
		}
		got, ok := c.store.GetFiling("file-016", 1)
		if !ok || got.CanonicalDigest != c.filing1.CanonicalDigest {
			t.Fatal("amendment overwrote the original filing")
		}
	})
}

// TestTodo_CONF_016_Golden pins the case chain digest: every sealed
// revision contributes, so any lineage drift fails loudly.
func TestTodo_CONF_016_Golden(t *testing.T) {
	c := buildConf016Case(t)
	digests := []string{
		c.incident.CanonicalDigest, c.injury.CanonicalDigest,
		c.reportability1.CanonicalDigest, c.reportability2.CanonicalDigest,
		c.claim.CanonicalDigest, c.payment1.CanonicalDigest,
		c.payment2.CanonicalDigest, c.payment3.CanonicalDigest,
		c.clearance.CanonicalDigest, c.action.CanonicalDigest,
		c.filing1.CanonicalDigest, c.filing2.CanonicalDigest,
		c.filing3.CanonicalDigest, c.reconciliation.CanonicalDigest,
	}
	chain := caseChainDigest(digests)
	const golden = "sha256:9ca591a3e92d7932a580695a33db389f73f56fb9cf6e4473fb7186246c3dfe2f"
	if chain != golden {
		t.Fatalf("case chain drifted: got %s, want %s", chain, golden)
	}
}
