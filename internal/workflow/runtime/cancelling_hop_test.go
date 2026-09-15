package runtime

import "testing"

// TestTodo_WF_STEP_003_CancellingHop pins when a routed CANCELLED terminal
// must pass through CANCELLING: only when the live status cannot reach
// CANCELLED directly but may begin cancelling.
func TestTodo_WF_STEP_003_CancellingHop(t *testing.T) {
	for _, from := range InstanceStatuses() {
		for _, to := range InstanceStatuses() {
			hop, needed := cancellingHop(from, to)
			want := to == InstanceCancelled && !LegalInstanceTransition(from, to) && LegalInstanceTransition(from, InstanceCancelling)
			if needed != want {
				t.Errorf("cancellingHop(%s, %s) needed = %v, want %v", from, to, needed, want)
			}
			if needed && (hop != InstanceCancelling || !LegalInstanceTransition(hop, to)) {
				t.Errorf("cancellingHop(%s, %s) = %s, want a legal CANCELLING hop", from, to, hop)
			}
		}
	}
	if _, needed := cancellingHop(InstanceRunning, InstanceCancelled); !needed {
		t.Error("RUNNING -> CANCELLED must hop through CANCELLING")
	}
	if _, needed := cancellingHop(InstanceCancelling, InstanceCancelled); needed {
		t.Error("CANCELLING -> CANCELLED needs no hop")
	}
	if _, needed := cancellingHop(InstanceRunning, InstanceCompleted); needed {
		t.Error("a non-cancelled terminal never hops")
	}
}
