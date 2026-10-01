package app

import (
	"context"
	"strings"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The agent preflight reaches the same admitted intent owner and authority
// amendment as ExecuteIntent. Creating a proposal does not grant execution.
func TestAgentWorkflowTargetPreservesCurrentIntentAuthority(t *testing.T) {
	h := newSubmitHarness(t)
	principal := submitPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	created, err := h.Service.CreateIntent(ctx, promoteWorkerCreateRequest(t, principal, "agent-workflow-owner"))
	if err != nil {
		fatalWithDiagnostic(t, "CreateIntent", err)
	}
	request := &intentsv1.ExecuteIntentRequest{IntentId: created.GetIntent().GetIntentId(), ExpectedInstanceVersion: created.GetIntent().GetInstanceVersion(), IdempotencyKey: "agent-workflow-execute"}
	if _, err := h.Service.ResolveAgentWorkflowTarget(context.Background(), request); err == nil {
		t.Fatal("missing verified principal admitted")
	}
	stale := &intentsv1.ExecuteIntentRequest{IntentId: request.IntentId, ExpectedInstanceVersion: request.ExpectedInstanceVersion + 1, IdempotencyKey: request.IdempotencyKey}
	if _, err := h.Service.ResolveAgentWorkflowTarget(ctx, stale); err == nil || !strings.Contains(err.Error(), "revision is not current") {
		t.Fatalf("stale intent=%v", err)
	}
	if _, err := h.Service.ResolveAgentWorkflowTarget(ctx, request); err == nil {
		t.Fatal("P1A cell granted agent workflow execution")
	}
	before, err := h.Store.LoadIntent(ctx, principal.Tenant().String(), request.IntentId)
	if err != nil {
		t.Fatal(err)
	}
	if before.InstanceVersion != request.ExpectedInstanceVersion {
		t.Fatal("refused preflight changed durable intent")
	}
	if _, err := (*IntentService)(nil).ResolveAgentWorkflowTarget(ctx, request); err == nil {
		t.Fatal("nil owner admitted")
	}
	if err := h.Service.AuthorizeAgentWorkflowObservation(ctx, request.IntentId, created.GetIntent().GetCorrelationId()); err == nil {
		t.Fatal("P1A observed execution authority granted")
	}
	h.Service.executionAuthority = &ExecutionAuthority{AuthorityDigest: "sha256:owner-amendment", AdmittedIntentTypes: map[string]bool{promotion.IntentType: true}, RequiredRole: "intent_author"}
	if err := h.Service.AuthorizeAgentWorkflowObservation(ctx, request.IntentId, created.GetIntent().GetCorrelationId()); err != nil {
		t.Fatal(err)
	}
	if err := h.Service.AuthorizeAgentWorkflowObservation(ctx, request.IntentId, "unrelated-correlation"); err == nil {
		t.Fatal("unrelated workflow observed")
	}
	if err := (*IntentService)(nil).AuthorizeAgentWorkflowObservation(ctx, request.IntentId, "correlation"); err == nil {
		t.Fatal("nil owner observation allowed")
	}
}
