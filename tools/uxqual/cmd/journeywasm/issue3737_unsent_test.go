package main

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3737Add(s *chatUnsentStore, conversation, body string) string {
	return s.add(chatUnsentEntry{Conversation: conversation, Body: body, Tenant: "t1", Subject: "alice"})
}

func TestIssue3737_EveryAttemptGetsItsOwnKeyAndRetryKeepsIt(t *testing.T) {
	s := &chatUnsentStore{}
	a := issue3737Add(s, "general", "ok")
	b := issue3737Add(s, "general", "ok")
	ea, _ := s.get(a)
	eb, _ := s.get(b)
	if ea.Key == "" || ea.Key == eb.Key || a == b {
		t.Fatalf("two sends of the same words are two messages: %q %q", ea.Key, eb.Key)
	}
	if !s.begin(a) {
		t.Fatal("first attempt refused")
	}
	if s.begin(a) {
		t.Fatal("a second attempt while one is in flight would post twice")
	}
	s.fail(a, chatui.UnsentFailed)
	if !s.begin(a) {
		t.Fatal("retry refused after a failure")
	}
	again, _ := s.get(a)
	if again.Key != ea.Key {
		t.Fatalf("retry must reuse the idempotency key: %q != %q", again.Key, ea.Key)
	}
}

func TestIssue3737_RowsFollowSendOrderAndShowSendingUntilFailure(t *testing.T) {
	s := &chatUnsentStore{}
	a := issue3737Add(s, "general", "first")
	b := issue3737Add(s, "general", "second")
	s.begin(a)
	s.fail(a, chatui.UnsentOffline)
	rows := s.rows()
	if len(rows) != 2 || rows[0].Body != "first" || rows[1].Body != "second" {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].Reason != chatui.UnsentOffline || rows[1].Reason != chatui.UnsentSending {
		t.Fatalf("reasons %+v", rows)
	}
	s.remove(b)
	if len(s.rows()) != 1 {
		t.Fatal("discard did not remove the row")
	}
}

func TestIssue3737_ReconnectResendsOnlyOfflineRowsOfTheSameIdentityInOrder(t *testing.T) {
	s := &chatUnsentStore{}
	a := issue3737Add(s, "general", "one")
	b := issue3737Add(s, "general", "two")
	c := issue3737Add(s, "general", "three")
	foreign := s.add(chatUnsentEntry{Conversation: "general", Body: "other person", Tenant: "t1", Subject: "bob"})
	for _, id := range []string{a, b, c, foreign} {
		s.begin(id)
	}
	s.fail(a, chatui.UnsentOffline)
	s.fail(b, chatui.UnsentFailed)
	s.fail(c, chatui.UnsentOffline)
	s.fail(foreign, chatui.UnsentOffline)
	got := s.resendable("t1", "alice")
	if len(got) != 2 || got[0] != a || got[1] != c {
		t.Fatalf("resendable %v (a=%s c=%s)", got, a, c)
	}
	// A different person signing in finds nothing, and the old rows are gone.
	if len(s.resendable("t1", "carol")) != 0 || len(s.rows()) != 0 {
		t.Fatalf("rows survived an identity change: %+v", s.rows())
	}
}

func TestIssue3737_AutomaticResendGivesUpAfterAFewTries(t *testing.T) {
	s := &chatUnsentStore{}
	a := issue3737Add(s, "general", "hello")
	for i := 0; i < chatUnsentAutoTries; i++ {
		s.begin(a)
		s.fail(a, chatui.UnsentOffline)
	}
	if got := s.resendable("t1", "alice"); len(got) != 0 {
		t.Fatalf("still retrying by itself forever: %v", got)
	}
	s.rearmOffline()
	if got := s.resendable("t1", "alice"); len(got) != 1 {
		t.Fatalf("the browser reporting a connection again should re-arm: %v", got)
	}
	for i := 0; i < chatUnsentAutoTries; i++ {
		s.begin(a)
		s.fail(a, chatui.UnsentOffline)
	}
	// The person's own Retry starts the count again.
	s.manual(a)
	if got := s.resendable("t1", "alice"); len(got) != 1 {
		t.Fatalf("manual retry should re-arm the automatic resend: %v", got)
	}
}

func TestIssue3737_ReasonFollowsTheConnection(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		online bool
		want   chatui.UnsentReason
	}{
		{"browser offline", errors.New("fetch failed"), false, chatui.UnsentOffline},
		{"server unreachable", status.Error(codes.Unavailable, "down"), true, chatui.UnsentOffline},
		{"timeout", status.Error(codes.DeadlineExceeded, "slow"), true, chatui.UnsentOffline},
		{"context deadline", context.DeadlineExceeded, true, chatui.UnsentOffline},
		{"refused", status.Error(codes.PermissionDenied, "no"), true, chatui.UnsentFailed},
		{"too long", status.Error(codes.InvalidArgument, "long"), true, chatui.UnsentFailed},
		{"internal", status.Error(codes.Internal, "boom"), true, chatui.UnsentFailed},
	}
	for _, c := range cases {
		if got := chatUnsentReason(c.err, c.online); got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestIssue3737_ForgetClearsEverything(t *testing.T) {
	s := &chatUnsentStore{}
	issue3737Add(s, "general", "secret")
	s.forget()
	if len(s.rows()) != 0 {
		t.Fatal("sign-out must drop unsent text")
	}
}
