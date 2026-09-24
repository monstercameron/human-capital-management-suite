package chatapps

import (
	"context"
	"errors"
	"testing"
)

type invocationSpy struct{ calls int }

func (s *invocationSpy) Call(context.Context, Installation, Callback) (CallbackResult, error) {
	s.calls++
	return CallbackResult{Accepted: true, Message: "invoked"}, nil
}

func TestTodo_CHAT_028(t *testing.T) {
	s, actor := fixture()
	spy := &invocationSpy{}
	s.Callback = spy
	install, err := s.Repo.Get(context.Background(), "t1:c1:app")
	if err != nil {
		t.Fatal(err)
	}
	install.Manifest.Commands[0].Arguments = []Argument{
		{Name: "count", Type: "integer", Required: true},
		{Name: "enabled", Type: "boolean"},
	}
	if err := s.Repo.Put(context.Background(), install); err != nil {
		t.Fatal(err)
	}

	result, err := s.Invoke(context.Background(), actor, install.ID, Callback{
		Command: "ping", IdempotencyKey: "selected-1",
		Args: map[string]string{"count": "3", "enabled": "false"},
	})
	if err != nil || !result.Accepted || spy.calls != 1 {
		t.Fatalf("valid selected command result=%+v err=%v callback calls=%d", result, err, spy.calls)
	}
	for _, cb := range []Callback{
		{Command: "quoted text: /ping", IdempotencyKey: "quoted", Args: map[string]string{"count": "3"}},
		{Command: "ping", IdempotencyKey: "unknown-argument", Args: map[string]string{"count": "3", "admin": "true"}},
		{Command: "ping", IdempotencyKey: "bad-integer", Args: map[string]string{"count": "3.5"}},
		{Command: "ping", IdempotencyKey: "missing-required"},
	} {
		if _, err := s.Invoke(context.Background(), actor, install.ID, cb); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrDenied) {
			t.Fatalf("invalid or unselected command accepted: callback=%+v err=%v", cb, err)
		}
	}
	if spy.calls != 1 {
		t.Fatalf("invalid command input reached callback %d times", spy.calls)
	}
}

func TestTodo_CHAT_028_Security(t *testing.T) {
	s, actor := fixture()
	spy := &invocationSpy{}
	s.Callback = spy
	install, err := s.Repo.Get(context.Background(), "t1:c1:app")
	if err != nil {
		t.Fatal(err)
	}
	install.Manifest.Scopes = append(install.Manifest.Scopes, "hcm:write")
	install.Manifest.Commands = append(install.Manifest.Commands, Command{Name: "change-pay", Scope: "hcm:write"})
	install.GrantedScopes = append(install.GrantedScopes, "hcm:write")
	if err := s.Repo.Put(context.Background(), install); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Invoke(context.Background(), actor, install.ID, Callback{Command: "change-pay", IdempotencyKey: "not-authorized"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("app installation grant widened invoker authority: %v", err)
	}
	actor.Scopes = append(actor.Scopes, "hcm:write")
	if _, err := s.Invoke(context.Background(), actor, install.ID, Callback{Command: "change-pay", IdempotencyKey: "authorized"}); err != nil || spy.calls != 1 {
		t.Fatalf("explicit invoker capability did not intersect app grant: err=%v callback calls=%d", err, spy.calls)
	}
	if spy.calls != 1 {
		t.Fatalf("unauthorized HCM command reached callback %d times", spy.calls)
	}
}
