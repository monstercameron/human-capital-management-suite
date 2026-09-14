package authz_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_TRUST_009 is the TRUST-009 primary test: record, population and
// relationship authorization.
func TestTodo_TRUST_009(t *testing.T) {
	subject := workerSubject(tenantAcme, subjectOtherID)

	t.Run("worker self resolves scope over their own record without a relationship fact", func(t *testing.T) {
		self := workerSubject(tenantAcme, subjectWorkerID)
		principal := newPrincipal(t, principalOpts{subject: subjectWorkerID, roles: []string{string(authz.RoleWorkerSelf)}})
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: self, EffectiveAt: baseInstant})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectAllow || scope.Relationship != authz.RelationshipSelf {
			t.Fatalf("got Effect=%s Relationship=%s, want ALLOW/SELF", scope.Effect, scope.Relationship)
		}
	})

	t.Run("worker self does not extend to another worker's record", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{subject: subjectWorkerID, roles: []string{string(authz.RoleWorkerSelf)}})
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", scope.Effect)
		}
	})

	t.Run("manager chain fact authorizes a manager", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
			Subject:       subject,
			EffectiveAt:   baseInstant,
			Relationships: []authz.RelationshipFact{managerFact(subject)},
		})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectAllow || scope.Relationship != authz.RelationshipManagerChain {
			t.Fatalf("got Effect=%s Relationship=%s, want ALLOW/MANAGER_CHAIN", scope.Effect, scope.Relationship)
		}
		if scope.MatchedFact == nil {
			t.Error("an allow built from a relationship fact must record the matched fact")
		}
	})

	t.Run("HR partner population fact authorizes an HR partner", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleHRPartner)}})
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
			Subject:       subject,
			EffectiveAt:   baseInstant,
			Relationships: []authz.RelationshipFact{hrPartnerFact(subject)},
		})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectAllow || scope.Relationship != authz.RelationshipHRPartner {
			t.Fatalf("got Effect=%s Relationship=%s, want ALLOW/HR_PARTNER", scope.Effect, scope.Relationship)
		}
	})

	t.Run("assigned-population fact authorizes a manager or HR partner", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleHRPartner)}})
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
			Subject:       subject,
			EffectiveAt:   baseInstant,
			Relationships: []authz.RelationshipFact{assignedPopulationFact(subject)},
		})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectAllow || scope.Relationship != authz.RelationshipAssignedPopulation {
			t.Fatalf("got Effect=%s Relationship=%s, want ALLOW/ASSIGNED_POPULATION", scope.Effect, scope.Relationship)
		}
	})

	t.Run("a manager without a manager-chain fact for this worker is denied, not granted from a fact about someone else", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})
		other := workerSubject(tenantAcme, "00000000-0000-4000-8000-000000000003")
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
			Subject:       subject,
			EffectiveAt:   baseInstant,
			Relationships: []authz.RelationshipFact{managerFact(other)},
		})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", scope.Effect)
		}
	})

	t.Run("a manager-chain fact outside its effective window does not authorize", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})
		fact := managerFact(subject)
		fact.Effective = mustInterval(t, farPast, recentPast)
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
			Subject:       subject,
			EffectiveAt:   shortlyAfter,
			Relationships: []authz.RelationshipFact{fact},
		})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED for a fact that lapsed before EffectiveAt", scope.Effect)
		}
	})

	t.Run("a manager-chain fact that only starts in the future does not yet authorize", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})
		fact := managerFact(subject)
		fact.Effective = mustOpenInterval(t, farFuture)
		fact.RecordedAt = mustRecordedAt(farFuture)
		fact.KnownAt = mustKnownAt(farFuture)
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
			Subject:       subject,
			EffectiveAt:   baseInstant,
			Relationships: []authz.RelationshipFact{fact},
		})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED for a fact that does not take effect until after EffectiveAt", scope.Effect)
		}
	})

	t.Run("administrative data roles hold scope without a relationship fact", func(t *testing.T) {
		for _, role := range []authz.RoleID{authz.RoleCompAdmin, authz.RolePayrollManager, authz.RoleAuditor} {
			principal := newPrincipal(t, principalOpts{roles: []string{string(role)}})
			scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant})
			if err != nil {
				t.Fatalf("ResolveAuthorizationScope(%s): %v", role, err)
			}
			if scope.Effect != authz.EffectAllow || scope.Relationship != authz.RelationshipAdministrative {
				t.Errorf("role=%s got Effect=%s Relationship=%s, want ALLOW/ADMINISTRATIVE", role, scope.Effect, scope.Relationship)
			}
		}
	})

	t.Run("a principal outside every relationship is denied, never a partial or unauthorized row", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager), string(authz.RoleHRPartner)}})
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant})
		if err != nil {
			t.Fatalf("ResolveAuthorizationScope: %v", err)
		}
		if scope.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", scope.Effect)
		}
		if scope.MatchedFact != nil {
			t.Error("a denied scope must not carry a matched fact")
		}
	})

	t.Run("population filtering returns only authorized subjects, bounded and never partial", func(t *testing.T) {
		authorized := workerSubject(tenantAcme, "00000000-0000-4000-8000-0000000000a1")
		unauthorized := workerSubject(tenantAcme, "00000000-0000-4000-8000-0000000000a2")
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})

		out, err := authz.FilterPopulation(principal, []authz.ScopeInput{
			{Subject: authorized, EffectiveAt: baseInstant, Relationships: []authz.RelationshipFact{managerFact(authorized)}},
			{Subject: unauthorized, EffectiveAt: baseInstant},
		})
		if err != nil {
			t.Fatalf("FilterPopulation: %v", err)
		}
		if len(out) != 1 || out[0] != authorized {
			t.Fatalf("FilterPopulation = %v, want exactly [%v]", out, authorized)
		}
	})
}

// TestTodo_TRUST_009_Security is the TRUST-009 security test. A principal
// must never be granted scope from a relationship fact about a different
// subject, from a fact that has lapsed, from a fact carrying a relationship
// kind their role does not hold, or from an unvalidated (unattributed)
// fact — and every one of those denials must be indistinguishable from a
// legitimate "no relationship at all" denial.
func TestTodo_TRUST_009_Security(t *testing.T) {
	subject := workerSubject(tenantAcme, subjectOtherID)
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})

	baseline, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant})
	if err != nil {
		t.Fatalf("ResolveAuthorizationScope(baseline): %v", err)
	}
	if baseline.Effect != authz.EffectDenied {
		t.Fatalf("baseline Effect = %s, want DENIED", baseline.Effect)
	}

	attacks := map[string]authz.ScopeInput{
		"fact about a different subject": {
			Subject: subject, EffectiveAt: baseInstant,
			Relationships: []authz.RelationshipFact{managerFact(workerSubject(tenantAcme, "00000000-0000-4000-8000-000000000099"))},
		},
		"fact with no source attribution": {
			Subject: subject, EffectiveAt: baseInstant,
			Relationships: []authz.RelationshipFact{{
				Kind: authz.RelationshipManagerChain, Subject: subject,
				Effective: mustOpenIntervalUnchecked(farPast), RecordedAt: mustRecordedAt(farPast), KnownAt: mustKnownAt(farPast),
			}},
		},
		"fact of a kind the principal's role does not hold": {
			Subject: subject, EffectiveAt: baseInstant,
			Relationships: []authz.RelationshipFact{hrPartnerFact(subject)},
		},
		"fact whose knowledge time is after its recorded time": {
			Subject: subject, EffectiveAt: baseInstant,
			Relationships: []authz.RelationshipFact{{
				Kind: authz.RelationshipManagerChain, Subject: subject, Source: "organization.manager_chain.v3",
				Effective: mustOpenIntervalUnchecked(farPast), RecordedAt: mustRecordedAt(farPast), KnownAt: mustKnownAt(shortlyAfter),
			}},
		},
	}

	for name, req := range attacks {
		t.Run(name, func(t *testing.T) {
			scope, err := authz.ResolveAuthorizationScope(principal, req)
			if err != nil {
				t.Fatalf("ResolveAuthorizationScope: %v", err)
			}
			if scope.Effect != authz.EffectDenied {
				t.Fatalf("Effect = %s, want DENIED", scope.Effect)
			}
			if scope.Reason != baseline.Reason || scope.RuleID != baseline.RuleID {
				t.Errorf("denial reason/rule (%s/%s) differs from the baseline no-relationship denial (%s/%s); this could let a caller infer which relationship almost matched",
					scope.Reason, scope.RuleID, baseline.Reason, baseline.RuleID)
			}
		})
	}
}

// TestTodo_TRUST_009_Mutation proves that scope resolution is genuinely
// bitemporal and source-attributed: mutating the effective window, the
// recorded/known times, or the source of an otherwise-authorizing fact
// changes the outcome, rather than the fields being accepted and ignored.
func TestTodo_TRUST_009_Mutation(t *testing.T) {
	subject := workerSubject(tenantAcme, subjectOtherID)
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})
	base := managerFact(subject)

	baseline, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
		Subject: subject, EffectiveAt: baseInstant, Relationships: []authz.RelationshipFact{base},
	})
	if err != nil {
		t.Fatalf("ResolveAuthorizationScope(base): %v", err)
	}
	if baseline.Effect != authz.EffectAllow {
		t.Fatalf("baseline Effect = %s, want ALLOW", baseline.Effect)
	}

	mutations := map[string]func(*authz.RelationshipFact){
		"effective window closed before EffectiveAt": func(f *authz.RelationshipFact) { f.Effective = mustInterval(t, farPast, recentPast) },
		"source cleared": func(f *authz.RelationshipFact) { f.Source = "" },
		"kind changed to a kind the role does not hold": func(f *authz.RelationshipFact) {
			f.Kind = authz.RelationshipHRPartner
		},
		"subject changed": func(f *authz.RelationshipFact) {
			f.Subject = workerSubject(tenantAcme, "00000000-0000-4000-8000-000000000042")
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			fact := base
			mutate(&fact)
			scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
				Subject: subject, EffectiveAt: baseInstant, Relationships: []authz.RelationshipFact{fact},
			})
			if err != nil {
				t.Fatalf("ResolveAuthorizationScope: %v", err)
			}
			if scope.Effect == authz.EffectAllow {
				t.Errorf("mutating %q left the fact able to authorize access", name)
			}
		})
	}
}

func TestScope_ValidationSelectionAndErrors(t *testing.T) {
	for _, tc := range []struct {
		kind authz.RelationshipKind
		wire string
	}{
		{authz.RelationshipUnspecified, "RELATIONSHIP_UNSPECIFIED"},
		{authz.RelationshipSelf, "SELF"},
		{authz.RelationshipManagerChain, "MANAGER_CHAIN"},
		{authz.RelationshipHRPartner, "HR_PARTNER"},
		{authz.RelationshipAssignedPopulation, "ASSIGNED_POPULATION"},
		{authz.RelationshipAdministrative, "ADMINISTRATIVE"},
		{authz.RelationshipKind(255), "RELATIONSHIP_UNSPECIFIED"},
	} {
		if got := tc.kind.String(); got != tc.wire {
			t.Errorf("RelationshipKind(%d).String() = %q, want %q", tc.kind, got, tc.wire)
		}
	}

	subject := workerSubject(tenantAcme, subjectOtherID)
	valid := managerFact(subject)
	invalid := []struct {
		name string
		fact authz.RelationshipFact
	}{
		{"unknown kind", func() authz.RelationshipFact { f := valid; f.Kind = authz.RelationshipSelf; return f }()},
		{"invalid subject", func() authz.RelationshipFact { f := valid; f.Subject = workerSubject("", subjectOtherID); return f }()},
		{"missing source", func() authz.RelationshipFact { f := valid; f.Source = ""; return f }()},
		{"invalid effective interval", func() authz.RelationshipFact { f := valid; f.Effective = values.EffectiveInterval{}; return f }()},
		{"known after recorded", func() authz.RelationshipFact { f := valid; f.KnownAt = mustKnownAt(shortlyAfter); return f }()},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fact.Validate(); !errors.Is(err, authz.ErrInvalidPolicyInput) {
				t.Fatalf("RelationshipFact.Validate() = %v, want ErrInvalidPolicyInput", err)
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid RelationshipFact.Validate() = %v", err)
	}

	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}})
	if _, err := authz.ResolveAuthorizationScope(nil, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant}); !errors.Is(err, authz.ErrInvalidPolicyInput) {
		t.Fatalf("ResolveAuthorizationScope(nil) = %v, want ErrInvalidPolicyInput", err)
	}
	if _, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: values.EntityRef{}, EffectiveAt: baseInstant}); !errors.Is(err, authz.ErrInvalidPolicyInput) {
		t.Fatalf("ResolveAuthorizationScope(invalid subject) = %v, want ErrInvalidPolicyInput", err)
	}
	if _, err := authz.FilterPopulation(nil, []authz.ScopeInput{{Subject: subject, EffectiveAt: baseInstant}}); !errors.Is(err, authz.ErrInvalidPolicyInput) {
		t.Fatalf("FilterPopulation(nil) = %v, want ErrInvalidPolicyInput", err)
	}

	late := valid
	late.Source = "z-source"
	early := valid
	early.Source = "a-source"
	scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{
		Subject: subject, EffectiveAt: baseInstant, Relationships: []authz.RelationshipFact{late, early},
	})
	if err != nil {
		t.Fatalf("ResolveAuthorizationScope with two valid facts: %v", err)
	}
	if scope.Effect != authz.EffectAllow || scope.MatchedFact == nil || scope.MatchedFact.Source != "a-source" {
		t.Fatalf("selected scope = %+v, want deterministic lowest source", scope)
	}
}
