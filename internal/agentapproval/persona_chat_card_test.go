package agentapproval

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTodo_AGENTP_014_ApprovalCardProjection(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	service, err := New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierSubmitGoverned, "low"))
	card, err := BuildPersonaChatCard(approval, "invocation-1", approval.AssignedUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	if card.Digest != approval.Digest || card.InvocationID != "invocation-1" || card.InvokerID != approval.AssignedUserID || !card.ExpiresAt.Equal(now.Add(PersonaChatCardLifetime)) {
		t.Fatalf("card bindings = %+v, approval=%+v", card, approval)
	}
	markup, err := RenderPersonaChatCardForViewer(card, approval.AssignedUserID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, card.Digest) || !strings.Contains(markup, "open_task_view") || !strings.Contains(markup, "/workspace/app/agents?task=task-1") {
		t.Fatalf("typed chat card lacks exact digest and task link: %s", markup)
	}
	if !strings.Contains(markup, "PromoteWorker v3") || !strings.Contains(markup, "job.level | L4 → L5") || !strings.Contains(markup, "report-7 (TRUSTED_INTERNAL)") || !strings.Contains(markup, "source is current as of the task checkpoint") {
		t.Fatalf("typed chat card omitted material review details: %s", markup)
	}
	other, err := RenderPersonaChatCardForViewer(card, "colleague")
	if err != nil || other != "" {
		t.Fatalf("card rendered to another member: markup=%q err=%v", other, err)
	}
	callback := PersonaChatCardCallback{Action: personaChatOpenTask, ActorID: approval.AssignedUserID, InvocationID: card.InvocationID, Now: now.Add(time.Minute), Current: approval}
	link, err := ResolvePersonaChatCardCallback(card, callback)
	if err != nil || link != card.TaskViewHref {
		t.Fatalf("valid task-view callback link=%q err=%v", link, err)
	}
	if _, err := ResolvePersonaChatCardCallback(card, PersonaChatCardCallback{Action: "approve", ActorID: approval.AssignedUserID, InvocationID: card.InvocationID, Now: now, Current: approval}); !errors.Is(err, ErrChatApprovalSurface) {
		t.Fatalf("chat callback bypassed AGENT2-006 decision surface: %v", err)
	}
}

func TestTodo_AGENTP_014_Golden(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	service, err := New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierSubmitGoverned, "low"))
	card, err := BuildPersonaChatCard(approval, "invocation-1", approval.AssignedUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RenderPersonaChatCardForViewer(card, approval.AssignedUserID)
	if err != nil {
		t.Fatal(err)
	}
	want := `<div class="chatapp-card" data-card-kind="agent_action_approval"><div data-field="approval_id">approval-1</div><div data-field="invocation_id">invocation-1</div><div data-field="invoker_id">user-1</div><div data-field="item_digest">__DIGEST__</div><div data-field="expires_at">2026-09-28T12:15:00Z</div><div data-field="task_view">/workspace/app/agents?task=task-1</div><div data-field="route">open_task_view</div><div data-field="review_reason">product_approval_surface_required</div><div data-field="actions">item-1: PromoteWorker v3</div><div data-field="material_changes">item-1: job.level | L4 → L5</div><div data-field="sources">item-1: report-7 (TRUSTED_INTERNAL)</div><div data-field="uncertainty">item-1: source is current as of the task checkpoint</div></div>`
	want = strings.ReplaceAll(want, "__DIGEST__", card.Digest)
	if got != want {
		t.Fatalf("typed approval card bytes changed:\n got %s\nwant %s", got, want)
	}
}

func TestTodo_AGENTP_014_ApprovalCardSecurity(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	service, err := New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierSubmitGoverned, "low"))
	card, err := BuildPersonaChatCard(approval, "invocation-1", approval.AssignedUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*PersonaChatCard, *PersonaChatCardCallback)
		want   error
	}{
		{name: "different actor", change: func(_ *PersonaChatCard, cb *PersonaChatCardCallback) { cb.ActorID = "colleague" }, want: ErrChatCardDenied},
		{name: "different invocation", change: func(_ *PersonaChatCard, cb *PersonaChatCardCallback) { cb.InvocationID = "older-invocation" }, want: ErrChatCardDenied},
		{name: "expired", change: func(_ *PersonaChatCard, cb *PersonaChatCardCallback) { cb.Now = card.ExpiresAt }, want: ErrChatCardExpired},
		{name: "changed digest", change: func(_ *PersonaChatCard, cb *PersonaChatCardCallback) {
			cb.Current.Items[0].MaterialFields[0].After = "L9"
		}, want: ErrChatCardStale},
		{name: "tampered link", change: func(c *PersonaChatCard, _ *PersonaChatCardCallback) { c.TaskViewHref = "https://attacker.example/" }, want: ErrChatCardInvalid},
		{name: "tampered lifetime", change: func(c *PersonaChatCard, _ *PersonaChatCardCallback) {
			c.ExpiresAt = c.IssuedAt.Add(PersonaChatCardLifetime + time.Second)
		}, want: ErrChatCardInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changedCard := card
			changedCard.Card.Values = cloneStringMap(card.Card.Values)
			changedCallback := PersonaChatCardCallback{Action: personaChatOpenTask, ActorID: approval.AssignedUserID, InvocationID: card.InvocationID, Now: now.Add(time.Minute), Current: cloneApproval(approval)}
			tc.change(&changedCard, &changedCallback)
			if _, err := ResolvePersonaChatCardCallback(changedCard, changedCallback); !errors.Is(err, tc.want) {
				t.Fatalf("callback error=%v, want %v", err, tc.want)
			}
		})
	}
	if _, err := BuildPersonaChatCard(approval, "invocation-1", "colleague", now); !errors.Is(err, ErrChatCardInvalid) {
		t.Fatalf("non-assignee card construction error=%v", err)
	}
	if _, err := BuildPersonaChatCard(approval, "invocation-1", approval.AssignedUserID, approval.ExpiresAt); !errors.Is(err, ErrChatCardExpired) {
		t.Fatalf("expired approval card construction error=%v", err)
	}
}

func TestTodo_AGENTP_014_T4StepUpAndBatchAreTaskViewOnly(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	service, err := New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		items []ActionItem
	}{
		{name: "external write", items: []ActionItem{b2Item("external", TierExternalWrite, "low")}},
		{name: "step up", items: []ActionItem{b2Item("high-risk", TierSubmitGoverned, "HIGH")}},
		{name: "batch", items: []ActionItem{b2Item("item-a", TierSubmitGoverned, "low"), b2Item("item-b", TierSubmitGoverned, "low")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			approval, err := service.Create(CreateRequest{ID: "approval-" + tc.name, TenantID: "tenant-1", TaskID: "task-1", AssignedUserID: "user-1", AgentID: "agent-1", OriginChain: []ActorRef{{Kind: ActorAgent, ID: "agent-1"}}, Items: tc.items, TaskExpiresAt: now.Add(time.Hour), Now: now})
			if err != nil {
				t.Fatal(err)
			}
			card, err := BuildPersonaChatCard(approval, "invocation-1", "user-1", now)
			if err != nil {
				t.Fatal(err)
			}
			if card.Card.Values["route"] != personaChatOpenTask || card.Card.Values["review_reason"] == "product_approval_surface_required" {
				t.Fatalf("restricted action did not carry its task-view reason: %+v", card.Card)
			}
			if _, err := ResolvePersonaChatCardCallback(card, PersonaChatCardCallback{Action: "approve", ActorID: "user-1", InvocationID: "invocation-1", Now: now, Current: approval}); !errors.Is(err, ErrChatApprovalSurface) {
				t.Fatalf("restricted chat approval was accepted: %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_014_Mutation(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	service, err := New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierSubmitGoverned, "low"))
	card, err := BuildPersonaChatCard(approval, "invocation-1", approval.AssignedUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	card.InvokerID = ""
	if _, err := ResolvePersonaChatCardCallback(card, PersonaChatCardCallback{Action: personaChatOpenTask, ActorID: "user-1", InvocationID: "invocation-1", Now: now.Add(time.Minute), Current: approval}); !errors.Is(err, ErrChatCardDenied) {
		t.Fatalf("callback accepted after invoker binding was removed: %v", err)
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
