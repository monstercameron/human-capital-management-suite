package learning

import (
	"fmt"
	"testing"
	"time"
)

func assignableVersion(t *testing.T, r *Registry) CourseVersion {
	t.Helper()
	c := mustCourse(t, r)
	return mustVersion(t, r, c.ID)
}

func baseAssignment(v CourseVersion) Assignment {
	return Assignment{
		ID: "asg-1", LearnerID: "worker-7", CourseID: v.CourseID, Version: v.Version,
		Tenant: "tenant-acme", RequirementSource: "path-newhire-safety",
		DueAt: dueAt(), Capacity: 50,
		WindowStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}
}

func TestTodo_LEARN_003(t *testing.T) {
	r := NewRegistry()
	v := assignableVersion(t, r)
	a, err := r.Assign(testCaller, baseAssignment(v))
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if a.State != AssignmentAssigned {
		t.Fatalf("assignment state = %q", a.State)
	}
	enrolled, err := r.Enroll(testCaller, a.ID, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if enrolled.State != AssignmentEnrolled {
		t.Fatalf("enrolled state = %q", enrolled.State)
	}
	// GREEN: requirement source, due date, waiver/escalation and
	// participation state are recorded.
	if enrolled.RequirementSource != "path-newhire-safety" || enrolled.DueAt.IsZero() {
		t.Fatal("assignment omits requirement source or due date")
	}
	waived, err := r.Assign(testCaller, Assignment{
		ID: "asg-waive", LearnerID: "worker-8", CourseID: v.CourseID, Version: v.Version,
		Tenant: "tenant-acme", RequirementSource: "manager-request",
		DueAt: dueAt(), Capacity: 50, WaiverRef: "waiver-ada-4",
		WindowStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("waived Assign: %v", err)
	}
	if waived.WaiverRef != "waiver-ada-4" {
		t.Fatal("waiver reference not recorded")
	}
}

func BenchmarkTodo_LEARN_003(b *testing.B) {
	r := NewRegistry()
	c, err := r.DefineCourse(testCaller, Course{
		ID: "crs-bench", Title: "Bench", Provider: "acme-academy",
		Tenant:  "tenant-acme",
		Locales: []CourseLocale{{Language: "en", Region: "US", Formats: []string{"text"}}},
	})
	if err != nil {
		b.Fatalf("DefineCourse: %v", err)
	}
	v, err := r.DefineCourseVersion(testCaller, CourseVersion{
		CourseID: c.ID, Version: 1, Tenant: "tenant-acme",
		ContentRef: "bench-content", AssessmentRefs: []string{"q"},
		CompletionRule: "x", CredentialTemplate: "t", ExpiryPolicy: "e",
		ExternalAuthority: "a", Locale: "en-US", PassingScore: "80", MaxAttempts: 3,
	})
	if err != nil {
		b.Fatalf("DefineCourseVersion: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a, err := r.Assign(testCaller, Assignment{
			ID:        fmt.Sprintf("bench-asg-%d", i),
			LearnerID: fmt.Sprintf("worker-bench-%d", i),
			CourseID:  v.CourseID, Version: v.Version,
			Tenant: "tenant-acme", RequirementSource: "bench",
			DueAt: dueAt(), Capacity: 1000000,
			WindowStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		})
		_ = a
		if err != nil {
			b.Fatalf("Assign: %v", err)
		}
	}
}

func TestTodo_LEARN_003_Property(t *testing.T) {
	r := NewRegistry()
	v := assignableVersion(t, r)
	// Enrolling twice is idempotent, not a duplicate.
	a, err := r.Assign(testCaller, baseAssignment(v))
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	first, err := r.Enroll(testCaller, a.ID, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	second, err := r.Enroll(testCaller, a.ID, time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("re-enroll: %v", err)
	}
	if second.Digest != first.Digest {
		t.Fatal("re-enroll minted a new record")
	}
}

func FuzzTodo_LEARN_003(f *testing.F) {
	f.Add([]byte("asg-1"), []byte("worker-7"), int64(50))
	f.Fuzz(func(t *testing.T, id, learner []byte, capacity int64) {
		a := Assignment{
			ID: string(id), LearnerID: string(learner),
			CourseID: "crs-safety-101", Version: 2, Tenant: "tenant-acme",
			RequirementSource: "bench", DueAt: dueAt(), Capacity: int(capacity),
			WindowStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			WindowEnd:   time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		}
		// Must never panic; empty ids and non-positive capacity never validate.
		if err := ValidateAssignment(a); err == nil &&
			(len(id) == 0 || len(learner) == 0 || capacity <= 0) {
			t.Fatal("empty/invalid assignment validated")
		}
	})
}

func TestTodo_LEARN_003_Security(t *testing.T) {
	r := NewRegistry()
	v := assignableVersion(t, r)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	if _, err := r.Assign(rival, baseAssignment(v)); err == nil {
		t.Fatal("cross-tenant assignment accepted")
	}
	if len(r.Journal()) != 2 { // course + version journals only
		t.Fatalf("refused assignment left effects: %d entries", len(r.Journal()))
	}
}

func TestTodo_LEARN_003_Mutation(t *testing.T) {
	r := NewRegistry()
	v := assignableVersion(t, r)
	// Mutant A: duplicate assignment must be killed.
	if _, err := r.Assign(testCaller, baseAssignment(v)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if _, err := r.Assign(testCaller, baseAssignment(v)); err == nil {
		t.Fatal("duplicate-assignment mutant survived")
	}
	// Mutant B: enrollment outside the window must be killed.
	if _, err := r.Enroll(testCaller, "asg-1", time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("out-of-window mutant survived")
	}
	// Mutant C: non-positive capacity must be killed.
	bad := baseAssignment(v)
	bad.ID = "asg-bad"
	bad.Capacity = 0
	if _, err := r.Assign(testCaller, bad); err == nil {
		t.Fatal("zero-capacity mutant survived")
	}
}
