package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestChannelWidgetsIntegration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "widgets", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}})
	ctx := context.Background()
	w, err := s.ChannelWidgets(ctx, "tenant-a", "tenant-a", "widgets", "worker", todoAllow)
	if err != nil || w.Team.Revision != 1 || w.Project.Revision != 1 || len(w.Team.Members) != 2 || w.Team.CanPin {
		t.Fatalf("initial: %+v, %v", w, err)
	}
	if _, err := s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 1, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PINNED", Pinned: true}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member pin: %v", err)
	}
	w, err = s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 1, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PURPOSE", Purpose: "Ship the release"}, todoAllow)
	if err != nil || w.Team.Revision != 2 || w.Team.Purpose != "Ship the release" {
		t.Fatalf("purpose: %+v, %v", w, err)
	}
	if _, err := s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 1, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PURPOSE", Purpose: "stale"}, todoAllow); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
	w, err = s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "owner", 2, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PINNED", Pinned: true}, todoAllow)
	if err != nil || !w.Team.Pinned || !w.Team.CanPin {
		t.Fatalf("pin: %+v, %v", w, err)
	}
	w, err = s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 3, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_ROLE_LABEL", MemberHomeTenantID: "tenant-a", MemberSubjectID: "worker", RoleLabel: "Release lead"}, todoAllow)
	if err != nil || w.Team.Members[1].RoleLabel != "Release lead" {
		t.Fatalf("label: %+v, %v", w, err)
	}
	w, err = s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 1, ChannelWidgetMutation{Kind: "PROJECT", Operation: "SET_DETAILS", Title: "Launch", Summary: "Channel planning notes"}, todoAllow)
	if err != nil || w.Project.Revision != 2 || w.Project.Title != "Launch" {
		t.Fatalf("details: %+v, %v", w, err)
	}
	w, err = s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 2, ChannelWidgetMutation{Kind: "PROJECT", Operation: "ADD_MILESTONE", Milestone: ChannelProjectMilestone{Text: "Ready for review", Status: "PLANNED", OwnerHomeTenantID: "tenant-a", OwnerSubjectID: "worker", DueDate: "2026-10-01"}}, todoAllow)
	if err != nil || len(w.Project.Milestones) != 1 || w.Project.Milestones[0].ID == "" || w.Project.Milestones[0].OwnerSubjectID != "worker" {
		t.Fatalf("milestone: %+v, %v", w, err)
	}
	if _, err := s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 3, ChannelWidgetMutation{Kind: "PROJECT", Operation: "ADD_MILESTONE", Milestone: ChannelProjectMilestone{Text: "Bad", Status: "PLANNED", OwnerHomeTenantID: "tenant-b", OwnerSubjectID: "outsider"}}, todoAllow); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("foreign owner: %v", err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now() WHERE tenant_id='tenant-a' AND conversation_id='widgets' AND member_id='worker'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	w, err = s.ChannelWidgets(ctx, "tenant-a", "tenant-a", "widgets", "owner", todoAllow)
	if err != nil || len(w.Team.Members) != 1 || w.Project.Milestones[0].OwnerSubjectID != "" {
		t.Fatalf("revoked projection: %+v, %v", w, err)
	}
	if _, err := s.ChannelWidgets(ctx, "tenant-a", "tenant-a", "widgets", "worker", todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked read: %v", err)
	}
	if _, err := s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "widgets", "worker", 4, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PURPOSE", Purpose: "after leave"}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked write: %v", err)
	}
	if _, err := s.ChannelWidgets(ctx, "tenant-b", "tenant-b", "widgets", "owner", todoAllow); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("other tenant: %v", err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_membership SET state='active',left_at=NULL,joined_at=now()+interval '1 second',revision=revision+1 WHERE tenant_id='tenant-a' AND conversation_id='widgets' AND member_id='worker'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	w, err = s.ChannelWidgets(ctx, "tenant-a", "tenant-a", "widgets", "owner", todoAllow)
	if err != nil || len(w.Team.Members) != 2 || w.Team.Members[1].RoleLabel != "" || w.Project.Milestones[0].OwnerSubjectID != "" {
		t.Fatalf("rejoin revived binding: %+v, %v", w, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var operation, actor, kind string
		e := tx.QueryRow(ctx, `SELECT operation,actor_id,kind FROM chat_channel_widget_revision WHERE tenant_id='tenant-a' AND conversation_id='widgets' AND kind='TEAM' AND revision=2`).Scan(&operation, &actor, &kind)
		if e != nil {
			return e
		}
		if operation != "SET_PURPOSE" || actor != "worker" || kind != "TEAM" {
			t.Errorf("revision: %s %s %s", operation, actor, kind)
		}
		var recordKind string
		e = tx.QueryRow(ctx, `SELECT kind FROM chat_record_inventory WHERE tenant_id='tenant-a' AND record_id='widget:widgets:TEAM:2'`).Scan(&recordKind)
		if e == nil && recordKind != "CHANNEL_WIDGET" {
			t.Errorf("inventory kind: %s", recordKind)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func TestChannelWidgetValidation(t *testing.T) {
	for _, m := range []ChannelWidgetMutation{{Kind: "UNKNOWN", Operation: "SET_PINNED"}, {Kind: "PROJECT", Operation: "ADD_MILESTONE", Milestone: ChannelProjectMilestone{Text: "x", Status: "OTHER"}}, {Kind: "PROJECT", Operation: "ADD_MILESTONE", Milestone: ChannelProjectMilestone{Text: "x", Status: "DONE", DueDate: "2026-02-30"}}, {Kind: "TEAM", Operation: "SET_ROLE_LABEL", RoleLabel: "x"}, {Kind: "TEAM", Operation: "SET_PURPOSE", Purpose: "\x00"}} {
		if !errors.Is(validateChannelWidgetMutation(m), chat.ErrInvalidArgument) {
			t.Fatalf("accepted %+v", m)
		}
	}
}

func TestChannelWidgetsGuestGrantRenewal(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "shared-widgets", TenantID: "host", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {HomeTenantID: "guest", MemberID: "visitor", Role: "member", State: "active"}})
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_share_grant(id,tenant_id,conversation_id,consumer_tenant,version,scope,classification,residency,proposed_by,accepted_by,accepted_at,expires_at) VALUES('widget-grant','host','shared-widgets','guest',1,'conversation','','','owner','visitor',now(),now()+interval '1 day')`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	w, err := s.ChannelWidgets(ctx, "host", "guest", "shared-widgets", "visitor", todoAllow)
	if err != nil || len(w.Team.Members) != 2 || w.Team.CanPin {
		t.Fatalf("guest initial: %+v %v", w, err)
	}
	w, err = s.MutateChannelWidget(ctx, "host", "guest", "shared-widgets", "visitor", 1, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_ROLE_LABEL", MemberHomeTenantID: "guest", MemberSubjectID: "visitor", RoleLabel: "Partner coordinator"}, todoAllow)
	if err != nil || w.Team.Members[0].RoleLabel != "Partner coordinator" {
		t.Fatalf("guest label: %+v %v", w, err)
	}
	w, err = s.MutateChannelWidget(ctx, "host", "guest", "shared-widgets", "visitor", 1, ChannelWidgetMutation{Kind: "PROJECT", Operation: "ADD_MILESTONE", Milestone: ChannelProjectMilestone{Text: "Partner signoff", Status: "PLANNED", OwnerHomeTenantID: "guest", OwnerSubjectID: "visitor"}}, todoAllow)
	if err != nil || w.Project.Milestones[0].OwnerSubjectID != "visitor" {
		t.Fatalf("guest milestone: %+v %v", w, err)
	}
	if _, err := s.MutateChannelWidget(ctx, "host", "guest", "shared-widgets", "visitor", 2, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PINNED", Pinned: true}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("guest pin: %v", err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_share_grant SET revoked_at=now(),revoked_by='owner' WHERE id='widget-grant'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChannelWidgets(ctx, "host", "guest", "shared-widgets", "visitor", todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked guest read: %v", err)
	}
	w, err = s.ChannelWidgets(ctx, "host", "host", "shared-widgets", "owner", todoAllow)
	if err != nil || len(w.Team.Members) != 1 || w.Project.Milestones[0].OwnerSubjectID != "" {
		t.Fatalf("owner after revoke: %+v %v", w, err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_share_grant(id,tenant_id,conversation_id,consumer_tenant,version,scope,classification,residency,proposed_by,accepted_by,accepted_at,expires_at) VALUES('widget-grant-new','host','shared-widgets','guest',2,'conversation','','','owner','visitor',now(),now()+interval '1 day')`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	w, err = s.ChannelWidgets(ctx, "host", "guest", "shared-widgets", "visitor", todoAllow)
	if err != nil || len(w.Team.Members) != 2 || w.Team.Members[0].RoleLabel != "" || w.Project.Milestones[0].OwnerSubjectID != "" {
		t.Fatalf("renewed grant revived binding: %+v %v", w, err)
	}
}
