package transport

import (
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func trustedFieldsPrincipal(t *testing.T, kind trust.SubjectKind) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("acme-corp"), Subject: "actor-1", SubjectKind: kind,
		OrganizationScopeID: "org-east", Roles: []string{"intent_author"},
		Purposes: []string{"hcm_operations"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest-actor-1",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func TestTodo_REV_103_05_TrustedRequestBoundary(t *testing.T) {
	principal := trustedFieldsPrincipal(t, trust.SubjectKindHuman)
	resolved, err := ApplyTrustedContext(nil, principal)
	if err != nil || resolved.TenantID != "acme-corp" || resolved.OrganizationScopeID != "org-east" || resolved.Purpose != "hcm_operations" {
		t.Fatalf("nil-message scope = %+v, error=%+v", resolved, err)
	}

	for name, scope := range map[string]*commonv1.ScopeContext{
		"tenant":       {TenantId: "other-tenant"},
		"organization": {OrganizationScopeId: "org-west"},
	} {
		_, got := ApplyTrustedContext(&intentsv1.GetIntentRequest{Scope: scope}, principal)
		if got == nil || got.Code() != envelope.CodeInvalidArgument || got.ReasonRef() != reasonCallerSelectedAuthority {
			t.Errorf("caller-selected %s was not rejected: %+v", name, got)
		}
	}
	_, denied := ApplyTrustedContext(&intentsv1.GetIntentRequest{Scope: &commonv1.ScopeContext{Purpose: "payroll_export"}}, principal)
	if denied == nil || denied.Code() != envelope.CodePermissionDenied || denied.ReasonRef() != reasonPurposeNotAuthorized {
		t.Fatalf("unauthorized purpose result = %+v", denied)
	}

	request := &intentsv1.GetIntentRequest{Scope: &commonv1.ScopeContext{Purpose: "hcm_operations"}}
	resolved, err = ApplyTrustedContext(request, principal)
	if err != nil || resolved.TenantID != "acme-corp" || request.GetScope().GetTenantId() != "acme-corp" || request.GetScope().GetOrganizationScopeId() != "org-east" {
		t.Fatalf("server context was not projected into request: scope=%+v resolved=%+v err=%+v", request.GetScope(), resolved, err)
	}

	for kind, want := range map[trust.SubjectKind]intentsv1.InitiatorKind{
		trust.SubjectKindHuman:       intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN,
		trust.SubjectKindService:     intentsv1.InitiatorKind_INITIATOR_KIND_SERVICE,
		trust.SubjectKindAgent:       intentsv1.InitiatorKind_INITIATOR_KIND_AGENT,
		trust.SubjectKindIntegration: intentsv1.InitiatorKind_INITIATOR_KIND_INTEGRATION,
	} {
		create := &intentsv1.CreateIntentRequest{}
		_, err := ApplyTrustedContext(create, trustedFieldsPrincipal(t, kind))
		if err != nil || create.GetInitiator().GetPrincipalId() != "actor-1" || create.GetInitiator().GetKind() != want {
			t.Errorf("principal projection for %q = %+v, error=%+v", kind, create.GetInitiator(), err)
		}
	}
	if got := initiatorKind(trust.SubjectKind(99)); got != intentsv1.InitiatorKind_INITIATOR_KIND_UNSPECIFIED {
		t.Fatalf("unknown subject kind mapped to %v", got)
	}

	forged := &intentsv1.CreateIntentRequest{Initiator: &intentsv1.PrincipalReference{PrincipalId: "victim"}}
	_, got := ApplyTrustedContext(forged, principal)
	if got == nil || got.Code() != envelope.CodeInvalidArgument || got.ReasonRef() != reasonCallerSelectedAuthority {
		t.Fatalf("caller-selected initiator was not rejected: %+v", got)
	}
}
