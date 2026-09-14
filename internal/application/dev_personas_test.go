package application

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestComposeDevPersonasIssuesFourDistinctVerifiedIdentities(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := ServeConfig{DevBrowserLogin: true, DevHMACKey: testDevKey, Issuer: DefaultIssuer, Audience: DefaultAudience, Tenant: LocalDevTenant}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	personas := composeDevPersonas(verifier, cfg, func() time.Time { return now })
	if len(personas) != 4 {
		t.Fatalf("personas = %d, want 4", len(personas))
	}
	wantSubject := map[string]string{
		"admin": "hc-050-rafael-torres", "hiring-manager": "hc-004-darius-bennett",
		"finance-partner": "hc-054-thomas-baker", "individual-contributor": "hc-051-linh-tran",
	}
	wantName := map[string]string{
		"admin": "Rafael Torres", "hiring-manager": "Darius Bennett",
		"finance-partner": "Thomas Baker", "individual-contributor": "Linh Tran",
	}
	wantPurpose := map[string]string{
		"admin": "compensation_review", "hiring-manager": "compensation_review",
		"finance-partner": "compensation_review", "individual-contributor": "self_service_view",
	}
	planned, err := demoworkforce.Plan(pgstore.TenantID(cfg.Tenant))
	if err != nil {
		t.Fatal(err)
	}
	plannedByKey := make(map[string]bool, len(planned))
	for _, worker := range planned {
		plannedByKey[worker.Row.WorkerKey] = worker.Row.LifecycleStatus == "active"
	}
	seen := map[string]bool{}
	for _, persona := range personas {
		principal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify %s: %v", persona.ID, verifyErr)
		}
		if principal.Subject() != wantSubject[persona.ID] {
			t.Errorf("%s subject = %q, want %q", persona.ID, principal.Subject(), wantSubject[persona.ID])
		}
		if persona.Name != wantName[persona.ID] {
			t.Errorf("%s name = %q, want %q", persona.ID, persona.Name, wantName[persona.ID])
		}
		if !principal.AuthorizesPurpose(wantPurpose[persona.ID]) {
			t.Errorf("%s does not authorize its assigned purpose %q", persona.ID, wantPurpose[persona.ID])
		}
		if persona.ID == "payroll-manager" && (principal.HasRole("payroll_manager") || !principal.HasRole("worker_self")) {
			t.Error("the payroll-labelled demo persona must remain limited to its verified self-service grant")
		}
		if !plannedByKey[principal.Subject()] {
			t.Errorf("%s subject %q is not an active worker in the durable demo seed plan", persona.ID, principal.Subject())
		}
		if persona.WorkerRef != principal.Subject() {
			t.Errorf("%s worker binding = %q, want verified subject %q", persona.ID, persona.WorkerRef, principal.Subject())
		}
		if seen[principal.Subject()] {
			t.Errorf("duplicate subject %q", principal.Subject())
		}
		seen[principal.Subject()] = true
	}
}

// purposeBackedByRoles reports whether any role in roles is granted purpose
// over some data domain in authz.PolicyTable -- the live P1A bootstrap
// policy every request is actually evaluated against, never a hand-written
// expectation of it. AnyPurpose domains (worker.core, worker.contact) are
// deliberately excluded: they grant regardless of declared purpose, so they
// can never be evidence that a role backs one specific purpose over another.
func purposeBackedByRoles(roles []string, purpose string) bool {
	for _, role := range roles {
		for _, grant := range authz.PolicyTable[authz.RoleID(role)] {
			if !grant.AnyPurpose && slices.Contains(grant.Purposes, purpose) {
				return true
			}
		}
	}
	return false
}

// TestComposeDevPersonasAccessAndPurposeMatchRoleBundle is UXAUDIT-014's
// remaining assertion: a persona's signed purpose, and any capability its
// access label's own wording implies, must be backed by the role bundle its
// credential actually carries.
//
// RED (measured live): payroll-manager's spec named access "Payroll manager"
// and purpose "payroll_processing" while workspace.DevPersonaRoles("payroll-
// manager") returns only "worker_self" -- a role authz.PolicyTable never
// grants PurposePayrollProcessing to. The label and the purpose both named a
// capability the signed credential could not exercise.
//
// This is derived from authz.PolicyTable and each persona's own signed
// roles/purposes, the way loginPersonaDescription is derived from the
// productui registry, rather than from a hardcoded expected-label list: a
// future persona spec that reintroduces this mismatch fails here without
// this test needing to name it in advance.
//
// Mutation-verified: temporarily reverting the payroll-manager spec in
// composeDevPersonas (internal/application/serve.go) back to access:
// "Payroll manager", purpose: "payroll_processing" made this test FAIL on
// both assertions --
//
//	dev_personas_test.go:NN: payroll-manager: signed purpose "payroll_processing" is not granted to role bundle [worker_self] by any P1A domain rule
//	dev_personas_test.go:NN: payroll-manager: access label "Payroll manager" implies purpose "payroll_processing" (keyword "payroll") that role bundle [worker_self] does not back
//
// reverting the spec back to access: "" (worker-derived), purpose:
// "self_service_view" made it PASS again. See the todo close-out report for
// the exact run transcript.
func TestComposeDevPersonasAccessAndPurposeMatchRoleBundle(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := ServeConfig{DevBrowserLogin: true, DevHMACKey: testDevKey, Issuer: DefaultIssuer, Audience: DefaultAudience, Tenant: LocalDevTenant}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	personas := composeDevPersonas(verifier, cfg, func() time.Time { return now })
	if len(personas) == 0 {
		t.Fatal("no personas composed")
	}

	// knownPurposes are authz's own exported purpose tokens -- the actual
	// values access-label prose could be implying -- not a paraphrase of
	// them typed into this test.
	knownPurposes := []string{
		authz.PurposeSelfService, authz.PurposeCompensationReview, authz.PurposePayrollProcessing,
		authz.PurposePerformanceReview, authz.PurposeAccommodationCase, authz.PurposeCaseManagement,
		authz.PurposeImmigrationCase, authz.PurposeAuditReview,
	}

	seenAccess := map[string]bool{}
	for _, persona := range personas {
		principal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify %s: %v", persona.ID, verifyErr)
		}
		for _, purpose := range principal.Purposes() {
			if !purposeBackedByRoles(persona.Roles, purpose) {
				t.Errorf("%s: signed purpose %q is not granted to role bundle %v by any P1A domain rule", persona.ID, purpose, persona.Roles)
			}
		}

		access := strings.ToLower(persona.Access)
		for _, purpose := range knownPurposes {
			keyword, _, cut := strings.Cut(purpose, "_")
			if !cut || !strings.Contains(access, keyword) {
				continue
			}
			if !purposeBackedByRoles(persona.Roles, purpose) {
				t.Errorf("%s: access label %q implies purpose %q (keyword %q) that role bundle %v does not back", persona.ID, persona.Access, purpose, keyword, persona.Roles)
			}
		}

		if persona.Access == "" {
			t.Errorf("%s: access label is empty", persona.ID)
		}
		seenAccess[persona.Access] = true
	}

	// Four personas sharing three distinguishable authorization shapes means
	// one pair of identical role bundles is unavoidable (UXAUDIT-014); that
	// pair's cards must still read as two different things, not four labels
	// collapsing into fewer than four.
	if len(seenAccess) != len(personas) {
		t.Errorf("access labels are not all distinct: %d distinct labels for %d personas", len(seenAccess), len(personas))
	}
}

func TestComposeDevPersonasStaysOffOutsideExplicitDevLogin(t *testing.T) {
	if got := composeDevPersonas(nil, ServeConfig{}, nil); len(got) != 0 {
		t.Fatalf("disabled personas = %d", len(got))
	}
}

func TestComposeDevPersonasDoesNotAdvertiseHarborCareWorkersForAnotherTenant(t *testing.T) {
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(testDevKey), Issuer: DefaultIssuer, Audience: DefaultAudience})
	if err != nil {
		t.Fatal(err)
	}
	cfg := ServeConfig{DevBrowserLogin: true, Tenant: "another-tenant", Issuer: DefaultIssuer, Audience: DefaultAudience}
	if got := composeDevPersonas(verifier, cfg, time.Now); len(got) != 0 {
		t.Fatalf("foreign tenant received %d HarborCare personas", len(got))
	}
}
