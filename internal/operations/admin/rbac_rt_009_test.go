package admin_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// rt9Principal builds a hermetic *trust.Principal with an explicit subject
// and purpose set, so the RBAC-RT-009 tests can separate "what the token
// signs" (roles) from "what the durable bindings grant" (subjects).
func rt9Principal(t *testing.T, subject string, purposes []string, roles ...string) *trust.Principal {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               "acme-corp",
		Subject:              subject,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                roles,
		Purposes:             purposes,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-rt9-" + subject,
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest-rt9-" + subject,
	})
	if err != nil {
		t.Fatalf("rt9Principal: %v", err)
	}
	return p
}

func rt9Grant(t *testing.T, approver, subject string, grantedAt, expiresAt time.Time) admin.OperatorGrant {
	t.Helper()
	g, err := admin.NewOperatorGrant(approver, subject, "rt9 reviewable justification", grantedAt, expiresAt)
	if err != nil {
		t.Fatalf("NewOperatorGrant: %v", err)
	}
	return g
}

// TestTodo_RBAC_RT_009 is the PRIMARY matrix entry: administrator and
// operator authority come from durable, reviewable bindings, and operator
// field disclosure follows the policy table rather than an unconditional
// grant. A token that merely signs the operator (or administrator) role
// authorizes nothing on its own.
func TestTodo_RBAC_RT_009(t *testing.T) {
	now := time.Now().UTC()

	t.Run("a durable grant authorizes the bound operator", func(t *testing.T) {
		grant := rt9Grant(t, "principal:rbac-admin", "principal:rbac-operator", now.Add(-time.Hour), now.Add(time.Hour))
		// The principal carries no roles at all: the binding, not the
		// token, is what authorizes.
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"})
		if err := admin.AuthorizeOperator(p, []admin.OperatorGrant{grant}, now); err != nil {
			t.Fatalf("AuthorizeOperator(bound) = %v, want nil", err)
		}
	})

	t.Run("a token-only operator claim is refused", func(t *testing.T) {
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
		if err := admin.AuthorizeOperator(p, nil, now); !errors.Is(err, admin.ErrOperatorGrantRequired) {
			t.Fatalf("AuthorizeOperator(token-only) = %v, want ErrOperatorGrantRequired", err)
		}
	})

	t.Run("an expired grant authorizes nothing", func(t *testing.T) {
		grant := rt9Grant(t, "principal:rbac-admin", "principal:rbac-operator", now.Add(-2*time.Hour), now.Add(-time.Hour))
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
		if err := admin.AuthorizeOperator(p, []admin.OperatorGrant{grant}, now); !errors.Is(err, admin.ErrOperatorGrantRequired) {
			t.Fatalf("AuthorizeOperator(expired) = %v, want ErrOperatorGrantRequired", err)
		}
	})

	t.Run("a premature grant authorizes nothing", func(t *testing.T) {
		grant := rt9Grant(t, "principal:rbac-admin", "principal:rbac-operator", now.Add(time.Hour), now.Add(2*time.Hour))
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"})
		if err := admin.AuthorizeOperator(p, []admin.OperatorGrant{grant}, now); !errors.Is(err, admin.ErrOperatorGrantRequired) {
			t.Fatalf("AuthorizeOperator(premature) = %v, want ErrOperatorGrantRequired", err)
		}
	})

	t.Run("a grant for another subject authorizes nothing", func(t *testing.T) {
		grant := rt9Grant(t, "principal:rbac-admin", "principal:someone-else", now.Add(-time.Hour), now.Add(time.Hour))
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
		if err := admin.AuthorizeOperator(p, []admin.OperatorGrant{grant}, now); !errors.Is(err, admin.ErrOperatorGrantRequired) {
			t.Fatalf("AuthorizeOperator(other subject) = %v, want ErrOperatorGrantRequired", err)
		}
	})

	t.Run("operator disclosure follows the policy table", func(t *testing.T) {
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
		fields := []people.FieldID{people.FieldWorkerNumber, people.FieldGrade, people.FieldPayZone}
		decision := admin.OperatorWorkerAuthorizationForRoles(p, []string{"comp_admin"}, fields)
		if err := decision.Validate(); err != nil {
			t.Fatalf("decision.Validate: %v", err)
		}
		if err := decision.Covers(fields); err != nil {
			t.Fatalf("decision.Covers: %v", err)
		}
		if ruling, ok := decision.RulingFor(people.FieldWorkerNumber); !ok || ruling.Effect != people.EffectAllow {
			t.Fatalf("worker_number ruling = %+v (ok=%v), want ALLOW", ruling, ok)
		}
		// Grade and pay zone are compensation-adjacent: the policy table
		// grants compensation only under compensation_review or
		// payroll_processing, never under operator_diagnostics. The legacy
		// unconditional constructor allows both, which is the RED this
		// todo closes.
		for _, f := range []people.FieldID{people.FieldGrade, people.FieldPayZone} {
			ruling, ok := decision.RulingFor(f)
			if !ok || ruling.Effect != people.EffectDeny {
				t.Fatalf("field %s ruling = %+v (ok=%v), want DENY", f, ruling, ok)
			}
			if ruling.Reason == "" {
				t.Fatalf("field %s denial carries no reason token", f)
			}
		}
		legacy := admin.OperatorWorkerAuthorization(fields)
		if ruling, _ := legacy.RulingFor(people.FieldGrade); ruling.Effect != people.EffectAllow {
			t.Fatal("legacy constructor changed: expected the unconditional ALLOW to stay for the legacy path")
		}
	})

	t.Run("every worker-state field has a reviewed domain mapping", func(t *testing.T) {
		for _, f := range people.AllFields() {
			domain, ok := admin.OperatorFieldDomain(f)
			if !ok {
				t.Fatalf("field %s has no operator disclosure mapping: add it to the reviewed table, never fail open", f)
			}
			if domain != authz.DomainCore && domain != authz.DomainCompensation && domain != authz.DomainContact {
				t.Fatalf("field %s maps to unexpected domain %q", f, domain)
			}
		}
	})
}

// TestTodo_RBAC_RT_009_Security proves token-only operator/admin claims are
// refused and separation of duties holds: the grant approver and the
// operator must differ, at issuance and at evaluation, and no unreviewable
// grant ever authorizes.
func TestTodo_RBAC_RT_009_Security(t *testing.T) {
	now := time.Now().UTC()

	t.Run("a self-approved grant is refused at issuance", func(t *testing.T) {
		_, err := admin.NewOperatorGrant("principal:rbac-operator", "principal:rbac-operator", "self approval", now.Add(-time.Hour), now.Add(time.Hour))
		if !errors.Is(err, admin.ErrSeparationOfDuties) {
			t.Fatalf("NewOperatorGrant(self-approved) = %v, want ErrSeparationOfDuties", err)
		}
	})

	t.Run("a self-approved grant is refused at evaluation", func(t *testing.T) {
		grant := admin.OperatorGrant{
			Subject:   "principal:rbac-operator",
			Approver:  "principal:rbac-operator",
			Reason:    "hand-built self approval",
			GrantedAt: now.Add(-time.Hour),
			ExpiresAt: now.Add(time.Hour),
		}
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
		if err := admin.AuthorizeOperator(p, []admin.OperatorGrant{grant}, now); !errors.Is(err, admin.ErrSeparationOfDuties) {
			t.Fatalf("AuthorizeOperator(self-approved) = %v, want ErrSeparationOfDuties", err)
		}
	})

	t.Run("case-only differences do not escape separation of duties", func(t *testing.T) {
		_, err := admin.NewOperatorGrant("PRINCIPAL:RBAC-OPERATOR", "principal:rbac-operator", "case dodge", now.Add(-time.Hour), now.Add(time.Hour))
		if !errors.Is(err, admin.ErrSeparationOfDuties) {
			t.Fatalf("NewOperatorGrant(case-dodged self-approval) = %v, want ErrSeparationOfDuties", err)
		}
	})

	t.Run("a token-only administrator claim confers no operator authority", func(t *testing.T) {
		p := rt9Principal(t, "principal:rbac-admin", []string{"compensation_review"}, "hcm_admin", "comp_admin")
		if err := admin.AuthorizeOperator(p, nil, now); !errors.Is(err, admin.ErrOperatorGrantRequired) {
			t.Fatalf("AuthorizeOperator(admin token-only) = %v, want ErrOperatorGrantRequired", err)
		}
	})

	t.Run("holding both duties on one token still requires a grant", func(t *testing.T) {
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, "hcm_admin", admin.OperatorRole)
		if err := admin.AuthorizeOperator(p, nil, now); !errors.Is(err, admin.ErrOperatorGrantRequired) {
			t.Fatalf("AuthorizeOperator(both-roles token-only) = %v, want ErrOperatorGrantRequired", err)
		}
	})

	t.Run("unreviewable grants never authorize", func(t *testing.T) {
		cases := map[string]admin.OperatorGrant{
			"empty subject":  {Approver: "a", Reason: "r", GrantedAt: now.Add(-time.Hour)},
			"empty approver": {Subject: "s", Reason: "r", GrantedAt: now.Add(-time.Hour)},
			"empty reason":   {Subject: "s", Approver: "a", GrantedAt: now.Add(-time.Hour)},
			"zero grantedAt": {Subject: "s", Approver: "a", Reason: "r"},
			"inverted window": {
				Subject: "s", Approver: "a", Reason: "r",
				GrantedAt: now.Add(time.Hour), ExpiresAt: now.Add(-time.Hour),
			},
		}
		for name, grant := range cases {
			if err := grant.Validate(); err == nil {
				t.Fatalf("Validate(%s): got nil, want an error", name)
			}
		}
		p := rt9Principal(t, "s", []string{"operator_diagnostics"})
		for name, grant := range cases {
			if err := admin.AuthorizeOperator(p, []admin.OperatorGrant{grant}, now); err == nil {
				t.Fatalf("AuthorizeOperator(%s): got nil, want an error", name)
			}
		}
	})

	t.Run("a nil principal is refused without a decision", func(t *testing.T) {
		if err := admin.AuthorizeOperator(nil, nil, now); !errors.Is(err, admin.ErrNoPrincipal) {
			t.Fatalf("AuthorizeOperator(nil) = %v, want ErrNoPrincipal", err)
		}
		decision := admin.OperatorWorkerAuthorizationForRoles(nil, []string{"comp_admin"}, []people.FieldID{people.FieldWorkerNumber})
		if decision.SubjectDisclosable {
			t.Fatal("nil-principal disclosure is disclosable, want withheld")
		}
		if ruling, ok := decision.RulingFor(people.FieldWorkerNumber); !ok || ruling.Effect != people.EffectDeny {
			t.Fatalf("nil-principal ruling = %+v (ok=%v), want DENY", ruling, ok)
		}
	})

	t.Run("an operator with no durable role discloses nothing", func(t *testing.T) {
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
		fields := []people.FieldID{people.FieldWorkerNumber, people.FieldLegalName}
		decision := admin.OperatorWorkerAuthorizationForRoles(p, nil, fields)
		for _, f := range fields {
			if ruling, ok := decision.RulingFor(f); !ok || ruling.Effect != people.EffectDeny {
				t.Fatalf("roleless field %s ruling = %+v (ok=%v), want DENY", f, ruling, ok)
			}
		}
	})

	t.Run("a purpose the principal does not authorize discloses nothing", func(t *testing.T) {
		p := rt9Principal(t, "principal:rbac-operator", []string{"self_service_view"}, admin.OperatorRole)
		decision := admin.OperatorWorkerAuthorizationForRoles(p, []string{"comp_admin"}, []people.FieldID{people.FieldWorkerNumber})
		if ruling, ok := decision.RulingFor(people.FieldWorkerNumber); !ok || ruling.Effect != people.EffectDeny {
			t.Fatalf("purpose-mismatched ruling = %+v (ok=%v), want DENY", ruling, ok)
		}
	})

	t.Run("an unmapped field is denied, never allowed", func(t *testing.T) {
		p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
		field := people.FieldID("not.a.real.field")
		decision := admin.OperatorWorkerAuthorizationForRoles(p, []string{"comp_admin"}, []people.FieldID{field})
		if ruling, ok := decision.RulingFor(field); !ok || ruling.Effect != people.EffectDeny {
			t.Fatalf("unmapped field ruling = %+v (ok=%v), want DENY", ruling, ok)
		}
	})
}

// TestTodo_RBAC_RT_009_Integration proves the durable chain end to end
// inside this package: issuance, binding evaluation and table-driven
// disclosure compose, and stripping the durable roles (the revocation
// analog) keeps the binding but withholds every field.
func TestTodo_RBAC_RT_009_Integration(t *testing.T) {
	now := time.Now().UTC()
	grant := rt9Grant(t, "principal:rbac-admin", "principal:rbac-operator", now.Add(-time.Hour), now.Add(time.Hour))
	p := rt9Principal(t, "principal:rbac-operator", []string{"operator_diagnostics"}, admin.OperatorRole)
	fields := []people.FieldID{people.FieldWorkerNumber, people.FieldLegalName, people.FieldGrade}

	if err := admin.AuthorizeOperator(p, []admin.OperatorGrant{grant}, now); err != nil {
		t.Fatalf("AuthorizeOperator: %v", err)
	}
	decision := admin.OperatorWorkerAuthorizationForRoles(p, []string{"comp_admin"}, fields)
	if err := decision.Validate(); err != nil {
		t.Fatalf("decision.Validate: %v", err)
	}
	if err := decision.Covers(fields); err != nil {
		t.Fatalf("decision.Covers: %v", err)
	}
	if ruling, _ := decision.RulingFor(people.FieldWorkerNumber); ruling.Effect != people.EffectAllow {
		t.Fatalf("worker_number = %v, want ALLOW for a comp_admin-durable operator", ruling.Effect)
	}
	if ruling, _ := decision.RulingFor(people.FieldGrade); ruling.Effect != people.EffectDeny {
		t.Fatalf("grade = %v, want DENY without a compensation purpose", ruling.Effect)
	}

	// Revocation analog: the binding still names the subject, so authority
	// still evaluates, but with no durable role every field is withheld.
	stripped := admin.OperatorWorkerAuthorizationForRoles(p, nil, fields)
	if err := stripped.Validate(); err != nil {
		t.Fatalf("stripped.Validate: %v", err)
	}
	for _, f := range fields {
		if ruling, ok := stripped.RulingFor(f); !ok || ruling.Effect != people.EffectDeny {
			t.Fatalf("stripped field %s = %+v (ok=%v), want DENY", f, ruling, ok)
		}
	}

	if got := admin.GrantsForSubject([]admin.OperatorGrant{grant}, "PRINCIPAL:RBAC-OPERATOR"); len(got) != 1 {
		t.Fatalf("GrantsForSubject matched %d grants, want 1", len(got))
	}
	if got := admin.GrantsForSubject([]admin.OperatorGrant{grant}, "principal:someone-else"); len(got) != 0 {
		t.Fatalf("GrantsForSubject matched %d grants, want 0", len(got))
	}
}
