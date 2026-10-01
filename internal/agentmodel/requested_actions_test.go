package agentmodel

import (
	"context"
	"strings"
	"testing"
)

func TestTodo_AGENTP_021_TypedRequestedActions(t *testing.T) {
	for _, test := range []struct {
		body            string
		denied, invalid bool
	}{
		{body: `{"text":"","tool_proposals":[],"requested_actions":["change_compensation","submit_payroll"]}`, denied: true},
		{body: `{"text":"Governed policy","tool_proposals":[],"requested_actions":["read_policy"]}`},
		{body: `{"text":"","tool_proposals":[],"requested_actions":["invented_action"]}`, invalid: true},
		{body: `{"text":"I changed payroll","tool_proposals":[],"requested_actions":["submit_payroll"]}`, invalid: true},
		{body: `{"text":"Governed policy","tool_proposals":[]}`, invalid: true},
	} {
		adapter, provider, request := typedAdapterTestFixture(t, test.body)
		request.ActionPolicy = &RequestedActionPolicy{ProfileDigest: "sha256:" + strings.Repeat("a", 64), Allowed: []RequestedAction{ActionReadPolicy}}
		result, err := adapter.Invoke(context.Background(), request)
		if (err != nil) != test.invalid || provider.calls != 1 {
			t.Fatalf("result=%+v err=%v calls=%d invalid=%v", result, err, provider.calls, test.invalid)
		}
		if test.invalid {
			continue
		}
		allowed, err := CheckRequestedActions(*request.ActionPolicy, result.RequestedActions)
		if err != nil || allowed == test.denied || provider.request.ActionPolicy != nil {
			t.Fatalf("typed action boundary: allowed=%v err=%v", allowed, err)
		}
		if !strings.Contains(string(provider.request.Output.Schema), "requested_actions") {
			t.Fatal("action classification bypassed the typed schema")
		}
	}
}
