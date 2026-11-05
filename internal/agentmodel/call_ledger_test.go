package agentmodel

import (
	"context"
	"testing"
)

// The AGENTCOST-001 wiring: the recording adapter records into the ledger
// carried on the call context (one ledger per run), keeps passing the call
// through untouched, and advertises the wrapped adapter's identity so the
// composed gateway still sees its pinned selection.

type callLedgerTestAdapter struct {
	identity ModelIdentity
	usage    ModelUsage
	calls    int
}

func (f *callLedgerTestAdapter) Capabilities() AdapterCapabilities {
	return AdapterCapabilities{ContractVersions: []int{ContractVersion}}
}

func (f *callLedgerTestAdapter) Identity() ModelIdentity { return f.identity }

func (f *callLedgerTestAdapter) Invoke(context.Context, ModelRequest) (ModelResult, error) {
	f.calls++
	return ModelResult{Text: "ok", Finish: FinishComplete, Usage: f.usage, Provider: f.identity}, nil
}

func TestRecordingAdapterContextLedger(t *testing.T) {
	inner := &callLedgerTestAdapter{
		identity: ModelIdentity{ProviderID: "schemaflux", ModelID: "gpt-6-luna", Version: "2026-09-01"},
		usage:    ModelUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CostMicros: 60},
	}
	// The composed gateway wraps its adapters once with no ledger of their
	// own; the run carries its ledger on the call context.
	recorder := NewRecordingAdapter(inner, nil, nil)
	var ledger CallLedger
	ctx := WithCallLedger(t.Context(), &ledger)
	ctx = WithCallContext(ctx, CallContext{
		TenantID: "t1", AgentID: "policy", TriggerKind: "mention", RunID: "r9",
		CallSite: "persona_run_executor:answer", Stage: CallStageAnswer, Kind: CallKindGeneration,
	})
	result, err := recorder.Invoke(ctx, ModelRequest{ContractVersion: ContractVersion})
	if err != nil || result.Text != "ok" || inner.calls != 1 {
		t.Fatalf("the recording adapter changed the call: %v %+v calls=%d", err, result, inner.calls)
	}
	got := ledger.Lines()
	if len(got) != 1 || got[0].RunID != "r9" || got[0].InputTokens != 100 || got[0].CostMicros != 60 || got[0].Basis != CostMeasured {
		t.Fatalf("context ledger lines = %+v", got)
	}
}

func TestRecordingAdapterContextLedgerPreferred(t *testing.T) {
	inner := &callLedgerTestAdapter{
		identity: ModelIdentity{ProviderID: "schemaflux", ModelID: "gpt-6-luna", Version: "2026-09-01"},
		usage:    ModelUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, CostMicros: 6},
	}
	var constructed, contextual CallLedger
	recorder := NewRecordingAdapter(inner, &constructed, nil)
	ctx := WithCallLedger(t.Context(), &contextual)
	if _, err := recorder.Invoke(ctx, ModelRequest{ContractVersion: ContractVersion}); err != nil {
		t.Fatal(err)
	}
	if len(contextual.Lines()) != 1 || len(constructed.Lines()) != 0 {
		t.Fatalf("context ledger = %d lines, constructed ledger = %d lines", len(contextual.Lines()), len(constructed.Lines()))
	}
}

func TestRecordingAdapterIdentity(t *testing.T) {
	identity := ModelIdentity{ProviderID: "openai", ModelID: "gpt-6-luna", Version: "2026-10-01"}
	recorder := NewRecordingAdapter(&callLedgerTestAdapter{identity: identity}, nil, nil)
	identified, ok := any(recorder).(interface{ Identity() ModelIdentity })
	if !ok || identified.Identity() != identity {
		t.Fatalf("the recording adapter hides the wrapped identity: %v %v", ok, identified)
	}
	unidentified := NewRecordingAdapter(&agentcost001BareAdapter{}, nil, nil)
	bare, ok := any(unidentified).(interface{ Identity() ModelIdentity })
	if !ok || bare.Identity() != (ModelIdentity{}) {
		t.Fatalf("an unidentified inner adapter must fail closed, got %v %v", ok, bare)
	}
}

type agentcost001BareAdapter struct{}

func (agentcost001BareAdapter) Capabilities() AdapterCapabilities {
	return AdapterCapabilities{ContractVersions: []int{ContractVersion}}
}

func (agentcost001BareAdapter) Invoke(context.Context, ModelRequest) (ModelResult, error) {
	return ModelResult{Text: "ok", Finish: FinishComplete}, nil
}

func TestRecordingAdapterScheduleVersion(t *testing.T) {
	inner := &callLedgerTestAdapter{
		identity: ModelIdentity{ProviderID: "schemaflux", ModelID: "gpt-6-luna", Version: "2026-09-01"},
		usage:    ModelUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12},
	}
	var ledger CallLedger
	recorder := NewRecordingAdapter(inner, &ledger, func(ModelIdentity, ModelUsage) (int64, bool) { return 7, false })
	recorder = recorder.WithScheduleVersion("schemaflux-2026-10-01")
	ctx := WithCallContext(t.Context(), CallContext{TenantID: "t1", AgentID: "policy", RunID: "r1", CallSite: "site"})
	if _, err := recorder.Invoke(ctx, ModelRequest{ContractVersion: ContractVersion, TaskProfile: "chat.answer"}); err != nil {
		t.Fatal(err)
	}
	got := ledger.Lines()
	if len(got) != 1 || got[0].ScheduleVersion != "schemaflux-2026-10-01" || got[0].CostMicros != 7 || got[0].Basis != CostEstimated {
		t.Fatalf("scheduled line = %+v", got)
	}
}
