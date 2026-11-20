package main

import "testing"

// Issue 3819: the browser client reads the reason the person typed for each kind
// of decision, and recognises the service naming the reason field.
func TestIssue3819_ClientReadsTheReason(t *testing.T) {
	values := map[string]string{"gate-reason-s1": "Welcome", "gate-bulk-reason": "Batch"}
	for action, want := range map[string]string{"admit": "Welcome", "decline": "Welcome", "bulk-admit": "Batch", "bulk-decline": "Batch"} {
		if got := chatgateReasonValue(action, "s1", values); got != want {
			t.Fatalf("%s: %q", action, got)
		}
		if !chatgateReasonRefused(action, map[string]string{"reason": "invalid_argument"}) {
			t.Fatalf("%s: reason refusal not recognised", action)
		}
		if chatgateReasonRefused(action, map[string]string{"team": "invalid_argument"}) {
			t.Fatalf("%s: another field taken for the reason", action)
		}
	}
	if chatgateReasonRefused("submit", map[string]string{"reason": "invalid_argument"}) {
		t.Fatal("an applicant's answers have no decision reason")
	}
	if chatgateReasonValue("submit", "s1", values) == "" {
		t.Fatal("other actions must not be held for a reason")
	}
}
