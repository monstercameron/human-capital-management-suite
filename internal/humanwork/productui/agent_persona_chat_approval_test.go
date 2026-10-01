package productui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func personaApprovalTestCard(now time.Time) PersonaApprovalCard {
	return PersonaApprovalCard{
		ID: "approval-1", InvocationID: "invocation-1", InvokerID: "user-1", ItemDigest: "sha256:item-1", IssuedAt: now,
		ExpiresAt: PersonaApprovalExpiry(now), Tier: "T3", Summary: "Submit the promotion draft", Definition: "promotion.submit/v1",
		Fields: []PersonaApprovalField{{Name: "worker", Before: "worker-1", After: "worker-1"}, {Name: "level", Before: "L3", After: "L4"}}, Sources: []string{"comp-plan-1"}, Taint: []string{"GOVERNED"},
		DigestFresh: true, OpenTaskHref: "/workspace/app/agents?task=task-1", Ephemeral: true, CardLabel: "Persona approval", FieldsLabel: "Material changes", ActionLabel: "Approve",
	}
}

type personaApprovalPort struct {
	mu    sync.Mutex
	calls []PersonaApprovalDecision
	err   error
}

func (p *personaApprovalPort) RecheckAndSubmit(_ context.Context, decision PersonaApprovalDecision) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, decision)
	return p.err
}

func (p *personaApprovalPort) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func TestTodo_AGENTP_014(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalTestCard(now)
	view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	markup, err := ui.RenderToString(RenderPersonaApprovalCard(view, card, now.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`agent-delivery="ephemeral"`, `agent-visibility="invoker"`, `agent-item-digest="sha256:item-1"`, `agent-approval-action="approve"`, "Material changes", "comp-plan-1"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("approval card missing %q: %s", want, markup)
		}
	}
	port := &personaApprovalPort{}
	controller := NewPersonaApprovalControllerWithClock(port, func() time.Time { return now })
	if err := controller.Register(card); err != nil {
		t.Fatal(err)
	}
	decision := PersonaApprovalDecision{CardID: card.ID, InvocationID: card.InvocationID, InvokerID: card.InvokerID, ItemDigest: card.ItemDigest, ActorID: card.InvokerID, At: now.Add(time.Minute)}
	if err := controller.Approve(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(controller.Approve(context.Background(), decision), ErrPersonaApprovalReplay) || port.count() != 1 {
		t.Fatalf("approval was not single-submit: calls=%d", port.count())
	}
	taskOnly := card
	taskOnly.Tier, taskOnly.BatchSize, taskOnly.OpenTaskHref = "T4", 2, "/workspace/app/agents?task=task-1"
	taskMarkup, err := ui.RenderToString(RenderPersonaApprovalCard(view, taskOnly, now.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(taskMarkup, `agent-approval-action="approve"`) || !strings.Contains(taskMarkup, `agent-task-link=`) {
		t.Fatalf("restricted approval offered a chat action: %s", taskMarkup)
	}
}

func TestTodo_AGENTP_014_Golden(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalTestCard(now)
	view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	markup, err := ui.RenderToString(RenderPersonaApprovalCard(view, card, now))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="persona-chat-approval-card"`, `data-agent-card-kind="action-approval"`, `data-agent-invocation-id="invocation-1"`, `<code class="persona-approval-digest">sha256:item-1</code>`, `<button`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("golden approval markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "yes") || strings.Contains(markup, "reaction") || strings.Contains(markup, "reply") {
		t.Fatalf("golden card exposed an alternate consent path: %s", markup)
	}
}

func TestTodo_AGENTP_014_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalTestCard(now)
	port := &personaApprovalPort{}
	controller := NewPersonaApprovalControllerWithClock(port, func() time.Time { return now })
	if err := controller.Register(card); err != nil {
		t.Fatal(err)
	}
	base := PersonaApprovalDecision{CardID: card.ID, InvocationID: card.InvocationID, InvokerID: card.InvokerID, ItemDigest: card.ItemDigest, At: now}
	for name, mutate := range map[string]func(*PersonaApprovalDecision){
		"colleague":        func(d *PersonaApprovalDecision) { d.ActorID = "colleague" },
		"stale digest":     func(d *PersonaApprovalDecision) { d.ActorID = card.InvokerID; d.ItemDigest = "sha256:changed" },
		"stale invocation": func(d *PersonaApprovalDecision) { d.ActorID = card.InvokerID; d.InvocationID = "invocation-old" },
	} {
		d := base
		mutate(&d)
		if err := controller.Approve(context.Background(), d); err == nil {
			t.Fatalf("%s callback was accepted", name)
		}
	}
	expiredController := NewPersonaApprovalControllerWithClock(port, func() time.Time { return card.ExpiresAt })
	if err := expiredController.Register(card); err != nil {
		t.Fatal(err)
	}
	expiredDecision := base
	expiredDecision.ActorID = card.InvokerID
	if err := expiredController.Approve(context.Background(), expiredDecision); !errors.Is(err, ErrPersonaApprovalExpired) {
		t.Fatalf("expired callback = %v", err)
	}
	view := NewView(PageChat, "tenant-1", "colleague", "scope-1")
	markup, err := ui.RenderToString(RenderPersonaApprovalCard(view, card, now))
	if err != nil {
		t.Fatal(err)
	}
	if markup != "" && strings.Contains(markup, "sha256:item-1") {
		t.Fatalf("colleague received invoker card: %s", markup)
	}
}

func TestTodo_AGENTP_014_Race(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalTestCard(now)
	port := &personaApprovalPort{}
	controller := NewPersonaApprovalControllerWithClock(port, func() time.Time { return now })
	if err := controller.Register(card); err != nil {
		t.Fatal(err)
	}
	decision := PersonaApprovalDecision{CardID: card.ID, InvocationID: card.InvocationID, InvokerID: card.InvokerID, ItemDigest: card.ItemDigest, ActorID: card.InvokerID, At: now}
	results := make(chan error, 2)
	go func() { results <- controller.Approve(context.Background(), decision) }()
	go func() { results <- controller.Suspend(card.ID) }()
	var successes int
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 || port.count() > 1 {
		t.Fatalf("approve/suspend race had %d successes and %d submissions", successes, port.count())
	}
}

func TestTodo_AGENTP_014_Mutation(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalTestCard(now)
	port := &personaApprovalPort{}
	controller := NewPersonaApprovalControllerWithClock(port, func() time.Time { return now })
	if err := controller.Register(card); err != nil {
		t.Fatal(err)
	}
	if err := controller.Approve(context.Background(), PersonaApprovalDecision{CardID: card.ID, InvocationID: card.InvocationID, InvokerID: card.InvokerID, ItemDigest: card.ItemDigest, ActorID: "attacker", At: now}); !errors.Is(err, ErrPersonaApprovalDenied) {
		t.Fatalf("removed invoker binding would be accepted: %v", err)
	}
	if port.count() != 0 {
		t.Fatalf("denied actor reached submit port: %d", port.count())
	}
}

func TestTodo_AGENTP_014_Browser(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalTestCard(now)
	owner := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	ownerMarkup, err := ui.RenderToString(RenderPersonaApprovalCard(owner, card, now))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ownerMarkup, `data-agent-visibility="invoker"`) {
		t.Fatalf("owner card did not declare invoker-only visibility: %s", ownerMarkup)
	}
	member := NewView(PageChat, "tenant-1", "user-2", "scope-1")
	memberMarkup, err := ui.RenderToString(RenderPersonaApprovalCard(member, card, now))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(memberMarkup, "persona-chat-approval-card") || strings.Contains(memberMarkup, card.ItemDigest) {
		t.Fatalf("member saw invoker approval card: %s", memberMarkup)
	}
}
