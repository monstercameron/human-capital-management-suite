package chatapps

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_CHAT_042(t *testing.T) {
	s, actor := fixture()
	e := Event{
		ID: "chat-event-1", Tenant: actor.Tenant, Conversation: actor.Conversation,
		InstallationID: "t1:c1:app", Type: "post.created", Sequence: 1,
		Payload: []byte(`{"post_id":"post-1"}`), ExpiresAt: s.now().Add(time.Minute),
	}
	e.Signature = s.SignEvent(e)
	if err := s.Deliver(context.Background(), e); err != nil {
		t.Fatalf("deliver signed event: %v", err)
	}

	token, err := s.IssueCursor(Cursor{Tenant: actor.Tenant, Principal: actor.Principal, Conversation: actor.Conversation, Installation: "t1:c1:app", Expires: s.now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	events, next, err := s.Pull(context.Background(), actor, token, 10)
	if err != nil || len(events) != 1 || events[0].ID != e.ID || next == "" || next == token {
		t.Fatalf("pull events=%+v next=%q err=%v", events, next, err)
	}

	if err := s.Deliver(context.Background(), e); !errors.Is(err, ErrReplay) {
		t.Fatalf("duplicate delivery error=%v, want replay", err)
	}
}

func TestTodo_CHAT_042_Recovery(t *testing.T) {
	s, actor := fixture()
	e := Event{
		ID: "recovered-event", Tenant: actor.Tenant, Conversation: actor.Conversation,
		InstallationID: "t1:c1:app", Type: "post.created", Sequence: 1,
		Payload: []byte(`{"post_id":"post-recovered"}`), ExpiresAt: s.now().Add(time.Minute),
	}
	e.Signature = s.SignEvent(e)
	if err := s.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}

	// Recreate the service over the same durable-repository contract, as after
	// a process restart, and make sure the original committed event is pullable.
	restarted := &Service{Repo: s.Repo, Secret: append([]byte(nil), s.Secret...), Now: s.Now, Authority: s.Authority}
	token, err := restarted.IssueCursor(Cursor{Tenant: actor.Tenant, Principal: actor.Principal, Conversation: actor.Conversation, Installation: "t1:c1:app", Expires: restarted.now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	events, _, err := restarted.Pull(context.Background(), actor, token, 10)
	if err != nil || len(events) != 1 || events[0].ID != e.ID {
		t.Fatalf("recovered events=%+v err=%v", events, err)
	}
}

func TestTodo_CHAT_042_Security(t *testing.T) {
	s, actor := fixture()
	other := Manifest{AppID: "other", Version: 1, Scopes: []string{"chat:invoke"}}
	if _, err := s.Install(context.Background(), actor, other, other.Scopes, "manager"); err != nil {
		t.Fatal(err)
	}
	// Simulate a committed event for a different installation appearing first
	// in the shared conversation feed. Pull must skip it and advance its signed
	// cursor without revealing its payload.
	foreign := Event{ID: "foreign-event", Tenant: actor.Tenant, Conversation: actor.Conversation, InstallationID: "t1:c1:other", Type: "post.created", Sequence: 1, Payload: []byte(`{"secret":"foreign"}`), ExpiresAt: s.now().Add(time.Minute)}
	owned := Event{ID: "owned-event", Tenant: actor.Tenant, Conversation: actor.Conversation, InstallationID: "t1:c1:app", Type: "post.created", Sequence: 2, Payload: []byte(`{"post_id":"visible"}`), ExpiresAt: s.now().Add(time.Minute)}
	for _, event := range []Event{foreign, owned} {
		recorder, ok := s.Repo.(EventRecorder)
		if !ok {
			t.Fatal("repository does not support atomic event recording")
		}
		if err := recorder.AppendOnce(context.Background(), event, s.now()); err != nil {
			t.Fatal(err)
		}
	}
	token, err := s.IssueCursor(Cursor{Tenant: actor.Tenant, Principal: actor.Principal, Conversation: actor.Conversation, Installation: "t1:c1:app", Expires: s.now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	events, advanced, err := s.Pull(context.Background(), actor, token, 1)
	if err != nil || len(events) != 0 || advanced == token {
		t.Fatalf("foreign event leaked or cursor stalled: events=%+v token_advanced=%v err=%v", events, advanced != token, err)
	}
	events, _, err = s.Pull(context.Background(), actor, advanced, 1)
	if err != nil || len(events) != 1 || events[0].ID != owned.ID || string(events[0].Payload) != string(owned.Payload) {
		t.Fatalf("owned event after filtered record=%+v err=%v", events, err)
	}
}

func TestTodo_CHAT_042_Golden(t *testing.T) {
	s := &Service{Secret: []byte("secret"), Now: func() time.Time { return time.Unix(1000, 0).UTC() }}
	e := Event{
		ID: "e1", Tenant: "t1", Conversation: "c1", InstallationID: "t1:c1:app",
		Type: "post", Sequence: 1, Payload: []byte(`{"x":1}`), ExpiresAt: time.Unix(1100, 0).UTC(),
	}
	if got, want := s.SignEvent(e), "8EFPbgzxEhWjacLg9zjl8ISnKabHsfr92tXfc6PaTWk"; got != want {
		t.Fatalf("webhook signature golden=%q, want %q", got, want)
	}
}
