package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func onboardingCardService(f *fakeStore) *Service {
	s := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f})
	return s
}

func TestIssue3811_OnboardingCardBodyRoundTrips(t *testing.T) {
	card := OnboardingCard{Subject: "maria", Worker: "w-1", Employment: "emp-1"}
	body, err := OnboardingCardBody(card)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParseOnboardingCard(body)
	if !ok || got != card {
		t.Fatalf("parsed %+v, %v from %q", got, ok, body)
	}
	for _, bad := range []OnboardingCard{
		{Subject: "", Worker: "w", Employment: "e"},
		{Subject: "m", Worker: "", Employment: "e"},
		{Subject: "m", Worker: "w", Employment: ""},
		{Subject: "m|x", Worker: "w", Employment: "e"},
		{Subject: "m", Worker: "w\n", Employment: "e"},
	} {
		if _, err := OnboardingCardBody(bad); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("OnboardingCardBody(%+v) err = %v, want invalid", bad, err)
		}
	}
	if _, ok := ParseOnboardingCard(OnboardingCardMarker + "welcome|m"); ok {
		t.Error("a truncated body parsed")
	}
	if !IsSystemBody(body) {
		t.Error("an onboarding card must count as a system body (kept out of search and agent context)")
	}
}

// A person cannot type a card into a conversation, nor edit one in.
func TestIssue3811_HandTypedOnboardingCardIsRefused_Security(t *testing.T) {
	body, _ := OnboardingCardBody(OnboardingCard{Subject: "u1", Worker: "w", Employment: "e"})
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	s := onboardingCardService(f)
	_, err := s.SendPost(context.Background(), SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "look: " + body, IdempotencyKey: "k"})
	if !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("send = %v, mutations %d", err, f.mutations)
	}
	_, err = s.EditPost(context.Background(), EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", ExpectedRevision: 1, Body: body})
	if !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("edit = %v, mutations %d", err, f.mutations)
	}
}

func TestIssue3811_PostOnboardingCardWritesToTheNewHiresPrivateConversation(t *testing.T) {
	f := &fakeStore{membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "dm", SubjectID: "system", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	s := onboardingCardService(f)
	card := OnboardingCard{Subject: "maria", Worker: "w-1", Employment: "emp-1"}
	post, err := s.PostOnboardingCard(context.Background(), OnboardingCardPost{TenantID: "t1", Sender: Principal{TenantID: "t1", SubjectID: "system"}, Card: card, IdempotencyKey: "emp-1:prop-1"})
	if err != nil {
		t.Fatal(err)
	}
	if f.conversation.Kind != Direct || len(f.created) != 2 {
		t.Fatalf("conversation = %+v members %+v, want a direct conversation of exactly two (no manager or buddy)", f.conversation, f.created)
	}
	got, ok := ParseOnboardingCard(f.sent.Body)
	if !ok || got != card || post.ID == "" || f.sent.AuthorID != "system" {
		t.Fatalf("sent = %+v (%+v, %v)", f.sent, got, ok)
	}
	var recipient bool
	for _, m := range f.created {
		recipient = recipient || m.SubjectID == "maria"
	}
	if !recipient {
		t.Errorf("the new hire is not a member: %+v", f.created)
	}
}

func TestIssue3811_PostOnboardingCardRefusesBadRequests_Security(t *testing.T) {
	f := &fakeStore{}
	s := onboardingCardService(f)
	card := OnboardingCard{Subject: "maria", Worker: "w", Employment: "e"}
	system := Principal{TenantID: "t1", SubjectID: "system"}
	for name, r := range map[string]OnboardingCardPost{
		"sender in another tenant": {TenantID: "t1", Sender: Principal{TenantID: "t2", SubjectID: "system"}, Card: card, IdempotencyKey: "k"},
		"sends to itself":          {TenantID: "t1", Sender: system, Card: OnboardingCard{Subject: "system", Worker: "w", Employment: "e"}, IdempotencyKey: "k"},
		"no key":                   {TenantID: "t1", Sender: system, Card: card},
		"bad card":                 {TenantID: "t1", Sender: system, Card: OnboardingCard{}, IdempotencyKey: "k"},
	} {
		if _, err := s.PostOnboardingCard(context.Background(), r); err == nil || f.mutations != 0 {
			t.Errorf("%s: err %v, mutations %d", name, err, f.mutations)
		}
	}
}
