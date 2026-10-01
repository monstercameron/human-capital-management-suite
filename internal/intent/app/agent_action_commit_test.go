package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type agentCommitAuthorityTest struct {
	pin AgentIntentCommitPin
	err error
}

func (a *agentCommitAuthorityTest) VerifyAgentIntentCommit(_ context.Context, pin AgentIntentCommitPin) error {
	a.pin = pin
	return a.err
}

func TestAgentOriginRequiresCurrentCommitAuthority(t *testing.T) {
	h := newSubmitHarness(t)
	p := submitPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), p)
	req := promoteWorkerCreateRequest(t, p, "agent-commit-pin")
	origin := "agent-run:immutable-source-pin"
	req.OriginEventRef = &origin
	created, err := h.Service.CreateIntent(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	call := PromotionStepCall{IntentID: created.Intent.IntentId, Delegation: runtime.ExecutionDelegation{TenantKey: p.Tenant().String(), Subject: p.Subject()}}
	if err := h.Service.verifyAgentIntentCommit(ctx, call); !errors.Is(err, ErrAgentActionDraft) {
		t.Fatalf("agent origin with missing commit authority: %v", err)
	}
	authority := &agentCommitAuthorityTest{err: ErrAgentActionDraft}
	if err := h.Service.BindAgentIntentCommitAuthority(authority); err != nil {
		t.Fatal(err)
	}
	if err := h.Service.verifyAgentIntentCommit(ctx, call); !errors.Is(err, ErrAgentActionDraft) {
		t.Fatalf("revoked origin accepted: %v", err)
	}
	if authority.pin.TenantID != p.Tenant().String() || authority.pin.IntentID != call.IntentID || authority.pin.ForUser != p.Subject() || authority.pin.OriginEventRef != origin {
		t.Fatalf("commit source binding: %+v", authority.pin)
	}
	page, err := h.Store.ListIntents(ctx, p.Tenant().String(), 50, "")
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("authority check created business drafts: %v %v", page, err)
	}
}
