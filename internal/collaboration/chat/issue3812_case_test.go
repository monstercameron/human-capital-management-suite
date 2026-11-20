package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// casePort is a stand-in for the store of case bindings.
type casePort struct {
	binding CaseBinding
	bound   bool
	err     error
}

func (c *casePort) ActiveCase(context.Context, string, string) (CaseBinding, bool, error) {
	return c.binding, c.bound, c.err
}

func caseBinding(restricted bool) *casePort {
	return &casePort{bound: true, binding: CaseBinding{TenantID: "t1", ConversationID: "c1", CaseRef: "ER-2041", OwnerID: "hr", Restricted: restricted}}
}

func caseService(kind ConversationKind, port *casePort, authority Authority) (*Service, *fakeStore) {
	f := &fakeStore{
		conversation: Conversation{ID: "c1", TenantID: "t1", Kind: kind, OwnerID: "mgr", Revision: 1},
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "mgr", Role: Manager, Revision: 1, JoinedAt: timePtr(time.Unix(1, 0)), HistoryVisibility: FullHistory},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "mgr", Body: "private words", Revision: 1, Sequence: 3},
	}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	if authority == nil {
		authority = referenceAuthority{}
	}
	s.SetAuthority(authority)
	if port != nil {
		s.SetCaseBindings(port)
	}
	return s, f
}

func caseAdd(as, who string) AddMembershipRequest {
	return AddMembershipRequest{Principal: Principal{TenantID: "t1", SubjectID: as}, Membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: who}}
}

func caseRemove(as, who string) RemoveMembershipRequest {
	return RemoveMembershipRequest{Principal: Principal{TenantID: "t1", SubjectID: as}, TenantID: "t1", ConversationID: "c1", SubjectID: who, HomeTenantID: "t1", ExpectedRevision: 1}
}

func wantCaseRefusal(t *testing.T, err error, reason string) {
	t.Helper()
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v; want a permission denial", err)
	}
	got, ok := CaseReason(err)
	if !ok || got != reason {
		t.Fatalf("reason = %q, %v; want %q", got, ok, reason)
	}
}

func TestIssue3812_MembershipChangesNeedTheCaseOwner_Security(t *testing.T) {
	ctx := context.Background()
	t.Run("add by the conversation owner is refused", func(t *testing.T) {
		s, f := caseService(PrivateChannel, caseBinding(true), nil)
		_, err := s.AddMembership(ctx, caseAdd("mgr", "eve"))
		wantCaseRefusal(t, err, ReasonCaseMembership)
		if f.mutations != 0 {
			t.Fatalf("mutations = %d; a refused add wrote a membership", f.mutations)
		}
	})
	t.Run("add by the case owner is allowed", func(t *testing.T) {
		s, f := caseService(PrivateChannel, caseBinding(true), nil)
		if _, err := s.AddMembership(ctx, caseAdd("hr", "eve")); err != nil || f.put.SubjectID != "eve" {
			t.Fatalf("err = %v, put = %+v; want the case owner to add eve", err, f.put)
		}
	})
	t.Run("nobody joins a bound public channel by themselves", func(t *testing.T) {
		s, f := caseService(PublicChannel, caseBinding(true), nil)
		_, err := s.AddMembership(ctx, caseAdd("eve", "eve"))
		wantCaseRefusal(t, err, ReasonCaseMembership)
		if f.mutations != 0 {
			t.Fatal("a refused self-join wrote a membership")
		}
	})
	t.Run("a group invite needs the case owner", func(t *testing.T) {
		s, f := caseService(Group, caseBinding(false), nil)
		_, err := s.InviteGroupMember(ctx, caseAdd("mgr", "eve"))
		wantCaseRefusal(t, err, ReasonCaseMembership)
		if _, err := s.InviteGroupMember(ctx, caseAdd("hr", "eve")); err != nil || f.put.SubjectID != "eve" {
			t.Fatalf("case owner invite: err = %v, put = %+v", err, f.put)
		}
	})
	t.Run("a removal needs the case owner, and never removes the case owner", func(t *testing.T) {
		s, f := caseService(PrivateChannel, caseBinding(true), nil)
		_, err := s.RemoveMembership(ctx, caseRemove("mgr", "eve"))
		wantCaseRefusal(t, err, ReasonCaseMembership)
		if _, err := s.RemoveMembership(ctx, caseRemove("hr", "eve")); err != nil {
			t.Fatalf("case owner removal: %v", err)
		}
		_, err = s.RemoveMembership(ctx, caseRemove("hr", "hr"))
		wantCaseRefusal(t, err, ReasonCaseMembership)
		_, err = s.RemoveMembership(ctx, caseRemove("mgr", "hr"))
		wantCaseRefusal(t, err, ReasonCaseMembership)
		if f.mutations != 1 {
			t.Fatalf("mutations = %d; want only the case owner's own removal of eve", f.mutations)
		}
	})
	t.Run("a participant may still leave", func(t *testing.T) {
		s, f := caseService(PrivateChannel, caseBinding(true), nil)
		if _, err := s.RemoveMembership(ctx, caseRemove("eve", "eve")); err != nil || f.mutations != 1 {
			t.Fatalf("leave: err = %v, mutations = %d", err, f.mutations)
		}
	})
	t.Run("a group revoke needs the case owner", func(t *testing.T) {
		s, f := caseService(Group, caseBinding(false), nil)
		_, err := s.RevokeGroupMember(ctx, caseRemove("mgr", "eve"))
		wantCaseRefusal(t, err, ReasonCaseMembership)
		if f.mutations != 0 {
			t.Fatal("a refused revoke wrote")
		}
		if _, err := s.RevokeGroupMember(ctx, caseRemove("hr", "eve")); err != nil {
			t.Fatalf("case owner revoke: %v", err)
		}
	})
}

func TestIssue3812_UnboundConversationKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	s, f := caseService(PrivateChannel, &casePort{}, nil)
	if _, err := s.AddMembership(ctx, caseAdd("mgr", "eve")); err != nil || f.put.SubjectID != "eve" {
		t.Fatalf("owner add without a binding: err = %v", err)
	}
	if _, err := s.AddMembership(ctx, caseAdd("hr", "eve")); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("a non-owner add without a binding: err = %v; want a denial", err)
	}
	if _, ok := CaseReason(ErrPermissionDenied); ok {
		t.Fatal("an ordinary denial reads as a case refusal")
	}
}

func TestIssue3812_UnknownBindingFailsClosed_Security(t *testing.T) {
	ctx := context.Background()
	s, f := caseService(PrivateChannel, &casePort{err: errors.New("db down")}, nil)
	if _, err := s.AddMembership(ctx, caseAdd("mgr", "eve")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("add: err = %v; want unavailable", err)
	}
	if _, err := s.RemoveMembership(ctx, caseRemove("mgr", "eve")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("remove: err = %v; want unavailable", err)
	}
	if err := s.CaseRestriction(ctx, "t1", "c1", CaseActionForward); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("restriction: err = %v; want unavailable", err)
	}
	if f.mutations != 0 {
		t.Fatal("a failure to read the binding let a write through")
	}
}

func TestIssue3812_ForwardRefusedFromRestrictedCase_Security(t *testing.T) {
	ctx := context.Background()
	forward := ForwardPostRequest{Principal: Principal{TenantID: "t1", SubjectID: "mgr"}, SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1", DestinationTenantID: "t1", DestinationConversationID: "c1", IdempotencyKey: "fwd-1"}
	s, f := caseService(PrivateChannel, caseBinding(true), referenceAuthority{})
	_, err := s.ForwardPost(ctx, forward)
	wantCaseRefusal(t, err, ReasonCaseForward)
	if f.mutations != 0 {
		t.Fatal("a refused forward wrote a post")
	}
	// A case that is bound but not restricted keeps its roster rule and nothing more.
	s, f = caseService(PrivateChannel, caseBinding(false), referenceAuthority{})
	if _, err := s.ForwardPost(ctx, forward); err != nil || f.mutations != 1 {
		t.Fatalf("forward from an unrestricted case: err = %v, mutations = %d", err, f.mutations)
	}
}

func TestIssue3812_ShareLinkRefusedForRestrictedCase_Security(t *testing.T) {
	ctx := context.Background()
	s, _ := caseService(PrivateChannel, caseBinding(true), referenceAuthority{})
	for _, post := range []string{"", "p1"} {
		_, err := s.CreateShareLink(ctx, Principal{TenantID: "t1", SubjectID: "mgr"}, "t1", "c1", post)
		wantCaseRefusal(t, err, ReasonCaseShare)
	}
	s, _ = caseService(PrivateChannel, caseBinding(false), referenceAuthority{})
	if _, err := s.CreateShareLink(ctx, Principal{TenantID: "t1", SubjectID: "mgr"}, "t1", "c1", "p1"); err != nil {
		t.Fatalf("link for an unrestricted case: %v", err)
	}
}

func TestIssue3812_SaveRefusedForRestrictedCase_Security(t *testing.T) {
	ctx := context.Background()
	f := &chatsaveFixture{fakeStore: &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "c1", HistoryVisibility: FullHistory}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "body", Sequence: 7}}}
	s := NewService(f, func() time.Time { return time.Unix(1000, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f.fakeStore})
	s.SetCaseBindings(caseBinding(true))
	_, err := s.SaveForLater(ctx, SavedRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1"})
	wantCaseRefusal(t, err, ReasonCaseSave)
	if len(f.items) != 0 {
		t.Fatal("a refused save kept an item")
	}
	s.SetCaseBindings(caseBinding(false))
	if _, err := s.SaveForLater(ctx, SavedRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1"}); err != nil || len(f.items) != 1 {
		t.Fatalf("save from an unrestricted case: err = %v", err)
	}
}

func TestIssue3812_RefusalNamesNoCase(t *testing.T) {
	msg := (&CaseRestrictionError{Action: CaseActionForward}).Error()
	if strings.Contains(msg, "ER-2041") || strings.Contains(msg, "hr") {
		t.Fatalf("refusal text %q names the case or its owner", msg)
	}
}
