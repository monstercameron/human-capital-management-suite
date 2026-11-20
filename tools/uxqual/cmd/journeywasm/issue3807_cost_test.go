package main

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestIssue3807_ClientCarriesOwnCostToTheCard(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "viewer"}
	var rows []personaChatInvocation
	if err := json.Unmarshal([]byte(`[{"invocation_id":"i1","conversation_id":"room","post_id":"p1","thread_id":"p1","invoker_id":"viewer","status":"COMPLETED","used_units":3,"allowance_remaining_pct":88},
		{"invocation_id":"i2","conversation_id":"room","post_id":"p2","thread_id":"p2","invoker_id":"viewer","status":"COMPLETED"},
		{"invocation_id":"i3","conversation_id":"room","post_id":"p3","thread_id":"p3","invoker_id":"viewer","status":"COMPLETED","allowance_remaining_pct":0},
		{"invocation_id":"i4","conversation_id":"room","post_id":"p4","thread_id":"p4","invoker_id":"someone-else","status":"COMPLETED","used_units":9,"allowance_remaining_pct":10}]`), &rows); err != nil {
		t.Fatal(err)
	}
	out := personaChatInvocations(rows, cfg, "room")
	if len(out) != 3 {
		t.Fatalf("rows=%d, want 3 (another invoker's row is dropped)", len(out))
	}
	first := out[0].Projection.Cost
	if first == nil || first.UsedUnits != 3 || first.AllowanceRemainingPct == nil || *first.AllowanceRemainingPct != 88 {
		t.Fatalf("cost lost: %+v", first)
	}
	if out[1].Projection.Cost != nil {
		t.Fatalf("a run with no figures drew a cost: %+v", out[1].Projection.Cost)
	}
	if third := out[2].Projection.Cost; third == nil || third.AllowanceRemainingPct == nil || *third.AllowanceRemainingPct != 0 {
		t.Fatalf("an exhausted allowance was dropped: %+v", third)
	}
}
