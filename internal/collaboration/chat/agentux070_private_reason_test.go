package chat

import "testing"

// TestTodo_AGENTUX_070_PrivateReasonMarker pins the code that travels beside a
// private answer: one marker per known reason, never one for an unknown reason,
// and a split that returns exactly the text that was joined.
func TestTodo_AGENTUX_070_PrivateReasonMarker(t *testing.T) {
	answer := "You carry over 40 hours.\n\nSources\n- Paid time off policy <!--chat.agent.source.readable:true-->"
	for _, reason := range []string{PrivateReasonAgent, PrivateReasonAsked, PrivateReasonAudience} {
		marker := PrivateReasonMarker(reason)
		if marker == "" || !ValidPrivateReason(reason) {
			t.Fatalf("reason %q has no marker", reason)
		}
		clean, got := SplitPrivateReason(answer + "\n" + marker)
		if clean != answer || got != reason {
			t.Errorf("reason %q: split = (%q, %q)", reason, clean, got)
		}
		// A copy saved after the card's own context token keeps that token.
		withContext, got := SplitPrivateReason(answer + "\n" + marker + "\n\n[chat-agent-question-context:abc](/chat/share/x)")
		if want := answer + "\n\n[chat-agent-question-context:abc](/chat/share/x)"; withContext != want || got != reason {
			t.Errorf("reason %q with context token: split = (%q, %q)", reason, withContext, got)
		}
	}
	if PrivateReasonMarker("nonsense") != "" || ValidPrivateReason("") {
		t.Error("an unknown reason produced a marker")
	}
	for _, body := range []string{answer, answer + "\n<!--chat.agent.private:nonsense-->", answer + "\n<!--chat.agent.private:agent"} {
		if clean, got := SplitPrivateReason(body); clean != body || got != "" {
			t.Errorf("malformed marker changed %q into (%q, %q)", body, clean, got)
		}
	}
}
