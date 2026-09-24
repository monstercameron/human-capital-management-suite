package capability

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubSink struct {
	called   int
	txCalled int
}

func (s *stubSink) RecordInvocation(_ context.Context, evt InvocationEvidence) (string, error) {
	s.called++
	return "ev-1", nil
}

func (s *stubSink) RecordInvocationTx(ctx context.Context, evt InvocationEvidence) (string, error) {
	s.txCalled++
	return s.RecordInvocation(ctx, evt)
}

type failingSink struct{ err error }

func (s failingSink) RecordInvocation(context.Context, InvocationEvidence) (string, error) {
	return "", s.err
}

func (s failingSink) RecordInvocationTx(context.Context, InvocationEvidence) (string, error) {
	return "", s.err
}

func TestGateway_NewGateway(t *testing.T) {
	r, _ := NewBootstrapRegistry()
	g := NewGateway(r, &stubSink{})
	if g == nil {
		t.Fatal("nil gateway")
	}
}

func TestGateway_WithClock(t *testing.T) {
	r := NewRegistry()
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g := NewGateway(r, &stubSink{}, WithClock(func() time.Time { return fixed }))
	if g.now().UnixNano() != fixed.UnixNano() {
		t.Fatal("clock not set")
	}
}

func TestGateway_InvokeUnknown(t *testing.T) {
	r := NewRegistry()
	sink := &stubSink{}
	g := NewGateway(r, sink)
	_, err := g.Invoke(context.Background(), InvokeRequest{
		Capability:    Key{ID: "unknown.cap", Version: 1},
		Authorization: Authorization{Decision: Allow, Scopes: []string{"*"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	ge, ok := err.(*GatewayError)
	if !ok {
		t.Fatalf("not gateway error %T", err)
	}
	if ge.Code != CodeUnknownCapability {
		t.Fatalf("code %s", ge.Code)
	}
	if sink.txCalled != 1 {
		t.Fatalf("transaction-aware evidence calls = %d, want one for refusal", sink.txCalled)
	}
}

func TestGateway_HandlerFailureEvidenceFailureIsReturned(t *testing.T) {
	want := errors.New("evidence append failed")
	def := Definition{
		ID: "hcmnext.test.failure", Version: 1, OwnerDomain: "test",
		RequestSchema:  SchemaRef{SchemaID: "hcmnext.test.v1", Version: 1, ProtobufFullName: "hcmnext.test.v1.Request"},
		ResponseSchema: SchemaRef{SchemaID: "hcmnext.test.v1", Version: 1, ProtobufFullName: "hcmnext.test.v1.Response"},
		ErrorSchema:    SchemaRef{SchemaID: "hcmnext.test.v1", Version: 1, ProtobufFullName: "hcmnext.test.v1.Error"},
		EffectClass:    EffectReadOnly, ReadData: DataDomainFieldSet{DataDomains: []string{"test"}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:test.read",
		LegalBasisRef: "legal.test.v1", EntitlementRef: "entitlement.test.v1", SLOClassRef: "slo.test.v1", TestRef: "test:gateway-failure",
	}
	registry := NewRegistry()
	handlerCalled := false
	if err := registry.Register(def, func(context.Context, any) (any, error) {
		handlerCalled = true // the handler may have staged an effect before failing
		return nil, errors.New("handler failed after effect")
	}); err != nil {
		t.Fatal(err)
	}
	g := NewGateway(registry, failingSink{err: want})
	_, err := g.Invoke(context.Background(), InvokeRequest{
		Capability:    def.Key(),
		Authorization: Authorization{Decision: Allow, Scopes: []string{def.AuthZScopeRef}},
	})
	if !errors.Is(err, want) {
		t.Fatalf("post-handler evidence failure = %v, want %v", err, want)
	}
	var refusal *GatewayError
	if errors.As(err, &refusal) {
		t.Fatalf("evidence failure after handler execution was masked as a normal refusal: %+v", refusal)
	}
	if !handlerCalled {
		t.Fatal("test did not reach the handler before evidence failed")
	}
}

func TestGateway_InvokeDeniedAuth(t *testing.T) {
	r, _ := NewBootstrapRegistry()
	sink := &stubSink{}
	g := NewGateway(r, sink)
	list := r.List()
	if len(list) == 0 {
		t.Fatal("empty registry")
	}
	key := list[0].Definition.Key()
	_, err := g.Invoke(context.Background(), InvokeRequest{
		Capability:    key,
		Authorization: Authorization{Decision: Deny, Reason: "test deny"},
	})
	if err == nil {
		t.Fatal("expected deny")
	}
	ge, _ := err.(*GatewayError)
	if ge.Code != CodeUnauthorized {
		t.Fatalf("code %s", ge.Code)
	}
}

func TestGateway_InvokeAllowed(t *testing.T) {
	r, _ := NewBootstrapRegistry()
	sink := &stubSink{}
	g := NewGateway(r, sink)
	rec := r.List()[0]
	key := rec.Definition.Key()
	res, err := g.Invoke(context.Background(), InvokeRequest{
		Capability:    key,
		Payload:       map[string]string{"hello": "world"},
		Authorization: Authorization{Decision: Allow, Scopes: []string{"*"}, SubjectRef: "user:1"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.EvidenceID == "" {
		t.Fatal("empty evidence")
	}
	if m, ok := res.Response.(map[string]any); !ok || m["capability"] != key.ID {
		t.Fatalf("response %+v", res.Response)
	}
	if sink.txCalled != 1 {
		t.Fatalf("transaction-aware evidence calls = %d, want one for invocation", sink.txCalled)
	}
}
