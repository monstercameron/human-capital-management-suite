package edge_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_EDGE_001 is the EDGE-001 primary test.
//
// RED: reject oversize headers/body, missing deadline, untrusted forwarded
// host and malformed framing before domain work.
//
// GREEN: an accepted envelope has normalized route/context/correlation and
// identical gRPC semantics - proven here by driving
// [transporttest.RefusalMatrix] through all three published edges (native
// gRPC, the HTTP/Connect edge and the gRPC-over-WebSocket tunnel) of one
// composed, ephemeral-PostgreSQL-backed cell and asserting the outcomes are
// field-identical.
func TestTodo_EDGE_001(t *testing.T) {
	h := newHarness(t)
	edges := h.callers()

	for _, sc := range transporttest.RefusalMatrix() {
		t.Run(sc.Name, func(t *testing.T) {
			outcomes := make(map[string]transporttest.ConformanceOutcome, len(edges))
			for name, call := range edges {
				outcomes[name] = call(sc)
			}
			transporttest.AssertConformant(t, sc, outcomes)
		})
	}
}

// findScenario returns the named row of [transporttest.RefusalMatrix], or
// fails tb. It exists so the matrix stays the one place a row's shape is
// defined; a test that needs to inspect one row's answer in detail (rather
// than only its cross-edge parity) still drives it from there.
func findScenario(tb testing.TB, name string) transporttest.ConformanceScenario {
	tb.Helper()
	for _, sc := range transporttest.RefusalMatrix() {
		if sc.Name == name {
			return sc
		}
	}
	tb.Fatalf("no scenario named %q in RefusalMatrix", name)
	return transporttest.ConformanceScenario{}
}

// TestTodo_EDGE_001_Golden pins the exact shape of one refusal's owned
// projection through the HTTP/Connect edge: the reason, the single violation
// naming the request root, the non-retryable classification and the
// canonical HTTP status. A conformance matrix that only checked "the three
// edges agree" without also pinning what they agree on would pass just as
// happily if all three quietly drifted together.
func TestTodo_EDGE_001_Golden(t *testing.T) {
	h := newHarness(t)
	sc := findScenario(t, "unknown field")

	outcome := h.connectCaller()(sc)
	if outcome.Err == nil {
		t.Fatal("a material unknown field was accepted")
	}
	if got := outcome.Err.Code(); got != envelope.CodeInvalidArgument {
		t.Errorf("code = %v, want INVALID_ARGUMENT", got)
	}
	if got := outcome.Err.ReasonRef(); got != "structural.request_rejected" {
		t.Errorf("reason = %q, want structural.request_rejected", got)
	}
	if outcome.Err.Retryable() {
		t.Error("a malformed request must not be classified retryable")
	}
	if got := outcome.Err.HTTPStatus(); got != http.StatusBadRequest {
		t.Errorf("HTTP status = %d, want 400", got)
	}
	violations := outcome.Err.Violations()
	if len(violations) != 1 {
		t.Fatalf("violations = %+v, want exactly one", violations)
	}
	if violations[0].FieldPath != "(request)" || violations[0].RuleRef != "strict_decoding.unknown_field" {
		t.Errorf("violation = %+v, want (request)/strict_decoding.unknown_field", violations[0])
	}
}

// TestTodo_EDGE_001_Integration proves the acceptance path, not only the
// refusal path: a request that admission accepts reaches the identical real
// domain code on all three edges, including when that domain code itself
// returns an owned error (NOT_FOUND for an intent that does not exist). A
// suite that only exercised admission-stage refusals would never notice three
// edges disagreeing about what happens after admission succeeds.
func TestTodo_EDGE_001_Integration(t *testing.T) {
	h := newHarness(t)
	edges := h.callers()

	t.Run("a domain-level NOT_FOUND is identical on every edge", func(t *testing.T) {
		sc := transporttest.ConformanceScenario{
			Name:       "unknown intent id resolves to a domain NOT_FOUND",
			Method:     transporttest.ConformanceMethodGetIntent,
			Credential: transporttest.ConformanceCredentialValid,
			IntentID:   "intent-edge001-does-not-exist",
			WantCode:   envelope.CodeNotFound,
		}
		outcomes := make(map[string]transporttest.ConformanceOutcome, len(edges))
		for name, call := range edges {
			outcomes[name] = call(sc)
		}
		transporttest.AssertConformant(t, sc, outcomes)
	})

	t.Run("a valid call is actually answered on every edge, not merely admitted", func(t *testing.T) {
		sc := findScenario(t, "valid call")
		for name, call := range edges {
			if outcome := call(sc); outcome.Err != nil {
				t.Errorf("%s: ListIntents failed: %v", name, outcome.Err)
			}
		}
	})
}

// TestTodo_EDGE_001_Security is the interesting adversarial case ENDPOINT-002
// already covers for two edges: an authenticated caller who names another
// tenant in the request body itself, not merely in metadata. It is checked
// here across all three edges because the body-scope check
// (ApplyTrustedContext) runs inside the same [transport.Admit] the metadata
// screen does, and a conformance claim that tested only the metadata half of
// the trusted-request boundary would miss a regression in the other half.
func TestTodo_EDGE_001_Security(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	headers := map[string]string{
		transport.AuthorizationMetadataKey: h.token,
		transport.RequestIDMetadataKey:     "req-edge001-security",
	}
	crossTenantRequest := func() *intentsv1.GetIntentRequest {
		return &intentsv1.GetIntentRequest{
			IntentId: "intent-edge001-cross-tenant",
			Scope:    &commonv1.ScopeContext{TenantId: "victim-corp-edge001"},
		}
	}

	outcomes := map[string]transporttest.ConformanceOutcome{}

	nativeCtx, cancel := context.WithTimeout(attachMetadata(ctx, headers), defaultCallDeadline)
	defer cancel()
	if _, err := h.grpcIntent.GetIntent(nativeCtx, crossTenantRequest()); err == nil {
		t.Fatal("native gRPC accepted a cross-tenant scope from an authenticated caller")
	} else {
		outcomes["native_grpc"] = transporttest.ConformanceOutcome{Err: ownedFromGRPCLike(err)}
	}

	tunnelCtx, cancel2 := context.WithTimeout(attachMetadata(ctx, headers), defaultCallDeadline)
	defer cancel2()
	if _, err := h.tunnelIntent.GetIntent(tunnelCtx, crossTenantRequest()); err == nil {
		t.Fatal("the tunnel accepted a cross-tenant scope from an authenticated caller")
	} else {
		outcomes["tunnel"] = transporttest.ConformanceOutcome{Err: ownedFromGRPCLike(err)}
	}

	connectCtx, cancel3 := context.WithTimeout(ctx, defaultCallDeadline)
	defer cancel3()
	if _, err := h.edgeIntent.GetIntent(connectCtx, crossTenantRequest(), clientHeaders(headers)...); err == nil {
		t.Fatal("the HTTP/Connect edge accepted a cross-tenant scope from an authenticated caller")
	} else {
		outcomes["connect_http_edge"] = transporttest.ConformanceOutcome{Err: ownedFromConnect(err)}
	}

	sc := transporttest.ConformanceScenario{Name: "a body field cannot select another tenant", WantCode: envelope.CodeInvalidArgument}
	transporttest.AssertConformant(t, sc, outcomes)
}

// FuzzTodo_EDGE_001 fuzzes the reserved-metadata screen directly through
// [transport.Admit], configured with this suite's own verifier and audience.
// It does not drive the fuzz corpus over three real wire transports - that
// would be too slow to be a useful fuzz target - but it exercises the exact
// function every one of those transports calls (grpcserver's admit(),
// edge's admissionInterceptor, and the tunnel's forwarded per-RPC admission
// are three call sites for the one function this fuzzes), so a regression
// here is a regression on all three edges at once.
func FuzzTodo_EDGE_001(f *testing.F) {
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      testSigningKey,
		Issuer:   testIssuer,
		Audience: testAudience,
		Now:      func() time.Time { return baseTime },
	})
	if err != nil {
		f.Fatalf("NewHMACVerifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: testIssuer, Audience: testAudience, Subject: testSubject, SubjectKind: "human",
		Tenant: testTenant, OrganizationScopeID: testOrgScope, Roles: []string{"intent_author"},
		Purposes: []string{"workforce_analytics"}, AuthenticationMethod: "bearer_token",
		Assurance: "substantial", SessionRef: "session-fuzz",
		IssuedAtUnix: baseTime.Add(-time.Minute).Unix(), ExpiresAtUnix: baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		f.Fatalf("issue credential: %v", err)
	}
	cfg := transport.Config{Verifier: verifier, Audience: testAudience, Now: func() time.Time { return baseTime }}

	f.Add("x-tenant", "victim-corp")
	f.Add("x-request-id", "abc")
	f.Add("X-Principal", "root")
	f.Add("content-type", "application/grpc")
	f.Add("", "")
	f.Add("x-purpose", strings.Repeat("p", 4000))

	f.Fuzz(func(t *testing.T, metaName, metaValue string) {
		md := transport.MapMetadata{transport.AuthorizationMetadataKey: {"Bearer " + token}}
		if metaName != "" {
			md[strings.ToLower(metaName)] = []string{metaValue}
		}
		req := &intentsv1.GetIntentRequest{IntentId: "intent-fuzz-edge001"}

		_, inv, admitErr := transport.Admit(context.Background(), cfg, transport.AdmissionRequest{
			Metadata: md,
			Method:   "/hcmnext.intents.v1.IntentService/GetIntent",
			Kind:     transport.KindGRPC,
			Message:  req,
		})
		if admitErr != nil {
			if inv != nil {
				t.Fatal("Admit returned both an error and an invocation")
			}
			return
		}
		if trust.IsReservedMetadataKey(metaName) {
			t.Fatalf("reserved metadata name %q was admitted", metaName)
		}
		if inv.Principal().Subject() != testSubject {
			t.Fatalf("admitted request resolved subject %q, want %q", inv.Principal().Subject(), testSubject)
		}
		if inv.TenantID() != testTenant {
			t.Fatalf("admitted request resolved tenant %q, want %q", inv.TenantID(), testTenant)
		}
	})
}
