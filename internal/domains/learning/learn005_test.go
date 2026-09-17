package learning

import (
	"strings"
	"testing"
	"time"
)

func passingOutcome() AssessmentOutcome {
	return AssessmentOutcome{
		Score: "88", MaxScore: "100", AttemptsUsed: 1, AttemptsAllowed: 3,
		Proctored: true, ProctorRef: "proctor-77", EvidenceRef: "evidence-proctor-77",
	}
}

func verifiedCompletion(t *testing.T, r *Registry) CompletionRecord {
	t.Helper()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	rec, err := r.AcceptCompletion(testCaller, completionEvent())
	if err != nil {
		t.Fatalf("AcceptCompletion: %v", err)
	}
	verified, err := r.VerifyCompletion(testCaller, rec.EventID, "evidence-proctor-77")
	if err != nil {
		t.Fatalf("VerifyCompletion: %v", err)
	}
	return verified
}

func TestTodo_LEARN_005(t *testing.T) {
	r := NewRegistry()
	verifiedCompletion(t, r)
	cred, err := r.IssueCredential(testCaller, CredentialRequest{
		LearnerID: "worker-7", CourseID: "crs-safety-101", Version: 2,
		Tenant: "tenant-acme", Outcome: passingOutcome(),
		At: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("IssueCredential: %v", err)
	}
	// GREEN: credential binds issuer, scope, dates, evidence and
	// revocation status.
	if cred.Issuer != "osha-outreach" {
		t.Fatalf("issuer = %q, want the version authority", cred.Issuer)
	}
	if strings.TrimSpace(cred.Scope) == "" || cred.IssuedAt.IsZero() ||
		cred.ExpiresAt.IsZero() || strings.TrimSpace(cred.EvidenceRef) == "" {
		t.Fatalf("credential omits scope/dates/evidence: %+v", cred)
	}
	if cred.Revoked {
		t.Fatal("fresh credential is revoked")
	}
	if cred.Digest == "" {
		t.Fatal("credential carries no digest")
	}
	// Failing score yields FAIL, not a credential.
	failing := passingOutcome()
	failing.Score = "42"
	if got := r.EvaluateAssessment(mustVersionDigest(t, r), failing); got != AssessmentFail {
		t.Fatalf("failing score evaluates %q", got)
	}
	if _, err := r.IssueCredential(testCaller, CredentialRequest{
		LearnerID: "worker-7", CourseID: "crs-safety-101", Version: 2,
		Tenant: "tenant-acme", Outcome: failing,
		At: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("credential issued on FAIL")
	}
}

func mustVersionDigest(t *testing.T, r *Registry) CourseVersion {
	t.Helper()
	v, ok := r.LookupVersion("crs-safety-101", 2)
	if !ok {
		t.Fatal("version not recorded")
	}
	return v
}

func TestTodo_LEARN_005_Property(t *testing.T) {
	r := NewRegistry()
	verifiedCompletion(t, r)
	// UNKNOWN (missing evidence) issues nothing and explains why.
	unknown := passingOutcome()
	unknown.EvidenceRef = ""
	if got := r.EvaluateAssessment(mustVersionDigest(t, r), unknown); got != AssessmentUnknown {
		t.Fatalf("evidenceless outcome evaluates %q", got)
	}
	if _, err := r.IssueCredential(testCaller, CredentialRequest{
		LearnerID: "worker-7", CourseID: "crs-safety-101", Version: 2,
		Tenant: "tenant-acme", Outcome: unknown,
		At: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("credential issued on UNKNOWN")
	}
}

func FuzzTodo_LEARN_005(f *testing.F) {
	f.Add([]byte("88"), 1, 3, true)
	f.Fuzz(func(t *testing.T, score []byte, used, allowed int, proctored bool) {
		out := AssessmentOutcome{
			Score: string(score), MaxScore: "100",
			AttemptsUsed: used, AttemptsAllowed: allowed,
			Proctored: proctored, ProctorRef: "p", EvidenceRef: "e",
		}
		// Must never panic; verdicts stay in the closed vocabulary.
		switch got := EvaluateAssessmentStatic(out, "80"); got {
		case AssessmentPass, AssessmentFail, AssessmentUnknown:
		default:
			t.Fatalf("verdict %q outside closed vocabulary", got)
		}
	})
}

func TestTodo_LEARN_005_Security(t *testing.T) {
	r := NewRegistry()
	verifiedCompletion(t, r)
	// Issuance requires a verified completion: mere acceptance is not
	// enough, and other learners' completions do not transfer.
	r2 := NewRegistry()
	verifiedCompletion(t, r2)
	out := passingOutcome()
	_, err := r2.IssueCredential(testCaller, CredentialRequest{
		LearnerID: "worker-stranger", CourseID: "crs-safety-101", Version: 2,
		Tenant: "tenant-acme", Outcome: out,
		At: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("credential issued without the learner's verified completion")
	}
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	if _, err := r.IssueCredential(rival, CredentialRequest{
		LearnerID: "worker-7", CourseID: "crs-safety-101", Version: 2,
		Tenant: "tenant-acme", Outcome: out,
		At: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("cross-tenant issuance accepted")
	}
}

func TestTodo_LEARN_005_Mutation(t *testing.T) {
	r := NewRegistry()
	verifiedCompletion(t, r)
	at := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	newReq := func() CredentialRequest {
		return CredentialRequest{
			LearnerID: "worker-7", CourseID: "crs-safety-101", Version: 2,
			Tenant: "tenant-acme", Outcome: passingOutcome(), At: at,
		}
	}
	// Mutant A: exhausted attempts must be killed.
	req := newReq()
	req.Outcome.AttemptsUsed = 4
	if _, err := r.IssueCredential(testCaller, req); err == nil {
		t.Fatal("exhausted-attempts mutant survived")
	}
	// Mutant B: unproctored assessment where proctoring is required must
	// be killed.
	req = newReq()
	req.Outcome.Proctored = false
	req.Outcome.ProctorRef = ""
	if _, err := r.IssueCredential(testCaller, req); err == nil {
		t.Fatal("unproctored mutant survived")
	}
	// Mutant C: completion accepted but never verified must be killed.
	r3 := NewRegistry()
	c := mustCourse(t, r3)
	mustVersion(t, r3, c.ID)
	if _, err := r3.AcceptCompletion(testCaller, completionEvent()); err != nil {
		t.Fatalf("AcceptCompletion: %v", err)
	}
	if _, err := r3.IssueCredential(testCaller, newReq()); err == nil {
		t.Fatal("unverified-completion mutant survived")
	}
}
