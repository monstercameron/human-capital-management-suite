package program

import (
	"strings"
	"testing"
	"time"
)

func reconFixture() ([]Expectation, []Observation) {
	expected := []Expectation{
		{EnrollmentID: "enr-1", Participant: "worker-7", ProgramID: "bonus-fy26",
			RevisionDigest: "rev-digest-1", ExpectedState: string(EnrollmentEnrolled),
			ExpectedOutcomeDigest: "outcome-digest-1"},
		{EnrollmentID: "enr-2", Participant: "worker-8", ProgramID: "bonus-fy26",
			RevisionDigest: "rev-digest-1", ExpectedState: string(EnrollmentEnrolled),
			ExpectedOutcomeDigest: "outcome-digest-2"},
	}
	observed := []Observation{
		{EnrollmentID: "enr-1", Source: "payroll", ObservedState: string(EnrollmentEnrolled),
			ObservedOutcomeDigest: "outcome-digest-1",
			At:                    time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
		{EnrollmentID: "enr-2", Source: "payroll", ObservedState: string(EnrollmentWithdrawn),
			ObservedOutcomeDigest: "outcome-digest-2",
			At:                    time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
		{EnrollmentID: "enr-9", Source: "payroll", ObservedState: string(EnrollmentEnrolled),
			ObservedOutcomeDigest: "outcome-digest-9",
			At:                    time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
	}
	return expected, observed
}

func TestTodo_PROGRAM_006(t *testing.T) {
	c := testCatalog(t)
	expected, observed := reconFixture()
	discrepancies, repairs, err := c.Reconcile(expected, observed)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	// enr-2 conflicts (enrolled vs withdrawn); enr-9 is extra.
	if len(discrepancies) != 2 {
		t.Fatalf("discrepancies = %v", discrepancies)
	}
	if len(repairs) != 2 {
		t.Fatalf("repairs = %v", repairs)
	}
	for _, r := range repairs {
		if strings.TrimSpace(r.Action) == "" || strings.TrimSpace(r.Owner) == "" {
			t.Fatalf("repair plan without action/owner: %+v", r)
		}
	}
	// Sealing while discrepancies stand returns PROGRAM_006_REJECTED with
	// zero effects.
	journalBefore := len(c.Journal())
	_, err = c.SealReconciliation(expected, observed)
	if err == nil {
		t.Fatal("dirty reconciliation sealed")
	}
	rej, ok := AsRejection(err)
	if !ok || rej.Code != CodeRejected006 {
		t.Fatalf("seal error = %v, want %s", err, CodeRejected006)
	}
	if strings.TrimSpace(rej.Field) == "" || strings.TrimSpace(rej.State) == "" {
		t.Fatalf("rejection omits field/state: %+v", rej)
	}
	if len(c.Journal()) != journalBefore {
		t.Fatal("refused seal left journal effects")
	}
}

func TestTodo_PROGRAM_006_Property(t *testing.T) {
	c := testCatalog(t)
	// Clean reconciliation seals with a stable digest.
	expected := []Expectation{
		{EnrollmentID: "enr-1", Participant: "worker-7", ProgramID: "bonus-fy26",
			RevisionDigest: "rev-1", ExpectedState: string(EnrollmentEnrolled),
			ExpectedOutcomeDigest: "outcome-1"},
	}
	observed := []Observation{
		{EnrollmentID: "enr-1", Source: "payroll", ObservedState: string(EnrollmentEnrolled),
			ObservedOutcomeDigest: "outcome-1",
			At:                    time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
	}
	first, err := c.SealReconciliation(expected, observed)
	if err != nil {
		t.Fatalf("clean seal: %v", err)
	}
	second, err := c.SealReconciliation(expected, observed)
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	if first != second || first == "" {
		t.Fatal("seal digest unstable or empty")
	}
	// Missing expectation coverage is explicit, never silent.
	discrepancies, _, err := c.Reconcile(nil, observed)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(discrepancies) != 1 || discrepancies[0].Kind != DiscrepancyExtra {
		t.Fatalf("uncovered observation misclassified: %v", discrepancies)
	}
}

func FuzzTodo_PROGRAM_006(f *testing.F) {
	f.Add([]byte("enr-1"), []byte("ENROLLED"), []byte("payroll"))
	f.Fuzz(func(t *testing.T, id, state, source []byte) {
		c := NewCatalog()
		// Must never panic; garbage states classify as conflicting,
		// never as silently reconciled.
		discrepancies, _, err := c.Reconcile(
			[]Expectation{{EnrollmentID: "enr-1", ExpectedState: string(EnrollmentEnrolled)}},
			[]Observation{{EnrollmentID: string(id), Source: string(source),
				ObservedState: string(state),
				At:            time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}},
		)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		_ = discrepancies
		if _, err := c.SealReconciliation(
			[]Expectation{{EnrollmentID: "enr-1", ExpectedState: string(EnrollmentEnrolled)}},
			[]Observation{{EnrollmentID: string(id), Source: string(source),
				ObservedState: string(state),
				At:            time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}},
		); err == nil && (len(id) == 0 || string(state) != string(EnrollmentEnrolled)) {
			t.Fatal("garbage observation sealed reconciled")
		}
	})
}

func TestTodo_PROGRAM_006_Security(t *testing.T) {
	c := testCatalog(t)
	// Observations from an unauthorized source are refused, not classified.
	_, _, err := c.Reconcile(
		[]Expectation{{EnrollmentID: "enr-1", ExpectedState: string(EnrollmentEnrolled)}},
		[]Observation{{EnrollmentID: "enr-1", Source: "",
			ObservedState: string(EnrollmentEnrolled),
			At:            time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}},
	)
	if err == nil {
		t.Fatal("sourceless observation classified")
	}
}

func TestTodo_PROGRAM_006_Mutation(t *testing.T) {
	c := testCatalog(t)
	// Mutant: a stale outcome digest reported reconciled must be killed
	// with PROGRAM_006_REJECTED.
	expected := []Expectation{
		{EnrollmentID: "enr-1", Participant: "worker-7", ProgramID: "bonus-fy26",
			RevisionDigest: "rev-1", ExpectedState: string(EnrollmentEnrolled),
			ExpectedOutcomeDigest: "outcome-current"},
	}
	observed := []Observation{
		{EnrollmentID: "enr-1", Source: "payroll", ObservedState: string(EnrollmentEnrolled),
			ObservedOutcomeDigest: "outcome-stale",
			At:                    time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
	}
	_, err := c.SealReconciliation(expected, observed)
	if err == nil {
		t.Fatal("stale-outcome mutant survived")
	}
	rej, ok := AsRejection(err)
	if !ok || rej.Code != CodeRejected006 {
		t.Fatalf("stale seal error = %v, want %s", err, CodeRejected006)
	}
	// Mutant: missing observation (expected enrollment never seen) must
	// also refuse the seal.
	_, err = c.SealReconciliation(expected, nil)
	if err == nil {
		t.Fatal("missing-observation mutant survived")
	}
}
