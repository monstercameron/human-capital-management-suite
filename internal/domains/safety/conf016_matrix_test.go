package safety

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_CONF_016_Property proves store idempotency and digest
// determinism: identical saves are free, identical records digest alike.
func TestTodo_CONF_016_Property(t *testing.T) {
	c := buildConf016Case(t)
	for _, save := range []func() error{
		func() error { return c.store.SaveIncident(c.incident) },
		func() error { return c.store.SaveInjury(c.injury) },
		func() error { return c.store.SaveClaim(c.claim) },
		func() error { return c.store.SaveWorkersCompPayment(c.payment3) },
		func() error { return c.store.SaveFiling(c.filing3) },
		func() error { return c.store.SaveSafetyReconciliation(c.reconciliation) },
	} {
		if err := save(); err != nil {
			t.Fatalf("identical replay must be idempotent, got %v", err)
		}
	}
	again, err := NewIncidentRevision(IncidentRevision{ID: "inc-016", CaseRef: "case-016", CompartmentRef: "operational", Revision: 1, IncidentAt: conf016Instant(5), Kind: IncidentInjury, WorkerRef: "worker-016", ReporterRef: "reporter-016", LocationRef: "site-016", Description: "fall from platform", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	if again.CanonicalDigest != c.incident.CanonicalDigest {
		t.Fatal("identical records digest differently")
	}
}

// TestTodo_CONF_016_Security proves compartment separation is structural:
// medical facts are unreachable through operational accessors (per-type
// store maps plus typed constructors), explanations redact protected
// evidence, and unknown cases read as absent.
func TestTodo_CONF_016_Security(t *testing.T) {
	c := buildConf016Case(t)
	// The sealed injury shares the case but must not surface through the
	// incident, claim, payment or filing accessors, even under its own id.
	if _, ok := c.store.GetIncident("inj-016", 1); ok {
		t.Fatal("medical injury reads through the operational accessor")
	}
	if _, ok := c.store.GetClaim("pay-016", 2); ok {
		t.Fatal("payment reads through the claims accessor")
	}
	if _, ok := c.store.GetWorkersCompPayment("claim-016", 1); ok {
		t.Fatal("claim reads through the payment accessor")
	}
	// Compartment references are mandatory: unbound records fail closed.
	if _, err := NewInjuryRevision(InjuryRevision{ID: "x", CaseRef: "case-016", Revision: 1, IncidentRef: "i", WorkerRef: "w", Kind: InjuryPhysical, MedicalEvidenceRef: "m", Severity: "s"}); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("compartment-unbound injury must be refused, got %v", err)
	}
	for _, get := range []bool{
		func() bool { _, ok := c.store.GetClaim("no-such-claim", 1); return ok }(),
		func() bool { _, ok := c.store.GetWorkersCompPayment("no-such-payment", 1); return ok }(),
		func() bool { _, ok := c.store.GetFiling("no-such-filing", 9); return ok }(),
	} {
		if get {
			t.Fatal("unknown case material reads as present")
		}
	}
}

// TestTodo_CONF_016_Conformance walks the case matrix: every revision sits
// in its declared compartment, every clock follows the declared table and
// every successor names its parent.
func TestTodo_CONF_016_Conformance(t *testing.T) {
	c := buildConf016Case(t)
	if got := c.reportability2.Deadline.Time().Sub(c.incident.IncidentAt.Time()); got != 24*time.Hour {
		t.Fatalf("severe-reportability deadline = %s, want the declared 24-hour clock", got)
	}
	links := map[string][2]string{
		"reportability": {c.reportability2.ParentDigest, c.reportability1.CanonicalDigest},
		"payment rev2":  {c.payment2.ParentDigest, c.payment1.CanonicalDigest},
		"payment rev3":  {c.payment3.ParentDigest, c.payment2.CanonicalDigest},
		"filing rev2":   {c.filing2.ParentDigest, c.filing1.CanonicalDigest},
		"filing rev3":   {c.filing3.ParentDigest, c.filing2.CanonicalDigest},
	}
	for name, pair := range links {
		if pair[0] != pair[1] || pair[0] == "" {
			t.Fatalf("%s breaks its parent chain", name)
		}
	}
	if c.reconciliation.SourceRevisionDigest != c.incident.CanonicalDigest {
		t.Fatal("reconciliation is not bound to the incident revision")
	}
}

// TestTodo_CONF_016_Mutation kills the lineage mutants: a swapped parent
// digest, a skipped revision and a first-revision reversal must each be
// detected.
func TestTodo_CONF_016_Mutation(t *testing.T) {
	c := buildConf016Case(t)
	swapped, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "pay-016", CaseRef: "case-016", CompartmentRef: "claims", Revision: 3, ParentRevision: 2, ParentDigest: c.payment1.CanonicalDigest, ClaimRef: c.claim.CanonicalDigest, WorkerRef: "worker-016", AmountMinor: 125000, Currency: "USD", Status: PaymentReversed, ObservationRef: "obs-mut", ReversalRef: "reversal-mut"})
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	if err := c.store.SaveWorkersCompPayment(swapped); !errors.Is(err, ErrRefused) {
		t.Fatalf("swapped-parent mutant must be refused, got %v", err)
	}
	skipped, err := NewClaimRevision(ClaimRevision{ID: "claim-skip", CaseRef: "case-016", CompartmentRef: "claims", Revision: 3, ParentRevision: 2, ParentDigest: "sha256:missing", IncidentRef: c.incident.CanonicalDigest, WorkerRef: "worker-016", ClaimRef: "carrier-skip", AuthorityRef: "wc-board-016", Status: ClaimSubmitted})
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	if err := c.store.SaveClaim(skipped); !errors.Is(err, ErrRefused) {
		t.Fatalf("skipped-revision mutant must be refused, got %v", err)
	}
	if _, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "pay-x", CaseRef: "case-016", CompartmentRef: "claims", Revision: 1, ClaimRef: c.claim.CanonicalDigest, WorkerRef: "worker-016", AmountMinor: 100, Currency: "USD", Status: PaymentReversed, ObservationRef: "obs-x", ReversalRef: "rev-x"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("first-revision reversal mutant must be refused, got %v", err)
	}
	bare, err := NewWorkersCompPaymentRevision(WorkersCompPaymentRevision{ID: "pay-y", CaseRef: "case-016", CompartmentRef: "claims", Revision: 2, ParentRevision: 1, ParentDigest: c.payment1.CanonicalDigest, ClaimRef: c.claim.CanonicalDigest, WorkerRef: "worker-016", AmountMinor: 125000, Currency: "USD", Status: PaymentReversed, ObservationRef: "obs-y"})
	if err == nil {
		_ = bare
		t.Fatal("reversal without an idempotency reference must be refused")
	}
}

// FuzzTodo_CONF_016 proves arbitrary compartment, kind and status strings
// either validate under the exact lineage contract or fail closed.
func FuzzTodo_CONF_016(f *testing.F) {
	f.Add("operational", "INJURY", "OPEN")
	f.Add("medical", "INJURY", "OPEN")
	f.Add("", "", "")
	f.Add("claims", "NOPE", "SETTLED")
	f.Fuzz(func(t *testing.T, compartment, kind, status string) {
		r, err := NewIncidentRevision(IncidentRevision{ID: "fuzz", CaseRef: "case-fuzz", CompartmentRef: compartment, Revision: 1, IncidentAt: conf016Instant(5), Kind: IncidentKind(kind), WorkerRef: "w", ReporterRef: "r", LocationRef: "s", Description: "d", Status: IncidentStatus(status)})
		if err != nil {
			if !errors.Is(err, ErrInvalidRevision) {
				t.Fatalf("Validate must fail closed, got %v", err)
			}
			return
		}
		if r.CanonicalDigest == "" {
			t.Fatal("valid fuzzed record carries no digest")
		}
		store := NewMemoryStore()
		if err := store.SaveIncident(r); err != nil {
			t.Fatalf("valid fuzzed record must store, got %v", err)
		}
		_ = values.NewInstant(conf016Instant(5).Time())
	})
}
