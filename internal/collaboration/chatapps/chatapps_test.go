package chatapps

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

type callbackStub struct{}

type authorityStub struct{}

type auditedRepositoryStub struct {
	*MemoryRepository
	writes int
}

func (r *auditedRepositoryStub) PutAudited(_ context.Context, _ Installation, _ Actor, _, _ string, _ uint64) error {
	r.writes++
	return nil
}

func TestTodo_CHAT_047_Security_AuditScopeBoundToMutation(t *testing.T) {
	r := &auditedRepositoryStub{MemoryRepository: NewMemoryRepository()}
	s := &Service{Repo: r}
	a := Actor{Tenant: "host", Conversation: "conv", Principal: "owner"}
	v := Installation{Tenant: a.Tenant, Conversation: a.Conversation}
	for _, bound := range []Actor{
		{Tenant: "foreign", Conversation: "conv", Principal: "owner"},
		{Tenant: "host", Conversation: "other", Principal: "owner"},
		{Tenant: "host", Conversation: "conv", Principal: "other"},
	} {
		if err := s.put(WithAudit(context.Background(), bound, "home", 2), v, a, "chat.app.install"); !errors.Is(err, ErrDenied) {
			t.Fatalf("scope %+v accepted: %v", bound, err)
		}
	}
	if err := s.put(WithAudit(context.Background(), a, "home", 2), v, a, "chat.app.install"); err != nil || r.writes != 1 {
		t.Fatalf("authorized write err=%v writes=%d", err, r.writes)
	}
}

func (authorityStub) CanManageApp(context.Context, Actor, string) error       { return nil }
func (authorityStub) CanUseConversation(context.Context, Actor, string) error { return nil }

func (callbackStub) Call(_ context.Context, _ Installation, _ Callback) (CallbackResult, error) {
	return CallbackResult{Accepted: true, Message: "ok"}, nil
}

type intentStub struct{ calls int }

func (s *intentStub) Propose(_ context.Context, _ Proposal) (ProposalReceipt, error) {
	s.calls++
	return ProposalReceipt{IntentID: "intent-1", Status: "PROPOSED"}, nil
}

func fixture() (*Service, Actor) {
	r := NewMemoryRepository()
	now := time.Unix(1000, 0).UTC()
	s := &Service{Repo: r, Secret: []byte("secret"), Now: func() time.Time { return now }, Callback: callbackStub{}, Authority: authorityStub{}}
	a := Actor{Tenant: "t1", Principal: "manager", Conversation: "c1", Scopes: []string{"chat:invoke"}}
	m := Manifest{AppID: "app", Version: 1, Scopes: []string{"chat:invoke"}, Commands: []Command{{Name: "ping", Scope: "chat:invoke"}}, Cards: []CardType{{Kind: "notice", Fields: []Field{{Name: "text", Type: "string", Required: true}}}}, Agent: &AgentManifest{DisplayName: "Helper", Triggers: []TriggerSource{TriggerMention}, MaxDepth: 2, CostCeiling: 10}}
	if _, err := s.Install(context.Background(), a, m, []string{"chat:invoke"}, "manager"); err != nil {
		panic(err)
	}
	return s, a
}

func TestInvocationUsesAuthorityIntersectionAndRevocation(t *testing.T) {
	s, a := fixture()
	if _, err := s.Invoke(context.Background(), a, "t1:c1:app", Callback{Command: "ping", IdempotencyKey: "1"}); err != nil {
		t.Fatal(err)
	}
	a.Scopes = nil
	if _, err := s.Invoke(context.Background(), a, "t1:c1:app", Callback{Command: "ping", IdempotencyKey: "2"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing invoker grant: %v", err)
	}
	a.Scopes = []string{"chat:invoke"}
	if _, err := s.ChangeStatus(context.Background(), a, "t1:c1:app", Revoked); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Invoke(context.Background(), a, "t1:c1:app", Callback{Command: "ping", IdempotencyKey: "3"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked callback: %v", err)
	}
}

func TestWebhookSignatureAndReplay(t *testing.T) {
	s, _ := fixture()
	e := Event{ID: "e1", Tenant: "t1", Conversation: "c1", InstallationID: "t1:c1:app", Type: "post", Sequence: 1, ExpiresAt: time.Unix(1100, 0), Payload: []byte(`{"x":1}`)}
	e.Signature = s.SignEvent(e)
	if err := s.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(context.Background(), e); !errors.Is(err, ErrReplay) {
		t.Fatalf("duplicate delivery: %v", err)
	}
	e.ID = "e2"
	e.Signature = "bad"
	if err := s.Deliver(context.Background(), e); !errors.Is(err, ErrDenied) {
		t.Fatalf("forged delivery: %v", err)
	}
}

func TestWebhookCannotRetargetInstallation(t *testing.T) {
	s, a := fixture()
	m := Manifest{AppID: "other", Version: 1, Scopes: []string{"chat:invoke"}}
	if _, err := s.Install(context.Background(), a, m, []string{"chat:invoke"}, "manager"); err != nil {
		t.Fatal(err)
	}
	e := Event{ID: "retarget", Tenant: "t1", Conversation: "c1", InstallationID: "t1:c1:other", Type: "post", Sequence: 1, ExpiresAt: time.Unix(1100, 0)}
	e.Signature = s.SignEvent(e)
	if err := s.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	e.Conversation = "other-conversation"
	e.Signature = s.SignEvent(e)
	if err := s.Deliver(context.Background(), e); !errors.Is(err, ErrDenied) {
		t.Fatalf("retargeted event: %v", err)
	}
}

type fakeResolver struct{ ips []net.IPAddr }

func (r fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) { return r.ips, nil }

func TestResolvedOriginRejectsDNSRebinding(t *testing.T) {
	if err := ValidateResolvedOrigin(context.Background(), "https://hooks.example.test/callback", fakeResolver{ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}); !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("private DNS target: %v", err)
	}
}

func TestAgentTriggerLoopAndBudget(t *testing.T) {
	s, _ := fixture()
	base := Trigger{Tenant: "t1", AgentInstallation: "t1:c1:app", Conversation: "c1", Source: string(TriggerMention), IdempotencyKey: "x"}
	if err := s.AdmitTrigger(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	base.Depth = 2
	if err := s.AdmitTrigger(context.Background(), base); !errors.Is(err, ErrLoop) {
		t.Fatalf("loop: %v", err)
	}
	base.Depth = 0
	base.Cost = 11
	if err := s.AdmitTrigger(context.Background(), base); !errors.Is(err, ErrBudget) {
		t.Fatalf("budget: %v", err)
	}
}

func TestProposalUsesBusinessIntentPort(t *testing.T) {
	s, a := fixture()
	stub := &intentStub{}
	s.Intent = stub
	p := Proposal{Tenant: a.Tenant, Principal: a.Principal, Conversation: a.Conversation, AgentInstallation: "t1:c1:app", IntentType: "hcm.leave.request", IdempotencyKey: "i1"}
	r, err := s.ProposeIntent(context.Background(), a, p)
	if err != nil || r.IntentID != "intent-1" || stub.calls != 1 {
		t.Fatalf("proposal=%+v err=%v calls=%d", r, err, stub.calls)
	}
	p.Principal = "other"
	if _, err := s.ProposeIntent(context.Background(), a, p); !errors.Is(err, ErrDenied) {
		t.Fatalf("forged proposal: %v", err)
	}
}
