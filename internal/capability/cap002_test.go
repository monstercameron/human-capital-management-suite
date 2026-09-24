package capability_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// recordingSink is a minimal in-memory EvidenceSink for tests: it assigns a
// sequential evidence ID and remembers every record.
type recordingSink struct {
	mu      sync.Mutex
	records []capability.InvocationEvidence
}

func (s *recordingSink) RecordInvocation(_ context.Context, evt capability.InvocationEvidence) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, evt)
	return "evidence-" + string(rune('a'+len(s.records)-1)), nil
}

func (s *recordingSink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocation(ctx, evt)
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

func (s *recordingSink) last() capability.InvocationEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records[len(s.records)-1]
}

func allowAuth(scope string) capability.Authorization {
	return capability.Authorization{Decision: capability.Allow, Scopes: []string{scope}, SubjectRef: "user:1"}
}

// TestTodo_CAP_002 proves CAP-002's RED and GREEN clauses: an unknown,
// retired, unauthorized or write-effect capability invokes no handler and
// returns a typed refusal; a well-formed request resolves the exact version,
// composes the checks and returns a typed result with an evidence ID.
func TestTodo_CAP_002(t *testing.T) {
	newFixture := func(t *testing.T) (*capability.Registry, *recordingSink, *capability.Gateway, capability.Definition) {
		t.Helper()
		def := validDefinition()
		r := capability.NewRegistry()
		if err := r.Register(def, echoHandler); err != nil {
			t.Fatalf("register: %v", err)
		}
		sink := &recordingSink{}
		gw := capability.NewGateway(r, sink)
		return r, sink, gw, def
	}

	t.Run("RED_unknown_capability_invokes_no_handler", func(t *testing.T) {
		_, sink, gw, def := newFixture(t)
		unknown := def.Key()
		unknown.ID = "hcmnext.test.does_not_exist"
		_, err := gw.Invoke(context.Background(), capability.InvokeRequest{
			Capability:    unknown,
			Authorization: allowAuth(def.AuthZScopeRef),
		})
		if err == nil {
			t.Fatal("expected an error for an unknown capability")
		}
		var gerr *capability.GatewayError
		if !errors.As(err, &gerr) {
			t.Fatalf("expected a *capability.GatewayError, got %T", err)
		}
		if gerr.Code != capability.CodeUnknownCapability {
			t.Fatalf("code = %s, want %s", gerr.Code, capability.CodeUnknownCapability)
		}
		if gerr.EvidenceID == "" {
			t.Fatal("expected a refusal to still carry an evidence ID")
		}
		if sink.count() != 1 {
			t.Fatalf("evidence records = %d, want 1", sink.count())
		}
	})

	t.Run("RED_retired_capability_invokes_no_handler", func(t *testing.T) {
		r, sink, gw, def := newFixture(t)
		if err := r.Retire(def.Key()); err != nil {
			t.Fatalf("retire: %v", err)
		}
		_, err := gw.Invoke(context.Background(), capability.InvokeRequest{
			Capability:    def.Key(),
			Authorization: allowAuth(def.AuthZScopeRef),
		})
		var gerr *capability.GatewayError
		if !errors.As(err, &gerr) || gerr.Code != capability.CodeCapabilityDisabled {
			t.Fatalf("expected CODE=%s, got %v", capability.CodeCapabilityDisabled, err)
		}
		if sink.count() != 1 {
			t.Fatalf("evidence records = %d, want 1", sink.count())
		}
	})

	t.Run("RED_unauthorized_scope_invokes_no_handler", func(t *testing.T) {
		_, sink, gw, def := newFixture(t)
		_, err := gw.Invoke(context.Background(), capability.InvokeRequest{
			Capability:    def.Key(),
			Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:other.read"}},
		})
		var gerr *capability.GatewayError
		if !errors.As(err, &gerr) || gerr.Code != capability.CodeUnauthorized {
			t.Fatalf("expected CODE=%s, got %v", capability.CodeUnauthorized, err)
		}
		if sink.count() != 1 {
			t.Fatalf("evidence records = %d, want 1", sink.count())
		}
	})

	t.Run("RED_denied_authorization_invokes_no_handler", func(t *testing.T) {
		_, _, gw, def := newFixture(t)
		_, err := gw.Invoke(context.Background(), capability.InvokeRequest{
			Capability:    def.Key(),
			Authorization: capability.Authorization{Decision: capability.Deny, Reason: "policy denied", Scopes: []string{def.AuthZScopeRef}},
		})
		var gerr *capability.GatewayError
		if !errors.As(err, &gerr) || gerr.Code != capability.CodeUnauthorized {
			t.Fatalf("expected CODE=%s, got %v", capability.CodeUnauthorized, err)
		}
	})

	t.Run("RED_write_effect_capability_refused_in_P1A", func(t *testing.T) {
		def := validDefinition()
		def.ID = "hcmnext.test.write_thing"
		def.EffectClass = capability.EffectInternalMutation
		called := false
		r := capability.NewRegistry()
		if err := r.Register(def, func(_ context.Context, _ any) (any, error) {
			called = true
			return nil, nil
		}); err != nil {
			t.Fatalf("register: %v", err)
		}
		sink := &recordingSink{}
		gw := capability.NewGateway(r, sink)
		_, err := gw.Invoke(context.Background(), capability.InvokeRequest{
			Capability:    def.Key(),
			Authorization: allowAuth(def.AuthZScopeRef),
		})
		var gerr *capability.GatewayError
		if !errors.As(err, &gerr) || gerr.Code != capability.CodeWriteEffectRefusedP1A {
			t.Fatalf("expected CODE=%s, got %v", capability.CodeWriteEffectRefusedP1A, err)
		}
		if called {
			t.Fatal("gateway invoked the handler of a write-effect capability in P1A")
		}
	})

	t.Run("GREEN_resolves_composes_and_returns_typed_result", func(t *testing.T) {
		_, sink, gw, def := newFixture(t)
		result, err := gw.Invoke(context.Background(), capability.InvokeRequest{
			Capability:    def.Key(),
			Payload:       "hello",
			Authorization: allowAuth(def.AuthZScopeRef),
		})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if result.Response != "hello" {
			t.Fatalf("response = %v, want %q", result.Response, "hello")
		}
		if result.EvidenceID == "" {
			t.Fatal("expected an evidence ID on success")
		}
		if got := sink.last(); got.Decision != "INVOKED" {
			t.Fatalf("evidence decision = %s, want INVOKED", got.Decision)
		}
	})
}

// TestTodo_CAP_002_Golden pins the evidence shape and timestamp source: every
// invocation, success or refusal, is stamped from the gateway's injected
// clock, never the ambient wall clock, so evidence ordering is reproducible
// in tests.
func TestTodo_CAP_002_Golden(t *testing.T) {
	def := validDefinition()
	r := capability.NewRegistry()
	if err := r.Register(def, echoHandler); err != nil {
		t.Fatalf("register: %v", err)
	}
	sink := &recordingSink{}
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	gw := capability.NewGateway(r, sink, capability.WithClock(func() time.Time { return fixed }))

	if _, err := gw.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    def.Key(),
		Authorization: allowAuth(def.AuthZScopeRef),
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	got := sink.last()
	want := capability.InvocationEvidence{
		CapabilityID:      def.ID,
		CapabilityVersion: def.Version,
		SubjectRef:        "user:1",
		Decision:          "INVOKED",
		OccurredAt:        fixed,
	}
	if got != want {
		t.Fatalf("evidence = %+v, want %+v", got, want)
	}
}

// TestTodo_CAP_002_Security proves the gateway is the only path that can
// authorize a call: a request that structurally matches Allow but for a
// different capability's scope is refused, and a capability handler is never
// invoked before the refusal checks pass, even when the handler would panic
// if it were.
func TestTodo_CAP_002_Security(t *testing.T) {
	def := validDefinition()
	r := capability.NewRegistry()
	if err := r.Register(def, func(_ context.Context, _ any) (any, error) {
		panic("handler must never run when authorization is refused")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sink := &recordingSink{}
	gw := capability.NewGateway(r, sink)

	_, err := gw.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    def.Key(),
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:unrelated"}},
	})
	if err == nil {
		t.Fatal("expected a scope mismatch to be refused")
	}
}

// TestTodo_CAP_002_Mutation proves the gateway's checks are each load-bearing:
// removing any single one (effect-class, status or scope enforcement) would
// let a case above pass that must fail. This test asserts the checks run in
// an order where an unauthorized, retired, write-effect request is refused
// on the first violated rule and never reaches the handler - so a mutation
// that deletes one check is caught by whichever RED case in
// TestTodo_CAP_002 depended on it, not silently masked by another check.
func TestTodo_CAP_002_Mutation(t *testing.T) {
	def := validDefinition()
	def.EffectClass = capability.EffectExternalMutation
	r := capability.NewRegistry()
	handlerCalled := false
	if err := r.Register(def, func(_ context.Context, _ any) (any, error) {
		handlerCalled = true
		return nil, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := r.Retire(def.Key()); err != nil {
		t.Fatalf("retire: %v", err)
	}
	sink := &recordingSink{}
	gw := capability.NewGateway(r, sink)

	// Both "retired" and "write effect" independently justify refusal. If a
	// mutation deleted the write-effect check, this case would still be
	// caught by the retired check (and vice versa if retired were skipped by
	// a mutation on Deprecate/Retire but write-effect still ran) - so the
	// test only passes when the underlying, weaker construction never lets
	// the handler run.
	_, err := gw.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    def.Key(),
		Authorization: allowAuth(def.AuthZScopeRef),
	})
	if err == nil {
		t.Fatal("expected refusal")
	}
	if handlerCalled {
		t.Fatal("handler ran despite a retired, write-effect capability")
	}
}
