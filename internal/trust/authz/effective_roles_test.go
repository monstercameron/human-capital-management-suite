package authz_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestEffectiveRolesOverrideGovernsScope proves the server-resolved set
// replaces credential claims at the scope stage in both directions: a
// credential administrator reduced to worker_self by durable assignments
// loses the administrative grant, and a credential worker_self elevated by
// durable assignments gains it.
func TestEffectiveRolesOverrideGovernsScope(t *testing.T) {
	subject := workerSubject(tenantAcme, subjectOtherID)
	admin := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposeCompensationReview}})
	worker := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})

	legacy, err := authz.ResolveAuthorizationScope(admin, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant})
	if err != nil || legacy.Effect != authz.EffectAllow {
		t.Fatalf("credential comp_admin scope = %v, %v, want ALLOW", legacy, err)
	}
	revoked, err := authz.ResolveAuthorizationScope(admin, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant, EffectiveRoles: []string{"worker_self"}})
	if err != nil {
		t.Fatalf("ResolveAuthorizationScope: %v", err)
	}
	if revoked.Effect != authz.EffectDenied {
		t.Fatalf("durable worker_self scope = %s, want DENIED despite the comp_admin credential", revoked.Effect)
	}
	granted, err := authz.ResolveAuthorizationScope(worker, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant, EffectiveRoles: []string{"comp_admin"}})
	if err != nil {
		t.Fatalf("ResolveAuthorizationScope: %v", err)
	}
	if granted.Effect != authz.EffectAllow || granted.Relationship != authz.RelationshipAdministrative {
		t.Fatalf("durable comp_admin scope = %s/%s, want ALLOW/ADMINISTRATIVE despite the worker_self credential", granted.Effect, granted.Relationship)
	}
	emptied, err := authz.ResolveAuthorizationScope(admin, authz.ScopeInput{Subject: subject, EffectiveAt: baseInstant, EffectiveRoles: []string{}})
	if err != nil {
		t.Fatalf("ResolveAuthorizationScope: %v", err)
	}
	if emptied.Effect != authz.EffectDenied {
		t.Fatalf("empty durable set scope = %s, want DENIED (never fall back to the credential)", emptied.Effect)
	}
}

// TestEffectiveRolesOverrideGovernsFields proves the field stage reads the
// resolved set: the revoked administrator's compensation ruling denies while
// the legacy credential path still allows, pinning the compat behavior for
// callers with no role store.
func TestEffectiveRolesOverrideGovernsFields(t *testing.T) {
	admin := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposeCompensationReview}})
	fields := []authz.FieldID{authz.FieldBaseSalary}

	legacy, err := authz.ResolveFields(admin, authz.PurposeCompensationReview, fields, nil)
	if err != nil || legacy.Rulings[authz.FieldBaseSalary].Effect != authz.EffectAllow {
		t.Fatalf("credential comp_admin ruling = %+v, %v, want ALLOW", legacy.Rulings[authz.FieldBaseSalary], err)
	}
	revoked, err := authz.ResolveFieldsWithRoles(admin, []string{"worker_self"}, authz.PurposeCompensationReview, fields, nil)
	if err != nil {
		t.Fatalf("ResolveFieldsWithRoles: %v", err)
	}
	if revoked.Rulings[authz.FieldBaseSalary].Effect != authz.EffectDenied {
		t.Fatalf("durable worker_self ruling = %s, want DENIED despite the comp_admin credential", revoked.Rulings[authz.FieldBaseSalary].Effect)
	}
	if _, err := authz.ResolveFieldsWithRoles(nil, []string{"worker_self"}, authz.PurposeCompensationReview, fields, nil); err == nil {
		t.Error("nil principal produced a ruling, want an error")
	}
}

// TestEffectiveRolesOverrideGovernsDirectory proves the directory point
// masks pay for the revoked administrator while keeping the legacy
// credential path for callers with no role store.
func TestEffectiveRolesOverrideGovernsDirectory(t *testing.T) {
	admin := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposeCompensationReview}})
	other := authz.DirectorySubject{}

	legacy, err := authz.ResolveDirectoryDisclosure(admin, "", other)
	if err != nil || legacy.Pay.Effect != authz.EffectAllow {
		t.Fatalf("credential comp_admin pay = %+v, %v, want ALLOW", legacy.Pay, err)
	}
	revoked, err := authz.ResolveDirectoryDisclosureWithRoles(admin, []string{"worker_self"}, "", other)
	if err != nil {
		t.Fatalf("ResolveDirectoryDisclosureWithRoles: %v", err)
	}
	if revoked.Pay.Effect != authz.EffectDenied || revoked.LegalName.Effect != authz.EffectDenied {
		t.Fatalf("durable worker_self disclosure = %s/%s, want DENIED/DENIED", revoked.Pay.Effect, revoked.LegalName.Effect)
	}
	if _, err := authz.ResolveDirectoryDisclosureWithRoles(nil, []string{"worker_self"}, "", other); err == nil {
		t.Error("nil principal produced a ruling, want an error")
	}
}

// TestEffectiveRolesOverrideGovernsEnforceAndDigest proves Enforce composes
// the override through both stages and binds it into the evidence digest: a
// revoked administrator's subject is not disclosable, the digest differs
// from the credential decision, and identical overrides digest identically.
func TestEffectiveRolesOverrideGovernsEnforceAndDigest(t *testing.T) {
	subject := workerSubject(tenantAcme, subjectOtherID)
	admin := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposeCompensationReview}})
	fields := []authz.FieldID{authz.FieldBaseSalary}

	legacy, err := authz.Enforce(authz.Request{Principal: admin, Purpose: authz.PurposeCompensationReview, EffectiveAt: baseInstant, Subject: subject, Fields: fields})
	if err != nil || !legacy.SubjectDisclosable {
		t.Fatalf("credential Enforce disclosable = %v, %v, want true", legacy.SubjectDisclosable, err)
	}
	revokedReq := authz.Request{Principal: admin, EffectiveRoles: []string{"worker_self"}, Purpose: authz.PurposeCompensationReview, EffectiveAt: baseInstant, Subject: subject, Fields: fields}
	revoked, err := authz.Enforce(revokedReq)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if revoked.SubjectDisclosable {
		t.Fatal("durable worker_self subject is disclosable, want refused despite the comp_admin credential")
	}
	if err := revoked.Validate(); err != nil {
		t.Fatalf("revoked decision fails Validate: %v", err)
	}
	if revoked.InputsDigest == legacy.InputsDigest {
		t.Error("the durable override left the evidence digest unchanged")
	}
	again, err := authz.Enforce(revokedReq)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if again.InputsDigest != revoked.InputsDigest {
		t.Error("identical overrides produced different evidence digests")
	}
}
