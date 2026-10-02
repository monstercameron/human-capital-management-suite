package authz

import (
	"reflect"
	"testing"
)

func TestPolicy_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestPolicy_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestPolicy_EffectWireAndValidity(t *testing.T) {
	cases := []struct {
		effect Effect
		wire   string
		valid  bool
	}{
		{EffectUnspecified, "EFFECT_UNSPECIFIED", false},
		{EffectAllow, "ALLOW", true},
		{EffectDenied, "DENIED", true},
		{EffectRedacted, "REDACTED", true},
		{EffectWithheld, "WITHHELD", true},
		{Effect(255), "EFFECT_UNSPECIFIED", false},
	}
	for _, tc := range cases {
		if got := tc.effect.String(); got != tc.wire {
			t.Errorf("Effect(%d).String() = %q, want %q", tc.effect, got, tc.wire)
		}
		if got := tc.effect.Valid(); got != tc.valid {
			t.Errorf("Effect(%d).Valid() = %v, want %v", tc.effect, got, tc.valid)
		}
	}
}

func TestPolicy_RoleAndGrantSelectionIsDeterministic(t *testing.T) {
	if got := rolesOf([]string{"unknown", string(RoleAuditor), string(RoleWorkerSelf), string(RoleAuditor)}); !reflect.DeepEqual(got, []RoleID{RoleWorkerSelf, RoleAuditor}) {
		t.Fatalf("rolesOf returned %v, want fixed-order recognized roles", got)
	}

	anyPurpose := PurposeGrant{AnyPurpose: true}
	if !anyPurpose.appliesTo("unregistered-purpose") {
		t.Fatal("AnyPurpose grant did not apply to an arbitrary purpose")
	}
	listed := PurposeGrant{Purposes: []string{"one", "two"}}
	if !listed.appliesTo("one") || listed.appliesTo("three") {
		t.Fatal("purpose-list grant did not enforce membership")
	}

	if got := dedupeSorted(nil); got != nil {
		t.Fatalf("dedupeSorted(nil) = %v, want nil", got)
	}
	input := []string{"b", "", "a", "b", ""}
	got := dedupeSorted(input)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("dedupeSorted(%v) = %v, want [a b]", input, got)
	}
	if !reflect.DeepEqual(input, []string{"b", "", "a", "b", ""}) {
		t.Fatalf("dedupeSorted mutated its input: %v", input)
	}
}

func TestPolicy_RolesAuthorizePurpose(t *testing.T) {
	if !RolesAuthorizePurpose([]string{string(RoleWorkerSelf)}, PurposePersonaMention) {
		t.Fatal("worker self does not authorize the explicit persona mention purpose")
	}
	if RolesAuthorizePurpose([]string{string(RoleWorkerSelf)}, "not-a-purpose") {
		t.Fatal("unknown purpose was authorized")
	}
	if RolesAuthorizePurpose([]string{string(RoleWorkerSelf)}, PurposeAuditReview) {
		t.Fatal("any-purpose core data grant established an unrelated purpose")
	}
}
