package main

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// The rating stored on the server reaches the page with the agent activity, so
// it is there after a reload; a change still on its way to the server is not
// overwritten by the activity's older value; and an undone rating is gone.
func TestTodo_CHATBUG_066(t *testing.T) {
	// The activity as the server sends it after a reload: one rated answer.
	var payload struct {
		Invocations []personaChatInvocation `json:"invocations"`
	}
	wire := `{"invocations":[{"invocation_id":"run-1","post_id":"q1","conversation_id":"general","invoker_id":"walt","status":"COMPLETED","feedback":"helpful"},{"invocation_id":"run-2","post_id":"q2","conversation_id":"general","invoker_id":"walt","status":"COMPLETED"}]}`
	if err := json.Unmarshal([]byte(wire), &payload); err != nil {
		t.Fatal(err)
	}
	var ledger personaFeedbackLedger
	stored := ledger.stored(payload.Invocations)
	if len(stored) != 1 || stored["run-1"] != chatui.AgentFeedbackHelpful {
		t.Fatalf("the stored rating did not reach the page: %+v", stored)
	}
	if ledger.rating("run-1") != chatui.AgentFeedbackHelpful {
		t.Fatal("a failed change would not fall back to the stored rating")
	}

	// The person changes it to "Not right": until the server answers, the
	// activity's older "helpful" does not come back.
	ledger.begin("run-1")
	if again := ledger.stored(payload.Invocations); again["run-1"] != chatui.AgentFeedbackHelpful {
		t.Fatalf("the ledger lost what the server last confirmed: %+v", again)
	}
	ledger.confirm("run-1", chatui.AgentFeedbackNotRight)
	if during := ledger.stored(payload.Invocations); during["run-1"] != chatui.AgentFeedbackNotRight {
		t.Fatalf("the activity's older rating replaced a change in flight: %+v", during)
	}
	ledger.end("run-1")
	payload.Invocations[0].Feedback = chatui.AgentFeedbackNotRight
	if after := ledger.stored(payload.Invocations); after["run-1"] != chatui.AgentFeedbackNotRight {
		t.Fatalf("the confirmed change is not shown: %+v", after)
	}

	// The rating is removed: the activity without it empties the control.
	payload.Invocations[0].Feedback = ""
	if removed := ledger.stored(payload.Invocations); len(removed) != 0 || ledger.rating("run-1") != "" {
		t.Fatalf("an undone rating is still stored: %+v", removed)
	}
	// A value the page does not know is no rating.
	payload.Invocations[1].Feedback = "five-stars"
	if unknown := ledger.stored(payload.Invocations); len(unknown) != 0 {
		t.Fatalf("an unknown rating was shown: %+v", unknown)
	}
	// What the page is handed is its own copy.
	ledger.confirm("run-2", chatui.AgentFeedbackHelpful)
	shown := ledger.shown()
	shown["run-2"] = "tampered"
	if ledger.rating("run-2") != chatui.AgentFeedbackHelpful {
		t.Fatal("the page's copy is the ledger's own map")
	}
	// Removing a rating empties it on the page at once, without touching the map a render may hold.
	before := map[string]string{"run-2": chatui.AgentFeedbackHelpful, "run-3": chatui.AgentFeedbackNotRight}
	if after := personaFeedbackWithout(before, "run-2"); len(after) != 1 || after["run-3"] != chatui.AgentFeedbackNotRight || len(before) != 2 {
		t.Fatalf("removing one rating: %+v (before %+v)", after, before)
	}
}
