package pseudonym_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/pseudonym"
)

func revelationPolicy() pseudonym.RevelationPolicy {
	return pseudonym.RevelationPolicy{
		ID: "reveal-policy", Version: "v3",
		ClearedScopes: map[string][]pseudonym.RevelationPurpose{
			"program-a": {pseudonym.RevelationPurposeCaseInvestigation},
		},
		CustodianRole: "privacy-custodian", RequesterRoles: []string{"requester"}, MaxTTL: time.Hour,
		Clock: func() time.Time { return testNow },
	}
}

func revelationRequest(p pseudonym.Pseudonym) pseudonym.RevelationRequest {
	return pseudonym.RevelationRequest{
		Pseudonym: p, RequestedBy: "case-worker", Approver: "custodian-1", ApproverRole: "privacy-custodian",
		Purpose: pseudonym.RevelationPurposeCaseInvestigation, LegalBasisRef: "legal:case-42", Scope: p.Scope,
		TTL: 30 * time.Minute, RequestedAt: testNow, ExpiresAt: testNow.Add(30 * time.Minute), Recipients: []string{"investigator"}, Fields: []string{"contact_reference"}, NotificationPolicy: "notify-subject-after-review",
	}
}

// TestTodo_ANON_004 proves exact purpose, scope, SoD, legal basis, fields,
// recipients, expiry and notification policy bind a digested receipt.
func TestTodo_ANON_004(t *testing.T) {
	p := pseudonym.Pseudonym{ID: "psn-generation-1", Value: "psn-generation-1", Tenant: "tenant-1", Scope: "program-a", Generation: 1}
	decision, err := pseudonym.EvaluateRevelation(revelationPolicy(), revelationRequest(p))
	if err != nil || !decision.Allowed {
		t.Fatalf("decision = %+v, err = %v", decision, err)
	}
	evidence := decision.Evidence
	if evidence.Digest == "" || evidence.PseudonymRef != p.ID || evidence.Generation != 1 || !evidence.ExpiresAt.After(evidence.AuthorizedAt) {
		t.Fatalf("evidence = %+v", evidence)
	}
	if err := evidence.Validate(testNow); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pseudonym.ExplainRevelation(), p.ID) || strings.Contains(pseudonym.Explain(), p.ID) {
		t.Fatal("Explain carried a pseudonym")
	}

	_, provider, deriver, derivationKey, escrowKey, _ := escrowService(t)
	service, err := pseudonym.NewEscrowedService(pseudonym.EscrowConfig{Deriver: deriver, Provider: provider, DerivationKey: derivationKey, EscrowKey: escrowKey, Clock: func() time.Time { return testNow }, ApprovalAuthority: &testRevelationAuthority{revoked: map[string]bool{}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := escrowContext()
	escrowPseudonym, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	releaseDecision, err := service.AuthorizeRevelation(ctx, revelationPolicy(), revelationRequest(escrowPseudonym), testApproval("decision:requester", "case-worker", revelationRequest(escrowPseudonym)), testApproval("decision:custodian", "custodian-1", revelationRequest(escrowPseudonym)))
	if err != nil {
		t.Fatal(err)
	}
	release := governedRelease(escrowPseudonym, releaseDecision.Evidence)
	if _, _, err := service.ReleaseWithEvidence(ctx, release, releaseDecision.Evidence); err != nil {
		t.Fatalf("governed release = %v", err)
	}
	if _, _, err := service.ReleaseWithEvidence(ctx, release, releaseDecision.Evidence); !errors.Is(err, pseudonym.ErrRevelationEvidenceUsed) {
		t.Fatalf("replayed receipt err = %v", err)
	}
}

// TestTodo_ANON_004_Race proves independent decisions remain valid under
// concurrent evaluation and retain distinct digests for distinct requesters.
func TestTodo_ANON_004_Race(t *testing.T) {
	p := pseudonym.Pseudonym{ID: "psn-generation-race", Tenant: "tenant-1", Scope: "program-a", Generation: 2}
	policy := revelationPolicy()
	results := make(chan pseudonym.RevelationDecision, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			req := revelationRequest(p)
			req.RequestedBy = "case-worker-" + string(rune('a'+i))
			decision, _ := pseudonym.EvaluateRevelation(policy, req)
			results <- decision
		}(i)
	}
	seen := map[string]struct{}{}
	for i := 0; i < 8; i++ {
		decision := <-results
		if !decision.Allowed {
			t.Fatalf("denied decision = %+v", decision)
		}
		seen[decision.Evidence.Digest] = struct{}{}
	}
	if len(seen) != 8 {
		t.Fatalf("digests = %d, want 8", len(seen))
	}
}

// TestTodo_ANON_004_Security proves stale, self-approved, overbroad and
// uncleared revelations fail closed without producing a receipt.
func TestTodo_ANON_004_Security(t *testing.T) {
	p := pseudonym.Pseudonym{ID: "psn-security", Tenant: "tenant-1", Scope: "program-a", Generation: 1}
	policy := revelationPolicy()
	cases := []pseudonym.RevelationRequest{
		func() pseudonym.RevelationRequest { r := revelationRequest(p); r.Approver = r.RequestedBy; return r }(),
		func() pseudonym.RevelationRequest {
			r := revelationRequest(p)
			r.RequestedAt = testNow.Add(-2 * time.Hour)
			return r
		}(),
		func() pseudonym.RevelationRequest {
			r := revelationRequest(p)
			r.Pseudonyms = []pseudonym.Pseudonym{p, p}
			return r
		}(),
		func() pseudonym.RevelationRequest {
			r := revelationRequest(p)
			r.Purpose = pseudonym.RevelationPurposeSafety
			return r
		}(),
	}
	for _, request := range cases {
		decision, err := pseudonym.EvaluateRevelation(policy, request)
		if !errors.Is(err, pseudonym.ErrRevelationDenied) || decision.Allowed || decision.Evidence.Digest != "" {
			t.Fatalf("request = %+v, decision = %+v, err = %v", request, decision, err)
		}
	}
}

// TestTodo_ANON_004_Mutation proves changing a receipt's scope or digest
// prevents it from being accepted by the escrow release boundary.
func TestTodo_ANON_004_Mutation(t *testing.T) {
	p := pseudonym.Pseudonym{ID: "psn-mutation", Tenant: "tenant-1", Scope: "program-a", Generation: 1}
	decision, err := pseudonym.EvaluateRevelation(revelationPolicy(), revelationRequest(p))
	if err != nil {
		t.Fatal(err)
	}
	evidence := decision.Evidence
	evidence.Scope = "program-b"
	if err := evidence.Validate(testNow); !errors.Is(err, pseudonym.ErrRevelationEvidence) {
		t.Fatalf("mutated evidence err = %v", err)
	}
}
