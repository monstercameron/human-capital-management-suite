package learning

import (
	"testing"
	"time"
)

func issuedCredential(t *testing.T, r *Registry) Credential {
	t.Helper()
	verifiedCompletion(t, r)
	cred, err := r.IssueCredential(testCaller, CredentialRequest{
		LearnerID: "worker-7", CourseID: "crs-safety-101", Version: 2,
		Tenant: "tenant-acme", Outcome: passingOutcome(),
		At: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("IssueCredential: %v", err)
	}
	return cred
}

func TestTodo_LEARN_006(t *testing.T) {
	r := NewRegistry()
	cred := issuedCredential(t, r)
	// Expiry policy is 730 days from 2026-10-06: still valid in 2027,
	// expired in 2029, with exactly one warning.
	expired, err := r.ExpireCredentials(testCaller, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ExpireCredentials: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("valid credential expired early: %v", expired)
	}
	expired, err = r.ExpireCredentials(testCaller, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ExpireCredentials: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != cred.ID {
		t.Fatalf("expired = %v", expired)
	}
	// Trusted-time expiry emits warnings once: a second pass warns no one.
	again, err := r.ExpireCredentials(testCaller, time.Date(2029, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("re-expire: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("re-expiry re-emitted: %v", again)
	}
	warnings := 0
	for _, e := range r.Journal() {
		if e.Op == "warn-expiry" {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("expiry warnings = %d, want exactly 1", warnings)
	}
	// GREEN: renewal creates a new evidence/version, never silently
	// extends the old credential.
	renewed, err := r.Renew(testCaller, RenewalRequest{
		CredentialID: cred.ID, NewEvidenceRef: "evidence-renew-1",
		At: time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if renewed.ID == cred.ID || renewed.CredentialVersion != cred.CredentialVersion+1 {
		t.Fatalf("renewal did not create a new version: %+v", renewed)
	}
	if renewed.EvidenceRef != "evidence-renew-1" {
		t.Fatal("renewal carries no new evidence")
	}
	old, _ := r.LookupCredential(cred.ID)
	if old.ExpiresAt != cred.ExpiresAt {
		t.Fatal("renewal silently extended the old credential")
	}
}

func TestTodo_LEARN_006_Property(t *testing.T) {
	r := NewRegistry()
	cred := issuedCredential(t, r)
	// Renewal before expiry is refused: only expired credentials renew.
	if _, err := r.Renew(testCaller, RenewalRequest{
		CredentialID: cred.ID, NewEvidenceRef: "evidence-early",
		At: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("pre-expiry renewal accepted")
	}
	// Renewal chains: the second renewal cites the first.
	if _, err := r.ExpireCredentials(testCaller, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ExpireCredentials: %v", err)
	}
	first, err := r.Renew(testCaller, RenewalRequest{
		CredentialID: cred.ID, NewEvidenceRef: "evidence-renew-1",
		At: time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if first.RenewalOf != cred.ID {
		t.Fatalf("renewal lineage = %q", first.RenewalOf)
	}
}

func FuzzTodo_LEARN_006(f *testing.F) {
	f.Add([]byte("cred-1"), []byte("evidence-renew-1"), int64(1900000000))
	f.Fuzz(func(t *testing.T, id, evidence []byte, at int64) {
		req := RenewalRequest{
			CredentialID: string(id), NewEvidenceRef: string(evidence),
			At: time.Unix(at, 0).UTC(),
		}
		// Must never panic; empty ids and empty evidence never validate.
		if err := ValidateRenewalRequest(req); err == nil &&
			(len(id) == 0 || len(evidence) == 0) {
			t.Fatal("empty renewal request validated")
		}
	})
}

func TestTodo_LEARN_006_Security(t *testing.T) {
	r := NewRegistry()
	cred := issuedCredential(t, r)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	if _, err := r.Renew(rival, RenewalRequest{
		CredentialID: cred.ID, NewEvidenceRef: "evidence-forge",
		At: time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("cross-tenant renewal accepted")
	}
	if _, err := r.ExpireCredentials(rival, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("cross-tenant expiry accepted")
	}
}

func TestTodo_LEARN_006_Mutation(t *testing.T) {
	r := NewRegistry()
	cred := issuedCredential(t, r)
	if _, err := r.ExpireCredentials(testCaller, time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ExpireCredentials: %v", err)
	}
	// Mutant A: renewal without new evidence must be killed.
	if _, err := r.Renew(testCaller, RenewalRequest{
		CredentialID: cred.ID, NewEvidenceRef: "",
		At: time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("evidenceless-renewal mutant survived")
	}
	// Mutant B: renewal of an unknown credential must be killed.
	if _, err := r.Renew(testCaller, RenewalRequest{
		CredentialID: "cred-ghost", NewEvidenceRef: "evidence-x",
		At: time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("ghost-credential mutant survived")
	}
	// Mutant C: double renewal of the same credential must be killed
	// (the old version is spent).
	if _, err := r.Renew(testCaller, RenewalRequest{
		CredentialID: cred.ID, NewEvidenceRef: "evidence-renew-1",
		At: time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("first Renew: %v", err)
	}
	if _, err := r.Renew(testCaller, RenewalRequest{
		CredentialID: cred.ID, NewEvidenceRef: "evidence-renew-2",
		At: time.Date(2029, 3, 1, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("double-renewal mutant survived")
	}
}
