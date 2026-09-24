package chatapps

import (
	"context"
	"errors"
	"testing"
)

type chat045IntentPort struct {
	calls    int
	proposal Proposal
}

func (p *chat045IntentPort) Propose(_ context.Context, proposal Proposal) (ProposalReceipt, error) {
	p.calls++
	p.proposal = proposal
	return ProposalReceipt{IntentID: "intent-045", Status: "PROPOSED"}, nil
}

func chat045Proposal(a Actor) Proposal {
	return Proposal{
		Tenant: a.Tenant, Principal: a.Principal, Conversation: a.Conversation,
		AgentInstallation: a.Tenant + ":" + a.Conversation + ":app",
		IntentType:        "hcm.leave.request", IdempotencyKey: "agent-run-045",
		Arguments: map[string]string{"worker_ref": "worker-1", "start_date": "2026-10-01"},
	}
}

func TestTodo_CHAT_045(t *testing.T) {
	s, actor := fixture()
	port := &chat045IntentPort{}
	s.Intent = port
	proposal := chat045Proposal(actor)
	receipt, err := s.ProposeIntent(context.Background(), actor, proposal)
	if err != nil {
		t.Fatalf("ProposeIntent: %v", err)
	}
	if receipt.IntentID != "intent-045" || receipt.Status != "PROPOSED" {
		t.Fatalf("receipt = %+v", receipt)
	}
	if port.calls != 1 || port.proposal.IdempotencyKey != proposal.IdempotencyKey ||
		port.proposal.IntentType != proposal.IntentType || port.proposal.AgentInstallation != proposal.AgentInstallation {
		t.Fatalf("proposal port calls=%d proposal=%+v", port.calls, port.proposal)
	}
}

func TestTodo_CHAT_045_Security(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Actor, *Proposal, *Installation)
	}{
		{name: "forged tenant", change: func(_ *Actor, p *Proposal, _ *Installation) { p.Tenant = "other" }},
		{name: "forged principal", change: func(_ *Actor, p *Proposal, _ *Installation) { p.Principal = "other" }},
		{name: "forged conversation", change: func(_ *Actor, p *Proposal, _ *Installation) { p.Conversation = "other" }},
		{name: "missing agent installation", change: func(_ *Actor, p *Proposal, _ *Installation) { p.AgentInstallation = "" }},
		{name: "missing intent type", change: func(_ *Actor, p *Proposal, _ *Installation) { p.IntentType = "" }},
		{name: "missing idempotency key", change: func(_ *Actor, p *Proposal, _ *Installation) { p.IdempotencyKey = "" }},
		{name: "foreign installation tenant", change: func(_ *Actor, _ *Proposal, i *Installation) { i.Tenant = "other" }},
		{name: "foreign installation conversation", change: func(_ *Actor, _ *Proposal, i *Installation) { i.Conversation = "other" }},
		{name: "suspended installation", change: func(_ *Actor, _ *Proposal, i *Installation) { i.Status = Suspended }},
		{name: "revoked installation", change: func(_ *Actor, _ *Proposal, i *Installation) { i.Status = Revoked }},
		{name: "ordinary app installation", change: func(_ *Actor, _ *Proposal, i *Installation) { i.Manifest.Agent = nil }},
		{name: "installation manifest identity mismatch", change: func(_ *Actor, _ *Proposal, i *Installation) { i.Manifest.AppID = "other" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, actor := fixture()
			port := &chat045IntentPort{}
			s.Intent = port
			proposal := chat045Proposal(actor)
			installation, err := s.Repo.Get(context.Background(), proposal.AgentInstallation)
			if err != nil {
				t.Fatal(err)
			}
			tt.change(&actor, &proposal, &installation)
			if installation.ID != "" {
				if err := s.Repo.Put(context.Background(), installation); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.ProposeIntent(context.Background(), actor, proposal); !errors.Is(err, ErrDenied) {
				t.Fatalf("ProposeIntent error = %v, want ErrDenied", err)
			}
			if port.calls != 0 {
				t.Fatalf("proposal port called %d times for refused proposal", port.calls)
			}
		})
	}
}
