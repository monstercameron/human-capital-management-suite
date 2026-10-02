package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type chatstatePermissionFixture struct {
	*chatstateFixture
	grant ChannelStatusPermissionGrant
}

func (f *chatstatePermissionFixture) SearchChannelStatuses(context.Context, Principal, string, string, bool) ([]ChannelStatus, error) {
	return []ChannelStatus{f.status}, nil
}

func (f *chatstatePermissionFixture) ChannelStatusRolePermissions(context.Context, string, string, []string) (chatpolicy.StatusPermissions, error) {
	return chatpolicy.StatusPermissions{}, nil
}

func (*chatstatePermissionFixture) ChannelStatusMembershipCurrent(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}

func (f *chatstatePermissionFixture) SearchChannelStatusPermissions(ctx context.Context, _, _ string, check func(context.Context) error) ([]ChannelStatusPermissionGrant, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	return []ChannelStatusPermissionGrant{f.grant}, nil
}
func (f *chatstatePermissionFixture) PutChannelStatusPermission(ctx context.Context, p Principal, g ChannelStatusPermissionGrant, revision uint64, reason string, check func(context.Context, ChannelStatusPermissionGrant) error) (ChannelStatusPermissionGrant, error) {
	if revision != f.grant.Revision {
		return g, ErrConflict
	}
	if err := check(ctx, f.grant); err != nil {
		return g, err
	}
	g.Revision = revision + 1
	f.grant = g
	return g, nil
}

type chatstateReopener struct {
	*chatstateAuthority
	other bool
}

func (a *chatstateReopener) HasOtherChannelReopener(context.Context, Principal, Conversation, time.Time) (bool, error) {
	return a.other, nil
}

func (a *chatstateReopener) HasOtherChannelReopenerWithoutRole(context.Context, Principal, Conversation, string, time.Time) (bool, error) {
	return a.other, nil
}

func TestTodo_CHATSTATE_001_ReopenerSecurity(t *testing.T) {
	s, f, a, _ := chatstateService()
	p := Principal{TenantID: "tenant", SubjectID: "admin"}
	if err := s.CheckChannelReopenerRemoval(t.Context(), p, f.conversation); !errors.Is(err, ErrLastReopener) {
		t.Fatalf("no directory = %v", err)
	}
	reopeners := &chatstateReopener{chatstateAuthority: a}
	s.SetAuthority(reopeners)
	if err := s.CheckChannelReopenerRemoval(t.Context(), p, f.conversation); !errors.Is(err, ErrLastReopener) {
		t.Fatalf("last reopener = %v", err)
	}
	reopeners.other = true
	if err := s.CheckChannelReopenerRemoval(t.Context(), p, f.conversation); err != nil {
		t.Fatalf("other reopener = %v", err)
	}
	permissions := &chatstatePermissionFixture{chatstateFixture: f}
	s.store = permissions
	results, err := s.SearchChannelStatuses(t.Context(), p, "tenant", "", false)
	if err != nil || len(results) != 1 || results[0].ConversationID != "room" {
		t.Fatalf("status search = %+v %v", results, err)
	}
	g := ChannelStatusPermissionGrant{TenantID: "tenant", ConversationID: "room", RoleID: "announcer", PostAnnouncements: true}
	result, err := s.SetChannelStatusPermission(t.Context(), p, g, 0, "Assign announcers")
	if err != nil || result.Revision != 1 {
		t.Fatalf("grant = %+v %v", result, err)
	}
	grants, err := s.SearchChannelStatusPermissions(t.Context(), p, "tenant", "announcer")
	if err != nil || len(grants) != 1 || grants[0].RoleID != "announcer" {
		t.Fatalf("grant search = %+v %v", grants, err)
	}
	g.ChangeRestricted = true
	result, err = s.SetChannelStatusPermission(t.Context(), p, g, 1, "Assign reopeners")
	if err != nil || result.Revision != 2 {
		t.Fatalf("restricted grant = %+v %v", result, err)
	}
	reopeners.other = false
	g.ChangeRestricted = false
	if _, err = s.SetChannelStatusPermission(t.Context(), p, g, 2, "Revoke"); !errors.Is(err, ErrLastReopener) || !permissions.grant.ChangeRestricted {
		t.Fatalf("last removal = %v %+v", err, permissions.grant)
	}
	a.permissions.WorkspaceAdmin = false
	if _, err = s.SearchChannelStatusPermissions(t.Context(), p, "tenant", ""); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unprivileged grant search = %v", err)
	}
	if _, err = s.SetChannelStatusPermission(t.Context(), p, g, 2, "Revoke"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unprivileged grant = %v", err)
	}
}
