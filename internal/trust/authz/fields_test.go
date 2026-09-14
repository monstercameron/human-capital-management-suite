package authz_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_TRUST_010 is the TRUST-010 primary test: field-level and purpose
// authorization.
func TestTodo_TRUST_010(t *testing.T) {
	t.Run("compensation field is denied before any purpose is declared", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})
		decision, err := authz.ResolveFields(principal, "", []authz.FieldID{authz.FieldBaseSalary}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if decision.Rulings[authz.FieldBaseSalary].Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED with no purpose declared", decision.Rulings[authz.FieldBaseSalary].Effect)
		}
	})

	t.Run("compensation field is denied under an incompatible purpose", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{
			roles:    []string{string(authz.RoleManager)},
			purposes: []string{authz.PurposePerformanceReview},
		})
		decision, err := authz.ResolveFields(principal, authz.PurposePerformanceReview, []authz.FieldID{authz.FieldBaseSalary}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		ruling := decision.Rulings[authz.FieldBaseSalary]
		if ruling.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED: manager's compensation grant is scoped to compensation_review, not performance_review", ruling.Effect)
		}
	})

	t.Run("medical field is denied for a manager regardless of purpose", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{
			roles:    []string{string(authz.RoleManager)},
			purposes: []string{authz.PurposeAccommodationCase},
		})
		decision, err := authz.ResolveFields(principal, authz.PurposeAccommodationCase, []authz.FieldID{authz.FieldMedicalAccomodation}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if decision.Rulings[authz.FieldMedicalAccomodation].Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED: a manager has no medical grant at all", decision.Rulings[authz.FieldMedicalAccomodation].Effect)
		}
	})

	t.Run("bank field is denied for HR partner: only payroll-processing roles reach bank data", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{
			roles:    []string{string(authz.RoleHRPartner)},
			purposes: []string{authz.PurposePayrollProcessing},
		})
		decision, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing, []authz.FieldID{authz.FieldBankAccountNumber}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if decision.Rulings[authz.FieldBankAccountNumber].Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", decision.Rulings[authz.FieldBankAccountNumber].Effect)
		}
	})

	t.Run("case notes field is denied for worker self", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeCaseManagement}})
		decision, err := authz.ResolveFields(principal, authz.PurposeCaseManagement, []authz.FieldID{authz.FieldCaseNotes}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if decision.Rulings[authz.FieldCaseNotes].Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", decision.Rulings[authz.FieldCaseNotes].Effect)
		}
	})

	t.Run("comp admin is granted compensation and bank fields under payroll processing", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{
			roles:    []string{string(authz.RoleCompAdmin)},
			purposes: []string{authz.PurposePayrollProcessing},
		})
		decision, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing,
			[]authz.FieldID{authz.FieldBaseSalary, authz.FieldBankAccountNumber, authz.FieldTaxID}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		for _, f := range []authz.FieldID{authz.FieldBaseSalary, authz.FieldBankAccountNumber, authz.FieldTaxID} {
			if decision.Rulings[f].Effect != authz.EffectAllow {
				t.Errorf("field %s Effect = %s, want ALLOW", f, decision.Rulings[f].Effect)
			}
		}
	})

	t.Run("payroll manager can review compensation without receiving unrelated sensitive domains", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{
			roles: []string{string(authz.RolePayrollManager)}, purposes: []string{authz.PurposeCompensationReview},
		})
		decision, err := authz.ResolveFields(principal, authz.PurposeCompensationReview,
			[]authz.FieldID{authz.FieldBaseSalary, authz.FieldBankAccountNumber}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if decision.Rulings[authz.FieldBaseSalary].Effect != authz.EffectAllow {
			t.Fatalf("base salary = %s, want ALLOW", decision.Rulings[authz.FieldBaseSalary].Effect)
		}
		if decision.Rulings[authz.FieldBankAccountNumber].Effect != authz.EffectDenied {
			t.Fatalf("bank account = %s, want DENIED outside payroll_processing", decision.Rulings[authz.FieldBankAccountNumber].Effect)
		}
	})

	t.Run("auditor receives redacted values, never raw, and carries an obligation", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleAuditor)}, purposes: []string{authz.PurposeAuditReview}})
		decision, err := authz.ResolveFields(principal, authz.PurposeAuditReview, []authz.FieldID{authz.FieldBaseSalary}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		ruling := decision.Rulings[authz.FieldBaseSalary]
		if ruling.Effect != authz.EffectRedacted {
			t.Fatalf("Effect = %s, want REDACTED", ruling.Effect)
		}
		if len(ruling.Obligations) == 0 {
			t.Error("a redacted ruling must carry at least one obligation")
		}
	})

	t.Run("core and contact fields are allowed to any role holder regardless of purpose", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{"anything_at_all"}})
		decision, err := authz.ResolveFields(principal, "anything_at_all", []authz.FieldID{authz.FieldWorkerNumber, authz.FieldWorkEmail}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		for _, f := range []authz.FieldID{authz.FieldWorkerNumber, authz.FieldWorkEmail} {
			if decision.Rulings[f].Effect != authz.EffectAllow {
				t.Errorf("field %s Effect = %s, want ALLOW", f, decision.Rulings[f].Effect)
			}
		}
	})

	t.Run("an unknown field is denied, not silently dropped", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})
		decision, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing, []authz.FieldID{"not.a.real.field"}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if decision.Rulings["not.a.real.field"].Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED for an unregistered field", decision.Rulings["not.a.real.field"].Effect)
		}
	})

	t.Run("a purpose the principal is not authorized for denies every field, even core", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposePerformanceReview}})
		decision, err := authz.ResolveFields(principal, "unauthorized_purpose", []authz.FieldID{authz.FieldWorkerNumber}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if decision.Rulings[authz.FieldWorkerNumber].Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", decision.Rulings[authz.FieldWorkerNumber].Effect)
		}
	})

	t.Run("Covers reports the first field a decision is silent about", func(t *testing.T) {
		decision := authz.FieldDecision{Rulings: map[authz.FieldID]authz.FieldRuling{authz.FieldWorkerNumber: {Effect: authz.EffectAllow}}}
		if err := decision.Covers([]authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary}); err == nil {
			t.Error("Covers succeeded despite a missing ruling")
		}
	})
}

func TestFields_InvalidInputsAndClosedMasks(t *testing.T) {
	if _, err := authz.ResolveFields(nil, authz.PurposeSelfService, []authz.FieldID{authz.FieldWorkerNumber}, nil); !errors.Is(err, authz.ErrInvalidPolicyInput) {
		t.Fatalf("ResolveFields(nil) error = %v, want ErrInvalidPolicyInput", err)
	}

	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})
	decision, err := authz.ResolveFields(principal, authz.PurposeSelfService, nil, nil)
	if err != nil {
		t.Fatalf("ResolveFields with no fields: %v", err)
	}
	if decision.Purpose != authz.PurposeSelfService || decision.PolicyVersion != authz.PolicyVersion {
		t.Fatalf("decision metadata = %+v, want purpose and policy version", decision)
	}
	if err := decision.Covers(nil); err != nil {
		t.Fatalf("Covers(nil): %v", err)
	}
	if err := decision.Covers([]authz.FieldID{authz.FieldWorkerNumber}); !errors.Is(err, authz.ErrInvalidPolicyInput) {
		t.Fatalf("Covers on a silent decision = %v, want ErrInvalidPolicyInput", err)
	}

	// A role unknown to this policy is inert even when the principal is valid.
	unknown := newPrincipal(t, principalOpts{roles: []string{"role-from-another-policy"}, purposes: []string{authz.PurposeSelfService}})
	denied, err := authz.ResolveFields(unknown, authz.PurposeSelfService, []authz.FieldID{authz.FieldWorkerNumber}, nil)
	if err != nil {
		t.Fatalf("ResolveFields with an unknown role: %v", err)
	}
	if ruling := denied.Rulings[authz.FieldWorkerNumber]; ruling.Effect != authz.EffectDenied || ruling.Reason != "no_grant_for_domain" {
		t.Fatalf("unknown-role ruling = %+v, want deny-by-default", ruling)
	}
}
