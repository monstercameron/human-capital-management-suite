package main

import (
	"encoding/json"
	"testing"
)

func TestIssue3803_ActivityFeedCarriesOutcomeReceiptsToThePage(t *testing.T) {
	raw := `{"invocations":[],"outcomes":[
		{"receipt_id":"run-1","post_id":"p1","thread_id":"p1","conversation_id":"room","invoker_id":"grace","persona_id":"agent:a","agent_name":"Onboarding Coordinator","outcome":"succeeded"},
		{"receipt_id":"run-1","post_id":"p1","thread_id":"p1","conversation_id":"room","invoker_id":"grace","outcome":"succeeded"},
		{"receipt_id":"run-2","post_id":"p2","thread_id":"p2","conversation_id":"other-room","invoker_id":"grace","outcome":"failed"},
		{"receipt_id":"","post_id":"p3","conversation_id":"room","outcome":"failed"},
		{"receipt_id":"refused:p4","post_id":"p4","thread_id":"p4","conversation_id":"room","invoker_id":"ada","outcome":"refused"},
		{"receipt_id":"run-5","post_id":"p5","conversation_id":"room","outcome":""}]}`
	var payload personaChatActivity
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	got := personaChatOutcomes(payload.Outcomes, "room")
	if len(got) != 2 || got[0].ID != "run-1" || got[0].AgentName != "Onboarding Coordinator" || got[0].InvokerID != "grace" || got[0].Outcome != "succeeded" || got[1].ID != "refused:p4" || got[1].Outcome != "refused" {
		t.Fatalf("outcomes=%+v: one per run, only this conversation's, none without a run, message or outcome", got)
	}
	if none := personaChatOutcomes(nil, "room"); none == nil || len(none) != 0 {
		t.Fatalf("no receipts is an empty list, not a missing one: %+v", none)
	}
}
