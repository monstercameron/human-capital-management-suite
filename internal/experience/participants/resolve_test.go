package participants

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

const testCapabilityRef = "hcmnext.people.explain_worker_state/v1"

func testOwnerRegistry(t *testing.T) *SpecificationOwnerRegistry {
	t.Helper()
	registry, err := NewSpecificationOwnerRegistry(map[string]string{testCapabilityRef: "PEOPLE"})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func fixture(t *testing.T, roles ...string) (*trust.Principal, values.EntityRef, values.Instant) {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "principal-a", SubjectKind: trust.SubjectKindHuman, Roles: roles, Purposes: []string{"case_review"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-a"})
	if err != nil {
		t.Fatal(err)
	}
	return p, values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}, values.NewInstant(now)
}

func stage(t *testing.T, roles ...string) Stage {
	p, subject, at := fixture(t, roles...)
	return Stage{StageID: "stage-a", CapabilityRef: testCapabilityRef, OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: subject, Purpose: "case_review", EffectiveAt: at, Authorization: authz.Request{Principal: p, Subject: subject}}
}

func TestUserFlowParticipantResolutionNeverInfersAuthorityFromPersonaOrRepresentation(t *testing.T) {
	tests := []struct {
		name        string
		roles       []string
		wantAllowed bool
	}{
		{"unrelated persona", []string{"manager"}, false},
		{"self governance", []string{string(authz.RoleWorkerSelf)}, false},
		{"administrative governance", []string{string(authz.RoleAuditor)}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Resolve(stage(t, tc.roles...))
			if err != nil {
				t.Fatal(err)
			}
			if r.Allowed != tc.wantAllowed {
				t.Fatalf("Allowed=%v want %v", r.Allowed, tc.wantAllowed)
			}
			if !r.Allowed && (r.Subject != (values.EntityRef{}) || r.Relationship != authz.RelationshipUnspecified || r.AllowedActions != nil || r.CapabilityOwner != "") {
				t.Fatalf("denial disclosed governed detail: %+v", r)
			}
		})
	}
}

func TestTodo_UXFLOW_002_Property(t *testing.T) {
	p, subject, at := fixture(t, string(authz.RoleWorkerSelf))
	r, err := Resolve(Stage{StageID: "self", CapabilityRef: testCapabilityRef, OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "00000000-0000-4000-8000-000000000002"}, EffectiveAt: at, Authorization: authz.Request{Principal: p}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Allowed || r.SubjectDisclosable {
		t.Fatalf("self role crossed subject boundary: %+v", r)
	}
	_ = subject
}

func TestTodo_UXFLOW_002_Golden(t *testing.T) {
	p, subject, at := fixture(t, string(authz.RoleAuditor))
	exp := values.NewInstant(at.Time().Add(time.Hour))
	r, err := Resolve(Stage{StageID: "golden", CapabilityRef: testCapabilityRef, OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: subject, EffectiveAt: at, RequestedActions: []string{"view"}, Authorization: authz.Request{Principal: p}, Representation: &Representation{Kind: RepresentationAssisted, Representative: "rep-a", Subject: subject, EvidenceRef: "evidence-a", ExpiresAt: exp}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Allowed || r.Subject != subject || r.CapabilityOwner != "PEOPLE" || r.Representation.Representative != "rep-a" || r.AllowedActions[0] != "view" || r.EvidenceID == "" {
		t.Fatalf("unexpected resolution: %+v", r)
	}
}

func TestTodo_UXFLOW_002_Security(t *testing.T) {
	p, subject, at := fixture(t, string(authz.RoleAuditor))
	bad := subject
	bad.Id = "00000000-0000-4000-8000-000000000002"
	r, err := Resolve(Stage{StageID: "security", CapabilityRef: testCapabilityRef, OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: subject, EffectiveAt: at, Authorization: authz.Request{Principal: p}, Representation: &Representation{Kind: RepresentationOnBehalfOf, Representative: "rep-a", Subject: bad, EvidenceRef: "e"}})
	if err == nil || !errors.Is(err, ErrInvalidStage) {
		t.Fatalf("subject substitution error=%v", err)
	}
	// Assistance does not bypass recusal.
	r, err = Resolve(Stage{StageID: "recused", CapabilityRef: testCapabilityRef, OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: subject, EffectiveAt: at, Authorization: authz.Request{Principal: p}, Recused: true})
	if err != nil || r.Allowed || r.Reason != "recused" {
		t.Fatalf("recusal not enforced: %+v err=%v", r, err)
	}
}

func TestTodo_UXFLOW_002_Conformance(t *testing.T) {
	if _, err := NewSpecificationOwnerRegistry(map[string]string{"": "PEOPLE"}); err == nil {
		t.Fatal("incomplete specification-owner entry accepted")
	}
	defaultOwners, err := NewDefaultSpecificationOwnerRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if owner, ok := defaultOwners.Resolve(testCapabilityRef); !ok || owner != "PEOPLE" {
		t.Fatalf("default specification owner unresolved: %q %v", owner, ok)
	}
	p, subject, at := fixture(t, string(authz.RoleAuditor))
	exp := values.NewInstant(at.Time().Add(time.Hour))
	r, err := Resolve(Stage{StageID: "delegate", CapabilityRef: testCapabilityRef, OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: subject, EffectiveAt: at, RequestedActions: []string{"view", "approve"}, Authorization: authz.Request{Principal: p}, Delegation: &Delegation{GrantRef: "grant-a", FromPrincipal: "manager-a", ToPrincipal: p.Subject(), AllowedActions: []string{"view"}, ExpiresAt: exp}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Allowed || r.Reason != "delegation_scope" || r.AllowedActions != nil {
		t.Fatalf("overbroad delegated action offered: %+v", r)
	}
	if r.CapabilityOwner != "PEOPLE" {
		t.Fatalf("delegation denial lost accountable owner: %+v", r)
	}
	_, err = Resolve(Stage{StageID: "unowned", CapabilityRef: "hcmnext.unknown/v1", OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: subject, EffectiveAt: at, Authorization: authz.Request{Principal: p}})
	if err == nil || !errors.Is(err, ErrInvalidStage) {
		t.Fatalf("unowned capability accepted: %v", err)
	}
}

func TestTodo_UXFLOW_002_Mutation(t *testing.T) {
	p, subject, at := fixture(t, string(authz.RoleAuditor))
	r, err := Resolve(Stage{StageID: "expiry", CapabilityRef: testCapabilityRef, OwnerRegistry: testOwnerRegistry(t), Principal: p, Subject: subject, EffectiveAt: at, Authorization: authz.Request{Principal: p}, Representation: &Representation{Kind: RepresentationAssisted, Representative: "rep-a", Subject: subject, EvidenceRef: "e", ExpiresAt: at}})
	if !errors.Is(err, ErrExpired) || r.Allowed {
		t.Fatalf("expired representation accepted: %+v err=%v", r, err)
	}
}
