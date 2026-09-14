package authz_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestFinancePartnerReviewsCompensationOnlyUnderCompensationReview pins the
// PROMOUX-015 finance_partner template: administrative scope without any
// relationship fact, the worker core and compensation disclosed under
// compensation_review, compensation withheld under any other purpose, and no
// grant over contact, tax, bank, performance or medical data.
func TestFinancePartnerReviewsCompensationOnlyUnderCompensationReview(t *testing.T) {
	subject := workerSubject(tenantAcme, subjectOtherID)
	principal := newPrincipal(t, principalOpts{
		subject: "hc-054-thomas-baker", roles: []string{string(authz.RoleFinancePartner)},
		purposes: []string{authz.PurposeCompensationReview, authz.PurposeSelfService},
	})
	scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant})
	if err != nil {
		t.Fatalf("ResolveAuthorizationScope: %v", err)
	}
	if scope.Effect != authz.EffectAllow || scope.Relationship != authz.RelationshipAdministrative {
		t.Fatalf("scope = %s/%s, want ALLOW/ADMINISTRATIVE", scope.Effect, scope.Relationship)
	}

	fields := []authz.FieldID{authz.FieldJobTitle, authz.FieldBaseSalary, authz.FieldWorkEmail, authz.FieldTaxID,
		authz.FieldBankAccountNumber, authz.FieldPerformanceRating, authz.FieldMedicalAccomodation}
	review, err := authz.Enforce(authz.Request{Principal: principal, Purpose: authz.PurposeCompensationReview, EffectiveAt: baseInstant, Subject: subject, Fields: fields})
	if err != nil {
		t.Fatalf("Enforce(compensation_review): %v", err)
	}
	for field, want := range map[authz.FieldID]authz.Effect{
		authz.FieldJobTitle: authz.EffectAllow, authz.FieldBaseSalary: authz.EffectAllow,
		authz.FieldWorkEmail: authz.EffectDenied, authz.FieldTaxID: authz.EffectDenied, authz.FieldBankAccountNumber: authz.EffectDenied,
		authz.FieldPerformanceRating: authz.EffectDenied, authz.FieldMedicalAccomodation: authz.EffectDenied,
	} {
		if got := review.Fields[field].Effect; got != want {
			t.Errorf("compensation_review %s = %s, want %s", field, got, want)
		}
	}

	other, err := authz.Enforce(authz.Request{Principal: principal, Purpose: authz.PurposeSelfService, EffectiveAt: baseInstant, Subject: subject, Fields: []authz.FieldID{authz.FieldBaseSalary}})
	if err != nil {
		t.Fatalf("Enforce(self_service_view): %v", err)
	}
	if got := other.Fields[authz.FieldBaseSalary].Effect; got == authz.EffectAllow {
		t.Fatalf("base salary under self_service_view = %s, want not ALLOW", got)
	}
}
