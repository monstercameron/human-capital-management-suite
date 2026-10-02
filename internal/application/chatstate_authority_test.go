package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

type chatstateFacts struct {
	roles   []string
	revoked bool
}

func (f *chatstateFacts) ResolveChatFacts(_ context.Context, tenant, subject string, at time.Time) (chatpolicy.Principal, error) {
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: !f.revoked, Roles: f.roles, AuthorityRevision: 1}, nil
}

type chatstateMembers struct {
	chat.Store
	joined     time.Time
	roles      []string
	permission chatpolicy.StatusPermissions
	left       bool
}

func (f *chatstateMembers) GetMembership(_ context.Context, tenant, conversation, home, subject string) (chat.Membership, error) {
	member := chat.Membership{TenantID: tenant, ConversationID: conversation, HomeTenantID: home, SubjectID: subject, Role: chat.Manager, JoinedAt: &f.joined, Revision: 1}
	if f.left {
		member.LeftAt = &f.joined
	}
	return member, nil
}

type chatstateDirectoryFacts struct {
	*chatstateFacts
	candidates []chatpolicy.Principal
}

func (f chatstateDirectoryFacts) ChannelStatusCandidates(context.Context, string, time.Time) ([]chatpolicy.Principal, error) {
	return f.candidates, nil
}

type chatstateRoleMembers struct{ *chatstateMembers }

func (chatstateRoleMembers) ReadChannelStatus(context.Context, string, string) (chat.ChannelStatus, error) {
	return chat.ChannelStatus{Status: chatpolicy.StatusLocked}, nil
}
func (chatstateRoleMembers) CommitChannelStatus(context.Context, chat.ChangeChannelStatusRequest, time.Time, func(context.Context, chat.ChannelStatus) error) (chat.ChannelStatus, error) {
	return chat.ChannelStatus{}, chat.ErrUnavailable
}
func (chatstateRoleMembers) SweepChannelStatuses(context.Context, string, time.Time) (int, error) {
	return 0, chat.ErrUnavailable
}

func (f chatstateRoleMembers) ChannelStatusRolePermissions(_ context.Context, _, _ string, roles []string) (chatpolicy.StatusPermissions, error) {
	p := chatpolicy.StatusPermissions{}
	for _, role := range roles {
		if role == "reopener" {
			p.ChangeRestricted = true
		}
	}
	return p, nil
}

type chatstateNoPolicyTx struct{ dbport.Tx }

func (chatstateNoPolicyTx) Exec(context.Context, string, ...any) (int64, error) { return 1, nil }
func (chatstateNoPolicyTx) QueryRow(context.Context, string, ...any) dbport.Row {
	return chatstateNoPolicyRow{}
}

type chatstateNoPolicyRow struct{}

func (chatstateNoPolicyRow) Scan(...any) error { return dbport.ErrNoRows }

type chatstateNoPolicyStore struct{}

func (chatstateNoPolicyStore) RunTx(ctx context.Context, fn func(dbport.Tx) error) error {
	return fn(chatstateNoPolicyTx{})
}

func TestTodo_CHATSTATE_001_ReopenerAuthority(t *testing.T) {
	roleReader := &chatRoleReader{snapshot: roleaccess.Snapshot{Roles: []roleaccess.Role{{ID: chatpolicy.WorkspaceAdministratorRole, Active: true}, {ID: "retired", Active: false}}, Assignments: []roleaccess.Assignment{{WorkerRef: "admin", Version: 1, RoleIDs: []string{chatpolicy.WorkspaceAdministratorRole}}, {WorkerRef: "other", Version: 1, RoleIDs: []string{"retired"}}, {WorkerRef: "stale", Version: 0, RoleIDs: []string{chatpolicy.WorkspaceAdministratorRole}}}}}
	f := currentRoleChatFacts{roles: roleReader}
	r := chatstateHTTPRequest(t, "GET", ChannelStatusPath+"room", "")
	at := time.Now()
	candidates, err := f.ChannelStatusCandidates(r.Context(), "tenant-a", at)
	if err != nil || len(candidates) != 2 || len(candidates[0].Roles) != 1 || len(candidates[1].Roles) != 0 {
		t.Fatalf("current candidates = %+v %v", candidates, err)
	}
	if _, err = f.ChannelStatusCandidates(r.Context(), "other-tenant", at); err == nil {
		t.Fatal("candidate tenant leak")
	}
	facts := chatstateDirectoryFacts{chatstateFacts: &chatstateFacts{roles: []string{chatpolicy.WorkspaceAdministratorRole}}, candidates: []chatpolicy.Principal{{ID: "other", Tenant: "tenant-a", Active: true, Roles: []string{"reopener"}, AuthorityRevision: 1}}}
	members := chatstateRoleMembers{chatstateMembers: &chatstateMembers{joined: at.Add(-time.Hour)}}
	a := chatCurrentAuthority{source: newChatAuthoritySource(facts), members: members, policy: chatauthority.New(chatstateNoPolicyStore{})}
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "admin"}
	c := chat.Conversation{TenantID: "tenant-a", ID: "room", Kind: chat.PrivateChannel, Revision: 1}
	other, err := a.HasOtherChannelReopener(r.Context(), p, c, at)
	if err != nil || !other {
		t.Fatalf("independent reopener = %v %v", other, err)
	}
	other, err = a.HasOtherChannelReopenerWithoutRole(r.Context(), p, c, "reopener", at)
	if err != nil || other {
		t.Fatalf("revoked role counted as reopener = %v %v", other, err)
	}
	members.left = true
	other, err = a.HasOtherChannelReopener(r.Context(), p, c, at)
	if err != nil || other {
		t.Fatalf("departed private-channel member counted = %v %v", other, err)
	}
}
func (f *chatstateMembers) ChannelStatusRolePermissions(_ context.Context, tenant, conversation string, roles []string) (chatpolicy.StatusPermissions, error) {
	f.roles = append([]string(nil), roles...)
	return f.permission, nil
}

func (f *chatstateMembers) ChannelStatusMembershipCurrent(context.Context, string, string, string, string) (bool, error) {
	return !f.left, nil
}
func (f *chatstateMembers) PutChannelStatusPermission(context.Context, chat.Principal, chat.ChannelStatusPermissionGrant, uint64, string, func(context.Context, chat.ChannelStatusPermissionGrant) error) (chat.ChannelStatusPermissionGrant, error) {
	return chat.ChannelStatusPermissionGrant{}, chat.ErrUnavailable
}

func TestTodo_CHATSTATE_001_CurrentAuthority(t *testing.T) {
	facts := &chatstateFacts{roles: []string{chatpolicy.WorkspaceAdministratorRole, "announcer"}}
	members := &chatstateMembers{joined: time.Now().Add(-time.Hour), permission: chatpolicy.StatusPermissions{PostAnnouncements: true}}
	a := chatCurrentAuthority{source: newChatAuthoritySource(cachedChatFacts{inner: facts}), members: members}
	r := chatstateHTTPRequest(t, "GET", ChannelStatusPath+"room", "")
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "admin"}
	c := chat.Conversation{TenantID: "tenant-a", ID: "room", Kind: chat.PublicChannel}
	permissions, err := a.ChannelStatusPermissions(r.Context(), p, c, time.Now())
	if err != nil || !permissions.WorkspaceAdmin || !permissions.ChannelAdmin || !permissions.PostAnnouncements || len(members.roles) != 2 {
		t.Fatalf("permissions = %+v %v", permissions, err)
	}
	facts.roles = []string{"announcer"}
	permissions, err = a.ChannelStatusPermissions(r.Context(), p, c, time.Now())
	if err != nil || permissions.WorkspaceAdmin {
		t.Fatalf("revoked role cache used = %+v %v", permissions, err)
	}
	facts.revoked = true
	if _, err = a.ChannelStatusPermissions(r.Context(), p, c, time.Now()); err == nil {
		t.Fatal("revoked principal allowed")
	}
	facts.revoked = false
	c.TenantID = "other"
	if _, err = a.ChannelStatusPermissions(r.Context(), p, c, time.Now()); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("foreign tenant = %v", err)
	}
}
