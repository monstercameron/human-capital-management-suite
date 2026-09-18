package promotionexec

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Unit 1b (spec-compliance plan): the acknowledgement gate between
// reconciliation and completion. Reconciliation may verify every downstream
// leg, but the run still may not complete while the employee
// notification/acknowledgement obligation is open: team release before this
// signal would violate the ordering constraint the reference workflow states,
// so an unacknowledged promotion repairs instead of completing silently.
//
// Correlation is the proposal intent identity, not the worker: two concurrent
// intents for one worker (a promotion and a transfer) must never wake on each
// other's acknowledgement. The served subscriber resolves it through the
// driver's closed correlation vocabulary, and the served composition wires a
// durable subscriber and reader, so a parked run suspends honestly instead of
// erroring. Timeout enforcement (the TIMED_OUT edge) is the expiry sweeper:
// signals.ExpireDue marks the due wait EXPIRED and the scheduler's signal
// role resumes it, so an unacknowledged promotion repairs instead of parking
// forever.

func TestPromotionSignalNodeDeclaresDurableSubscription(t *testing.T) {
	def := Definition()
	var found *workflow.Node
	for i := range def.Nodes {
		if def.Nodes[i].ID == NodeAcknowledgeRelease {
			found = &def.Nodes[i]
		}
	}
	if found == nil {
		t.Fatalf("no %q node in the promotion definition", NodeAcknowledgeRelease)
	}
	if found.Type != workflow.StepSignal {
		t.Fatalf("acknowledge node type = %s, want SIGNAL", found.Type)
	}
	if found.Signal == nil {
		t.Fatalf("acknowledge node carries no SignalSpec")
	}
	spec := found.Signal
	if spec.EventType == "" {
		t.Fatalf("signal spec names no event type to correlate against")
	}
	if spec.CorrelationKeyExpression != "proposal.intent_id" {
		t.Fatalf("signal correlation = %q, want proposal.intent_id: an acknowledgement wakes its own intent, never a coworker's concurrent one", spec.CorrelationKeyExpression)
	}
	if !spec.ExpectedSchemaRef.Valid() {
		t.Fatalf("signal expected_schema_ref is not a versioned schema")
	}
	if len(spec.AcceptedSources) == 0 {
		t.Fatalf("signal spec accepts no source; a subscription that accepts anyone is not a subscription")
	}
	if !spec.Ordering.Valid() {
		t.Fatalf("signal ordering %q is not declared", string(spec.Ordering))
	}
	if spec.CloseAfterSeconds == 0 {
		t.Fatalf("signal subscription never closes; the close window must stay declared for the expiry sweeper")
	}
	if found.DeclaredEffect != capability.EffectPure {
		t.Fatalf("signal declared effect = %s, want PURE: a suspension mutates nothing", found.DeclaredEffect)
	}
	if found.FailureRoute != NodeEndRepairPlan {
		t.Fatalf("signal failure route = %q, want %q", found.FailureRoute, NodeEndRepairPlan)
	}
}

func TestPromotionSignalEdges(t *testing.T) {
	def := Definition()
	edges := map[[3]string]bool{}
	for _, edge := range def.Edges {
		edges[[3]string{edge.From, edge.To, edge.RouteKey}] = true
	}
	for _, want := range [][3]string{
		{NodeObserveReconciliation, NodeAcknowledgeRelease, "CONSISTENT"},
		{NodeAcknowledgeRelease, NodeEndComplete, "SUCCEEDED"},
		{NodeAcknowledgeRelease, NodeEndRepairPlan, "TIMED_OUT"},
		{NodeAcknowledgeRelease, NodeEndCancelled, "CANCELLED"},
	} {
		if !edges[want] {
			t.Errorf("missing edge %s -> %s [%s]", want[0], want[1], want[2])
		}
	}
	if edges[[3]string{NodeObserveReconciliation, NodeEndComplete, "CONSISTENT"}] {
		t.Errorf("reconciliation still completes directly; the acknowledgement gate is bypassed")
	}
}
