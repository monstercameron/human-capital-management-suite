package otelmw_test

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// attrKeys returns the sorted attribute-key set of span, so two spans'
// attribute shapes can be compared without caring about attribute order.
func attrKeys(span tracetest.SpanStub) []string {
	keys := make([]string, 0, len(span.Attributes))
	for _, kv := range span.Attributes {
		keys = append(keys, string(kv.Key))
	}
	sort.Strings(keys)
	return keys
}

// attrValue returns one attribute's string value from span.
func attrValue(span tracetest.SpanStub, key string) (string, bool) {
	for _, kv := range span.Attributes {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// findSpan returns the last exported span named name, so a test can find
// its own call's span even when the harness's setup produced others.
func findSpan(t testing.TB, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range slices.Backward(spans) {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("no exported span named %q among %d spans", name, len(spans))
	return tracetest.SpanStub{}
}

// awaitSpans polls h's Provider (ForceFlush, then GetSpans) to a deadline
// until at least want spans named procedure have been exported, since a
// client observing its own call complete is not synchronized with a
// server-side span's End() call — see
// TestTodo_OBS_002_TransportInterceptorsPropagateDeadline's own comment. It
// returns whatever matched at the deadline, which may be fewer than want.
func awaitSpans(t testing.TB, h *harness, procedure string, want int, timeout time.Duration) tracetest.SpanStubs {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if report := h.provider.ForceFlush(context.Background()); report.Err() != nil {
			t.Fatalf("ForceFlush: %v", report.Err())
		}
		var matched tracetest.SpanStubs
		for _, s := range h.spanExporter.GetSpans() {
			if s.Name == procedure {
				matched = append(matched, s)
			}
		}
		if len(matched) >= want || time.Now().After(deadline) {
			return matched
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestTodo_OBS_002_TransportInterceptorsEmitRedactedSpansOnBothTransports is
// the OBS-002 follow-up primary test doc.go's "Out of scope" section
// promises: the same call, made once over native gRPC (bufconn) and once
// over the HTTP edge (httptest), produces spans with an identical
// attribute-key set after redaction, and a caller-supplied header that is
// not part of the reserved trusted-context list — so admission lets the
// call through — never reaches a span attribute, because this package
// never reads raw metadata/headers at all; it only reads what admission
// already verified and placed in context.
func TestTodo_OBS_002_TransportInterceptorsEmitRedactedSpansOnBothTransports(t *testing.T) {
	h := newHarness(t)

	const spoofedPrincipalValue = "attacker-supplied-principal-should-never-appear"
	const procedure = "/hcmnext.intents.v1.IntentService/GetIntent"

	// gRPC call, plus one caller-supplied header that is not on
	// trust.ReservedMetadataKeys, so admission accepts the call rather than
	// rejecting it outright the way it would for a reserved name like
	// "x-principal".
	grpcCtx := h.grpcContext(context.Background(), "x-debug-principal-hint", spoofedPrincipalValue)
	grpcResp, err := h.grpcIntent.GetIntent(grpcCtx, &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
	if err != nil {
		t.Fatalf("grpc GetIntent: %v", err)
	}
	if grpcResp.GetIntent().GetIntentId() != transporttest.KnownIntentID {
		t.Fatalf("grpc GetIntent returned %q, want %q", grpcResp.GetIntent().GetIntentId(), transporttest.KnownIntentID)
	}

	// Edge call, same scenario, same spoofed header.
	edgeReq := edgeRequest(h, &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID},
		"x-debug-principal-hint", spoofedPrincipalValue)
	edgeResp, err := h.edgeIntent.GetIntent(context.Background(), edgeReq)
	if err != nil {
		t.Fatalf("edge GetIntent: %v", err)
	}
	if edgeResp.Msg.GetIntent().GetIntentId() != transporttest.KnownIntentID {
		t.Fatalf("edge GetIntent returned %q, want %q", edgeResp.Msg.GetIntent().GetIntentId(), transporttest.KnownIntentID)
	}

	if report := h.provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}

	spans := h.spanExporter.GetSpans()
	grpcSpan := findSpan(t, spans, procedure)
	// Both calls used the same span name; find the edge one by locating a
	// second span with the same name (GetSpans preserves export order).
	var edgeSpan tracetest.SpanStub
	seen := 0
	for _, s := range spans {
		if s.Name == procedure {
			seen++
			if seen == 2 {
				edgeSpan = s
			}
		}
	}
	if seen != 2 {
		t.Fatalf("expected 2 spans named %q (one per transport), found %d", procedure, seen)
	}

	grpcKeys, edgeKeys := attrKeys(grpcSpan), attrKeys(edgeSpan)
	if strings.Join(grpcKeys, ",") != strings.Join(edgeKeys, ",") {
		t.Fatalf("attribute-key sets differ between transports:\n  grpc: %v\n  edge: %v", grpcKeys, edgeKeys)
	}

	// correlation_id is the one context-propagation key the frozen
	// allow-list currently keeps for spans (see
	// internal/platform/telemetry/otel/trace.go); it must be present and
	// identical on both transports since both used fixedRequestID.
	grpcCorrelation, ok := attrValue(grpcSpan, "correlation_id")
	if !ok || grpcCorrelation != fixedRequestID {
		t.Fatalf("grpc span correlation_id = %q, %v; want %q", grpcCorrelation, ok, fixedRequestID)
	}
	edgeCorrelation, ok := attrValue(edgeSpan, "correlation_id")
	if !ok || edgeCorrelation != fixedRequestID {
		t.Fatalf("edge span correlation_id = %q, %v; want %q", edgeCorrelation, ok, fixedRequestID)
	}

	// request_id, evidence_ref and principal_ref are not registered
	// attribute keys today (doc.go, trace.go) and must be redacted on both
	// transports identically.
	for _, key := range []string{"request_id", "evidence_ref", "principal_ref"} {
		if _, ok := attrValue(grpcSpan, key); ok {
			t.Errorf("grpc span kept unregistered attribute %q; want redacted", key)
		}
		if _, ok := attrValue(edgeSpan, key); ok {
			t.Errorf("edge span kept unregistered attribute %q; want redacted", key)
		}
	}

	// The successful outcome is recorded through the allow-listed "outcome"
	// attribute on both transports.
	if v, ok := attrValue(grpcSpan, "outcome"); !ok || v != "success" {
		t.Errorf("grpc span outcome = %q, %v; want \"success\"", v, ok)
	}
	if v, ok := attrValue(edgeSpan, "outcome"); !ok || v != "success" {
		t.Errorf("edge span outcome = %q, %v; want \"success\"", v, ok)
	}

	// The whole point: a caller-supplied header admission did not reject
	// must still never reach an exported span attribute, on either
	// transport, because this package derives every identifier from
	// context transport.Admit already populated and never parses inbound
	// metadata/headers itself.
	for _, span := range []tracetest.SpanStub{grpcSpan, edgeSpan} {
		for _, kv := range span.Attributes {
			if strings.Contains(kv.Value.AsString(), spoofedPrincipalValue) {
				t.Errorf("span %q attribute %s carries the caller-supplied header value %q",
					span.Name, kv.Key, kv.Value.AsString())
			}
		}
	}
}

// TestTodo_OBS_002_TransportInterceptorsPropagateDeadline proves item (5):
// the context this package's interceptor hands to the real handler still
// carries the caller's deadline, on both transports. transporttest's
// BlockingIntentID scenario blocks SimulateIntent on <-ctx.Done(); it can
// only ever unblock if the deadline reached the handler, so a bounded test
// timeout that observes the call actually end proves propagation rather
// than assuming it.
func TestTodo_OBS_002_TransportInterceptorsPropagateDeadline(t *testing.T) {
	h := newHarness(t)
	const shortDeadline = 200 * time.Millisecond

	t.Run("grpc", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(h.grpcContext(context.Background()), shortDeadline)
		defer cancel()
		_, err := h.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: transporttest.BlockingIntentID})
		if err == nil {
			t.Fatal("grpc SimulateIntent on the blocking scenario returned success; want deadline expiry")
		}
		if st, ok := status.FromError(err); !ok || st.Code() != codes.DeadlineExceeded {
			t.Fatalf("grpc SimulateIntent error = %v; want DEADLINE_EXCEEDED", err)
		}
	})

	t.Run("edge", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), shortDeadline)
		defer cancel()
		req := edgeRequest(h, &intentsv1.SimulateIntentRequest{IntentId: transporttest.BlockingIntentID})
		_, err := h.edgeIntent.SimulateIntent(ctx, req)
		if err == nil {
			t.Fatal("edge SimulateIntent on the blocking scenario returned success; want deadline expiry")
		}
		var connectErr *connect.Error
		var ownedErr *envelope.Error
		switch {
		case errors.As(err, &connectErr) && connectErr.Code() == connect.CodeDeadlineExceeded:
			// Raw transport form.
		case errors.As(err, &ownedErr) && ownedErr.Code() == envelope.CodeDeadlineExceeded:
			// Canonical generated backend decodes into the owned error.
		default:
			t.Fatalf("edge SimulateIntent error = %v; want deadline_exceeded", err)
		}
	})

	// The client observing its own context expire (above) is independent of
	// when the blocked server-side handler goroutine actually unblocks and
	// this package's deferred span.End() runs: the client's local deadline
	// fires, grpc-go/connect-go report DeadlineExceeded to it immediately,
	// and only afterward does the cancellation reach the still-blocked
	// server handler over the wire. awaitSpans polls to a deadline rather
	// than assuming the server side has already finished by the time the
	// client call returned.
	const procedure = "/hcmnext.intents.v1.IntentService/SimulateIntent"
	spans := awaitSpans(t, h, procedure, 2, 3*time.Second)
	if len(spans) != 2 {
		t.Fatalf("found %d SimulateIntent spans within the deadline; want 2 (one per transport)", len(spans))
	}
	for i, span := range spans {
		// The server-side context can end either way once the client walks
		// away: its own (later-computed) deadline elapsing classifies as
		// DEADLINE_EXCEEDED, but the client's cancellation signal typically
		// arrives first on a fast loopback transport and classifies as
		// UNAVAILABLE (transport.classify's context.Canceled case) — both
		// are transport.OwnedError's deterministic projection of a real ctx
		// termination, and either one proves the deadline this package's
		// interceptor propagated is what ended the call.
		if v, ok := attrValue(span, "error_type"); !ok || (v != "DEADLINE_EXCEEDED" && v != "UNAVAILABLE") {
			t.Errorf("span %d error_type = %q, %v; want \"DEADLINE_EXCEEDED\" or \"UNAVAILABLE\"", i, v, ok)
		}
		if v, ok := attrValue(span, "outcome"); !ok || v != "error" {
			t.Errorf("span %d outcome = %q, %v; want \"error\"", i, v, ok)
		}
		if span.Status.Code != otelcodes.Error {
			t.Errorf("span %d status code = %v; want Error", i, span.Status.Code)
		}
	}
}

// TestTodo_OBS_002_TransportInterceptorsSetErrorSpanStatus proves item (4):
// span status and the "outcome"/"error_type" attributes are set from the
// envelope.Error code a handler returned, on both transports, for an
// ordinary typed domain failure (GetIntent's NOT_FOUND for an intent id the
// fixture does not know).
func TestTodo_OBS_002_TransportInterceptorsSetErrorSpanStatus(t *testing.T) {
	h := newHarness(t)
	const procedure = "/hcmnext.intents.v1.IntentService/GetIntent"
	const missingID = "intent-does-not-exist"

	_, err := h.grpcIntent.GetIntent(h.grpcContext(context.Background()), &intentsv1.GetIntentRequest{IntentId: missingID})
	if err == nil {
		t.Fatal("grpc GetIntent(missing) returned success; want NOT_FOUND")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.NotFound {
		t.Fatalf("grpc GetIntent(missing) error = %v; want NOT_FOUND", err)
	}

	_, err = h.edgeIntent.GetIntent(context.Background(), edgeRequest(h, &intentsv1.GetIntentRequest{IntentId: missingID}))
	if err == nil {
		t.Fatal("edge GetIntent(missing) returned success; want not_found")
	}
	var connectErr *connect.Error
	var ownedErr *envelope.Error
	switch {
	case errors.As(err, &connectErr) && connectErr.Code() == connect.CodeNotFound:
		// Raw transport form.
	case errors.As(err, &ownedErr) && ownedErr.Code() == envelope.CodeNotFound:
		// Canonical generated backend decodes into the owned error.
	default:
		t.Fatalf("edge GetIntent(missing) error = %v; want not_found", err)
	}

	spans := awaitSpans(t, h, procedure, 2, 3*time.Second)
	if len(spans) != 2 {
		t.Fatalf("found %d GetIntent spans within the deadline; want 2 (one per transport)", len(spans))
	}
	for i, span := range spans {
		if v, ok := attrValue(span, "error_type"); !ok || v != "NOT_FOUND" {
			t.Errorf("span %d error_type = %q, %v; want \"NOT_FOUND\"", i, v, ok)
		}
		if v, ok := attrValue(span, "outcome"); !ok || v != "error" {
			t.Errorf("span %d outcome = %q, %v; want \"error\"", i, v, ok)
		}
		if span.Status.Code != otelcodes.Error {
			t.Errorf("span %d status code = %v; want Error", i, span.Status.Code)
		}
	}
}
