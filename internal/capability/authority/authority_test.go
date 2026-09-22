package authority

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

var authorityBaseNow = time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)

const (
	authorityTenant  = "acme-corp"
	authorityOrg     = "org-na"
	authorityPurpose = "compensation_review"
)

type principalOpts struct {
	kind      trust.SubjectKind
	roles     []string
	purposes  []string
	assurance trust.Assurance
	session   string
	subject   string
	delegrefs []string
}

func authorityPrincipal(t *testing.T, opts principalOpts) *trust.Principal {
	t.Helper()
	if opts.assurance == trust.AssuranceUnspecified {
		opts.assurance = trust.AssuranceHigh
	}
	if opts.session == "" {
		opts.session = "authority-session"
	}
	if opts.subject == "" {
		opts.subject = "authority-subject"
	}
	kind := opts.kind
	if kind == trust.SubjectKindUnspecified {
		kind = trust.SubjectKindHuman
	}
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               authorityTenant,
		Subject:              opts.subject,
		SubjectKind:          kind,
		OrganizationScopeID:  authorityOrg,
		Roles:                opts.roles,
		Purposes:             opts.purposes,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            opts.assurance,
		SessionRef:           opts.session,
		DelegationRefs:       opts.delegrefs,
		IssuedAt:             authorityBaseNow.Add(-time.Minute),
		ExpiresAt:            authorityBaseNow.Add(time.Hour),
		CredentialDigest:     "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func readDef() capability.Definition {
	return capability.Definition{
		ID: "hcmnext.people.explain_worker_state", Version: 1,
		EffectClass:   capability.EffectReadOnly,
		ReadData:      capability.DataDomainFieldSet{DataDomains: []string{"worker", "employment", "position"}},
		RiskClass:     "LOW",
		AuthZScopeRef: "scope:people.read",
	}
}

func compDef() capability.Definition {
	return capability.Definition{
		ID: "hcmnext.rewards.simulate_compensation", Version: 1,
		EffectClass:   capability.EffectReadOnly,
		ReadData:      capability.DataDomainFieldSet{DataDomains: []string{"compensation", "budget"}},
		RiskClass:     "LOW",
		AuthZScopeRef: "scope:rewards.read",
	}
}

func writeDef() capability.Definition {
	return capability.Definition{
		ID: "hcmnext.people.update_worker", Version: 1,
		EffectClass:   capability.EffectExternalMutation,
		WriteData:     capability.DataDomainFieldSet{DataDomains: []string{"worker"}},
		RiskClass:     "HIGH",
		AuthZScopeRef: "scope:people.write",
	}
}

func lowWriteDef() capability.Definition {
	def := writeDef()
	def.ID = "hcmnext.operations.annotate_projection"
	def.RiskClass = "LOW"
	def.AuthZScopeRef = "scope:operations.write"
	return def
}

// verifiedDelegation evaluates a real delegation grant for delegate covering
// capabilities and purposes, so the authority tests exercise genuine
// server-verified effective authority rather than a hand-built struct.
func verifiedDelegation(t *testing.T, delegate string, capabilities, purposes []string) trust.EffectiveAuthority {
	t.Helper()
	scope := trust.AuthorityScope{
		Tenant: authorityTenant, OrganizationScopeID: authorityOrg,
		Capabilities: capabilities, Resources: []string{"worker"},
		Fields: []string{"status"}, Purposes: purposes,
		Assurance: trust.AssuranceHigh,
		NotBefore: authorityBaseNow.Add(-time.Hour), ExpiresAt: authorityBaseNow.Add(time.Hour),
	}
	eff, err := trust.EvaluateDelegation(trust.DelegationRequest{
		Grant: trust.DelegationGrant{
			GrantID: "grant-intapi003", RootID: "root-intapi003",
			Delegator: "manager-1", Delegate: delegate,
			Tenant: authorityTenant, OrganizationScopeID: authorityOrg,
			Capabilities: capabilities, Resources: []string{"worker"},
			Fields: []string{"status"}, Purposes: purposes,
			NotBefore: authorityBaseNow.Add(-time.Minute), ExpiresAt: authorityBaseNow.Add(time.Hour),
			RequiredAssurance: trust.AssuranceSubstantial,
		},
		Delegator: scope, Delegate: scope,
		EvaluatedAt: authorityBaseNow, CurrentRevocationEpoch: 0,
	})
	if err != nil {
		t.Fatalf("EvaluateDelegation: %v", err)
	}
	return eff
}

// satisfiedStepUp evaluates a real step-up obligation for def.ID under
// purpose at base time, bound to principal, so the authority tests prove
// the binding rather than trusting a hand-set Satisfied flag.
func satisfiedStepUp(t *testing.T, principal *trust.Principal, defID, purpose string, at time.Time) *stepup.Obligation {
	t.Helper()
	pol, err := stepup.NewObligationPolicy(stepup.ObligationRule{
		RuleID: "test.intapi003.write", Capability: defID, Purposes: []string{purpose},
		MinRisk: stepup.RiskCritical, MinAssurance: trust.AssuranceHigh, Recency: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewObligationPolicy: %v", err)
	}
	proof := stepup.Proof{
		ID: "proof-intapi003", Tenant: authorityTenant,
		Subject: principal.Subject(), SessionRef: principal.SessionRef(),
		Assurance:  trust.AssuranceHigh,
		Action:     "capability.write",
		ProposalID: "proposal-1",
		Scopes:     []string{"scope:people.write"},
		Purpose:    purpose,
		Capability: defID,
		Risk:       stepup.RiskCritical,
		IssuedAt:   at.Add(-time.Minute),
		ExpiresAt:  at.Add(4 * time.Minute),
	}
	ob := stepup.EvaluateObligation(pol, stepup.ObligationRequest{
		Operation: stepup.Operation{
			Action: "capability.write", ProposalID: "proposal-1",
			Scopes:     []string{"scope:people.write"},
			Tenant:     authorityTenant,
			Purpose:    purpose,
			Capability: defID,
			Risk:       stepup.RiskCritical,
		},
		Stage: stepup.StageExecution,
		At:    at,
	}, principal, &proof)
	if err := ob.Err(); err != nil {
		t.Fatalf("EvaluateObligation: %v", err)
	}
	return &ob
}

// TestTodo_INTAPI_003 is the primary test: effective authority on every
// capability call is the intersection of the caller's granted scopes, the
// capability's scope and the field policy, over server-verified delegation,
// with high-risk writes gated on step-up or dual approval.
func TestTodo_INTAPI_003(t *testing.T) {
	t.Run("a human with an authorized purpose invokes a read capability", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		def := readDef()
		got := Authorize(principal, authorityPurpose, def, Authority{}, authorityBaseNow)
		if got.Decision != capability.Allow {
			t.Fatalf("decision = %s (%q), want ALLOW", got.Decision, got.Reason)
		}
		if len(got.Scopes) != 1 || got.Scopes[0] != def.AuthZScopeRef {
			t.Fatalf("scopes = %v, want exactly [%s]", got.Scopes, def.AuthZScopeRef)
		}
		if got.SubjectRef != principal.Subject() || got.Tenant != authorityTenant {
			t.Fatalf("subject/tenant = %q/%q, want the verified principal", got.SubjectRef, got.Tenant)
		}
	})

	t.Run("no verified principal authorizes nothing", func(t *testing.T) {
		got := Authorize(nil, authorityPurpose, readDef(), Authority{}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a purpose the principal is not authorized for is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		got := Authorize(principal, "other_purpose", readDef(), Authority{}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("an empty purpose keeps the pre-INTAPI-003 pass-through", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		def := readDef()
		got := Authorize(principal, "", def, Authority{}, authorityBaseNow)
		if got.Decision != capability.Allow {
			t.Fatalf("decision = %s (%q), want ALLOW: purpose resolution is the caller's duty, not this decision's", got.Decision, got.Reason)
		}
	})

	t.Run("a machine observer invokes its least-privilege read", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			kind:  trust.SubjectKindService,
			roles: []string{string(authz.RoleMachineObserver)}, purposes: []string{authorityPurpose},
		})
		def := readDef()
		got := Authorize(principal, authorityPurpose, def, Authority{
			GrantedScopes: []string{def.AuthZScopeRef},
		}, authorityBaseNow)
		if got.Decision != capability.Allow {
			t.Fatalf("decision = %s (%q), want ALLOW", got.Decision, got.Reason)
		}
		if len(got.Scopes) != 1 || got.Scopes[0] != def.AuthZScopeRef {
			t.Fatalf("scopes = %v, want exactly [%s]", got.Scopes, def.AuthZScopeRef)
		}
	})

	t.Run("a machine holding a human role is refused even with the scope presented", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			kind:  trust.SubjectKindService,
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := readDef()
		got := Authorize(principal, authorityPurpose, def, Authority{
			GrantedScopes: []string{def.AuthZScopeRef},
		}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY: a token-claimed human role grants a machine nothing", got.Decision)
		}
	})

	t.Run("a machine is refused a scope outside its grant", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			kind:  trust.SubjectKindService,
			roles: []string{string(authz.RoleMachineObserver)}, purposes: []string{authorityPurpose},
		})
		got := Authorize(principal, authorityPurpose, readDef(), Authority{
			GrantedScopes: []string{"scope:rewards.read"},
		}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a machine observer cannot reach compensation data", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			kind:  trust.SubjectKindService,
			roles: []string{string(authz.RoleMachineObserver)}, purposes: []string{authorityPurpose},
		})
		def := compDef()
		got := Authorize(principal, authorityPurpose, def, Authority{
			GrantedScopes: []string{def.AuthZScopeRef},
		}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY: the observer template grants no compensation domain", got.Decision)
		}
	})

	t.Run("token delegation references alone authorize nothing new", func(t *testing.T) {
		withRefs := authorityPrincipal(t, principalOpts{
			roles: []string{"intent_author"}, purposes: []string{authorityPurpose},
			delegrefs: []string{"delegation:forged"},
		})
		withoutRefs := authorityPrincipal(t, principalOpts{
			roles: []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		def := readDef()
		gotWith := Authorize(withRefs, authorityPurpose, def, Authority{}, authorityBaseNow)
		gotWithout := Authorize(withoutRefs, authorityPurpose, def, Authority{}, authorityBaseNow)
		if gotWith.Decision != gotWithout.Decision {
			t.Fatalf("forged refs changed the decision from %s to %s", gotWithout.Decision, gotWith.Decision)
		}
	})

	t.Run("a verified delegation covering the call authorizes it", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			subject: "delegate-1",
			roles:   []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		def := readDef()
		eff := verifiedDelegation(t, "delegate-1", []string{def.ID}, []string{authorityPurpose})
		got := Authorize(principal, authorityPurpose, def, Authority{Delegation: &eff}, authorityBaseNow)
		if got.Decision != capability.Allow {
			t.Fatalf("decision = %s (%q), want ALLOW", got.Decision, got.Reason)
		}
	})

	t.Run("a verified delegation for another delegate authorizes nothing", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			subject: "delegate-2",
			roles:   []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		eff := verifiedDelegation(t, "delegate-1", []string{readDef().ID}, []string{authorityPurpose})
		got := Authorize(principal, authorityPurpose, readDef(), Authority{Delegation: &eff}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a high-risk write without step-up or dual approval is refused", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		got := Authorize(principal, authorityPurpose, writeDef(), Authority{}, authorityBaseNow)
		if got.Decision != capability.Deny {
			t.Fatalf("decision = %s, want DENY", got.Decision)
		}
	})

	t.Run("a high-risk write with a satisfied step-up is authorized", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := writeDef()
		ob := satisfiedStepUp(t, principal, def.ID, authorityPurpose, authorityBaseNow)
		got := Authorize(principal, authorityPurpose, def, Authority{StepUp: ob}, authorityBaseNow)
		if got.Decision != capability.Allow {
			t.Fatalf("decision = %s (%q), want ALLOW", got.Decision, got.Reason)
		}
	})

	t.Run("a high-risk write with recorded dual approval is authorized", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authorityPurpose},
		})
		def := writeDef()
		got := Authorize(principal, authorityPurpose, def, Authority{DualApproval: &DualApproval{
			Tenant: authorityTenant, Capability: def.ID, ProposalRef: "proposal-1",
			Approvers: []string{"manager-1", "finance-1"}, DecidedAt: authorityBaseNow.Add(-time.Minute),
		}}, authorityBaseNow)
		if got.Decision != capability.Allow {
			t.Fatalf("decision = %s (%q), want ALLOW", got.Decision, got.Reason)
		}
	})

	t.Run("a low-risk write needs no step-up", func(t *testing.T) {
		principal := authorityPrincipal(t, principalOpts{
			roles: []string{"intent_author"}, purposes: []string{authorityPurpose},
		})
		got := Authorize(principal, authorityPurpose, lowWriteDef(), Authority{}, authorityBaseNow)
		if got.Decision != capability.Allow {
			t.Fatalf("decision = %s (%q), want ALLOW", got.Decision, got.Reason)
		}
	})
}
