package agentmodel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/schemaflux/schemafluxtest"
)

type ownedTypedDispatcher struct {
	calls   int
	input   TypedModelInput
	request Request
	body    string
	err     error
}

func (d *ownedTypedDispatcher) DispatchTypedModel(_ context.Context, req Request, input TypedModelInput) (ModelResult, error) {
	d.calls++
	d.request = req
	d.input = input
	return ModelResult{Finish: FinishComplete, Structured: json.RawMessage(d.body), Provider: ModelIdentity{ProviderID: "openai", ModelID: "pinned"}, Usage: ModelUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30, CostMicros: 120}}, d.err
}

func TestTodo_AGENT2_026_ConfiguredTypedDispatch(t *testing.T) {
	global := schemafluxtest.New().Reply(`{"decision":"wrong global provider"}`)
	defer schemafluxtest.Install(t, global)()
	gateway, budget, audit, _ := gatewayFixture(t)
	dispatcher := &ownedTypedDispatcher{body: `{"decision":"approve"}`}
	gateway.typedDispatch = dispatcher
	got, err := Generate[modelAnswer](context.Background(), gateway, request())
	if err != nil {
		t.Fatal(err)
	}
	if got.Value.Decision != "approve" || got.CostMicros != 120 || got.Usage.Tokens != 30 || dispatcher.calls != 1 || global.CallCount() != 0 || len(budget.reservations) != 0 || len(audit.entries) != 1 {
		t.Fatalf("typed result=%+v calls=%d global=%d reservations=%d audit=%d", got, dispatcher.calls, global.CallCount(), len(budget.reservations), len(audit.entries))
	}
	if !json.Valid(dispatcher.input.Schema) || !strings.Contains(string(dispatcher.input.Schema), "decision") || !strings.Contains(dispatcher.input.Prompt, "Reviewed skill context") || dispatcher.request.Actor != request().Actor {
		t.Fatalf("unbound typed input=%+v", dispatcher.input)
	}
}

func TestTodo_AGENT2_026_ConfiguredTypedDispatchSecurity(t *testing.T) {
	for _, body := range []string{`{"decision":"approve","secret":"invalid"}`, `{"decision":42}`, `{"decision":""}`, `{} trailing`} {
		gateway, _, audit, _ := gatewayFixture(t)
		d := &ownedTypedDispatcher{body: body}
		gateway.typedDispatch = d
		req := request()
		req.MaxRetries = 0
		got, err := Generate[modelAnswer](context.Background(), gateway, req)
		if !errors.Is(err, ErrRetryLimit) || got.Value.Decision != "" || d.calls != 1 || len(audit.entries) != 1 {
			t.Fatalf("malformed result=%+v err=%v calls=%d", got, err, d.calls)
		}
	}
}

func TestTodo_AGENT2_026_ConfiguredTypedDispatchDoesNotReplayAttempt(t *testing.T) {
	gateway, _, audit, _ := gatewayFixture(t)
	d := &ownedTypedDispatcher{err: errors.New("provider unavailable")}
	gateway.typedDispatch = d
	req := request()
	req.MaxRetries = 2
	_, err := Generate[modelAnswer](context.Background(), gateway, req)
	if !errors.Is(err, ErrRetryLimit) || d.calls != 1 || len(audit.entries) != 1 {
		t.Fatalf("replayed task attempt: error=%v calls=%d audit=%d", err, d.calls, len(audit.entries))
	}
}

func TestTodo_AGENT_024_PricedAdapter(t *testing.T) {
	typed, raw, req := typedAdapterTestFixture(t, `{"decision":"approve"}`)
	req.Limits.MaxCostMicros = 1_000_000
	adapter, err := NewPricedAdapter(raw, typed.selection, typed.pricing)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.Invoke(context.Background(), req)
	if err != nil || got.Usage.CostMicros != 120 || adapter.Identity() != raw.Identity() || len(adapter.Capabilities().OutputModes) == 0 {
		t.Fatalf("priced=%+v err=%v", got, err)
	}
	req.ModelProfile = "foreign"
	if _, err := adapter.Invoke(context.Background(), req); !errors.Is(err, ErrInvalidModelRequest) || raw.calls != 1 {
		t.Fatalf("foreign profile=%v calls=%d", err, raw.calls)
	}
}

func TestTodo_AGENT2_026_ConfiguredBudgetPause(t *testing.T) {
	gateway, budget, _, _ := gatewayFixture(t)
	dispatcher := &ownedTypedDispatcher{err: ErrBudgetFailed}
	gateway.typedDispatch = dispatcher
	_, err := Generate[modelAnswer](context.Background(), gateway, request())
	if !errors.Is(err, ErrBudgetFailed) || dispatcher.calls != 1 || len(budget.reservations) != 0 {
		t.Fatalf("budget pause=%v calls=%d outer=%d", err, dispatcher.calls, len(budget.reservations))
	}
}
