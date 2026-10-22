package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATGATE_009_Client: the page's fields become the request of each
// action of the consumers of answers. A rule row with no question or no answer is
// skipped, which is how a rule is removed; every other field is read by the name
// the page gave it.
func TestTodo_CHATGATE_009_Client(t *testing.T) {
	v := chatui.GateView{Locale: "en-US", Unanswered: []string{"p1", "p2"}}
	values := map[string]string{
		"gate-seg-0-field": "office", "gate-seg-0-value": " Denver ", "gate-seg-0-label": "Denver crew", "gate-seg-0-segment": "Denver office",
		"gate-seg-1-field": "office", "gate-seg-1-value": "", "gate-seg-1-label": "dropped",
		"gate-seg-2-field": "", "gate-seg-2-value": "Austin",
		"gate-seg-3-field": "shift", "gate-seg-3-value": "Night", "gate-seg-3-segment": "Night shift",
		"gate-seg-reason": "First rules", "gate-ops-review": "24", "gate-ops-escalate": " 72 ",
		"gate-existing-mode": "ask", "gate-existing-by": "2026-11-01", "gate-override-person": "ana", "gate-override-reason": " Invited by the director ",
	}
	checks := map[string]bool{"gate-levels-address": true, "gate-levels-restrict": false}
	req := chatgateClientRequest{}
	chatgateConsumerRequest(&req, "segments-save", "", values, checks, v)
	if len(req.Rules) != 2 || req.Rules[0].Value != "Denver" || req.Rules[0].Label != "Denver crew" || req.Rules[0].ID != "rule-1" || req.Rules[1].Field != "shift" || req.Rules[1].ID != "rule-2" || req.Reason != "First rules" {
		t.Fatalf("segments-save: %+v", req)
	}
	req = chatgateClientRequest{}
	chatgateConsumerRequest(&req, "segments-revert", "3", map[string]string{}, checks, v)
	if req.RulesVersion != 3 || req.Reason != chatui.GateText("en-US", "segSave") {
		t.Fatalf("segments-revert: %+v", req)
	}
	req = chatgateClientRequest{}
	chatgateConsumerRequest(&req, "levels-save", "", values, checks, v)
	if !req.Addressing || req.Restricted {
		t.Fatalf("levels-save: %+v", req)
	}
	req = chatgateClientRequest{}
	chatgateConsumerRequest(&req, "ops-save", "", values, checks, v)
	if req.ReviewAfterHours != 24 || req.EscalateAfterHours != 72 {
		t.Fatalf("ops-save: %+v", req)
	}
	req = chatgateClientRequest{}
	chatgateConsumerRequest(&req, "existing-save", "", values, checks, v)
	if req.ExistingMode != "ask" || req.AnswerBy != "2026-11-01" {
		t.Fatalf("existing-save: %+v", req)
	}
	req = chatgateClientRequest{}
	chatgateConsumerRequest(&req, "override", "", values, checks, v)
	if req.Person != "ana" || req.Reason != "Invited by the director" {
		t.Fatalf("override: %+v", req)
	}
	// The list confirmed is the list that was on the page, copied.
	req = chatgateClientRequest{}
	chatgateConsumerRequest(&req, "remove-unanswered", "", values, checks, v)
	v.Unanswered[0] = "changed"
	if len(req.People) != 2 || req.People[0] != "p1" {
		t.Fatalf("remove-unanswered: %+v", req)
	}
	// An action that is not one of these changes nothing.
	req = chatgateClientRequest{Action: "submit"}
	chatgateConsumerRequest(&req, "submit", "", values, checks, v)
	if len(req.Rules) != 0 || req.Reason != "" || req.Person != "" {
		t.Fatalf("submit: %+v", req)
	}
}
