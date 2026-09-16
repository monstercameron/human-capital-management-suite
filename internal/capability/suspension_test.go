package capability_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// fixedSuspension is a suspension source with one canned answer.
type fixedSuspension struct {
	suspend map[string]string
	asked   []string
}

func (s *fixedSuspension) SuspendedCapability(_ context.Context, id string) (string, bool) {
	s.asked = append(s.asked, id)
	reason, ok := s.suspend[id]
	return reason, ok
}

// TestSuspendedCapabilityIsRefusedBeforeAuthorizationAndHandler proves the
// gateway's suspension check: a suspended capability refuses with
// CAPABILITY_SUSPENDED and its handler never runs even for a fully authorized
// caller, the refusal is evidenced like every other, a source that names no
// reason still produces one, and a gateway composed without a source behaves
// exactly as it did before.
func TestSuspendedCapabilityIsRefusedBeforeAuthorizationAndHandler(t *testing.T) {
	ctx := context.Background()
	def := validDefinition()
	registry := capability.NewRegistry()
	invoked := 0
	handler := func(c context.Context, payload any) (any, error) { invoked++; return payload, nil }
	if err := registry.Register(def, handler); err != nil {
		t.Fatalf("register: %v", err)
	}

	// No suspension source: the gateway is unchanged.
	sink := &recordingSink{}
	plain := capability.NewGateway(registry, sink)
	if _, err := plain.Invoke(ctx, capability.InvokeRequest{Capability: def.Key(), Authorization: allowAuth(def.AuthZScopeRef)}); err != nil {
		t.Fatalf("invoke without a suspension source: %v", err)
	}
	if invoked != 1 {
		t.Fatalf("handler ran %d times, want 1", invoked)
	}

	// Suspended: refused before the handler, with evidence.
	src := &fixedSuspension{suspend: map[string]string{def.ID: "authority family workflow.repair is suspended"}}
	sink = &recordingSink{}
	gw := capability.NewGateway(registry, sink, capability.WithSuspensions(src))
	_, err := gw.Invoke(ctx, capability.InvokeRequest{Capability: def.Key(), Authorization: allowAuth(def.AuthZScopeRef)})
	var gerr *capability.GatewayError
	if !errors.As(err, &gerr) || gerr.Code != capability.CodeCapabilitySuspended {
		t.Fatalf("invoke of a suspended capability = %v, want %s", err, capability.CodeCapabilitySuspended)
	}
	if gerr.Reason != "authority family workflow.repair is suspended" {
		t.Errorf("refusal reason = %q; the source's reason was dropped", gerr.Reason)
	}
	if invoked != 1 {
		t.Fatalf("a suspended capability ran its handler")
	}
	if sink.count() != 1 || sink.last().Decision != "REFUSED" || sink.last().ReasonCode != capability.CodeCapabilitySuspended {
		t.Errorf("suspension evidence = %+v", sink.last())
	}

	// The suspension outranks authorization: an unauthorized caller gets the
	// suspension refusal, so no caller can probe past it.
	_, err = gw.Invoke(ctx, capability.InvokeRequest{Capability: def.Key(), Authorization: capability.Authorization{Decision: capability.Deny}})
	if !errors.As(err, &gerr) || gerr.Code != capability.CodeCapabilitySuspended {
		t.Fatalf("unauthorized invoke of a suspended capability = %v, want %s", err, capability.CodeCapabilitySuspended)
	}

	// A source that suspends without a reason still yields one.
	quiet := capability.NewGateway(registry, &recordingSink{}, capability.WithSuspensions(&fixedSuspension{suspend: map[string]string{def.ID: ""}}))
	_, err = quiet.Invoke(ctx, capability.InvokeRequest{Capability: def.Key(), Authorization: allowAuth(def.AuthZScopeRef)})
	if !errors.As(err, &gerr) || gerr.Code != capability.CodeCapabilitySuspended || gerr.Reason == "" {
		t.Fatalf("reasonless suspension = %v", err)
	}

	// A source that admits the capability changes nothing.
	admitting := capability.NewGateway(registry, &recordingSink{}, capability.WithSuspensions(&fixedSuspension{}))
	if _, err := admitting.Invoke(ctx, capability.InvokeRequest{Capability: def.Key(), Authorization: allowAuth(def.AuthZScopeRef)}); err != nil {
		t.Fatalf("invoke under an admitting suspension source: %v", err)
	}
	if invoked != 2 {
		t.Fatalf("handler ran %d times, want 2", invoked)
	}
}

// TestTodo_WF_RUN_039_Suspension proves the real bridge: an overdue operator
// bypass obligation suspends the governed capability at the capability gateway
// itself, not merely in a report, and the review lifts it. The operator
// gateway's suspension source satisfies capability.SuspensionSource
// structurally, so this is the production wiring, not a stand-in.
func TestTodo_WF_RUN_039_Suspension(t *testing.T) {
	ctx := context.Background()
	const tenant = values.TenantId("tenant-ops")
	recordedAt := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	now := recordedAt

	journal := operator.NewMemoryJournal()
	obligation := operator.Obligation{
		ID: operator.ObligationID(tenant, "key-bypass"), Tenant: tenant, Kind: operator.KindWorkflowRepair,
		Family: operator.FamilyRepair, Scope: operator.Scope{Resource: "workflow_instance", IDs: []string{"instance-1"}},
		Operator: "operator:ana", Approver: "approver:lead", AuthorityKind: operator.AuthorityBreakGlass,
		AuthorityRef: "bg-39", IdempotencyKey: "key-bypass", RequestDigest: "sha256:request",
		TicketRef: "INC-39", Bypassed: []string{operator.BypassJITAuthority}, BypassReason: "region down",
		RecordedAt: recordedAt, DueAt: recordedAt.Add(operator.ReviewWindow),
	}.Sealed()
	if err := journal.RecordObligation(ctx, obligation); err != nil {
		t.Fatalf("record obligation: %v", err)
	}
	src, err := operator.NewCapabilitySuspensions(journal, operator.FixedTenant(tenant), func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCapabilitySuspensions: %v", err)
	}
	var _ capability.SuspensionSource = src

	repair := validDefinition()
	repair.ID, repair.AuthZScopeRef = "workflow.repair.retry_node", "scope:workflow.repair"
	unrelated := validDefinition()
	unrelated.ID, unrelated.AuthZScopeRef = "workflow.instances.pause", "scope:workflow.instances"
	registry := capability.NewRegistry()
	for _, def := range []capability.Definition{repair, unrelated} {
		if err := registry.Register(def, echoHandler); err != nil {
			t.Fatalf("register %s: %v", def.ID, err)
		}
	}
	gw := capability.NewGateway(registry, &recordingSink{}, capability.WithSuspensions(src))
	invoke := func(def capability.Definition) error {
		_, err := gw.Invoke(ctx, capability.InvokeRequest{Capability: def.Key(), Authorization: allowAuth(def.AuthZScopeRef)})
		return err
	}

	// Inside the review window both capabilities invoke.
	if err := invoke(repair); err != nil {
		t.Fatalf("repair capability inside the review window: %v", err)
	}

	// Past the due review the repair capability is refused at the gateway and
	// the unrelated family is untouched.
	now = obligation.DueAt
	var gerr *capability.GatewayError
	if err := invoke(repair); !errors.As(err, &gerr) || gerr.Code != capability.CodeCapabilitySuspended {
		t.Fatalf("repair capability past its due review = %v, want %s", err, capability.CodeCapabilitySuspended)
	}
	if err := invoke(unrelated); err != nil {
		t.Fatalf("an unrelated family was suspended: %v", err)
	}

	// The review lifts the suspension.
	if _, err := journal.DischargeObligation(ctx, tenant, obligation.ID, operator.ObligationReview{
		Reviewer: "reviewer:sam", Outcome: operator.ObligationJustified, Note: "outage confirmed", At: now,
	}); err != nil {
		t.Fatalf("discharge: %v", err)
	}
	if err := invoke(repair); err != nil {
		t.Fatalf("repair capability after the review: %v", err)
	}
}
