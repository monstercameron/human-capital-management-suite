// REV-046-03 RED: ExpireCredentials must authorize and expire by each
// credential's own Tenant, never a hardcoded tenant literal.
package learning

import (
	"errors"
	"testing"
	"time"
)

var rev046Caller = Caller{ID: "lms-ops", Tenants: []string{"tenant-acme", "tenant-beta"}}

func rev046Seed(t *testing.T, r *Registry, caller Caller, tenant, courseID, learnerID, eventID string) Credential {
	t.Helper()
	course, err := r.DefineCourse(caller, Course{
		ID: courseID, Title: "Workplace Safety", Provider: "acme-academy",
		Tenant: tenant,
		Locales: []CourseLocale{
			{Language: "en", Region: "US", Formats: []string{"video-captioned", "text-screen-reader"}},
		},
	})
	if err != nil {
		t.Fatalf("DefineCourse %s: %v", tenant, err)
	}
	if _, err := r.DefineCourseVersion(caller, CourseVersion{
		CourseID: course.ID, Version: 2, Tenant: tenant,
		ContentRef: "content-safety-101-v2", PrerequisiteRefs: []string{"crs-orientation@1"},
		AssessmentRefs: []string{"quiz-safety-101"}, CompletionRule: "all-modules-and-quiz",
		CredentialTemplate: "safety-cert-v1", ExpiryPolicy: "valid-730-days",
		ExternalAuthority: "osha-outreach", Locale: "en-US",
		PassingScore: "80", MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("DefineCourseVersion %s: %v", tenant, err)
	}
	r.RegisterProvider("acme-lms")
	rec, err := r.AcceptCompletion(caller, CompletionEvent{
		EventID: eventID, LearnerID: learnerID,
		CourseID: courseID, Version: 2,
		AssessmentRef:   "quiz-safety-101",
		CompletedAt:     time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
		SourceAuthority: "acme-lms",
		Tenant:          tenant,
	})
	if err != nil {
		t.Fatalf("AcceptCompletion %s: %v", tenant, err)
	}
	if _, err := r.VerifyCompletion(caller, rec.EventID, "evidence-proctor-77"); err != nil {
		t.Fatalf("VerifyCompletion %s: %v", tenant, err)
	}
	cred, err := r.IssueCredential(caller, CredentialRequest{
		LearnerID: learnerID, CourseID: courseID, Version: 2,
		Tenant: tenant, Outcome: passingOutcome(),
		At: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("IssueCredential %s: %v", tenant, err)
	}
	return cred
}

func rev046ExpiredIDs(expired []Credential) map[string]bool {
	out := make(map[string]bool, len(expired))
	for _, cred := range expired {
		out[cred.ID] = true
	}
	return out
}

func rev046WarnRefs(r *Registry) map[string]int {
	counts := map[string]int{}
	for _, e := range r.Journal() {
		if e.Op == "warn-expiry" {
			counts[e.Ref]++
		}
	}
	return counts
}

// TestTodo_REV_046_03 proves ExpireCredentials authorizes and expires by each
// credential's own Tenant: credentials under two distinct tenants expire and
// warn independently, a caller scoped to one tenant cannot sweep the other,
// and nothing is silently skipped.
func TestTodo_REV_046_03(t *testing.T) {
	r := NewRegistry()
	credA := rev046Seed(t, r, rev046Caller, "tenant-acme", "crs-safety-101", "worker-7", "evt-046-a")
	credB := rev046Seed(t, r, rev046Caller, "tenant-beta", "crs-safety-202", "worker-9", "evt-046-b")

	stillValid, err := r.ExpireCredentials(rev046Caller, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ExpireCredentials before expiry: %v", err)
	}
	if len(stillValid) != 0 {
		t.Fatalf("valid credentials expired early: %v", stillValid)
	}

	expired, err := r.ExpireCredentials(rev046Caller, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ExpireCredentials: %v", err)
	}
	got := rev046ExpiredIDs(expired)
	if len(got) != 2 || !got[credA.ID] || !got[credB.ID] {
		t.Fatalf("expired = %v, want both %s and %s", expired, credA.ID, credB.ID)
	}
	warns := rev046WarnRefs(r)
	if warns[credA.ID] != 1 || warns[credB.ID] != 1 {
		t.Fatalf("each credential must warn exactly once, warns = %v", warns)
	}

	again, err := r.ExpireCredentials(rev046Caller, time.Date(2029, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("re-expire: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("re-expiry re-emitted: %v", again)
	}

	// A caller scoped to one tenant must fail loudly on the foreign
	// credential instead of silently skipping it.
	r2 := NewRegistry()
	rev046Seed(t, r2, rev046Caller, "tenant-acme", "crs-safety-101", "worker-7", "evt-046-a")
	foreign := rev046Seed(t, r2, rev046Caller, "tenant-beta", "crs-safety-202", "worker-9", "evt-046-b")
	acmeOnly := Caller{ID: "lms-ops", Tenants: []string{"tenant-acme"}}
	if _, err := r2.ExpireCredentials(acmeOnly, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("single-tenant sweep error = %v, want ErrNotAuthorized", err)
	}
	for ref := range rev046WarnRefs(r2) {
		if ref == foreign.ID {
			t.Fatalf("foreign credential %s was warned without authorization", foreign.ID)
		}
	}
}

// TestTodo_REV_046_03_Security proves no cross-tenant authorization: a rival
// tenant cannot expire anyone, and a partially scoped caller is refused.
func TestTodo_REV_046_03_Security(t *testing.T) {
	r := NewRegistry()
	rev046Seed(t, r, rev046Caller, "tenant-acme", "crs-safety-101", "worker-7", "evt-046-a")
	rev046Seed(t, r, rev046Caller, "tenant-beta", "crs-safety-202", "worker-9", "evt-046-b")
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	if _, err := r.ExpireCredentials(rival, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("cross-tenant expiry error = %v, want ErrNotAuthorized", err)
	}
	if warns := rev046WarnRefs(r); len(warns) != 0 {
		t.Fatalf("refused expiry run warned: %v", warns)
	}
	empty := Caller{ID: "", Tenants: []string{"tenant-acme"}}
	if _, err := r.ExpireCredentials(empty, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("anonymous expiry error = %v, want ErrNotAuthorized", err)
	}
}

// TestTodo_REV_046_03_Mutation proves the tenant is read from each credential
// at handling time: flipping a credential's tenant after seeding moves its
// authorization and expiry to the new tenant instead of the hardcoded one.
func TestTodo_REV_046_03_Mutation(t *testing.T) {
	r := NewRegistry()
	cred := rev046Seed(t, r, rev046Caller, "tenant-acme", "crs-safety-101", "worker-7", "evt-046-a")

	r.mu.Lock()
	flipped, ok := r.credentials[cred.ID]
	if !ok {
		r.mu.Unlock()
		t.Fatalf("seeded credential %s missing", cred.ID)
	}
	flipped.Tenant = "tenant-beta"
	r.credentials[cred.ID] = flipped
	r.mu.Unlock()

	expired, err := r.ExpireCredentials(rev046Caller, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ExpireCredentials after tenant flip: %v", err)
	}
	if !rev046ExpiredIDs(expired)[cred.ID] {
		t.Fatalf("flipped credential %s was silently skipped: %v", cred.ID, expired)
	}

	// Mutant: flipping to a tenant nobody in the caller holds must be
	// refused under the credential's own (new) tenant.
	r2 := NewRegistry()
	ghost := rev046Seed(t, r2, rev046Caller, "tenant-acme", "crs-safety-101", "worker-7", "evt-046-a")
	r2.mu.Lock()
	moved, ok := r2.credentials[ghost.ID]
	if !ok {
		r2.mu.Unlock()
		t.Fatalf("seeded credential %s missing", ghost.ID)
	}
	moved.Tenant = "tenant-ghost"
	r2.credentials[ghost.ID] = moved
	r2.mu.Unlock()
	if _, err := r2.ExpireCredentials(rev046Caller, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("ghost-tenant expiry error = %v, want ErrNotAuthorized", err)
	}
}
