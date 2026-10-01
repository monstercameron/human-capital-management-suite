package agentgate

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENT2_005_RequiredPurposeRestriction(t *testing.T) {
	f := newGateFixture(t)
	otherPurpose := "workforce:export"
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: testTenant, Subject: "user-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: testOrg, Roles: []string{"manager"}, Purposes: []string{testPurpose, otherPurpose},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-1", IssuedAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(time.Hour),
		CredentialDigest: "credential-1",
	})
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	user := f.user
	user.Principal = principal
	f.grants.grants[0].Purposes = append(f.grants.grants[0].Purposes, otherPurpose)

	request := f.request()
	request.User = user
	request.Purpose = otherPurpose
	if _, err := f.gate.Authorize(context.Background(), request); deniedCode(t, err) != DenyPurpose {
		t.Fatalf("call using undeclared skill purpose = %v, want %s", err, DenyPurpose)
	}
	if len(f.pdp.calls) != 0 {
		t.Fatal("PDP was called after the skill-purpose restriction denied the call")
	}

	discovered, err := f.gate.Discover(context.Background(), DiscoveryRequest{User: user, Purpose: otherPurpose, At: f.now})
	if err != nil || len(discovered) != 0 {
		t.Fatalf("discovery using undeclared skill purpose = %+v, %v; want no skills", discovered, err)
	}
	if len(f.pdp.calls) != 0 {
		t.Fatal("PDP was called after discovery filtered the skill-purpose mismatch")
	}
}
