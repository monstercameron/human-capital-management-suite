package boundary

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

type fakeSpan struct {
	outcome telemetry.Outcome
	err     error
}

func (s *fakeSpan) End(outcome telemetry.Outcome, err error) { s.outcome, s.err = outcome, err }

type fakeSink struct {
	name  telemetry.SpanName
	attrs map[string]string
	span  *fakeSpan
}

func (s *fakeSink) Start(ctx context.Context, name telemetry.SpanName, attrs map[string]string) (context.Context, Span) {
	s.name, s.attrs, s.span = name, attrs, &fakeSpan{}
	return ctx, s.span
}

func TestBoundaryInstrumentationEmitsExactPayloadFreeSignalsAndPreservesBehavior(t *testing.T) {
	sink := &fakeSink{}
	want := errors.New("owned failure")
	got, err := Instrument(context.Background(), sink, Spec{
		Kind: KindProvider, Operation: "promote", Dependency: "payroll", Status: "timeout", SizeClass: "small", Retry: 1,
	}, func(context.Context) (string, error) { return "unchanged", want })
	if got != "unchanged" || !errors.Is(err, want) {
		t.Fatalf("result/error changed: %q, %v", got, err)
	}
	if sink.name != telemetry.SpanProviderCall || sink.span.outcome != telemetry.OutcomeFailure || !errors.Is(sink.span.err, want) {
		t.Fatalf("span = %q, %+v", sink.name, sink.span)
	}
	wantAttrs := map[string]string{"operation": "promote", "dependency": "payroll", "status": "timeout", "size_class": "small", "retry": "true"}
	if !reflect.DeepEqual(sink.attrs, wantAttrs) {
		t.Fatalf("attrs = %#v, want %#v", sink.attrs, wantAttrs)
	}
	for _, forbidden := range []string{"body", "sql", "query", "header", "response"} {
		if _, ok := sink.attrs[forbidden]; ok {
			t.Fatalf("payload attribute %q emitted", forbidden)
		}
	}
}

func TestTodo_OBS_014_Golden(t *testing.T) {
	sink := &fakeSink{}
	_, err := Instrument(context.Background(), sink, Spec{Kind: KindHTTP, Operation: "get_intent", RouteTemplate: "/v1/intents/{intent_id}"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	if err != nil || sink.name != telemetry.SpanHTTPServer || sink.attrs["route"] != "/v1/intents/{intent_id}" {
		t.Fatalf("http signal = %q %#v err=%v", sink.name, sink.attrs, err)
	}
}

func TestTodo_OBS_014_Security(t *testing.T) {
	_, err := Instrument(context.Background(), &fakeSink{}, Spec{Kind: KindDatabase, Operation: "select_intent", RouteTemplate: "postgres://user:secret@host/db"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	if !errors.Is(err, ErrBoundaryPayload) {
		t.Fatalf("raw database endpoint accepted: %v", err)
	}
}

func TestTodo_OBS_014_Integration(t *testing.T) {
	called := false
	got, err := Instrument(context.Background(), nil, Spec{Kind: KindWorker, Operation: "resume_timer"}, func(context.Context) (int, error) { called = true; return 7, nil })
	if err != nil || got != 7 || !called {
		t.Fatalf("nil sink changed behavior: %d %v called=%v", got, err, called)
	}
}

func TestTodo_OBS_014_Race(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sink := &fakeSink{}
			value, err := Instrument(context.Background(), sink, Spec{Kind: KindWorker, Operation: "tick"}, func(context.Context) (int, error) { return 7, nil })
			if err == nil && (value != 7 || sink.name != telemetry.SpanJobPartition || sink.span.outcome != telemetry.OutcomeSuccess) {
				err = errors.New("concurrent instrumentation lost result or signal")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestTodo_OBS_014_Fault(t *testing.T) {
	want := errors.New("provider unavailable")
	_, err := Instrument(context.Background(), &fakeSink{}, Spec{Kind: KindProvider, Operation: "call"}, func(context.Context) (struct{}, error) { return struct{}{}, want })
	if !errors.Is(err, want) {
		t.Fatalf("fault error changed: %v", err)
	}
}

func BenchmarkTodo_OBS_014(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = Instrument(context.Background(), nil, Spec{Kind: KindHTTP, Operation: "get"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	}
}

func TestTodo_OBS_014_Mutation(t *testing.T) {
	_, err := Instrument(context.Background(), &fakeSink{}, Spec{Kind: KindHTTP, Operation: "get", RouteTemplate: "/v1/items?secret=1"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	if !errors.Is(err, ErrBoundaryPayload) {
		t.Fatal("raw query route survived boundary mutation")
	}
}

func TestBoundary_InstrumentMapsEveryKindAndOutcome(t *testing.T) {
	for _, tc := range []struct {
		kind Kind
		name telemetry.SpanName
	}{
		{KindHTTP, telemetry.SpanHTTPServer},
		{KindGRPC, telemetry.SpanGRPCServer},
		{KindDatabase, telemetry.SpanDBOperation},
		{KindWorker, telemetry.SpanJobPartition},
		{KindProvider, telemetry.SpanProviderCall},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			sink := &fakeSink{}
			got, err := Instrument(context.Background(), sink, Spec{Kind: tc.kind, Operation: "op", Retry: 0}, func(ctx context.Context) (string, error) {
				if ctx == nil {
					t.Fatal("callback received nil context")
				}
				return "ok", nil
			})
			if err != nil || got != "ok" || sink.name != tc.name || sink.span.outcome != telemetry.OutcomeSuccess || sink.span.err != nil {
				t.Fatalf("success = %q, %v, sink=%+v", got, err, sink)
			}
			if sink.attrs["operation"] != "op" {
				t.Fatalf("attrs = %#v", sink.attrs)
			}
			if _, ok := sink.attrs["retry"]; ok {
				t.Fatalf("retry=false should be omitted: %#v", sink.attrs)
			}
		})
	}
}

func TestBoundary_SpecValidationRejectsOperationAndPayloadShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec Spec
		want error
	}{
		{"unknown kind", Spec{Kind: "other", Operation: "op"}, ErrBoundaryOperation},
		{"missing operation", Spec{Kind: KindHTTP}, ErrBoundaryOperation},
		{"negative retry", Spec{Kind: KindHTTP, Operation: "op", Retry: -1}, ErrBoundaryOperation},
		{"newline", Spec{Kind: KindHTTP, Operation: "op\nsecret"}, ErrBoundaryPayload},
		{"nul", Spec{Kind: KindHTTP, Operation: "op\x00secret"}, ErrBoundaryPayload},
		{"query route", Spec{Kind: KindHTTP, Operation: "op", RouteTemplate: "/x?secret=1"}, ErrBoundaryPayload},
		{"fragment route", Spec{Kind: KindHTTP, Operation: "op", RouteTemplate: "/x#secret"}, ErrBoundaryPayload},
		{"absolute route", Spec{Kind: KindHTTP, Operation: "op", RouteTemplate: "https://host/x"}, ErrBoundaryPayload},
		{"operation punctuation", Spec{Kind: KindHTTP, Operation: "get thing"}, ErrBoundaryPayload},
		{"dependency punctuation", Spec{Kind: KindHTTP, Operation: "get", Dependency: "db?secret"}, ErrBoundaryPayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			_, err := Instrument(context.Background(), &fakeSink{}, tc.spec, func(context.Context) (struct{}, error) { called = true; return struct{}{}, nil })
			if !errors.Is(err, tc.want) || called {
				t.Fatalf("error=%v called=%v, want %v and no callback", err, called, tc.want)
			}
		})
	}
}
