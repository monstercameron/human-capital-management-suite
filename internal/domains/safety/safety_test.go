package safety

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func safetyInstant() values.Instant {
	return values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
}

func TestSafetyDomainSeparatesOperationalMedicalClaimAndRegulatoryAuthority(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "incident-1", CaseRef: "case-1", CompartmentRef: "operational", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentInjury, WorkerRef: "worker-secret", ReporterRef: "reporter-secret", LocationRef: "site-1", Description: "operational incident", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	injury, err := NewInjuryRevision(InjuryRevision{ID: "injury-1", CaseRef: "case-1", CompartmentRef: "medical", Revision: 1, IncidentRef: incident.CanonicalDigest, WorkerRef: "worker-secret", Kind: InjuryPhysical, MedicalEvidenceRef: "medical-evidence-1", Severity: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	determination, err := DetermineReportability(incident, OSHAReportableSevere, "reportability-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := determination.Deadline.Time().Sub(incident.IncidentAt.Time()); got != 24*time.Hour {
		t.Fatalf("deadline delta = %s", got)
	}
	claim, err := NewClaimRevision(ClaimRevision{ID: "claim-1", CaseRef: "case-1", CompartmentRef: "claims", Revision: 1, IncidentRef: incident.CanonicalDigest, WorkerRef: "worker-secret", ClaimRef: "claim-ref", AuthorityRef: "workers-comp-authority", Status: ClaimSubmitted})
	if err != nil {
		t.Fatal(err)
	}
	restriction, err := NewWorkRestrictionRevision(WorkRestrictionRevision{ID: "restriction-1", CaseRef: "case-1", CompartmentRef: "medical", Revision: 1, IncidentRef: incident.CanonicalDigest, WorkerRef: "worker-secret", Kind: RestrictionModifiedDuty, MedicalEvidenceRef: "medical-evidence-1", Status: RestrictionActive})
	if err != nil {
		t.Fatal(err)
	}
	action, err := NewCorrectiveActionRevision(CorrectiveActionRevision{ID: "action-1", CaseRef: "case-1", CompartmentRef: "operational", Revision: 1, IncidentRef: incident.CanonicalDigest, OwnerRef: "owner-1", DueRule: "verify-before-close", Action: "repair guard", Status: CorrectiveActionOpen})
	if err != nil {
		t.Fatal(err)
	}
	if injury.CanonicalDigest == "" || claim.CanonicalDigest == "" || restriction.CanonicalDigest == "" || action.CanonicalDigest == "" {
		t.Fatal("expected canonical digests")
	}
	if strings.Contains(incident.Explain(), "worker-secret") || strings.Contains(incident.Explain(), "reporter-secret") {
		t.Fatalf("sensitive incident values leaked: %q", incident.Explain())
	}
	if !strings.Contains(determination.Explain(), string(OSHAReportableSevere)) {
		t.Fatalf("reportability explanation = %q", determination.Explain())
	}
}

func TestTodo_SAFETY_001_Property(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "d", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 2, ParentRevision: 1, ParentDigest: incident.CanonicalDigest, IncidentAt: incident.IncidentAt, Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "updated", Status: IncidentClosed})
	if err != nil {
		t.Fatal(err)
	}
	if incident.Revision != 1 || child.ParentDigest != incident.CanonicalDigest || incident.CanonicalDigest == child.CanonicalDigest {
		t.Fatal("lineage or immutability contract failed")
	}
}

func TestTodo_SAFETY_001_Golden(t *testing.T) {
	for _, class := range []ReportabilityClass{NotReportable, OSHARecordable, OSHAReportableFatality, OSHAReportableSevere} {
		rule, ok := RuleFor(class)
		if !ok {
			t.Fatalf("missing rule for %s", class)
		}
		if rule.Citation == "" || rule.Clock == "" {
			t.Fatalf("incomplete rule for %s: %+v", class, rule)
		}
	}
}

func TestTodo_SAFETY_001_Race(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "d", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	const writers = 16
	var wait sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errs <- store.SaveIncident(incident)
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if _, ok := store.GetIncident("i", 1); !ok {
		t.Fatal("incident not found")
	}
}

func TestTodo_SAFETY_001_Fault(t *testing.T) {
	_, err := NewClaimRevision(ClaimRevision{ID: "claim", CaseRef: "case", CompartmentRef: "claims", Revision: 1, WorkerRef: "worker", ClaimRef: "claim-ref", AuthorityRef: "authority", Status: ClaimDraft})
	if !errors.Is(err, ErrIncidentRequired) {
		t.Fatalf("claim error = %v", err)
	}
	_, err = NewCorrectiveActionRevision(CorrectiveActionRevision{ID: "action", CaseRef: "case", CompartmentRef: "operational", Revision: 1, IncidentRef: "incident", OwnerRef: "owner", DueRule: "rule", Action: "action", Status: CorrectiveActionClosed})
	if !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("corrective-action error = %v", err)
	}
}

func TestTodo_SAFETY_001_Security(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentInjury, WorkerRef: "secret-worker", ReporterRef: "secret-reporter", LocationRef: "secret-location", Description: "secret description", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-worker", "secret-reporter", "secret-location", "secret description"} {
		if strings.Contains(incident.Explain(), secret) {
			t.Fatalf("secret %q leaked: %q", secret, incident.Explain())
		}
	}
}

func TestTodo_SAFETY_001_Conformance(t *testing.T) {
	table := ClockTable()
	if len(table) != 4 {
		t.Fatalf("clock table length = %d", len(table))
	}
	if table[1].Duration != 7*24*time.Hour || table[2].Duration != 8*time.Hour || table[3].Duration != 24*time.Hour {
		t.Fatalf("clock table = %+v", table)
	}
}

func TestTodo_SAFETY_001_Mutation(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "d", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	incident.CanonicalDigest = "sha256:forged"
	if err := incident.Validate(); err == nil {
		t.Fatal("forged digest accepted")
	}
}

func safetyFiling(t *testing.T, rev uint64, parent string, status FilingStatus, observation string) FilingRevision {
	t.Helper()
	var observedAt values.Instant
	if observation != "" {
		observedAt = values.NewInstant(safetyInstant().Time().Add(time.Duration(rev) * time.Minute))
	}
	r, err := NewFilingRevision(FilingRevision{ID: "filing", CaseRef: "case", CompartmentRef: "regulatory", Revision: rev, ParentRevision: rev - 1, ParentDigest: parent, IncidentRef: "incident", AuthorityRef: "authority", ProviderRef: "provider", SubmissionRef: "submission-" + string(rune('0'+rev)), SignerRef: "authorized-signer", SignatureRef: "signature-" + string(rune('0'+rev)), Status: status, ObservationRef: observation, ObservedAt: observedAt})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSafetyConformancePreservesFilingPaymentRestrictionAndCorrectionHistory(t *testing.T) {
	filing := safetyFiling(t, 1, "", FilingSubmitted, "")
	accepted := safetyFiling(t, 2, filing.CanonicalDigest, FilingAccepted, "obs-accepted")
	if accepted.ParentDigest != filing.CanonicalDigest || accepted.CanonicalDigest == filing.CanonicalDigest {
		t.Fatal("filing submission and acceptance must be separate revisions")
	}
	payment, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "payment", CaseRef: "case", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 125005, Currency: "USD", Status: PaymentSettled, ObservationRef: "bank-obs-1"})
	if err != nil {
		t.Fatal(err)
	}
	reversal, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "payment", CaseRef: "case", CompartmentRef: "claims", Revision: 2, ParentRevision: 1, ParentDigest: payment.CanonicalDigest, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: payment.AmountMinor, Currency: payment.Currency, Status: PaymentReversed, ObservationRef: "bank-obs-2", ReversalRef: "ledger-reversal-1"})
	if err != nil {
		t.Fatal(err)
	}
	if reversal.AmountMinor != payment.AmountMinor || reversal.CanonicalDigest == payment.CanonicalDigest {
		t.Fatal("reversal must preserve original amount and append a new event")
	}
	clearance, err := NewRestrictionClearanceRevision(RestrictionClearanceRevision{ID: "clearance", CaseRef: "case", CompartmentRef: "medical", Revision: 1, RestrictionRef: "restriction", WorkerRef: "worker", EvidenceRef: "evidence", EvidenceDigest: "sha256:evidence", AuthorityRef: "licensed-clinician", ObservationRef: "obs-clear"})
	if err != nil {
		t.Fatal(err)
	}
	if clearance.RestrictionRef == "" {
		t.Fatal("clearance must reference, not erase, restriction")
	}
	closed, err := NewSafetyReconciliationRevision(SafetyReconciliationRevision{ID: "recon", CaseRef: "case", CompartmentRef: "regulatory", Revision: 1, IncidentRef: "incident", SourceRevisionDigest: filing.CanonicalDigest, ObservationRef: "obs-reconcile", PriorObservedAt: values.NewInstant(safetyInstant().Time().Add(-time.Minute)), ObservedAt: safetyInstant(), AmendedFilingRef: accepted.CanonicalDigest, Status: ReconciliationClosed})
	if err != nil {
		t.Fatal(err)
	}
	if closed.AmendedFilingRef == "" {
		t.Fatal("closed reconciliation must retain amendment")
	}
}

func TestTodo_SAFETY_002_Property(t *testing.T) {
	if FreshObservation(safetyInstant(), safetyInstant()) {
		t.Fatal("same instant is not fresh")
	}
	if !FreshObservation(safetyInstant(), values.NewInstant(safetyInstant().Time().Add(time.Minute))) {
		t.Fatal("later observation must be fresh")
	}
}

func TestTodo_SAFETY_002_Golden(t *testing.T) {
	p, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "p", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 100, Currency: "USD", Status: PaymentObserved, ObservationRef: "obs"})
	if err != nil || p.AmountMinor != 100 || p.Currency != "USD" {
		t.Fatalf("exact payment fixture = %+v, %v", p, err)
	}
	if got, want := p.CanonicalDigest, "sha256:600a012bbcd60f49398f61590ad062c9577f05478be2db2c713abb0a7f80c58f"; got != want {
		t.Fatalf("payment canonical digest = %q, want %q", got, want)
	}
}

func TestTodo_SAFETY_002_Race(t *testing.T) {
	s := NewMemoryStore()
	r := safetyFiling(t, 1, "", FilingSubmitted, "")
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() { done <- s.SaveFiling(r) }()
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	got, ok := s.GetFiling(r.ID, r.Revision)
	if !ok || got.CanonicalDigest != r.CanonicalDigest {
		t.Fatalf("concurrent idempotent saves lost or changed state: %+v, %v", got, ok)
	}
}

func TestTodo_SAFETY_002_Fault(t *testing.T) {
	_, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "p", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 100, Currency: "USD", Status: PaymentSettled})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("payment without observation = %v", err)
	}
	_, err = NewSafetyReconciliationRevision(SafetyReconciliationRevision{ID: "r", CaseRef: "c", CompartmentRef: "regulatory", Revision: 1, IncidentRef: "i", SourceRevisionDigest: "sha", ObservationRef: "obs", PriorObservedAt: values.NewInstant(safetyInstant().Time().Add(-time.Minute)), ObservedAt: safetyInstant(), Status: ReconciliationClosed, Obligations: []string{"file"}})
	if !errors.Is(err, ErrPendingObligation) {
		t.Fatalf("pending obligation = %v", err)
	}
}

func TestTodo_SAFETY_002_Security(t *testing.T) {
	p, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "p", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "secret-claim", WorkerRef: "secret-worker", AmountMinor: 1, Currency: "USD", Status: PaymentObserved, ObservationRef: "obs"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(Explain(p), "secret-") {
		t.Fatalf("payment secret leaked: %q", Explain(p))
	}
}

func TestTodo_SAFETY_002_Conformance(t *testing.T) {
	store := NewMemoryStore()
	filing := safetyFiling(t, 1, "", FilingSubmitted, "")
	if err := store.SaveFiling(filing); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetFiling("filing", 1); !ok {
		t.Fatal("filing history missing")
	}
	payment, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "payment", CaseRef: "case", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 2500, Currency: "USD", Status: PaymentSettled, ObservationRef: "bank-observation"})
	if err != nil || store.SaveWorkersCompPayment(payment) != nil {
		t.Fatalf("save payment: %v", err)
	}
	clearance, err := NewRestrictionClearanceRevision(RestrictionClearanceRevision{ID: "clearance", CaseRef: "case", CompartmentRef: "medical", Revision: 1, RestrictionRef: "restriction", WorkerRef: "worker", EvidenceRef: "medical-note", EvidenceDigest: "sha256:medical-note", AuthorityRef: "licensed-clinician", ObservationRef: "medical-observation"})
	if err != nil || store.SaveRestrictionClearance(clearance) != nil {
		t.Fatalf("save clearance: %v", err)
	}
	reconciliation, err := NewSafetyReconciliationRevision(SafetyReconciliationRevision{ID: "reconciliation", CaseRef: "case", CompartmentRef: "regulatory", Revision: 1, IncidentRef: "incident", SourceRevisionDigest: filing.CanonicalDigest, ObservationRef: "regulator-observation", PriorObservedAt: values.NewInstant(safetyInstant().Time().Add(-time.Minute)), ObservedAt: safetyInstant(), RepairRef: "repair", Status: ReconciliationClosed})
	if err != nil || store.SaveSafetyReconciliation(reconciliation) != nil {
		t.Fatalf("save reconciliation: %v", err)
	}
	correction, err := NewCorrectionRevision(SafetyCorrectionRevision{ID: "correction", CaseRef: "case", CompartmentRef: "regulatory", Revision: 1, IncidentRef: "incident", SourceRevisionDigest: filing.CanonicalDigest, Reason: "late source correction", EvidenceRef: "evidence", AmendedFilingRef: filing.CanonicalDigest})
	if err != nil || store.SaveSafetyCorrection(correction) != nil {
		t.Fatalf("save correction: %v", err)
	}
	if got, ok := store.GetWorkersCompPayment(payment.ID, 1); !ok || got.CanonicalDigest != payment.CanonicalDigest {
		t.Fatal("payment history missing")
	}
	if got, ok := store.GetRestrictionClearance(clearance.ID, 1); !ok || got.CanonicalDigest != clearance.CanonicalDigest {
		t.Fatal("clearance history missing")
	}
	if got, ok := store.GetSafetyReconciliation(reconciliation.ID, 1); !ok || got.CanonicalDigest != reconciliation.CanonicalDigest {
		t.Fatal("reconciliation history missing")
	}
	if got, ok := store.GetSafetyCorrection(correction.ID, 1); !ok || got.CanonicalDigest != correction.CanonicalDigest {
		t.Fatal("correction history missing")
	}
}

func TestTodo_SAFETY_002_Mutation(t *testing.T) {
	p, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "p", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 100, Currency: "USD", Status: PaymentObserved, ObservationRef: "obs"})
	if err != nil {
		t.Fatal(err)
	}
	p.AmountMinor = 101
	if err := p.Validate(); err == nil {
		t.Fatal("mutated amount accepted")
	}
}

func TestTodo_SAFETY_002_AdversarialAuthorityMoneyAndLineage(t *testing.T) {
	filing := safetyFiling(t, 1, "", FilingRejected, "provider-rejection")
	retryAt := values.NewInstant(filing.ObservedAt.Time().Add(time.Minute))
	if _, err := ResubmitFiling(filing, "submission-2", filing.SignatureRef, "retry-observation", retryAt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("replayed signature accepted: %v", err)
	}
	resubmitted, err := ResubmitFiling(filing, "submission-2", "signature-2", "retry-observation", retryAt)
	if err != nil || resubmitted.ParentDigest != filing.CanonicalDigest || resubmitted.SignerRef != filing.SignerRef {
		t.Fatalf("signed resubmission provenance = %+v, %v", resubmitted, err)
	}
	for _, tc := range []WorkersCompPaymentRevision{
		{ID: "negative", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: -1, Currency: "USD", Status: PaymentObserved, ObservationRef: "obs"},
		{ID: "fractional-code", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 1, Currency: "usd", Status: PaymentObserved, ObservationRef: "obs"},
		{ID: "reversal", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 1, Currency: "USD", Status: PaymentReversed, ObservationRef: "obs"},
	} {
		if _, err := NewWorkersCompPaymentRevision(tc); err == nil {
			t.Fatalf("unsafe money fact accepted: %+v", tc)
		}
	}
	if _, err := NewRestrictionClearanceRevision(RestrictionClearanceRevision{ID: "clear", CaseRef: "c", CompartmentRef: "medical", Revision: 1, RestrictionRef: "restriction", WorkerRef: "worker", EvidenceRef: "note", EvidenceDigest: "sha256:note"}); err == nil {
		t.Fatal("clearance without explicit authority accepted")
	}
	if _, err := NewSafetyReconciliationRevision(SafetyReconciliationRevision{ID: "recon", CaseRef: "c", CompartmentRef: "regulatory", Revision: 1, IncidentRef: "incident", SourceRevisionDigest: filing.CanonicalDigest, ObservationRef: "same-observation", PriorObservedAt: safetyInstant(), ObservedAt: safetyInstant(), RepairRef: "repair", Status: ReconciliationClosed}); !errors.Is(err, ErrFreshObservationRequired) {
		t.Fatalf("stale observation closed reconciliation: %v", err)
	}
	store := NewMemoryStore()
	if err := store.SaveFiling(resubmitted); !errors.Is(err, ErrRefused) {
		t.Fatalf("orphan successor accepted: %v", err)
	}
	if err := store.SaveFiling(filing); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFiling(resubmitted); err != nil {
		t.Fatal(err)
	}
	resubmitted.ParentDigest = strings.Repeat("0", len(resubmitted.ParentDigest))
	resubmitted.CanonicalDigest = ""
	if err := store.SaveFiling(resubmitted); err == nil {
		t.Fatal("forged predecessor digest accepted")
	}
	settled, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "payment", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 500, Currency: "USD", Status: PaymentSettled, ObservationRef: "bank-settlement"})
	if err != nil || store.SaveWorkersCompPayment(settled) != nil {
		t.Fatalf("save settlement: %v", err)
	}
	reversal, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "payment", CaseRef: "c", CompartmentRef: "claims", Revision: 2, ParentRevision: 1, ParentDigest: settled.CanonicalDigest, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 500, Currency: "USD", Status: PaymentReversed, ObservationRef: "bank-reversal", ReversalRef: "reversal-key"})
	if err != nil || store.SaveWorkersCompPayment(reversal) != nil {
		t.Fatalf("save reversal: %v", err)
	}
	if err := store.SaveWorkersCompPayment(reversal); err != nil {
		t.Fatalf("exact reversal retry must be idempotent: %v", err)
	}
	otherSettlement, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "other-payment", CaseRef: "c", CompartmentRef: "claims", Revision: 1, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 500, Currency: "USD", Status: PaymentSettled, ObservationRef: "other-settlement"})
	if err != nil || store.SaveWorkersCompPayment(otherSettlement) != nil {
		t.Fatalf("save other settlement: %v", err)
	}
	forged, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "other-payment", CaseRef: "c", CompartmentRef: "claims", Revision: 2, ParentRevision: 1, ParentDigest: otherSettlement.CanonicalDigest, ClaimRef: "claim", WorkerRef: "worker", AmountMinor: 500, Currency: "USD", Status: PaymentReversed, ObservationRef: "other-observation", ReversalRef: "reversal-key"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWorkersCompPayment(forged); !errors.Is(err, ErrRefused) {
		t.Fatalf("replayed reversal reference accepted: %v", err)
	}
}

func TestResubmitFilingRequiresFreshObservedRejection(t *testing.T) {
	rejected := safetyFiling(t, 1, "", FilingRejected, "provider-rejection")
	freshAt := values.NewInstant(rejected.ObservedAt.Time().Add(time.Second))
	for name, prior := range map[string]FilingRevision{
		"accepted": safetyFiling(t, 1, "", FilingAccepted, "provider-acceptance"),
		"timeout":  safetyFiling(t, 1, "", FilingSubmitted, ""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResubmitFiling(prior, "submission-2", "signature-2", "retry-authority", freshAt); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("%s authorized duplicate submission: %v", name, err)
			}
		})
	}
	if _, err := ResubmitFiling(rejected, "submission-2", "signature-2", rejected.ObservationRef, freshAt); !errors.Is(err, ErrFreshObservationRequired) {
		t.Fatalf("reused observation authorized retry: %v", err)
	}
	if _, err := ResubmitFiling(rejected, "submission-2", "signature-2", "retry-authority", rejected.ObservedAt); !errors.Is(err, ErrFreshObservationRequired) {
		t.Fatalf("stale observation time authorized retry: %v", err)
	}
}
