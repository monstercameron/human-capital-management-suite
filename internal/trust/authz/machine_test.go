package authz

import (
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_INTAPI_003_MachineRoles is the RED test for INTAPI-003's machine
// leg: the policy table's role templates are all human, so a machine
// principal either matches nothing (fail closed, by accident) or, worse,
// matches a human template its token names. Either way no machine template
// grants least privilege on purpose.
func TestTodo_INTAPI_003_MachineRoles(t *testing.T) {
	t.Run("machine templates exist and grant least privilege", func(t *testing.T) {
		for _, tc := range []struct {
			role         RoleID
			allowDomains []DataDomain
		}{
			{RoleMachineObserver, []DataDomain{DomainCore, DomainContact}},
			{RoleIntegrationSync, []DataDomain{DomainCore}},
		} {
			grants, ok := MachinePolicyTable[tc.role]
			if !ok {
				t.Fatalf("MachinePolicyTable has no %q template", tc.role)
			}
			for _, domain := range []DataDomain{
				DomainCore, DomainContact, DomainCompensation, DomainTax,
				DomainBank, DomainPerformance, DomainMedical,
				DomainEmployeeRelations, DomainImmigration,
			} {
				grant, ok := grants[domain]
				want := slices.Contains(tc.allowDomains, domain)
				if !want && ok {
					t.Errorf("%s grants %s, want deny by default", tc.role, domain)
					continue
				}
				if want && (!ok || grant.Effect != EffectAllow) {
					t.Errorf("%s over %s = %+v, want an allow grant", tc.role, domain, grant)
				}
			}
		}
	})

	t.Run("role matching is gated by subject kind", func(t *testing.T) {
		if got := RolesForKind(trust.SubjectKindService, []string{string(RoleCompAdmin)}); len(got) != 0 {
			t.Fatalf("service holding comp_admin matches %v, want nothing", got)
		}
		if got := RolesForKind(trust.SubjectKindHuman, []string{string(RoleCompAdmin)}); len(got) != 1 || got[0] != RoleCompAdmin {
			t.Fatalf("human holding comp_admin matches %v, want [comp_admin]", got)
		}
		if got := RolesForKind(trust.SubjectKindService, []string{string(RoleMachineObserver)}); len(got) != 1 || got[0] != RoleMachineObserver {
			t.Fatalf("service holding machine_observer matches %v, want [machine_observer]", got)
		}
		if got := RolesForKind(trust.SubjectKindHuman, []string{string(RoleMachineObserver)}); len(got) != 0 {
			t.Fatalf("human holding machine_observer matches %v, want nothing", got)
		}
		if got := RolesForKind(trust.SubjectKindAgent, []string{string(RoleMachineObserver), string(RoleManager)}); len(got) != 0 {
			t.Fatalf("agent matches %v, want nothing", got)
		}
		if got := RolesForKind(trust.SubjectKindIntegration, []string{string(RoleIntegrationSync)}); len(got) != 1 || got[0] != RoleIntegrationSync {
			t.Fatalf("integration holding integration_sync matches %v, want [integration_sync]", got)
		}
	})

	t.Run("machine scope grants are least privilege and never wildcards", func(t *testing.T) {
		observer := GrantedCapabilityScopes(trust.SubjectKindService, []string{string(RoleMachineObserver)})
		if !slices.Contains(observer, "scope:people.read") {
			t.Fatalf("machine_observer scopes = %v, want scope:people.read", observer)
		}
		for _, s := range observer {
			if s == "*" {
				t.Fatalf("machine_observer scopes = %v, must never contain a wildcard", observer)
			}
		}
		if got := GrantedCapabilityScopes(trust.SubjectKindIntegration, []string{string(RoleIntegrationSync)}); len(got) != 0 {
			t.Fatalf("integration_sync scopes = %v, want none", got)
		}
		if got := GrantedCapabilityScopes(trust.SubjectKindService, []string{string(RoleCompAdmin)}); len(got) != 0 {
			t.Fatalf("service naming a human role scopes = %v, want none", got)
		}
	})
}
