package workflow

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// TestEffectRoleVocabularyDerivesFromEffectClass proves the role vocabulary is
// keyed off capability.EffectClass rather than a parallel taxonomy: a read
// admits no role, an internal mutation admits all three, and an external or
// irreversible mutation is only ever a downstream effect.
func TestEffectRoleVocabularyDerivesFromEffectClass(t *testing.T) {
	for _, r := range []EffectRole{RoleAuthoritativeCore, RoleDownstreamEffect, RoleDerivedUpdate} {
		if !r.Valid() {
			t.Fatalf("%s is not valid", r)
		}
	}
	for _, r := range []EffectRole{"", "CACHE", "authoritative_core"} {
		if r.Valid() {
			t.Fatalf("%q is valid", r)
		}
	}
	want := map[capability.EffectClass][]EffectRole{
		capability.EffectPure:                         nil,
		capability.EffectReadOnly:                     nil,
		capability.EffectInternalMutation:             {RoleAuthoritativeCore, RoleDownstreamEffect, RoleDerivedUpdate},
		capability.EffectExternalMutation:             {RoleDownstreamEffect},
		capability.EffectIrreversibleExternalMutation: {RoleDownstreamEffect},
		"UNKNOWN_CLASS":                               nil,
	}
	for class, roles := range want {
		if got := AdmittedEffectRoles(class); !reflect.DeepEqual(got, roles) {
			t.Fatalf("AdmittedEffectRoles(%s) = %v, want %v", class, got, roles)
		}
		for _, r := range []EffectRole{RoleAuthoritativeCore, RoleDownstreamEffect, RoleDerivedUpdate} {
			admitted := false
			for _, w := range roles {
				admitted = admitted || w == r
			}
			if roleAdmitted(class, r) != admitted {
				t.Fatalf("roleAdmitted(%s, %s) = %v, want %v", class, r, !admitted, admitted)
			}
		}
	}
	plan := &CompiledWorkflow{Nodes: []CompiledNode{{ID: "a", EffectRole: RoleAuthoritativeCore}, {ID: "b"}, {ID: "c", EffectRole: RoleAuthoritativeCore}}}
	if got := plan.NodesWithRole(RoleAuthoritativeCore); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("NodesWithRole = %v", got)
	}
	if got := plan.NodesWithRole(RoleDerivedUpdate); got != nil {
		t.Fatalf("NodesWithRole(DERIVED_UPDATE) = %v, want none", got)
	}
}
