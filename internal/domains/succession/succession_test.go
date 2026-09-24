package succession

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func successionInstant() values.Instant {
	return values.NewInstant(time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC))
}

func successionRole(t *testing.T) CriticalRole {
	t.Helper()
	r, err := NewCriticalRole(CriticalRole{RoleID: "role-chief", Revision: 1, PositionRef: "position-1", JobRevisionRef: "job-7", OwnerRef: "owner-1", AuthorityRef: "authority-1", EffectiveAt: successionInstant(), KnownAt: successionInstant(), EvidenceRefs: []string{"evidence-role-1"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func successionAssessment(t *testing.T, id string) SuccessorReadinessRevision {
	t.Helper()
	a, err := NewSuccessorReadinessRevision(SuccessorReadinessRevision{SuccessorID: id, Revision: 1, Readiness: ReadinessReadyNow, VacancyRisk: VacancyRiskMedium, AssessedBy: "assessor-1", AssessmentSource: "assessment-1", EvidenceRefs: []string{"evidence-" + id}, EffectiveAt: successionInstant(), KnownAt: successionInstant()})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func successionSlate(t *testing.T, visibility DisclosureScope, scopes []string, authorized bool) SuccessionSlate {
	t.Helper()
	r := successionRole(t)
	s, err := NewSuccessionSlate(SuccessionSlate{SlateID: "slate-1", Revision: 1, CriticalRoleID: r.RoleID, CriticalRoleRevision: r.Revision, CriticalRoleDigest: r.CanonicalDigest, PositionRef: r.PositionRef, JobRevisionRef: r.JobRevisionRef, NominatorID: "manager-1", NominatorRole: "ROLE_MANAGER", NominatorAuthorizationRef: "nomination-authority-1", NominatorAuthorized: authorized, Visibility: visibility, DeclaredScopes: scopes, Candidates: []SuccessorReadinessRevision{successionAssessment(t, "worker-1")}, EffectiveAt: successionInstant(), KnownAt: successionInstant()})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSuccessionSlateRequiresCriticalRoleAuthorizedNominationAndDatedReadiness(t *testing.T) {
	s := successionSlate(t, DisclosureScoped, []string{"talent.read"}, true)
	if _, err := s.CandidatesFor("talent.read"); err != nil {
		t.Fatal(err)
	}
	unauthorized := s
	unauthorized.NominatorAuthorized = false
	if err := unauthorized.Validate(); err != ErrUnauthorizedNomination {
		t.Fatal("unauthorized nomination accepted")
	}
	if _, err := s.CandidatesFor("other.scope"); err != ErrSlateMembershipWithheld {
		t.Fatalf("CandidatesFor error = %v, want withheld", err)
	}
}

func TestTodo_SUCCESSION_001_Property(t *testing.T) {
	role := successionRole(t)
	if role.CanonicalDigest == "" || role.Canonical() == nil {
		t.Fatal("role is not digested")
	}
	if role.PositionRef == "" || role.JobRevisionRef == "" {
		t.Fatal("role binding is incomplete")
	}
}

func TestTodo_SUCCESSION_001_Golden(t *testing.T) {
	a := successionAssessment(t, "worker-1")
	if !strings.Contains(string(a.Canonical()), "READY_NOW") {
		t.Fatal("canonical readiness band missing")
	}
}

func TestTodo_SUCCESSION_001_Race(t *testing.T) {
	store := NewMemorySlateStore()
	slate := successionSlate(t, DisclosureScoped, []string{"talent.read"}, true)
	const workers = 12
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- store.Save(slate) }()
	}
	wg.Wait()
	close(results)
	saved, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			saved++
		case err == ErrConflictingCurrentRevision:
			conflicts++
		default:
			t.Fatalf("concurrent Save error=%v", err)
		}
	}
	if saved != 1 || conflicts != workers-1 {
		t.Fatalf("concurrent saves: saved=%d conflicts=%d", saved, conflicts)
	}
	current, err := store.Current(slate.CriticalRoleID)
	if err != nil || current.CanonicalDigest != slate.CanonicalDigest {
		t.Fatalf("current slate=%+v err=%v", current, err)
	}
}

func TestTodo_SUCCESSION_001_Fault(t *testing.T) {
	r := successionRole(t)
	r.PositionRef = ""
	if err := r.Validate(); !strings.Contains(err.Error(), "position_ref") {
		t.Fatalf("Validate() = %v, want position_ref", err)
	}
}

func TestTodo_SUCCESSION_001_Security(t *testing.T) {
	s := successionSlate(t, DisclosureWithheld, nil, true)
	if _, err := s.CandidatesFor("talent.read"); err != ErrSlateMembershipWithheld {
		t.Fatalf("withheld slate leaked: %v", err)
	}
	e, err := s.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.Digest, "worker-1") {
		t.Fatal("digest explanation contains candidate id")
	}
}

func TestTodo_SUCCESSION_001_Conformance(t *testing.T) {
	role := successionRole(t)
	next, err := role.Revise(CriticalRole{PositionRef: role.PositionRef, JobRevisionRef: "job-8", OwnerRef: role.OwnerRef, AuthorityRef: role.AuthorityRef, EffectiveAt: role.EffectiveAt, KnownAt: role.KnownAt, EvidenceRefs: []string{"evidence-role-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if next.ParentDigest != role.CanonicalDigest || role.Revision != 1 {
		t.Fatal("role lineage was not preserved")
	}
}

func TestTodo_SUCCESSION_001_Mutation(t *testing.T) {
	s := successionSlate(t, DisclosureScoped, []string{"talent.read"}, true)
	if _, err := s.Revise([]SuccessorReadinessRevision{successionAssessment(t, "worker-2")}, "manager-2", "authority-2"); err != nil {
		t.Fatal(err)
	}
	if len(s.Candidates) != 1 || s.Candidates[0].SuccessorID != "worker-1" {
		t.Fatal("parent slate was mutated")
	}
}
