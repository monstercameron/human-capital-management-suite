package app

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// TestTodo_REV_049_01 requires the production promotion dispatcher to return
// the universal preflight plan and its multi-child composition with the
// domain calculation. The plan digest is the identity downstream review binds.
func TestTodo_REV_049_01(t *testing.T) {
	ctx := context.Background()
	handlers, request, sims := rev00601SimulateCall(t, ctx)
	answer := rev00601Simulate(t, ctx, handlers, request, sims)

	if answer.PreflightPlan.Digest == "" {
		t.Fatal("promotion dispatcher returned no compiled universal preflight plan")
	}
	if answer.PreflightPlan.Request.Definition != (intent.Ref{TypeID: promotion.IntentType, Version: 1}) {
		t.Errorf("preflight definition = %+v", answer.PreflightPlan.Request.Definition)
	}
	if answer.PreflightPlan.Request.Snapshot.Digest != answer.Simulation.InputsDigest {
		t.Errorf("preflight snapshot = %q, domain inputs = %q",
			answer.PreflightPlan.Request.Snapshot.Digest, answer.Simulation.InputsDigest)
	}
	if answer.Composition.Digest == "" {
		t.Fatal("promotion dispatcher returned no compiled multi-child composition")
	}
	if answer.Composition.Plan.ParentProposal != answer.Simulation.ResultDigest {
		t.Errorf("composition proposal = %q, want intent %q",
			answer.Composition.Plan.ParentProposal, answer.Simulation.ResultDigest)
	}
}

// TestTodo_REV_049_01_Integration proves the capability registry and governed
// gateway return the same compiled plans through the production dispatch path.
func TestTodo_REV_049_01_Integration(t *testing.T) {
	ctx := context.Background()
	handlers, request, sims := rev00601SimulateCall(t, ctx)
	registry, err := newCapabilityRegistry(handlers)
	if err != nil {
		t.Fatalf("capability registry: %v", err)
	}
	key := capability.Key{ID: promotion.IntentType, Version: 1}
	record, ok := registry.Lookup(key)
	if !ok {
		t.Fatalf("promotion capability %s missing", key)
	}
	gateway := capability.NewGateway(registry, NewMemoryEvidenceSink())
	result, err := gateway.Invoke(ctx, capability.InvokeRequest{
		Capability: key,
		Payload: promotionCall{
			Mode: promotionModeSimulate, Request: request, IntentID: rev00601Instance().IntentID,
			Simulations: sims, ControlSnapshotDigest: "sha256:rev00601-control",
			RevalidationRule: "promotion.revalidation/v1",
		},
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{record.Definition.AuthZScopeRef}},
	})
	if err != nil {
		t.Fatalf("governed promotion dispatch: %v", err)
	}
	answer, ok := result.Response.(promotionAnswer)
	if !ok {
		t.Fatalf("promotion response type = %T", result.Response)
	}
	if answer.PreflightPlan.Digest == "" || answer.Composition.Digest == "" || result.EvidenceID == "" {
		t.Fatalf("governed response missing plans or invocation evidence: preflight=%q composition=%q evidence=%q",
			answer.PreflightPlan.Digest, answer.Composition.Digest, result.EvidenceID)
	}
}

// TestTodo_REV_049_01_Golden pins the universal preflight and composition
// digests for the checked-in promotion fixture.
func TestTodo_REV_049_01_Golden(t *testing.T) {
	ctx := context.Background()
	handlers, request, sims := rev00601SimulateCall(t, ctx)
	answer := rev00601Simulate(t, ctx, handlers, request, sims)
	// The plan binds the governed current-pay and budget snapshot now, rather
	// than the duplicate values carried by the request. Both pinned digests
	// therefore change when that canonical preflight input changes.
	if answer.PreflightPlan.Digest != "sha256:109b88b30e51fcda6034347079c6c22dc85187b08a6af19fa9032ffeb44e7176" {
		t.Fatalf("preflight digest = %q", answer.PreflightPlan.Digest)
	}
	if answer.Composition.Digest != "sha256:f6b59def0d902855f974a1dd5395ec496584a1aade26efb31b662a9846258881" {
		t.Fatalf("composition digest = %q", answer.Composition.Digest)
	}
}
