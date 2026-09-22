package trust_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_INTAPI_003_DelegationNarrowing is the RED test for INTAPI-003's
// verified-delegation leg: acting on behalf of a person requires a delegation
// verified on the server and intersected with that person's CURRENT
// authority. Evaluating the grant once is not enough: when the delegator's
// authority shrinks afterwards, the effective authority must shrink with it,
// never stay at the stale, wider grant.
func TestTodo_INTAPI_003_DelegationNarrowing(t *testing.T) {
	evaluated := func(t *testing.T) trust.EffectiveAuthority {
		t.Helper()
		got, err := trust.EvaluateDelegation(trust.DelegationRequest{
			Grant: delegationGrant(), Delegator: delegationScope(),
			Delegate: delegationScope(), EvaluatedAt: baseTime, CurrentRevocationEpoch: 2,
		})
		if err != nil {
			t.Fatalf("EvaluateDelegation: %v", err)
		}
		return got
	}

	t.Run("a narrower current authority narrows the grant", func(t *testing.T) {
		eff := evaluated(t)
		current := delegationScope()
		current.Fields = []string{"owner"}
		narrowed, err := trust.NarrowToCurrentAuthority(eff, current, baseTime)
		if err != nil {
			t.Fatalf("NarrowToCurrentAuthority: %v", err)
		}
		if len(narrowed.Capabilities) != 1 || narrowed.Capabilities[0] != "case.read" {
			t.Fatalf("capabilities = %v, want [case.read]", narrowed.Capabilities)
		}
		if len(narrowed.Fields) != 0 {
			t.Fatalf("fields = %v, want empty: the grant names status, the current authority names owner", narrowed.Fields)
		}
		if narrowed.NotBefore.Before(eff.NotBefore) || narrowed.ExpiresAt.After(eff.ExpiresAt) {
			t.Fatalf("window = [%v, %v], want it within [%v, %v]",
				narrowed.NotBefore, narrowed.ExpiresAt, eff.NotBefore, eff.ExpiresAt)
		}
		if narrowed.DecisionID == "" || narrowed.DecisionID == eff.DecisionID {
			t.Fatalf("decision = %q, want a fresh decision id distinct from %q", narrowed.DecisionID, eff.DecisionID)
		}
	})

	t.Run("a disjoint current authority fails closed", func(t *testing.T) {
		eff := evaluated(t)
		current := delegationScope()
		current.Capabilities = []string{"unrelated.admin"}
		if _, err := trust.NarrowToCurrentAuthority(eff, current, baseTime); !errors.Is(err, trust.ErrDelegationExpanded) {
			t.Fatalf("error = %v, want ErrDelegationExpanded", err)
		}
	})

	t.Run("a current authority from another tenant fails closed", func(t *testing.T) {
		eff := evaluated(t)
		current := delegationScope()
		current.Tenant = "other-corp"
		if _, err := trust.NarrowToCurrentAuthority(eff, current, baseTime); !errors.Is(err, trust.ErrDelegationTenant) {
			t.Fatalf("error = %v, want ErrDelegationTenant", err)
		}
	})

	t.Run("narrowing outside the grant window fails closed", func(t *testing.T) {
		eff := evaluated(t)
		if _, err := trust.NarrowToCurrentAuthority(eff, delegationScope(), baseTime.Add(2*time.Hour)); !errors.Is(err, trust.ErrDelegationExpired) {
			t.Fatalf("error = %v, want ErrDelegationExpired", err)
		}
	})
}
