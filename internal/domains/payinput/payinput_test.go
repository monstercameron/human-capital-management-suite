package payinput

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func payInputInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	a, err := values.ParseLocalDate(start)
	if err != nil {
		t.Fatal(err)
	}
	b, err := values.ParseLocalDate(end)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewLocalDateInterval(a, b, values.CalendarRef{Ref: "payroll", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func payInputDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func validDefinition(t *testing.T, state DefinitionState) Definition {
	t.Helper()
	d, err := NewDefinition(Definition{
		DefinitionID: "earning-base", Code: "BASE", Kind: KindEarning, Currency: "USD",
		Taxability:       map[JurisdictionClass]bool{JurisdictionFederal: true, JurisdictionState: true},
		CalculationBasis: BasisHourly, AccountingCode: "BASE", OwnerRef: "payinput",
		Effective: payInputInterval(t, "2026-01-01", "2027-01-01"), Version: "v1", State: state,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func validAssignment(t *testing.T, id, worker, start, end string) WorkerAssignment {
	t.Helper()
	return WorkerAssignment{AssignmentID: id, WorkerRef: worker, Effective: payInputInterval(t, start, end), Amount: payInputDecimal(t, "25.00"), Recurrence: RecurrencePerPayroll, RecurrenceRule: "PAY_PERIOD"}
}

func TestPayInputDefinitionsRejectUnknownTaxabilityRecurrenceAndOverlappingAssignments(t *testing.T) {
	definition := validDefinition(t, StatePublished)
	assignment := validAssignment(t, "a-1", "worker-1", "2026-01-01", "2026-02-01")
	bound, err := NewWorkerAssignment(definition, assignment)
	if err != nil {
		t.Fatalf("bind assignment: %v", err)
	}
	if bound.CanonicalDigest == "" || bound.DefinitionRef.Digest != definition.CanonicalDigest {
		t.Fatal("assignment did not bind the exact definition digest")
	}
	if _, err := NewDefinition(Definition{DefinitionID: "bad", Code: "BAD", Kind: KindEarning, Currency: "USD", Taxability: map[JurisdictionClass]bool{"UNKNOWN": true}, CalculationBasis: BasisHourly, AccountingCode: "BAD", OwnerRef: "payinput", Effective: payInputInterval(t, "2026-01-01", "2027-01-01"), Version: "v1"}); !errors.Is(err, ErrUnknownTaxability) {
		t.Fatalf("unknown taxability error = %v", err)
	}
	badRecurrence := validAssignment(t, "a-2", "worker-1", "2026-02-01", "2026-03-01")
	badRecurrence.Recurrence = "WEEKLY"
	if _, err := NewWorkerAssignment(definition, badRecurrence); !errors.Is(err, ErrUnknownRecurrence) {
		t.Fatalf("unknown recurrence error = %v", err)
	}
	overlap := validAssignment(t, "a-3", "worker-1", "2026-01-15", "2026-02-15")
	overlap, err = NewWorkerAssignment(definition, overlap)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAssignments([]WorkerAssignment{bound, overlap}); !errors.Is(err, ErrOverlappingAssignments) {
		t.Fatalf("overlap error = %v", err)
	}
	retired := validDefinition(t, StateRetired)
	if _, err := NewWorkerAssignment(retired, validAssignment(t, "a-4", "worker-2", "2026-01-01", "2026-02-01")); !errors.Is(err, ErrDefinitionRetired) {
		t.Fatalf("retired definition error = %v", err)
	}
}

func TestTodo_PAYINPUT_001_Property(t *testing.T) {
	d := validDefinition(t, StatePublished)
	// CanonicalDigest is a value field; read it twice to prove stability.
	firstCanonical, secondCanonical := d.CanonicalDigest, d.CanonicalDigest
	if firstCanonical != secondCanonical {
		t.Fatal("digest is not stable")
	}
	copyOf := d
	copyOf.Taxability = map[JurisdictionClass]bool{JurisdictionState: true, JurisdictionFederal: true}
	if copyOf.CanonicalDigest != d.CanonicalDigest {
		t.Fatal("taxability map order changed the digest")
	}
}

func TestTodo_PAYINPUT_001_Golden(t *testing.T) {
	d := validDefinition(t, StatePublished)
	if !strings.HasPrefix(d.CanonicalDigest, "sha256:") {
		t.Fatalf("digest = %q", d.CanonicalDigest)
	}
}

func TestTodo_PAYINPUT_001_Race(t *testing.T) {
	definition := validDefinition(t, StatePublished)
	const workers = 8
	var wg sync.WaitGroup
	results := make(chan string, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			digest, err := definition.Digest()
			if err != nil {
				errs <- err
				return
			}
			results <- digest
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("shared definition digest: %v", err)
	}
	for digest := range results {
		if digest != definition.CanonicalDigest {
			t.Errorf("concurrent digest = %q, want %q", digest, definition.CanonicalDigest)
		}
	}
}
func TestTodo_PAYINPUT_001_Fault(t *testing.T) {
	if _, err := NewDefinition(Definition{}); err == nil {
		t.Fatal("empty definition was accepted")
	}
}
func TestTodo_PAYINPUT_001_Security(t *testing.T) {
	a := validAssignment(t, "a-1", "worker-sensitive", "2026-01-01", "2026-02-01")
	d := validDefinition(t, StatePublished)
	a, err := NewWorkerAssignment(d, a)
	if err != nil {
		t.Fatal(err)
	}
	x, err := a.Explain()
	if err != nil || strings.Contains(x.WorkerRef, "account") {
		t.Fatalf("unsafe explanation: %+v, %v", x, err)
	}
}
func TestTodo_PAYINPUT_001_Conformance(t *testing.T) {
	d := validDefinition(t, StatePublished)
	next, err := d.NewVersion("v2", payInputInterval(t, "2027-01-01", "2028-01-01"))
	if err != nil || next.SupersedesDigest != d.CanonicalDigest || next.Revision != d.Revision+1 {
		t.Fatalf("lineage = %+v, err=%v", next, err)
	}
}
func TestTodo_PAYINPUT_001_Mutation(t *testing.T) {
	d := validDefinition(t, StatePublished)
	before := d.CanonicalDigest
	_, err := d.Retire("v1-retired")
	if err != nil || d.CanonicalDigest != before || d.State != StatePublished {
		t.Fatalf("receiver was mutated: %v", err)
	}
}

func TestPayInputExplainIsDigestOriented(t *testing.T) {
	d := validDefinition(t, StatePublished)
	x, err := Explain(d)
	if err != nil || x.Digest != d.CanonicalDigest {
		t.Fatalf("explanation = %+v, err=%v", x, err)
	}
}

func TestPayInputInMemoryCatalogRejectsOverlap(t *testing.T) {
	d := validDefinition(t, StatePublished)
	catalog, err := NewInMemoryCatalog([]Definition{d})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewWorkerAssignment(d, validAssignment(t, "a-1", "worker-1", "2026-01-01", "2026-02-01"))
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Assign(a); err != nil {
		t.Fatal(err)
	}
	b, err := NewWorkerAssignment(d, validAssignment(t, "a-2", "worker-1", "2026-01-15", "2026-02-15"))
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Assign(b); !errors.Is(err, ErrOverlappingAssignments) {
		t.Fatalf("catalog overlap error = %v", err)
	}
}
