package program

import (
	"strings"
	"testing"
	"time"
)

func mustEnroll(t *testing.T, c *Catalog, programID string) Enrollment {
	t.Helper()
	e, err := c.CreateEnrollment(testCaller, Enrollment{
		ID: "enr-1", ProgramID: programID, Participant: "worker-7",
		Tenant: "tenant-acme", EligibilityRef: "elig-result-9",
		Elections: []string{"full"},
		StartsAt:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Reason:    "new-hire",
		Effects:   []string{"payroll-flag"},
	})
	if err != nil {
		t.Fatalf("CreateEnrollment: %v", err)
	}
	return e
}

func TestTodo_PROGRAM_004(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	e := mustEnroll(t, c, def.ID)
	if e.State != EnrollmentProposed || e.Version != 1 {
		t.Fatalf("new enrollment state=%q version=%d", e.State, e.Version)
	}
	enrolled, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentEnrolled,
		Reason: "eligibility-confirmed",
		At:     time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("ApplyTransition: %v", err)
	}
	if enrolled.State != EnrollmentEnrolled || enrolled.Version != 2 {
		t.Fatalf("enrolled state=%q version=%d", enrolled.State, enrolled.Version)
	}
	withdrawn, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentWithdrawn,
		Reason: "worker-request",
		At:     time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), ExpectedVersion: 2,
	})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if withdrawn.State != EnrollmentWithdrawn {
		t.Fatalf("withdrawn state=%q", withdrawn.State)
	}
	// The transaction journal records eligibility, elections, dates,
	// reason and downstream effects.
	found := false
	for _, entry := range c.Journal() {
		if strings.Contains(entry.Detail, "elig-result-9") && strings.Contains(entry.Detail, "payroll-flag") {
			found = true
		}
	}
	if !found {
		t.Fatal("journal omits eligibility/effects of the lifecycle transaction")
	}
}

func TestTodo_PROGRAM_004_Property(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	e := mustEnroll(t, c, def.ID)
	// Terminal WITHDRAWN admits no further transition.
	if _, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentEnrolled, Reason: "r",
		At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), ExpectedVersion: 1,
	}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	if _, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentWithdrawn, Reason: "r",
		At: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), ExpectedVersion: 2,
	}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentEnrolled, Reason: "rejoin",
		At: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ExpectedVersion: 3,
	}); err == nil {
		t.Fatal("transition out of terminal WITHDRAWN accepted")
	}
	// Duplicate transition (same target as current state) is refused.
	c2 := testCatalog(t)
	def2, err := c2.Define(testCaller, Definition{
		ID: "dup-prog", Name: "n", Type: ProgramBonus, Owner: "o",
		Scope: []string{"s"}, Funding: FundingEmployer, Outcomes: []string{"oc"},
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	e2 := mustEnroll(t, c2, def2.ID)
	if _, err := c2.ApplyTransition(testCaller, Transition{
		EnrollmentID: e2.ID, To: EnrollmentProposed, Reason: "noop",
		At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), ExpectedVersion: 1,
	}); err == nil {
		t.Fatal("duplicate no-op transition accepted")
	}
}

func FuzzTodo_PROGRAM_004(f *testing.F) {
	f.Add([]byte("enr-1"), []byte("ENROLLED"), uint64(1))
	f.Fuzz(func(t *testing.T, id, state []byte, version uint64) {
		tr := Transition{
			EnrollmentID: string(id), To: ParticipationState(state),
			Reason: "r", At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
			ExpectedVersion: version,
		}
		// Must never panic; unknown states must never validate.
		if err := ValidateTransition(tr); err == nil && !validParticipationState(ParticipationState(state)) {
			t.Fatal("unknown lifecycle state validated")
		}
	})
}

func TestTodo_PROGRAM_004_Security(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	e := mustEnroll(t, c, def.ID)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	_, err := c.ApplyTransition(rival, Transition{
		EnrollmentID: e.ID, To: EnrollmentEnrolled, Reason: "hijack",
		At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), ExpectedVersion: 1,
	})
	if err == nil {
		t.Fatal("cross-tenant transition accepted")
	}
	got, _ := c.LookupEnrollment(e.ID)
	if got.Version != 1 || got.State != EnrollmentProposed {
		t.Fatal("refused transition mutated enrollment")
	}
}

func TestTodo_PROGRAM_004_Mutation(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	e := mustEnroll(t, c, def.ID)
	// Mutant A: stale version must be killed.
	_, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentEnrolled, Reason: "r",
		At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), ExpectedVersion: 0,
	})
	if err == nil {
		t.Fatal("stale-version mutant survived")
	}
	// Mutant B: illegal jump PROPOSED->WITHDRAWN... is legal; use
	// ENROLLED->PROPOSED regression which must be killed.
	if _, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentEnrolled, Reason: "r",
		At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), ExpectedVersion: 1,
	}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, err := c.ApplyTransition(testCaller, Transition{
		EnrollmentID: e.ID, To: EnrollmentProposed, Reason: "regress",
		At: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), ExpectedVersion: 2,
	}); err == nil {
		t.Fatal("regression mutant survived")
	}
	// Mutant C: withdrawal against open obligations without a release
	// must be killed.
	c3 := testCatalog(t)
	def3, err := c3.Define(testCaller, Definition{
		ID: "oblig-prog", Name: "n", Type: ProgramBonus, Owner: "o",
		Scope: []string{"s"}, Funding: FundingEmployer, Outcomes: []string{"oc"},
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	e3, err := c3.CreateEnrollment(testCaller, Enrollment{
		ID: "enr-ob", ProgramID: def3.ID, Participant: "worker-9",
		Tenant: "tenant-acme", EligibilityRef: "elig-1",
		Elections: []string{"full"},
		StartsAt:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Reason:    "new-hire", Obligations: []string{"clawback-window"},
	})
	if err != nil {
		t.Fatalf("CreateEnrollment: %v", err)
	}
	if _, err := c3.ApplyTransition(testCaller, Transition{
		EnrollmentID: e3.ID, To: EnrollmentEnrolled, Reason: "r",
		At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), ExpectedVersion: 1,
	}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, err := c3.ApplyTransition(testCaller, Transition{
		EnrollmentID: e3.ID, To: EnrollmentWithdrawn, Reason: "quit",
		At: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), ExpectedVersion: 2,
	}); err == nil {
		t.Fatal("obligation-breaking withdrawal survived")
	}
}
