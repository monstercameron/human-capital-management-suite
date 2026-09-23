package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type failingMachineAuthority struct{ err error }

func (a failingMachineAuthority) Authorize(context.Context, Principal, Conversation, chatpolicy.Action, time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{}, a.err
}

func TestTodo_CHAT_043_MachineConversationDenialMatchesAbsentRoom(t *testing.T) {
	at := time.Now().UTC()
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "t1", Subject: "agent", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "machine-visibility", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), identity)
	p := Principal{TenantID: "t1", SubjectID: "agent"}
	f := &fakeStore{conversation: conversation()}
	s := NewService(f, time.Now)
	s.SetAuthority(rejectingAuthority{})
	checks := []struct {
		name string
		call func(string) error
	}{
		{"get", func(id string) error {
			_, err := s.GetConversation(ctx, GetConversationRequest{Principal: p, TenantID: "t1", ConversationID: id})
			return err
		}},
		{"posts", func(id string) error {
			_, err := s.ListPosts(ctx, ListPostsRequest{Principal: p, TenantID: "t1", ConversationID: id})
			return err
		}},
		{"send", func(id string) error {
			_, err := s.SendPost(ctx, SendPostRequest{Principal: p, TenantID: "t1", ConversationID: id, Body: "hello", IdempotencyKey: "key"})
			return err
		}},
		{"watch", func(id string) error {
			_, _, err := s.WatchConversationWithErrors(ctx, WatchConversationRequest{Principal: p, TenantID: "t1", ConversationID: id})
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			for _, id := range []string{"c1", "absent"} {
				if err := check.call(id); !errors.Is(err, ErrNotFound) {
					t.Fatalf("%s: %v", id, err)
				}
			}
			if err := check.call(""); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("invalid request changed to absence: %v", err)
			}
		})
	}
	if _, err := s.GetConversation(context.Background(), GetConversationRequest{Principal: p, TenantID: "t1", ConversationID: "c1"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("human-style denial: %v", err)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: Principal{TenantID: "t1", SubjectID: "other"}, TenantID: "t1", ConversationID: "c1"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("mismatched machine principal: %v", err)
	}
	if _, _, err := s.WatchConversationWithErrors(ctx, WatchConversationRequest{Principal: p, TenantID: "t1", ConversationID: "c1", AfterSequence: 7}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("plain machine watch offset: %v", err)
	}
	if _, err := s.SendPost(ctx, SendPostRequest{Principal: p, TenantID: "t1", ConversationID: "c1", Body: "hello", IdempotencyKey: "   "}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("blank normalized idempotency key: %v", err)
	}
}

func TestTodo_CHAT_043_Security_MachineAuthorityFailure(t *testing.T) {
	at := time.Now().UTC()
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "t1", Subject: "agent", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "machine-failure", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), identity)
	p := Principal{TenantID: "t1", SubjectID: "agent"}
	for _, tc := range []struct {
		name  string
		cause error
		want  error
	}{
		{"absent grant", ErrPermissionDenied, ErrNotFound},
		{"absent conversation", ErrNotFound, ErrNotFound},
		{"database outage", errors.New("database unavailable"), ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService(&fakeStore{conversation: conversation()}, time.Now)
			s.SetAuthority(failingMachineAuthority{err: tc.cause})
			_, err := s.GetConversation(ctx, GetConversationRequest{Principal: p, TenantID: "t1", ConversationID: "c1"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("authority error %v: got %v, want %v", tc.cause, err, tc.want)
			}
		})
	}
}
