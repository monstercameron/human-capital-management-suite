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

// Version 1.1.0 adds two more SIGNAL waits of the same shape between the
// core commit and its observations: the payroll and identity providers'
// confirmations of the outbox effects, correlated on the proposal revision
// (the effects are payroll:<revision> and iam:<revision>) and accepting only
// the one provider's source.
func TestPromotionSignalNodeDeclaresDurableSubscription(t *testing.T) {
	cases := []struct {
		node, eventType, correlation, source string
		closeAfter                           uint32
	}{
		{NodeAcknowledgeRelease, "hcmnext.events.promotion_ack", "proposal.intent_id", "hcmnext.integrations.hris", 1209600},
		{NodeAwaitPayrollConfirmation, "hcmnext.events.payroll_change_result", "proposal.revision_id", "hcmnext.integrations.payroll", 259200},
		{NodeAwaitAccessConfirmation, "hcmnext.events.access_change_result", "proposal.revision_id", "hcmnext.integrations.iam", 259200},
	}
	def := Definition()
	for _, tc := range cases {
		t.Run(tc.node, func(t *testing.T) {
			var found *workflow.Node
			for i := range def.Nodes {
				if def.Nodes[i].ID == tc.node {
					found = &def.Nodes[i]
				}
			}
			if found == nil {
				t.Fatalf("no %q node in the promotion definition", tc.node)
			}
			if found.Type != workflow.StepSignal {
				t.Fatalf("node type = %s, want SIGNAL", found.Type)
			}
			if found.Signal == nil {
				t.Fatalf("node carries no SignalSpec")
			}
			spec := found.Signal
			if spec.EventType != tc.eventType {
				t.Fatalf("signal event type = %q, want %q", spec.EventType, tc.eventType)
			}
			if spec.CorrelationKeyExpression != tc.correlation {
				t.Fatalf("signal correlation = %q, want %q: a signal wakes its own proposal, never a coworker's concurrent one", spec.CorrelationKeyExpression, tc.correlation)
			}
			if !spec.ExpectedSchemaRef.Valid() {
				t.Fatalf("signal expected_schema_ref is not a versioned schema")
			}
			if len(spec.AcceptedSources) != 1 || spec.AcceptedSources[0] != tc.source {
				t.Fatalf("signal sources = %v, want exactly [%s]; a subscription that accepts anyone is not a subscription", spec.AcceptedSources, tc.source)
			}
			if spec.Ordering != workflow.SignalOrderingNone {
				t.Fatalf("signal ordering %q, want NONE", string(spec.Ordering))
			}
			if uint64(spec.CloseAfterSeconds) != uint64(tc.closeAfter) {
				t.Fatalf("signal close window = %d, want %d for the expiry sweeper", spec.CloseAfterSeconds, tc.closeAfter)
			}
			if found.DeclaredEffect != capability.EffectPure {
				t.Fatalf("signal declared effect = %s, want PURE: a suspension mutates nothing", found.DeclaredEffect)
			}
			if found.FailureRoute != NodeEndRepairPlan {
				t.Fatalf("signal failure route = %q, want %q", found.FailureRoute, NodeEndRepairPlan)
			}
		})
	}
	// The frozen 1.0.0 graph carries only the acknowledgement gate.
	for _, node := range DefinitionV1_0().Nodes {
		if node.ID == NodeAwaitPayrollConfirmation || node.ID == NodeAwaitAccessConfirmation {
			t.Fatalf("the frozen 1.0.0 graph carries %s", node.ID)
		}
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
		{NodeExecutePromotion, NodeAwaitPayrollConfirmation, "SUCCEEDED"},
		{NodeAwaitPayrollConfirmation, NodeObservePayroll, "SUCCEEDED"},
		{NodeAwaitPayrollConfirmation, NodeEndRepairPlan, "TIMED_OUT"},
		{NodeAwaitPayrollConfirmation, NodeEndCancelled, "CANCELLED"},
		{NodeObservePayroll, NodeAwaitAccessConfirmation, "PASS"},
		{NodeAwaitAccessConfirmation, NodeObserveAccess, "SUCCEEDED"},
		{NodeAwaitAccessConfirmation, NodeEndRepairPlan, "TIMED_OUT"},
		{NodeAwaitAccessConfirmation, NodeEndCancelled, "CANCELLED"},
	} {
		if !edges[want] {
			t.Errorf("missing edge %s -> %s [%s]", want[0], want[1], want[2])
		}
	}
	if edges[[3]string{NodeObserveReconciliation, NodeEndComplete, "CONSISTENT"}] {
		t.Errorf("reconciliation still completes directly; the acknowledgement gate is bypassed")
	}
	if edges[[3]string{NodeExecutePromotion, NodeObservePayroll, "SUCCEEDED"}] || edges[[3]string{NodeObservePayroll, NodeObserveAccess, "PASS"}] {
		t.Errorf("an observation is reachable without its provider-confirmation wait")
	}
}
