package agentmodel

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type typedBusinessCandidate struct {
	Intent  string `json:"intent" schemaflux:"required"`
	Version uint32 `json:"version" schemaflux:"required"`
}

func typedBusinessFixture(t *testing.T, body string) (*SchemaFluxTypedAdapter[typedBusinessCandidate], *typedAdapterFixture, ModelRequest) {
	t.Helper()
	base, provider, req := typedAdapterTestFixture(t, body)
	adapter, err := NewSchemaFluxTypedAdapter[typedBusinessCandidate](provider, base.selection, base.pricing)
	if err != nil {
		t.Fatal(err)
	}
	req.RequiredFeatures = []ModelFeature{FeatureStructuredJSON}
	req.Output = OutputConstraint{Mode: OutputSchema, Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"intent":{"type":"string"},"version":{"type":"integer"}},"required":["intent","version"]}`)}
	req.Tools = nil
	return adapter, provider, req
}

func TestTodo_AGENT2_026_TypedBusinessSchemaFlux(t *testing.T) {
	adapter, provider, req := typedBusinessFixture(t, `{"intent":"leave.request","version":1}`)
	result, err := adapter.Invoke(context.Background(), req)
	if err != nil || string(result.Structured) != `{"intent":"leave.request","version":1}` || result.Text != "" || result.Usage.CostMicros != 120 || provider.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, provider.calls)
	}
	if !reflect.DeepEqual(provider.request.Messages[0], req.Messages[0]) || !reflect.DeepEqual(provider.request.Output, req.Output) || len(provider.request.Tools) != 0 {
		t.Fatal("classified input or approved output contract changed")
	}
}

func TestTodo_AGENT2_026_TypedBusinessSchemaBinding(t *testing.T) {
	adapter, provider, req := typedBusinessFixture(t, `{"intent":"leave.request","version":1}`)
	req.Output.Schema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"intent":{"type":"string"}},"required":["intent"]}`)
	result, err := adapter.Invoke(context.Background(), req)
	if err == nil || provider.calls != 0 || len(result.Structured) != 0 || result.Failure == nil {
		t.Fatalf("schema mismatch=%+v err=%v calls=%d", result, err, provider.calls)
	}
	var missing *typedAdapterFixture
	if _, err = NewSchemaFluxTypedAdapter[typedBusinessCandidate](missing, adapter.selection, adapter.pricing); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	if _, err = NewSchemaFluxTypedAdapter[string](provider, adapter.selection, adapter.pricing); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

func TestTodo_AGENT2_026_TypedBusinessInvalidOutput(t *testing.T) {
	for _, body := range []string{`{"intent":"leave.request","version":1,"principal":"forged"}`, `{"intent":"leave.request","version":0}`, `{"intent":"leave.request"}`, `{"intent":1,"version":1}`, `{} secret`} {
		adapter, provider, req := typedBusinessFixture(t, body)
		result, err := adapter.Invoke(context.Background(), req)
		if err == nil || result.Text != "" || len(result.Structured) != 0 || result.Failure == nil || strings.Contains(err.Error(), "secret") || provider.calls != 1 || result.Usage.CostMicros != 120 {
			t.Fatalf("invalid=%+v err=%v calls=%d", result, err, provider.calls)
		}
	}
}

func TestTodo_AGENT_024_TypedBusinessFaultAccounting(t *testing.T) {
	adapter, provider, req := typedBusinessFixture(t, "")
	provider.refuse = true
	result, err := adapter.Invoke(context.Background(), req)
	if err != nil || result.Refusal == nil || result.Usage.CostMicros != 120 {
		t.Fatalf("refusal=%+v err=%v", result, err)
	}
	provider.refuse = false
	provider.fail = true
	result, err = adapter.Invoke(context.Background(), req)
	if err == nil || result.Failure == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("failure=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = adapter.Invoke(ctx, req)
	if !errors.Is(err, context.Canceled) || result.Finish != FinishCancelled || provider.calls != 2 {
		t.Fatalf("cancel=%+v err=%v", result, err)
	}
}
