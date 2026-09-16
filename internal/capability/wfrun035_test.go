package capability_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// TestGatewayCopiesTheAuthorizedTenantOntoEveryEvidenceRecord proves the
// WF-RUN-035 tenant carriage: the gateway stamps the tenant the caller's
// verified principal names onto the INVOKED record and onto a refusal, and
// leaves it empty when the caller named none (a durable sink then refuses the
// record instead of guessing).
func TestGatewayCopiesTheAuthorizedTenantOntoEveryEvidenceRecord(t *testing.T) {
	def := validDefinition()
	r := capability.NewRegistry()
	if err := r.Register(def, echoHandler); err != nil {
		t.Fatalf("register: %v", err)
	}
	sink := &recordingSink{}
	fixed := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	gw := capability.NewGateway(r, sink, capability.WithClock(func() time.Time { return fixed }))

	allowed := allowAuth(def.AuthZScopeRef)
	allowed.Tenant = "acme"
	if _, err := gw.Invoke(context.Background(), capability.InvokeRequest{Capability: def.Key(), Authorization: allowed}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := sink.last(); got.Tenant != "acme" || got.Decision != "INVOKED" {
		t.Fatalf("invoked evidence = %+v, want tenant acme", got)
	}

	denied := capability.Authorization{Decision: capability.Deny, SubjectRef: "user:2", Tenant: "globex"}
	if _, err := gw.Invoke(context.Background(), capability.InvokeRequest{Capability: def.Key(), Authorization: denied}); err == nil {
		t.Fatal("a DENY authorization was invoked")
	}
	if got := sink.last(); got.Tenant != "globex" || got.Decision != "REFUSED" {
		t.Fatalf("refusal evidence = %+v, want tenant globex", got)
	}

	if _, err := gw.Invoke(context.Background(), capability.InvokeRequest{Capability: def.Key(), Authorization: allowAuth(def.AuthZScopeRef)}); err != nil {
		t.Fatalf("Invoke without tenant: %v", err)
	}
	if got := sink.last(); got.Tenant != "" {
		t.Fatalf("evidence invented tenant %q for a caller that named none", got.Tenant)
	}
}
