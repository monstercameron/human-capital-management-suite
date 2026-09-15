package app

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// TestTodo_WF_STEP_003_ClosureVocabulary pins the closure vocabulary the
// served decide path writes and reads back: each closure's transition reason
// maps back to exactly its own closure and item status, its refusal names the
// right sentinel, and its step event routes the right edge.
func TestTodo_WF_STEP_003_ClosureVocabulary(t *testing.T) {
	cases := []struct {
		kind    approvalClosure
		status  workitem.Status
		reason  string
		refusal error
		event   stepsapproval.EventKind
	}{
		{approvalClosureExpired, workitem.StatusExpired, journeyReasonExpired, ErrProposalDecisionExpired, stepsapproval.EventDecisionsChanged},
		{approvalClosureInvalidated, workitem.StatusCancelled, journeyReasonInvalidated, ErrProposalDecisionInvalidated, stepsapproval.EventInvalidated},
		{approvalClosureCancelled, workitem.StatusCancelled, journeyReasonCancelled, ErrProposalDecisionStage, stepsapproval.EventCancelled},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			if got := closureReason(c.kind); got != c.reason {
				t.Errorf("closureReason = %q, want %q", got, c.reason)
			}
			if got, ok := closureKindOf(c.reason, c.status); !ok || got != c.kind {
				t.Errorf("closureKindOf(%q, %s) = %q/%v, want %q", c.reason, c.status, got, ok, c.kind)
			}
			if err := closureRefusal(c.kind, "detail"); !errors.Is(err, c.refusal) {
				t.Errorf("closureRefusal = %v, want %v", err, c.refusal)
			}
			event := closureEvent(c.kind, "detail")
			if event.Kind != c.event {
				t.Errorf("closureEvent kind = %q, want %q", event.Kind, c.event)
			}
			if c.event != stepsapproval.EventDecisionsChanged && event.Reason != "detail" {
				t.Errorf("closureEvent reason = %q, want the closure detail", event.Reason)
			}
		})
	}
	for _, foreign := range []struct {
		reason string
		status workitem.Status
	}{
		{journeyReasonExpired, workitem.StatusCancelled},
		{journeyReasonInvalidated, workitem.StatusExpired},
		{"operator.cancelled", workitem.StatusCancelled},
	} {
		if kind, ok := closureKindOf(foreign.reason, foreign.status); ok {
			t.Errorf("closureKindOf(%q, %s) = %q, want not a decision closure", foreign.reason, foreign.status, kind)
		}
	}
}
