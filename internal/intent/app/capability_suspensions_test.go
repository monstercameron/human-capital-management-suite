// TestTodo_WF_RUN_039_ServedSuspension closes the 2026-09-24 reachability
// audit: the served cell capability gateway consults tenant-resolved bypass
// obligations, so an overdue obligation suspends the served capability.
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func wfRun039Principal(t *testing.T, tenant values.TenantId) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: "operator-1", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{"operator"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-1",
		IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest",
	})
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	return p
}

func wfRun039Overdue(t *testing.T, now time.Time) operator.Obligation {
	t.Helper()
	return operator.Obligation{
		ID: "obligation-1", Tenant: "acme", Kind: operator.KindWorkflowRepair, Family: operator.FamilyRepair,
		Scope:          operator.Scope{Resource: "workflow-instance", IDs: []string{"instance-1"}},
		Operator:       "operator-1",
		Approver:       "approver-1",
		AuthorityKind:  "JIT",
		AuthorityRef:   "grant-1",
		IdempotencyKey: "bypass-1",
		RequestDigest:  "sha256:request",
		Bypassed:       []string{operator.BypassJITAuthority},
		BypassReason:   "production is down",
		RecordedAt:     now.Add(-48 * time.Hour),
		DueAt:          now.Add(-24 * time.Hour),
	}.Sealed()
}

func wfRun039Definition(id string) capability.Definition {
	schema := func(name string) capability.SchemaRef {
		return capability.SchemaRef{SchemaID: id + "." + name, Version: 1, ProtobufFullName: "hcmnext.test.v1." + name}
	}
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: "test",
		RequestSchema: schema("request"), ResponseSchema: schema("response"), ErrorSchema: schema("error"),
		// Read-only so the admit path runs the handler: the suspension
		// check precedes the effect check, so the refusal proof holds for
		// any effect class.
		EffectClass:          capability.EffectReadOnly,
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:test",
		LegalBasisRef:        "legal.test.v1",
		EntitlementRef:       "entitlement.test.v1",
		SLOClassRef:          "slo.test.v1",
		TestRef:              "test:TestTodo_WF_RUN_039_ServedSuspension",
	}
}

func wfRun039Gateway(t *testing.T, src capability.SuspensionSource, ids ...string) *capability.Gateway {
	t.Helper()
	registry := capability.NewRegistry()
	for _, id := range ids {
		if err := registry.Register(wfRun039Definition(id), func(context.Context, any) (any, error) { return "ran", nil }); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	var options []capability.GatewayOption
	if src != nil {
		options = append(options, capability.WithSuspensions(src))
	}
	return capability.NewGateway(registry, NewMemoryEvidenceSink(), options...)
}

func wfRun039Invoke(ctx context.Context, gateway *capability.Gateway, id string) (any, error) {
	result, err := gateway.Invoke(ctx, capability.InvokeRequest{
		Capability:    capability.Key{ID: id, Version: 1},
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:test"}},
	})
	if err != nil {
		return nil, err
	}
	return result.Response, nil
}

func TestTodo_WF_RUN_039_ServedSuspension(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	ctx := trust.WithPrincipal(context.Background(), wfRun039Principal(t, "acme"))

	t.Run("overdue_obligation_suspends_the_served_capability", func(t *testing.T) {
		journal := operator.NewMemoryJournal()
		if err := journal.RecordObligation(ctx, wfRun039Overdue(t, now)); err != nil {
			t.Fatalf("record obligation: %v", err)
		}
		src, err := suspensionsOverStore(journal, func() time.Time { return now })
		if err != nil {
			t.Fatalf("suspension source: %v", err)
		}
		gateway := wfRun039Gateway(t, src, "workflow.repair.door")
		if _, err := wfRun039Invoke(ctx, gateway, "workflow.repair.door"); err == nil {
			t.Fatal("overdue obligation did not suspend the served capability")
		} else {
			var gatewayErr *capability.GatewayError
			if !errors.As(err, &gatewayErr) || gatewayErr.Code != capability.CodeCapabilitySuspended {
				t.Fatalf("refusal is not a suspension: %v", err)
			}
		}
	})

	t.Run("no_overdue_obligation_admits", func(t *testing.T) {
		src, err := suspensionsOverStore(operator.NewMemoryJournal(), func() time.Time { return now })
		if err != nil {
			t.Fatalf("suspension source: %v", err)
		}
		gateway := wfRun039Gateway(t, src, "workflow.repair.door")
		response, err := wfRun039Invoke(ctx, gateway, "workflow.repair.door")
		if err != nil {
			t.Fatalf("clean family refused: %v", err)
		}
		if response != "ran" {
			t.Fatalf("handler did not run: %v", response)
		}
	})

	t.Run("ungoverned_capabilities_never_consult_tenant", func(t *testing.T) {
		journal := operator.NewMemoryJournal()
		if err := journal.RecordObligation(ctx, wfRun039Overdue(t, now)); err != nil {
			t.Fatalf("record obligation: %v", err)
		}
		src, err := suspensionsOverStore(journal, func() time.Time { return now })
		if err != nil {
			t.Fatalf("suspension source: %v", err)
		}
		gateway := wfRun039Gateway(t, src, "people.read")
		// No principal in context at all: an ungoverned capability is
		// admitted without any tenant lookup.
		response, err := wfRun039Invoke(context.Background(), gateway, "people.read")
		if err != nil {
			t.Fatalf("ungoverned capability refused: %v", err)
		}
		if response != "ran" {
			t.Fatalf("handler did not run: %v", response)
		}
	})

	t.Run("composition_wires_only_with_a_database", func(t *testing.T) {
		if src := composeCapabilitySuspensions(nil, nil, nil); src != nil {
			t.Fatal("a cell without a database must keep today's unwired gateway")
		}
		var db dbport.Beginner
		if src := composeCapabilitySuspensions(db, nil, nil); src != nil {
			t.Fatal("a nil database must not wire suspensions")
		}
	})
}
