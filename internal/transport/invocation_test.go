package transport

import (
	"context"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_REV_103_05_AdmissionRequestPath(t *testing.T) {
	principal := newPreAdmissionTestPrincipal(t, "user-admitted")
	verifier := &preAdmissionTestVerifier{principals: map[string]*trust.Principal{"one": principal}}
	cfg := preAdmissionTestConfig(verifier)
	method := "/hcmnext.intents.v1.IntentService/GetIntent"
	metadata := MapMetadata{AuthorizationMetadataKey: {"Bearer one"}, RequestIDMetadataKey: {"req-admission"}}

	// Anonymous callers receive only the authentication failure; malformed
	// bodies are not parsed into a validity oracle before credentials verify.
	_, _, unauthenticated := Admit(context.Background(), cfg, AdmissionRequest{
		Method: method, Message: &intentsv1.GetIntentRequest{},
	})
	if unauthenticated == nil || unauthenticated.Code() != envelope.CodeUnauthenticated || unauthenticated.CorrelationID() == "" {
		t.Fatalf("anonymous malformed request result = %+v", unauthenticated)
	}

	_, _, malformed := Admit(context.Background(), cfg, AdmissionRequest{
		Metadata: metadata, Method: method, Message: &intentsv1.GetIntentRequest{},
	})
	if malformed == nil || malformed.Code() != envelope.CodeInvalidArgument || malformed.CorrelationID() != "req-admission" || malformed.EvidenceRef().ID != principal.EvidenceID() {
		t.Fatalf("authenticated malformed request projection = %+v", malformed)
	}

	forged := &intentsv1.GetIntentRequest{IntentId: "intent-1", Scope: &commonv1.ScopeContext{TenantId: "other-tenant"}}
	_, _, rejected := Admit(context.Background(), cfg, AdmissionRequest{Metadata: metadata, Method: method, Message: forged})
	if rejected == nil || rejected.Code() != envelope.CodeInvalidArgument || rejected.CorrelationID() != "req-admission" || rejected.EvidenceRef().ID != principal.EvidenceID() {
		t.Fatalf("forged trusted context projection = %+v", rejected)
	}

	valid := &intentsv1.GetIntentRequest{IntentId: "intent-1"}
	ctx, inv, err := Admit(context.Background(), cfg, AdmissionRequest{Metadata: metadata, Method: method, Kind: KindHTTPEdge, Message: valid})
	if err != nil || inv == nil || inv.Principal() != principal || inv.RequestID() != "req-admission" || inv.Method() != method || inv.Kind() != KindHTTPEdge || inv.TenantID() != "acme-corp" || inv.OrganizationScopeID() != "org-north-america" || inv.Purpose() != "hcm_operations" || inv.EvidenceID() != principal.EvidenceID() || inv.ReceivedAt().IsZero() {
		t.Fatalf("admitted invocation = %+v, error=%+v", inv, err)
	}
	if scoped, ok := InvocationFromContext(ctx); !ok || scoped != inv {
		t.Fatalf("context invocation = %+v, present=%v", scoped, ok)
	}
	if inv.String() == "" || strings.Contains(inv.String(), "digest-user-admitted") {
		t.Fatalf("invocation string is empty or leaks principal material: %q", inv.String())
	}
}

func TestTodo_REV_103_05_StreamDeadlineAndConfigDefaults(t *testing.T) {
	cfg := Config{}
	if cfg.now().IsZero() || cfg.maxDeadline() != defaultMaxDeadline || cfg.maxStreamDeadline() != defaultMaxStreamDeadline {
		t.Fatal("zero Config did not provide transport defaults")
	}
	if _, ok := cfg.validator().(DefaultValidator); !ok {
		t.Fatalf("default validator = %T", cfg.validator())
	}

	ctx, cancel := CapStreamDeadline(context.Background(), Config{MaxStreamDeadline: 40 * time.Millisecond})
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 40*time.Millisecond || time.Until(deadline) <= 0 {
		t.Fatalf("stream deadline = %v, present=%v", deadline, ok)
	}
	short, stop := context.WithTimeout(context.Background(), time.Millisecond)
	defer stop()
	kept, cancelKept := CapStreamDeadline(short, Config{MaxStreamDeadline: time.Minute})
	defer cancelKept()
	got, _ := kept.Deadline()
	want, _ := short.Deadline()
	if !got.Equal(want) {
		t.Fatalf("short caller deadline changed: got %v want %v", got, want)
	}
}
