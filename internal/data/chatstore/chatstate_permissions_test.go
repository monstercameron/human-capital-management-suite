package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATSTATE_001_PermissionIntegration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := t.Context()
	actor := chat.Principal{TenantID: "tenant-a", SubjectID: "u-1"}
	check := func(context.Context, chat.ChannelStatusPermissionGrant) error { return nil }
	grant := chat.ChannelStatusPermissionGrant{TenantID: "tenant-a", RoleID: "announcer", PostAnnouncements: true}
	stored, err := s.PutChannelStatusPermission(ctx, actor, grant, 0, "Assign announcers", check)
	if err != nil || stored.Revision != 1 {
		t.Fatalf("workspace grant = %+v %v", stored, err)
	}
	p, err := s.ChannelStatusRolePermissions(ctx, "tenant-a", "c-1", []string{"announcer"})
	if err != nil || !p.PostAnnouncements || p.ChangeRestricted {
		t.Fatalf("resolve = %+v %v", p, err)
	}
	active, err := s.ChannelStatusMembershipCurrent(ctx, "tenant-a", "c-1", "tenant-a", "u-1")
	if err != nil || !active {
		t.Fatalf("current membership = %v %v", active, err)
	}
	active, err = s.ChannelStatusMembershipCurrent(ctx, "tenant-b", "c-1", "tenant-a", "u-1")
	if err != nil || active {
		t.Fatalf("membership tenant leak = %v %v", active, err)
	}
	if err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='suspended' WHERE tenant_id='tenant-a' AND conversation_id='c-1'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	active, err = s.ChannelStatusMembershipCurrent(ctx, "tenant-a", "c-1", "tenant-a", "u-1")
	if err != nil || active {
		t.Fatalf("suspended membership = %v %v", active, err)
	}
	if err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='active' WHERE tenant_id='tenant-a' AND conversation_id='c-1'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p, err = s.ChannelStatusRolePermissions(ctx, "tenant-b", "c-1", []string{"announcer"})
	if err != nil || p.PostAnnouncements {
		t.Fatal("permission tenant leak")
	}
	grant.ConversationID = "c-1"
	grant.ChangeOpen = true
	if _, err = s.PutChannelStatusPermission(ctx, actor, grant, 0, "Channel announcer", check); err != nil {
		t.Fatal(err)
	}
	grants, err := s.SearchChannelStatusPermissions(ctx, "tenant-a", "announcer", func(context.Context) error { return nil })
	if err != nil || len(grants) != 2 {
		t.Fatalf("permission search = %+v %v", grants, err)
	}
	if _, err = s.SearchChannelStatusPermissions(ctx, "tenant-a", "", func(context.Context) error { return chat.ErrPermissionDenied }); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("permission search bypass = %v", err)
	}
	p, err = s.ChannelStatusRolePermissions(ctx, "tenant-a", "other", []string{"announcer"})
	if err != nil || p.ChangeOpen {
		t.Fatal("channel grant escaped scope")
	}
	if _, err = s.PutChannelStatusPermission(ctx, actor, grant, 0, "Stale", check); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale permission = %v", err)
	}
	grant.ChangeRestricted = true
	if _, err = s.PutChannelStatusPermission(ctx, actor, grant, 1, "Revoked", func(context.Context, chat.ChannelStatusPermissionGrant) error { return chat.ErrPermissionDenied }); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("permission revocation = %v", err)
	}
	p, _ = s.ChannelStatusRolePermissions(ctx, "tenant-a", "c-1", []string{"announcer"})
	if p.ChangeRestricted {
		t.Fatal("refused permission persisted")
	}
	if _, err = s.CommitChannelStatus(ctx, chatstateChange(chatpolicy.StatusArchived, 1), time.Now().UTC(), func(context.Context, chat.ChannelStatus) error { return nil }); err != nil {
		t.Fatal(err)
	}
	statuses, err := s.SearchChannelStatuses(ctx, actor, "tenant-a", "", true)
	if err != nil || len(statuses) != 1 {
		t.Fatalf("archive search = %+v %v", statuses, err)
	}
	statuses, err = s.SearchChannelStatuses(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "outsider"}, "tenant-a", "", true)
	if err != nil || len(statuses) != 0 {
		t.Fatal("archive metadata escaped membership")
	}
	for _, sql := range []string{`UPDATE chat_conversation SET name='renamed' WHERE tenant_id='tenant-a' AND id='c-1'`, `UPDATE chat_membership SET state='left' WHERE tenant_id='tenant-a' AND conversation_id='c-1'`} {
		err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error { _, err := tx.Exec(ctx, sql); return err })
		if err == nil {
			t.Fatalf("archived mutation succeeded: %s", sql)
		}
	}
	var enabled, forced bool
	if err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='chatstate_permission'::regclass`).Scan(&enabled, &forced)
	}); err != nil || !enabled || !forced {
		t.Fatalf("permission RLS = %v %v %v", enabled, forced, err)
	}
	var reason string
	if err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT reason FROM chat_audit_event WHERE tenant_id=$1 AND action='conversation.status_changed' ORDER BY sequence DESC LIMIT 1`, "tenant-a").Scan(&reason)
	}); err != nil || reason != "Incident review" {
		t.Fatalf("audit reason = %q %v", reason, err)
	}
}
