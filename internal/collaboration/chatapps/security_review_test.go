package chatapps

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

type recordingCallback struct {
	called       bool
	actor        Actor
	installation string
}

func (r *recordingCallback) Call(_ context.Context, _ Installation, cb Callback) (CallbackResult, error) {
	r.called = true
	r.actor = cb.Actor
	r.installation = cb.InstallationID
	return CallbackResult{Accepted: true}, nil
}

func TestTodo_CHAT_028_Security_CallbackCannotForgeInvoker(t *testing.T) {
	s, actor := fixture()
	callback := &recordingCallback{}
	s.Callback = callback
	_, err := s.Invoke(context.Background(), actor, "t1:c1:app", Callback{
		Command: "ping", IdempotencyKey: "forge-1",
		Actor: Actor{Tenant: "t1", Conversation: "c1", Principal: "admin", Scopes: []string{"chat:invoke", "hcm:write"}},
	})
	if err != nil {
		if !errors.Is(err, ErrDenied) && !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected forged callback error: %v", err)
		}
		if callback.called {
			t.Fatal("forged callback reached external client")
		}
		return
	}
	if !callback.called || callback.actor.Principal != actor.Principal || callback.actor.Tenant != actor.Tenant || callback.actor.Conversation != actor.Conversation || !slices.Equal(callback.actor.Scopes, actor.Scopes) || callback.installation != "t1:c1:app" {
		t.Fatalf("callback was not bound to invoker: called=%v actor=%+v", callback.called, callback.actor)
	}
}

func TestTodo_CHAT_041_Security_CallbackCannotRetargetInstallation(t *testing.T) {
	s, actor := fixture()
	callback := &recordingCallback{}
	s.Callback = callback
	_, err := s.Invoke(context.Background(), actor, "t1:c1:app", Callback{Command: "ping", IdempotencyKey: "retarget", InstallationID: "t1:c1:other"})
	if !errors.Is(err, ErrDenied) || callback.called {
		t.Fatalf("retargeted callback reached client: called=%v err=%v", callback.called, err)
	}
}

func TestTodo_CHAT_044_Security_TriggerCannotRetargetConversation(t *testing.T) {
	s, _ := fixture()
	err := s.AdmitTrigger(context.Background(), Trigger{AgentInstallation: "t1:c1:app", Tenant: "t1", Conversation: "c2", Source: string(TriggerMention), IdempotencyKey: "retarget-1"})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("retargeted agent trigger: %v", err)
	}
}

func TestTodo_CHAT_042_Security_CursorInstallationMustMatchActor(t *testing.T) {
	s, actor := fixture()
	token, err := s.IssueCursor(Cursor{Tenant: actor.Tenant, Principal: actor.Principal, Conversation: actor.Conversation, Installation: "another-tenant:another-conversation:app", Expires: s.now().Unix() + 60})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Pull(context.Background(), actor, token, 10)
	if !errors.Is(err, ErrDenied) && !errors.Is(err, ErrRevoked) {
		t.Fatalf("foreign installation cursor: %v", err)
	}
}

func TestTodo_CHAT_042_Security_CursorFiltersOtherInstallations(t *testing.T) {
	s, actor := fixture()
	other := Manifest{AppID: "other", Version: 1, Scopes: []string{"chat:invoke"}}
	if _, err := s.Install(context.Background(), actor, other, other.Scopes, "manager"); err != nil {
		t.Fatal(err)
	}
	for seq, install := range []string{"t1:c1:app", "t1:c1:other"} {
		e := Event{ID: install, Tenant: actor.Tenant, Conversation: actor.Conversation, InstallationID: install, Type: "post", Sequence: uint64(seq + 1), ExpiresAt: s.now().Add(60 * time.Second)}
		e.Signature = s.SignEvent(e)
		if err := s.Deliver(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	token, err := s.IssueCursor(Cursor{Tenant: actor.Tenant, Principal: actor.Principal, Conversation: actor.Conversation, Installation: "t1:c1:app", Expires: s.now().Unix() + 60})
	if err != nil {
		t.Fatal(err)
	}
	events, _, err := s.Pull(context.Background(), actor, token, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].InstallationID != "t1:c1:app" {
		t.Fatalf("installation cursor disclosed other app events: %+v", events)
	}
}

func TestTodo_CHAT_040_UpgradeSuspendAndRevoke(t *testing.T) {
	s, actor := fixture()
	ctx := context.Background()
	initial, err := s.Repo.Get(ctx, "t1:c1:app")
	if err != nil || !initial.Current(s.now()) || initial.Revision != 1 {
		t.Fatalf("initial installation=%+v err=%v", initial, err)
	}
	manifest := initial.Manifest
	manifest.Version = 2
	manifest.Scopes = append(manifest.Scopes, "hcm:write")
	if _, err := s.Upgrade(ctx, actor, manifest, []string{"chat:invoke", "hcm:write"}, "manager"); err != nil {
		t.Fatal(err)
	}
	upgraded, err := s.Repo.Get(ctx, initial.ID)
	if err != nil || upgraded.Version != 2 || upgraded.Revision != 2 || upgraded.Approver != "manager" {
		t.Fatalf("upgraded installation=%+v err=%v", upgraded, err)
	}
	if _, err := s.Upgrade(ctx, actor, manifest, []string{"chat:invoke"}, "manager"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("same manifest version accepted: %v", err)
	}
	if _, err := s.ChangeStatus(ctx, actor, initial.ID, Suspended); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EffectiveScopes(ctx, actor, initial.ID, "ping"); !errors.Is(err, ErrDenied) {
		t.Fatalf("suspended command: %v", err)
	}
	if _, err := s.ChangeStatus(ctx, actor, initial.ID, Active); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeStatus(ctx, actor, initial.ID, Revoked); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upgrade(ctx, actor, Manifest{AppID: manifest.AppID, Version: 3}, nil, "manager"); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked install upgraded: %v", err)
	}
	if _, err := s.ChangeStatus(ctx, actor, initial.ID, Active); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked install restored: %v", err)
	}
}

func TestTodo_CHAT_041_Security_TypedCardAndInvokerContext(t *testing.T) {
	s, actor := fixture()
	callback := &recordingCallback{}
	s.Callback = callback
	ctx := context.Background()
	for _, card := range []Card{{Kind: "notice"}, {Kind: "script", Values: map[string]string{"text": "bad"}}} {
		_, err := s.Invoke(ctx, actor, "t1:c1:app", Callback{Command: "ping", IdempotencyKey: "card-check", Card: &card})
		if !errors.Is(err, ErrInvalid) || callback.called {
			t.Fatalf("invalid card reached callback: card=%+v err=%v", card, err)
		}
	}
	card := Card{Kind: "notice", Values: map[string]string{"text": "ready"}}
	result, err := s.Invoke(ctx, actor, "t1:c1:app", Callback{Command: "ping", IdempotencyKey: "card-valid", Card: &card})
	if err != nil || !result.Accepted || !callback.called || callback.actor.Principal != actor.Principal {
		t.Fatalf("valid callback result=%+v err=%v actor=%+v", result, err, callback.actor)
	}
}

func TestTodo_CHAT_043_VisibleAgentStatus(t *testing.T) {
	s, actor := fixture()
	a, err := s.Agent(context.Background(), "t1:c1:app")
	if err != nil || a.ID != "app" || a.DisplayName != "Helper" || a.Status != Active || a.InstallationID != "t1:c1:app" || !slices.Equal(a.Capabilities, []string{"chat:invoke"}) {
		t.Fatalf("agent=%+v err=%v", a, err)
	}
	if _, err := s.ChangeStatus(context.Background(), actor, "t1:c1:app", Revoked); err != nil {
		t.Fatal(err)
	}
	a, err = s.Agent(context.Background(), "t1:c1:app")
	if err != nil || a.Status != Revoked {
		t.Fatalf("revoked agent=%+v err=%v", a, err)
	}
}
