package trust_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestSkillAuthorities_IntersectKeepsKeysAndClones(t *testing.T) {
	a := trust.SkillAuthorities{"read": {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Fields: []string{"name"}, Purposes: []string{"agent.read"}}}
	b := trust.SkillAuthorities{"read": {Capabilities: []string{"worker.read", "worker.write"}, Resources: []string{"worker:1", "worker:2"}, Fields: []string{"name", "status"}, Purposes: []string{"agent.read"}}, "write": {Capabilities: []string{"worker.write"}}}
	got := trust.IntersectSkillAuthorities(a, b)
	a["read"] = trust.SkillAuthority{Resources: []string{"mutated"}}
	if len(got) != 1 || got["read"].Resources[0] != "worker:1" || len(got["read"].Capabilities) != 1 {
		t.Fatalf("intersection = %#v", got)
	}
	clone := trust.CloneSkillAuthorities(got)
	clone["read"].Resources[0] = "changed"
	if got["read"].Resources[0] != "worker:1" {
		t.Fatal("clone mutated source")
	}
}

func TestEvaluateDelegation_ScopedMissingKeyFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	base := trust.AuthorityScope{Tenant: values.TenantId("acme"), OrganizationScopeID: "org", Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Purposes: []string{"agent.read"}, Assurance: trust.AssuranceLow, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), SkillAuthorities: trust.SkillAuthorities{"read": {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Purposes: []string{"agent.read"}}}}
	g := trust.DelegationGrant{GrantID: "grant", Delegator: "user", Delegate: "agent", Tenant: base.Tenant, OrganizationScopeID: "org", Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Purposes: []string{"agent.read"}, SkillAuthorities: trust.SkillAuthorities{"write": {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Purposes: []string{"agent.read"}}}, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredAssurance: trust.AssuranceLow}
	if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: g, Delegator: base, Delegate: base, EvaluatedAt: now, CurrentRevocationEpoch: 1}); err == nil {
		t.Fatal("missing scoped key unexpectedly evaluated")
	}
}

func TestEvaluateDelegation_DecisionDigestCanonicalizesSets(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	scope := trust.AuthorityScope{Tenant: "acme", OrganizationScopeID: "org", Capabilities: []string{"b", "a"}, Resources: []string{"r2", "r1"}, Fields: []string{"f2", "f1"}, Purposes: []string{"p2", "p1"}, Assurance: trust.AssuranceLow, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), SkillAuthorities: trust.SkillAuthorities{"read": {Capabilities: []string{"b", "a"}, Resources: []string{"r2", "r1"}, Fields: []string{"f2", "f1"}, Purposes: []string{"p2", "p1"}}}}
	grant := trust.DelegationGrant{GrantID: "digest", Delegator: "user", Delegate: "agent", Tenant: "acme", OrganizationScopeID: "org", Capabilities: []string{"a", "b"}, Resources: []string{"r1", "r2"}, Fields: []string{"f1", "f2"}, Purposes: []string{"p1", "p2"}, SkillAuthorities: scope.SkillAuthorities, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredAssurance: trust.AssuranceLow, RevocationEpoch: 1}
	one, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: grant, Delegator: scope, Delegate: scope, EvaluatedAt: now, CurrentRevocationEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	scope.Capabilities = []string{"a", "b"}
	scope.Resources = []string{"r1", "r2"}
	scope.Fields = []string{"f1", "f2"}
	scope.Purposes = []string{"p1", "p2"}
	scope.SkillAuthorities["read"] = trust.SkillAuthority{Capabilities: []string{"a", "b"}, Resources: []string{"r1", "r2"}, Fields: []string{"f1", "f2"}, Purposes: []string{"p1", "p2"}}
	two, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: grant, Delegator: scope, Delegate: scope, EvaluatedAt: now, CurrentRevocationEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	if one.DecisionID != two.DecisionID {
		t.Fatalf("permuted sets changed digest: %q != %q", one.DecisionID, two.DecisionID)
	}
}
