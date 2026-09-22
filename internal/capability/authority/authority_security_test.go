package authority

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

// TestTodo_INTAPI_003_Security proves the unauthorized cases deny without
// leaking authority: forged delegation references, cross-tenant grants,
// stale or misbound step-up proofs, wildcard roles and single-approver
// "dual" approvals never authorize a capability call.
func TestTodo_INTAPI_003_Security(t *testing.T) {
	t.Run("a machine with forged delegation references is still refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			kind:  trust.SubjectKindService,
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
			delegrefs: []string{"delegation:forged-grant"},
		})
		got := Authorize(principal, authorityPurpose, readDef(), Authority{}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a verified delegation from another tenant is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			subject: "delegate-1",
			roles:   []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		scope := trust.AuthorityScope{
			Tenant: "other-corp", OrganizationScopeID: authorityOrg,
			Capabilities: []string{readDef().ID}, Resources: []string{"worker"},
			Fields: []string{"status"}, Purposes: []string{authorityPurpose},
			Assurance: trust.AssuranceHigh,
			NotBefore: authorityBaseNow.Add(-time.Hour), ExpiresAt: authorityBaseNow.Add(time.Hour),
		}
		eff, err := trust.EvaluateDelegation(trust.DelegationRequest{
			Grant: trust.DelegationGrant{
				GrantID: "grant-x", Delegator: "manager-1", Delegate: "delegate-1",
				Tenant: "other-corp", OrganizationScopeID: authorityOrg,
				Capabilities: []string{readDef().ID}, Resources: []string{"worker"},
				Fields: []string{"status"}, Purposes: []string{authorityPurpose},
				NotBefore: authorityBaseNow.Add(-time.Minute), ExpiresAt: authorityBaseNow.Add(time.Hour),
				RequiredAssurance: trust.AssuranceSubstantial,
			},
			Delegator: scope, Delegate: scope,
			EvaluatedAt: authorityBaseNow, CurrentRevocationEpoch: 0,
		})
		if err != nil {
			t.Fatalf("EvaluateDelegation: %v", err)
		}
		got := Authorize(principal, authorityPurpose, readDef(), Authority{Delegation: &eff}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a verified delegation outside its window is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			subject: "delegate-1",
			roles:   []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		eff := verifiedDelegation(t, "delegate-1", []string{readDef().ID}, []string{authorityPurpose})
		got := Authorize(principal, authorityPurpose, readDef(), Authority{Delegation: &eff}, authorityBaseNow.Add(2*time.Hour))
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a verified delegation naming another capability is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			subject: "delegate-1",
			roles:   []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		eff := verifiedDelegation(t, "delegate-1", []string{compDef().ID}, []string{authorityPurpose})
		got := Authorize(principal, authorityPurpose, readDef(), Authority{Delegation: &eff}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a stale step-up proof is refused at effect time", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := writeDef()
		ob := satisfiedStepUp(t, principal, def.ID, authorityPurpose, authorityBaseNow.Add(-10*time.Minute))
		got := Authorize(principal, authorityPurpose, def, Authority{StepUp: ob}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY: the proof aged past its recency window", got.Decision)
		}
	})

	t.Run("a step-up bound to another session is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		other := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
			session: "another-session",
		})
		def := writeDef()
		ob := satisfiedStepUp(t, other, def.ID, authorityPurpose, authorityBaseNow)
		got := Authorize(principal, authorityPurpose, def, Authority{StepUp: ob}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a step-up for another capability is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := writeDef()
		ob := satisfiedStepUp(t, principal, lowWriteDef().ID, authorityPurpose, authorityBaseNow)
		got := Authorize(principal, authorityPurpose, def, Authority{StepUp: ob}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a wildcard role grants nothing and emits no wildcard", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			kind:  trust.SubjectKindService,
			roles: []string{"*"}, purposes: []string{authorityPurpose},
		})
		got := Authorize(principal, authorityPurpose, readDef(), Authority{
			GrantedScopes: []string{"*"},
		}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
		for _, s := range got.Scopes {
			if s == "*" {
				t.Fatalf("scopes = %v, must never contain a wildcard", got.Scopes)
			}
		}
	})

	t.Run("a service holding every human role still cannot read compensation", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			kind: trust.SubjectKindService,
			roles: []string{
				string(authz.RoleWorkerSelf), string(authz.RoleManager),
				string(authz.RoleHRPartner), string(authz.RoleCompAdmin),
				string(authz.RolePayrollManager), string(authz.RoleAuditor),
				string(authz.RoleFinancePartner),
			},
			purposes: []string{
				authz.PurposeSelfService, authz.PurposeCompensationReview,
				authz.PurposePayrollProcessing, authz.PurposePerformanceReview,
				authz.PurposeAccommodationCase, authz.PurposeCaseManagement,
				authz.PurposeImmigrationCase, authz.PurposeAuditReview,
			},
		})
		def := compDef()
		got := Authorize(principal, authz.PurposeCompensationReview, def, Authority{
			GrantedScopes: []string{def.AuthZScopeRef},
		}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a lone approver is not dual approval", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := writeDef()
		got := Authorize(principal, authorityPurpose, def, Authority{DualApproval: &DualApproval{
			Tenant: authorityTenant, Capability: def.ID, ProposalRef: "proposal-1",
			Approvers: []string{"manager-1", "manager-1"}, DecidedAt: authorityBaseNow.Add(-time.Minute),
		}}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY: one approver twice is not dual approval", got.Decision)
		}
	})

	t.Run("dual approval without a recorded proposal is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := writeDef()
		got := Authorize(principal, authorityPurpose, def, Authority{DualApproval: &DualApproval{
			Tenant: authorityTenant, Capability: def.ID,
			Approvers: []string{"manager-1", "finance-1"}, DecidedAt: authorityBaseNow.Add(-time.Minute),
		}}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("an unsatisfied step-up obligation authorizes nothing", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := writeDef()
		ob := stepup.Obligation{Required: true, Satisfied: false, Reason: stepup.ReasonNoProof}
		got := Authorize(principal, authorityPurpose, def, Authority{StepUp: &ob}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})
}
