package main

import (
	"encoding/json"
	"testing"
)

// TestIssue3818_JoinsCarryTheRequest: the list of gates tells the page where
// the person's own request stands, who it waits for and why it was refused.
func TestIssue3818_JoinsCarryTheRequest(t *testing.T) {
	raw := json.RawMessage(`[{"conversation":"sales","questions":2,"mode":"review","request":"review","reviewers":["Mia Manager"]},{"conversation":"hr","questions":1,"mode":"review","request":"declined","request_reason":"Full"},{"conversation":"ops","questions":1,"mode":"review"}]`)
	joins, ok := chatgateJoins(raw)
	if !ok || len(joins) != 3 {
		t.Fatalf("joins %+v %v", joins, ok)
	}
	if g := joins["sales"]; g.Request != "review" || len(g.Reviewers) != 1 || g.Reviewers[0] != "Mia Manager" {
		t.Fatalf("sales %+v", g)
	}
	if g := joins["hr"]; g.Request != "declined" || g.RequestReason != "Full" {
		t.Fatalf("hr %+v", g)
	}
	if g := joins["ops"]; g.Request != "" || g.RequestReason != "" || len(g.Reviewers) != 0 {
		t.Fatalf("ops %+v", g)
	}
}
