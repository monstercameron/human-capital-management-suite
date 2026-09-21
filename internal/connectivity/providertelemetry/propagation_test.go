package providertelemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
)

const echoed = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func TestOutboundHeadersFormatsTraceparentAndCorrelation(t *testing.T) {
	h := newHarness(t)
	ctx, span := h.rec.StartDelivery(logging.WithCorrelationID(context.Background(), "corr-42"), ProviderIAM, OpDeliver, "iam:1", 1)
	defer span.End(okResult(), nil)

	hdr := OutboundHeaders(ctx)
	sc := trace.SpanContextFromContext(ctx)
	want := "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-01"
	if got := hdr.Get("traceparent"); got != want {
		t.Fatalf("traceparent = %q, want %q", got, want)
	}
	if _, ok := providerwire.ValidTraceParent(hdr.Get("traceparent")); !ok {
		t.Fatal("traceparent is not W3C-valid")
	}
	if got := hdr.Get("X-Correlation-Id"); got != "corr-42" {
		t.Fatalf("X-Correlation-Id = %q", got)
	}
	if len(hdr) != 2 {
		t.Fatalf("OutboundHeaders returned extra headers: %v", hdr)
	}

	for _, bad := range []string{"has space", strings.Repeat("c", 129), "crlf\r\nX: y", ""} {
		out := OutboundHeaders(logging.WithCorrelationID(context.Background(), bad))
		if out.Get("X-Correlation-Id") != "" || out.Get("traceparent") != "" {
			t.Errorf("OutboundHeaders(%q) = %v, want nothing", bad, out)
		}
	}
	if got := OutboundHeaders(context.Background()); len(got) != 0 {
		t.Fatalf("background context produced headers: %v", got)
	}
}

func TestInboundContextLinksEchoedTraceNeverParents(t *testing.T) {
	h := newHarness(t)
	r := httptest.NewRequest(http.MethodPost, "/callbacks/payroll", nil)
	r.Header.Set("traceparent", echoed)
	r.Header.Set("X-Correlation-Id", "corr-echo")

	base := logging.WithCorrelationID(context.Background(), "corr-own")
	ctx, in := InboundContext(base, r)
	if !in.HasLink || in.Link.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || in.SecuritySignal != "" {
		t.Fatalf("InboundTrace = %+v", in)
	}
	if in.EchoedCorrelationID != "corr-echo" {
		t.Fatalf("EchoedCorrelationID = %q", in.EchoedCorrelationID)
	}
	if id, _ := logging.CorrelationID(ctx); id != "corr-own" {
		t.Fatalf("InboundContext adopted the echoed correlation id: %q", id)
	}
	if trace.SpanContextFromContext(ctx).IsValid() {
		t.Fatal("InboundContext installed the echoed trace as the context's span")
	}

	cctx, end := h.rec.StartCallback(ctx, ProviderPayroll)
	h.rec.CallbackReceived(cctx, ProviderPayroll, CallbackAccepted, 0, "evt_1", "payroll:1")
	end()
	sp := h.flushSpans(t)[0]
	if sp.Parent.IsValid() {
		t.Fatalf("callback span has a parent %v; the echoed trace must be a link only", sp.Parent)
	}
	if sp.SpanContext.TraceID().String() == "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatal("callback span joined the echoed trace")
	}
	if len(sp.Links) != 1 || sp.Links[0].SpanContext.SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("links = %+v, want the echoed span", sp.Links)
	}
}

func TestInboundContextRejectsMalformedAndDistrustsRemoteParent(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/callbacks/iam", nil)
	r.Header.Set("traceparent", "00-NOTHEX-00f067aa0ba902b7-01")
	r.Header.Set("X-Correlation-Id", strings.Repeat("x", 200))
	_, in := InboundContext(context.Background(), r)
	if in.HasLink || in.SecuritySignal == "" || in.EchoedCorrelationID != "" {
		t.Fatalf("malformed echo accepted: %+v", in)
	}
	if _, none := InboundContext(context.Background(), nil); none.HasLink {
		t.Fatal("nil request produced a link")
	}

	// A transport layer that already extracted the same untrusted header as
	// a remote parent must not get to parent the callback span either.
	h := newHarness(t)
	remote := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: mustTraceID(t, "4bf92f3577b34da6a3ce929d0e0e4736"), SpanID: mustSpanID(t, "00f067aa0ba902b7"),
		TraceFlags: trace.FlagsSampled, Remote: true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), remote)
	_, end := h.rec.StartCallback(ctx, ProviderIAM)
	end()
	sp := h.flushSpans(t)[0]
	if sp.Parent.IsValid() || len(sp.Links) != 1 || !sp.Links[0].SpanContext.Equal(remote) {
		t.Fatalf("remote parent trusted: parent %v links %+v", sp.Parent, sp.Links)
	}

	// A local parent span is kept.
	pctx, parent := h.rec.StartDelivery(context.Background(), ProviderIAM, OpStatus, "iam:1", 1)
	_, end = h.rec.StartCallback(pctx, ProviderIAM)
	end()
	parent.End(okResult(), nil)
	local := trace.SpanContextFromContext(pctx)
	var found bool
	for _, s := range h.flushSpans(t) {
		if s.SpanKind == trace.SpanKindServer && s.SpanContext.TraceID() == local.TraceID() {
			found = true
			if s.Parent.SpanID() != local.SpanID() {
				t.Fatalf("local parent dropped: %v", s.Parent)
			}
		}
	}
	if !found {
		t.Fatal("callback span under a local parent did not join the parent's trace")
	}
}

func mustTraceID(t *testing.T, s string) trace.TraceID {
	t.Helper()
	id, err := trace.TraceIDFromHex(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSpanID(t *testing.T, s string) trace.SpanID {
	t.Helper()
	id, err := trace.SpanIDFromHex(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
