package chatstore

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"testing"
	"time"
)

func TestIntegrate1ModerationCurrentRoles(t *testing.T) {
	store, p, _ := chatremoveDB(t)
	now := time.Now()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(p.TenantID), Subject: p.SubjectID, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "fixture", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(t.Context(), principal)
	roles := []string{"WORKSPACE_ADMIN"}
	store.CurrentRoles = func(_ context.Context, viewer chat.Principal) ([]string, error) {
		if viewer.TenantID != p.TenantID || viewer.SubjectID != p.SubjectID {
			t.Fatal("wrong role owner", viewer, p)
		}
		return roles, nil
	}
	if err = store.CanModerate(ctx, p, p.TenantID, "room", chat.PermissionReviewRemovedMessages); err != nil {
		t.Fatal("current administrator denied", err)
	}
	roles = nil
	if err = store.CanModerate(ctx, p, p.TenantID, "room", chat.PermissionReviewRemovedMessages); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("revoked administrator retained authority", err)
	}
	if err = store.CanModerate(t.Context(), p, p.TenantID, "room", chat.PermissionReviewRemovedMessages); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("roles resolved without trusted context", err)
	}
	roles = []string{"WORKSPACE_ADMIN"}
	if err = store.CanModerate(ctx, p, "another", "room", chat.PermissionReviewRemovedMessages); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("cross-tenant role projection", err)
	}
}

func TestIntegrate1ModerationGuestAppealWithCurrentRoles(t *testing.T) {
	s, host, _ := chatremoveDB(t)
	guest := chat.Principal{TenantID: "guest-tenant", SubjectID: "guest-author"}
	member, err := s.PutMembership(t.Context(), host, chat.Membership{TenantID: host.TenantID, ConversationID: "room", HomeTenantID: guest.TenantID, SubjectID: guest.SubjectID, Role: chat.Member, HistoryVisibility: chat.FullHistory})
	if err != nil {
		t.Fatal(err)
	}
	post, err := s.SendPost(t.Context(), chat.SendPostRequest{Principal: guest, TenantID: host.TenantID, ConversationID: "room", IdempotencyKey: "guest"}, chat.Post{AuthorID: guest.SubjectID, AuthorHomeTenantID: guest.TenantID, Body: "guest message"})
	if err != nil {
		t.Fatal(err)
	}
	chatremoveApply(t, s, host, post, "remove")
	if _, err = s.RemoveMembership(t.Context(), host, host.TenantID, "room", guest.TenantID, guest.SubjectID, member.Revision); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(guest.TenantID), Subject: guest.SubjectID, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "fixture", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(t.Context(), principal)
	s.CurrentRoles = func(context.Context, chat.Principal) ([]string, error) {
		t.Fatal("guest workspace roles projected into host")
		return nil, chat.ErrPermissionDenied
	}
	notices, err := s.ModerationNotices(ctx, guest, host.TenantID)
	if err != nil || len(notices) != 1 || !notices[0].CanAppeal {
		t.Fatalf("guest notices=%+v err=%v", notices, err)
	}
	if err = s.AppealRemoval(ctx, guest, host.TenantID, "room", post.ID, now); err != nil {
		t.Fatal("guest author appeal after leaving", err)
	}
	if err = s.CanModerate(ctx, guest, host.TenantID, "room", chat.PermissionReviewRemovedMessages); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("guest received host moderation authority", err)
	}
	other := guest
	other.SubjectID = "another-guest"
	notices, err = s.ModerationNotices(ctx, other, host.TenantID)
	if err != nil || len(notices) != 0 {
		t.Fatalf("guest notice leaked=%+v err=%v", notices, err)
	}
}
