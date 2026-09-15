package capability

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingSink struct{ records []InvocationEvidence }

func (s *recordingSink) RecordInvocation(_ context.Context, evt InvocationEvidence) (string, error) {
	s.records = append(s.records, evt)
	return "ev-invocation", nil
}

func invocationGateway(t *testing.T, at time.Time) (*Gateway, *recordingSink, Key, *int) {
	t.Helper()
	calls := 0
	def := bootstrapDefinition("hcmnext.test.read", "people", []string{"worker"})
	r := NewRegistry()
	if err := r.Register(def, func(ctx context.Context, payload any) (any, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			return nil, errors.New("handler ran without the invocation deadline")
		}
		if ctx.Err() != nil {
			return nil, errors.New("handler ran with its budget already spent")
		}
		return payload, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sink := &recordingSink{}
	return NewGateway(r, sink, WithClock(func() time.Time { return at })), sink, def.Key(), &calls
}

func allowRead() Authorization {
	return Authorization{Decision: Allow, Scopes: []string{"scope:people.read"}, SubjectRef: "hc-050"}
}

// TestInvocationEnvelopeGovernsTheCall proves the governed envelope is
// enforced before the handler and recorded on every decision.
func TestInvocationEnvelopeGovernsTheCall(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	valid := Invocation{Purpose: "PROMOTION_EXECUTION", Deadline: at.Add(time.Minute), IdempotencyKey: "instance/node/1", DeclaredEffects: []EffectClass{EffectReadOnly}}

	gateway, sink, key, calls := invocationGateway(t, at)
	env := valid
	result, err := gateway.Invoke(context.Background(), InvokeRequest{Capability: key, Payload: "x", Authorization: allowRead(), Invocation: &env})
	if err != nil || result.Response != "x" || *calls != 1 {
		t.Fatalf("valid envelope = %v, %v (calls %d); want the handler's answer", result, err, *calls)
	}
	got := sink.records[0]
	if got.Decision != "INVOKED" || got.Purpose != valid.Purpose || got.IdempotencyKey != valid.IdempotencyKey || !got.Deadline.Equal(valid.Deadline) || got.EffectClass != EffectReadOnly || got.SubjectRef != "hc-050" {
		t.Fatalf("invocation evidence = %+v, want the envelope recorded", got)
	}

	cases := []struct {
		name string
		edit func(*Invocation)
		want string
	}{
		{"no purpose", func(i *Invocation) { i.Purpose = " " }, CodeInvocationInvalid},
		{"no deadline", func(i *Invocation) { i.Deadline = time.Time{} }, CodeInvocationInvalid},
		{"no idempotency key", func(i *Invocation) { i.IdempotencyKey = "" }, CodeInvocationInvalid},
		{"no effect set", func(i *Invocation) { i.DeclaredEffects = nil }, CodeInvocationInvalid},
		{"deadline passed", func(i *Invocation) { i.Deadline = at }, CodeDeadlineExceeded},
		{"effect undeclared", func(i *Invocation) { i.DeclaredEffects = []EffectClass{EffectPure} }, CodeEffectUndeclared},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway, sink, key, calls := invocationGateway(t, at)
			env := valid
			tc.edit(&env)
			_, err := gateway.Invoke(context.Background(), InvokeRequest{Capability: key, Authorization: allowRead(), Invocation: &env})
			var gwErr *GatewayError
			if !errors.As(err, &gwErr) || gwErr.Code != tc.want {
				t.Fatalf("Invoke = %v, want %s", err, tc.want)
			}
			if *calls != 0 {
				t.Fatalf("handler ran %d times behind a refused envelope", *calls)
			}
			if len(sink.records) != 1 || sink.records[0].ReasonCode != tc.want || sink.records[0].IdempotencyKey != env.IdempotencyKey {
				t.Fatalf("refusal evidence = %+v", sink.records)
			}
		})
	}

	t.Run("a pinned clock far behind wall time still grants the remaining budget", func(t *testing.T) {
		past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		gateway, _, key, calls := invocationGateway(t, past)
		env := valid
		env.Deadline = past.Add(time.Minute)
		if _, err := gateway.Invoke(context.Background(), InvokeRequest{Capability: key, Payload: "x", Authorization: allowRead(), Invocation: &env}); err != nil || *calls != 1 {
			t.Fatalf("Invoke under a pinned past clock = %v (calls %d)", err, *calls)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		gateway, _, key, calls := invocationGateway(t, at)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		env := valid
		_, err := gateway.Invoke(ctx, InvokeRequest{Capability: key, Authorization: allowRead(), Invocation: &env})
		var gwErr *GatewayError
		if !errors.As(err, &gwErr) || gwErr.Code != CodeDeadlineExceeded || *calls != 0 {
			t.Fatalf("cancelled Invoke = %v (calls %d), want %s", err, *calls, CodeDeadlineExceeded)
		}
	})

	t.Run("unknown capability keeps the envelope and no effect class", func(t *testing.T) {
		gateway, sink, _, _ := invocationGateway(t, at)
		env := valid
		_, err := gateway.Invoke(context.Background(), InvokeRequest{Capability: Key{ID: "absent", Version: 1}, Authorization: allowRead(), Invocation: &env})
		if err == nil || sink.records[0].EffectClass != "" || sink.records[0].Purpose != valid.Purpose {
			t.Fatalf("unknown capability evidence = %+v, %v", sink.records, err)
		}
	})

	t.Run("nil envelope keeps the interactive contract", func(t *testing.T) {
		r, _ := NewBootstrapRegistry()
		sink := &recordingSink{}
		g := NewGateway(r, sink)
		def := r.List()[0].Definition
		if _, err := g.Invoke(context.Background(), InvokeRequest{Capability: def.Key(), Authorization: Authorization{Decision: Allow, Scopes: []string{def.AuthZScopeRef}}}); err != nil {
			t.Fatalf("interactive Invoke: %v", err)
		}
		if rec := sink.records[0]; rec.Purpose != "" || rec.IdempotencyKey != "" || !rec.Deadline.IsZero() || rec.EffectClass != "" {
			t.Fatalf("interactive evidence carries envelope fields: %+v", rec)
		}
	})
}
