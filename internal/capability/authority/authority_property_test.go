package authority

import (
	"math/rand"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

// TestTodo_INTAPI_003_Property proves a client never exceeds its grant: over
// generated kind/role/scope/purpose/definition combinations, every ALLOW
// emits exactly the capability's own scope and no more, a machine ALLOW
// always names a scope its server-resolved grant holds, a human ALLOW always
// names an authorized purpose, and a high-risk write ALLOW always carries a
// bound step-up or dual approval. The generator is seeded, so the proof is
// deterministic.
func TestTodo_INTAPI_003_Property(t *testing.T) {
	kinds := []trust.SubjectKind{
		trust.SubjectKindHuman, trust.SubjectKindService,
		trust.SubjectKindAgent, trust.SubjectKindIntegration,
	}
	rolePool := []string{
		"", "*", "intent_author", "promotion_operator",
		string(authz.RoleWorkerSelf), string(authz.RoleManager),
		string(authz.RoleHRPartner), string(authz.RoleCompAdmin),
		string(authz.RolePayrollManager), string(authz.RoleAuditor),
		string(authz.RoleFinancePartner), string(authz.RoleMachineObserver),
		string(authz.RoleIntegrationSync),
	}
	purposePool := []string{authorityPurpose, "other_purpose", authz.PurposeAuditReview}
	principalPurposePool := []string{"", authorityPurpose, "other_purpose", authz.PurposeAuditReview}
	defs := []capability.Definition{readDef(), compDef(), writeDef(), lowWriteDef()}

	rng := rand.New(rand.NewSource(19450303))
	var humanAllows, machineAllows int
	for i := 0; i < 2000; i++ {
		kind := kinds[rng.Intn(len(kinds))]
		roles := []string{rolePool[rng.Intn(len(rolePool))], rolePool[rng.Intn(len(rolePool))]}
		purposes := []string{principalPurposePool[rng.Intn(len(principalPurposePool))]}
		purpose := purposePool[rng.Intn(len(purposePool))]
		def := defs[rng.Intn(len(defs))]
		granted := []string{}
		if rng.Intn(2) == 0 {
			granted = append(granted, def.AuthZScopeRef)
		} else {
			granted = append(granted, "scope:unrelated.read")
		}
		principal := authorityPrincipal(t, principalOpts{
			kind: kind, roles: roles, purposes: purposes,
			subject: "property-subject",
		})
		auth := Authority{GrantedScopes: granted}
		if stepup.RiskForCapabilityClass(def.RiskClass) >= stepup.RiskCritical && def.EffectClass.IsWrite() && rng.Intn(2) == 0 {
			ob := satisfiedStepUp(t, principal, def.ID, purpose, authorityBaseNow)
			if ob != nil && rng.Intn(2) == 0 {
				auth.StepUp = ob
			} else {
				auth.DualApproval = &DualApproval{
					Tenant: authorityTenant, Capability: def.ID, ProposalRef: "proposal-1",
					Approvers: []string{"approver-1", "approver-2"}, DecidedAt: authorityBaseNow.Add(-time.Minute),
				}
			}
		}
		got := Authorize(principal, purpose, def, auth, authorityBaseNow)
		if got.Decision != capability.Allow {
			continue
		}
		if len(got.Scopes) != 1 || got.Scopes[0] != def.AuthZScopeRef {
			t.Fatalf("case %d: scopes = %v, want exactly [%s]", i, got.Scopes, def.AuthZScopeRef)
		}
		for _, s := range got.Scopes {
			if s == "*" {
				t.Fatalf("case %d: emitted a wildcard scope", i)
			}
		}
		if got.SubjectRef != principal.Subject() || got.Tenant != authorityTenant {
			t.Fatalf("case %d: subject/tenant = %q/%q, want the verified principal", i, got.SubjectRef, got.Tenant)
		}
		switch kind {
		case trust.SubjectKindHuman:
			humanAllows++
			if !principal.AuthorizesPurpose(purpose) {
				t.Fatalf("case %d: human allowed without an authorized purpose", i)
			}
		default:
			machineAllows++
			found := false
			for _, s := range granted {
				if s == def.AuthZScopeRef {
					found = true
				}
			}
			if !found {
				t.Fatalf("case %d: machine allowed a scope outside its grant", i)
			}
		}
		if def.EffectClass.IsWrite() && stepup.RiskForCapabilityClass(def.RiskClass) >= stepup.RiskCritical {
			if auth.StepUp == nil && auth.DualApproval == nil {
				t.Fatalf("case %d: high-risk write allowed with no step-up or dual approval", i)
			}
		}
	}
	if humanAllows == 0 {
		t.Fatal("no human ALLOW was generated: the property is vacuous")
	}
	if machineAllows == 0 {
		t.Fatal("no machine ALLOW was generated: the property is vacuous")
	}
	t.Logf("allows: %d human, %d machine", humanAllows, machineAllows)
}
