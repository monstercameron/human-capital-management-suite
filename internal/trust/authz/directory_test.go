package authz_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestResolveDirectoryDisclosure_ManagerChainScopesManagerPay proves the
// manager compensation grant discloses only within the reporting line: the
// same grant and purpose allow a direct report and deny a same-unit peer and
// an upward manager, with a stable chain-required rule on denial.
func TestResolveDirectoryDisclosure_ManagerChainScopesManagerPay(t *testing.T) {
	manager := newPrincipal(t, principalOpts{roles: []string{"manager"}, purposes: []string{authz.PurposeCompensationReview}})

	chain, err := authz.ResolveDirectoryDisclosure(manager, "", authz.DirectorySubject{InManagerChain: true})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(chain): %v", err)
	}
	if chain.Pay.Effect != authz.EffectAllow {
		t.Errorf("chain pay = %s, want ALLOW", chain.Pay.Effect)
	}
	if chain.LegalName.Effect != authz.EffectAllow || chain.ManagerLinkage.Effect != authz.EffectAllow {
		t.Errorf("chain identity/linkage = %s/%s, want ALLOW/ALLOW", chain.LegalName.Effect, chain.ManagerLinkage.Effect)
	}

	for _, subject := range []authz.DirectorySubject{{}, {InManagerChain: false}} {
		denied, err := authz.ResolveDirectoryDisclosure(manager, "", subject)
		if err != nil {
			t.Fatalf("ResolveDirectoryDisclosure(%+v): %v", subject, err)
		}
		if denied.Pay.Effect != authz.EffectDenied || denied.Pay.RuleID != "p1a.directory.manager_chain_required" {
			t.Errorf("peer pay = %s/%s, want DENIED/p1a.directory.manager_chain_required", denied.Pay.Effect, denied.Pay.RuleID)
		}
		if denied.LegalName.Effect != authz.EffectDenied {
			t.Errorf("peer legal name = %s, want DENIED", denied.LegalName.Effect)
		}
		if denied.ManagerLinkage.Effect != authz.EffectDenied {
			t.Errorf("peer linkage = %s, want DENIED", denied.ManagerLinkage.Effect)
		}
		if denied.PolicyVersion != authz.PolicyVersion || denied.Purpose != authz.PurposeCompensationReview {
			t.Errorf("disclosure carries %s/%s, want policy %s purpose compensation_review",
				denied.PolicyVersion, denied.Purpose, authz.PolicyVersion)
		}
	}
}

// TestResolveDirectoryDisclosure_SelfAndAdministrativeReach proves self,
// administrative and legacy administrator-token viewers keep the grant
// table's answer: self sees own pay, comp_admin sees pay, and the hcm_admin
// token discloses under its mapped comp_admin grant.
func TestResolveDirectoryDisclosure_SelfAndAdministrativeReach(t *testing.T) {
	self := newPrincipal(t, principalOpts{roles: []string{"worker_self"}, purposes: []string{authz.PurposeSelfService}})
	got, err := authz.ResolveDirectoryDisclosure(self, "", authz.DirectorySubject{Self: true})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(self): %v", err)
	}
	if got.Pay.Effect != authz.EffectAllow || got.LegalName.Effect != authz.EffectAllow {
		t.Errorf("self = %s/%s, want ALLOW/ALLOW", got.Pay.Effect, got.LegalName.Effect)
	}

	compAdmin := newPrincipal(t, principalOpts{roles: []string{"comp_admin"}, purposes: []string{authz.PurposeCompensationReview}})
	got, err = authz.ResolveDirectoryDisclosure(compAdmin, "", authz.DirectorySubject{})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(comp_admin): %v", err)
	}
	if got.Pay.Effect != authz.EffectAllow || got.Pay.RuleID != "p1a.comp_admin.compensation" {
		t.Errorf("comp_admin pay = %s/%s, want ALLOW/p1a.comp_admin.compensation", got.Pay.Effect, got.Pay.RuleID)
	}

	hcmAdmin := newPrincipal(t, principalOpts{roles: []string{"hcm_admin"}, purposes: []string{authz.PurposeCompensationReview}})
	got, err = authz.ResolveDirectoryDisclosure(hcmAdmin, "", authz.DirectorySubject{})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(hcm_admin): %v", err)
	}
	if got.Pay.Effect != authz.EffectAllow || got.Pay.RuleID != "p1a.comp_admin.compensation" {
		t.Errorf("hcm_admin pay = %s/%s, want ALLOW/p1a.comp_admin.compensation", got.Pay.Effect, got.Pay.RuleID)
	}
}

// TestResolveDirectoryDisclosure_DenyByDefault proves the closed world:
// worker_self compensation is self-only, an unrecognized role and a missing
// purpose disclose nothing, and a nil principal is a programming error, not
// a ruling.
func TestResolveDirectoryDisclosure_DenyByDefault(t *testing.T) {
	worker := newPrincipal(t, principalOpts{roles: []string{"worker_self"}, purposes: []string{authz.PurposeSelfService}})
	got, err := authz.ResolveDirectoryDisclosure(worker, "", authz.DirectorySubject{})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(worker_self, other): %v", err)
	}
	if got.Pay.Effect != authz.EffectDenied || got.LegalName.Effect != authz.EffectDenied {
		t.Errorf("worker_self other = %s/%s, want DENIED/DENIED", got.Pay.Effect, got.LegalName.Effect)
	}

	stranger := newPrincipal(t, principalOpts{roles: []string{"intent_author"}, purposes: []string{"hcm_operations"}})
	got, err = authz.ResolveDirectoryDisclosure(stranger, "", authz.DirectorySubject{})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(unknown role): %v", err)
	}
	if got.Pay.Effect != authz.EffectDenied {
		t.Errorf("unknown role pay = %s, want DENIED", got.Pay.Effect)
	}
	if got.ManagerLinkage.Effect != authz.EffectAllow {
		t.Errorf("unknown role linkage = %s, want ALLOW (legacy row-visible projection)", got.ManagerLinkage.Effect)
	}

	purposeless := newPrincipal(t, principalOpts{roles: []string{"manager"}, purposes: nil})
	got, err = authz.ResolveDirectoryDisclosure(purposeless, "", authz.DirectorySubject{InManagerChain: true})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(no purpose): %v", err)
	}
	if got.Pay.Effect != authz.EffectDenied {
		t.Errorf("purposeless pay = %s, want DENIED", got.Pay.Effect)
	}

	if _, err := authz.ResolveDirectoryDisclosure(nil, "", authz.DirectorySubject{}); err == nil {
		t.Error("nil principal produced a ruling, want an error")
	}
}

// TestResolveDirectoryDisclosure_AuditorIsRedactedNeverRaw proves the
// auditor's compensation ruling stays REDACTED with its logging obligation
// through the directory point, so the serializer masks rather than copies.
func TestResolveDirectoryDisclosure_AuditorIsRedactedNeverRaw(t *testing.T) {
	auditor := newPrincipal(t, principalOpts{roles: []string{"auditor"}, purposes: []string{authz.PurposeAuditReview}})
	got, err := authz.ResolveDirectoryDisclosure(auditor, "", authz.DirectorySubject{})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(auditor): %v", err)
	}
	if got.Pay.Effect != authz.EffectRedacted {
		t.Fatalf("auditor pay = %s, want REDACTED", got.Pay.Effect)
	}
	if len(got.Pay.Obligations) == 0 {
		t.Error("redacted pay carries no obligation")
	}
}

// TestResolveDirectoryDisclosure_HRPartnerStaysRoleWide documents the
// RBAC-RT-007 boundary: without relationship facts the hr_partner grant
// applies role-wide, so this point must not narrow it on its own.
func TestResolveDirectoryDisclosure_HRPartnerStaysRoleWide(t *testing.T) {
	partner := newPrincipal(t, principalOpts{roles: []string{"hr_partner"}, purposes: []string{authz.PurposeCompensationReview}})
	got, err := authz.ResolveDirectoryDisclosure(partner, "", authz.DirectorySubject{})
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosure(hr_partner): %v", err)
	}
	if got.Pay.Effect != authz.EffectAllow {
		t.Errorf("hr_partner pay = %s, want ALLOW until RBAC-RT-007", got.Pay.Effect)
	}
}
