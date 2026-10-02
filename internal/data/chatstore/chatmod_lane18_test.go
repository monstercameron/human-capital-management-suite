package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// chatmodChannel creates one channel with its members; the first is its manager
// unless noManager is set.
func chatmodChannel(t *testing.T, s *ModeratedAdapter, id, name string, kind chat.ConversationKind, noManager bool, members ...string) {
	t.Helper()
	c := chat.Conversation{ID: id, TenantID: "tenant-a", Kind: kind, Name: name, OwnerID: members[0], Revision: 1}
	var rows []chat.Membership
	for i, member := range members {
		role := chat.Member
		if i == 0 && !noManager {
			role = chat.Manager
		}
		rows = append(rows, chat.Membership{TenantID: "tenant-a", ConversationID: id, HomeTenantID: "tenant-a", SubjectID: member, Role: role, HistoryVisibility: chat.FullHistory})
	}
	if _, err := s.CreateConversation(context.Background(), c, rows, ""); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CHATMOD_003_Integration_Notify: a "notify" filter is saved only for
// a channel the person may know of that has a manager, and a hit is told to
// that channel's managers by a moderation notice, once, without any text.
func TestTodo_CHATMOD_003_Integration_Notify(t *testing.T) {
	adapter := adapterDB(t)
	moderated := NewModeratedAdapter(adapter)
	filters := NewFilterStore(adapter.Store)
	ctx := t.Context()
	chatmodChannel(t, moderated, "general", "general", chat.PublicChannel, false, "walt", "jake")
	chatmodChannel(t, moderated, "sec", "Security", chat.PublicChannel, false, "lead", "second-lead-as-member")
	chatmodChannel(t, moderated, "ops", "operations", chat.PrivateChannel, false, "opslead", "insider")
	chatmodChannel(t, moderated, "empty", "unmanaged", chat.PublicChannel, true, "someone")
	admin := chatfilter.Actor{Tenant: "tenant-a", Subject: "admin"}

	for _, target := range []string{"#Security", "security", "  #SECURITY "} {
		if err := filters.ResolveFilterTarget(ctx, admin, target); err != nil {
			t.Fatalf("target %q: %v", target, err)
		}
	}
	for name, target := range map[string]string{"no channel of that name": "#nowhere", "an empty name": "#", "a private channel the person is not in": "#operations", "a channel nobody manages": "#unmanaged"} {
		if err := filters.ResolveFilterTarget(ctx, admin, target); !errors.Is(err, chatfilter.ErrUnknownTarget) {
			t.Errorf("%s was accepted as a target: %v", name, err)
		}
	}
	if err := filters.ResolveFilterTarget(ctx, chatfilter.Actor{Tenant: "tenant-a", Subject: "insider"}, "#operations"); err != nil {
		t.Fatalf("a member could not name their private channel: %v", err)
	}
	if err := filters.ResolveFilterTarget(ctx, chatfilter.Actor{Tenant: "tenant-b", Subject: "admin"}, "#security"); !errors.Is(err, chatfilter.ErrUnknownTarget) {
		t.Fatalf("another workspace's channel was accepted: %v", err)
	}
	if err := filters.ResolveFilterTarget(ctx, chatfilter.Actor{Tenant: "tenant-a"}, "#security"); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatalf("nobody in particular resolved a target: %v", err)
	}

	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	record := chatfilter.Record{Tenant: "tenant-a", Channel: "general", Subject: "jake", At: at,
		Hit: chatfilter.Hit{RuleID: "codes", RuleName: "Client names", Version: "1.0.0", Action: "notify", Target: "#security", Digest: "sha256:0123456789abcdef", Masked: "[removed word]"}}
	if err := filters.DeliverFilterHit(ctx, record); err != nil {
		t.Fatal(err)
	}
	// A replay of the same hit is the same notice.
	if err := filters.DeliverFilterHit(ctx, record); err != nil {
		t.Fatal(err)
	}
	notices := func(subject string) []chat.ModerationNotice {
		t.Helper()
		out, err := moderated.ModerationNotices(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: subject}, "tenant-a")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := notices("lead")
	if len(got) != 1 {
		t.Fatalf("the manager of the named channel has %d notices, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.Outcome != chat.ModerationOutcomeFilterNotify || n.Reason != "Client names" || n.ConversationID != "general" || n.PostID != "" || n.CanAppeal || !n.At.Equal(at) {
		t.Fatalf("notice=%+v", n)
	}
	for _, other := range []string{"second-lead-as-member", "walt", "jake", "opslead", "admin"} {
		if rows := notices(other); len(rows) != 0 {
			t.Errorf("%s, who does not manage the named channel, was told: %+v", other, rows)
		}
	}
	// A second hit is a second notice; a flagged hit and a hit with no channel
	// to tell are delivered as nothing here.
	later := record
	later.At = at.Add(time.Minute)
	later.Channel = "not-a-conversation"
	if err := filters.DeliverFilterHit(ctx, later); err != nil {
		t.Fatal(err)
	}
	flagged := record
	flagged.Hit.Action, flagged.Hit.Target, flagged.At = "flag", "", at.Add(2*time.Minute)
	gone := record
	gone.Hit.Target, gone.At = "#renamed-away", at.Add(3*time.Minute)
	for _, r := range []chatfilter.Record{flagged, gone} {
		if err := filters.DeliverFilterHit(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got = notices("lead")
	if len(got) != 2 {
		t.Fatalf("after a second hit the manager has %d notices, want 2: %+v", len(got), got)
	}
	// Newest first: the hit outside a conversation is filed under the named channel.
	if got[0].ConversationID != "sec" || got[1].ConversationID != "general" {
		t.Fatalf("notices are about %q and %q", got[0].ConversationID, got[1].ConversationID)
	}
	// The summary counts them for the sidebar and does not take them for removals.
	summary, err := moderated.ModerationSummary(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "lead"}, "tenant-a")
	if err != nil || len(summary.Notices) != 2 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	// Nothing of a message is in the notice table: only the filter's name.
	var leaked int
	if err = filters.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_moderation_notice WHERE reason<>'Client names' OR post_id<>'' OR id LIKE '%sha256%'`).Scan(&leaked)
	}); err != nil || leaked != 0 {
		t.Fatalf("leaked=%d err=%v", leaked, err)
	}
}

// TestTodo_CHATMOD_005_Integration_Permissions: the stored answers replace the
// defaults for a role, a channel's answer replaces the workspace's, and only a
// workspace administrator reads or writes them.
func TestTodo_CHATMOD_005_Integration_Permissions(t *testing.T) {
	s, moderator, _ := chatremoveDB(t)
	filters := NewFilterStore(s.Store)
	ctx := context.Background()
	author := chat.Principal{TenantID: "tenant-a", SubjectID: "author"}
	boss := chat.Principal{TenantID: "tenant-a", SubjectID: "boss"}
	can := func(p chat.Principal, permission string) error {
		return s.CanModerate(ctx, p, "tenant-a", "room", permission)
	}

	// The defaults: a manager removes and manages their channel's filters, a
	// member reports, and neither reads removed messages.
	for permission, want := range map[string]bool{chat.PermissionRemoveMessages: true, chat.PermissionManageFilters: true, chat.PermissionReport: true, chat.PermissionReviewRemovedMessages: false} {
		if err := can(moderator, permission); (err == nil) != want {
			t.Errorf("default for a manager, %q: %v, want allowed=%v", permission, err, want)
		}
	}
	for permission, want := range map[string]bool{chat.PermissionRemoveMessages: false, chat.PermissionManageFilters: false, chat.PermissionReport: true, chat.PermissionReviewRemovedMessages: false} {
		if err := can(author, permission); (err == nil) != want {
			t.Errorf("default for a member, %q: %v, want allowed=%v", permission, err, want)
		}
	}
	if rows, err := s.ModerationPermissionRows(ctx, moderator, "tenant-a", ""); !errors.Is(err, chat.ErrPermissionDenied) || rows != nil {
		t.Fatalf("a manager read the permission rows: %+v %v", rows, err)
	}
	if err := s.removalTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES('tenant-a','boss','WORKSPACE_ADMIN')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ModerationPermissionRows(ctx, boss, "tenant-a", "")
	if err != nil || len(rows) != 0 {
		t.Fatalf("an administrator's first read: %+v %v", rows, err)
	}
	if _, err = s.ModerationPermissionRows(ctx, chat.Principal{TenantID: "tenant-b", SubjectID: "boss"}, "tenant-a", ""); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("another workspace's administrator read the rows: %v", err)
	}

	// The workspace takes "Manage filters" from managers and gives a role of its
	// own the right to read removed messages.
	if err = s.AssignModerationPermission(ctx, boss, "tenant-a", "", string(chat.Manager), chat.PermissionManageFilters, false); err != nil {
		t.Fatal(err)
	}
	if err = s.AssignModerationPermission(ctx, boss, "tenant-a", "", "hr_partner", chat.PermissionReviewRemovedMessages, true); err != nil {
		t.Fatal(err)
	}
	if err = can(moderator, chat.PermissionManageFilters); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a manager still manages filters after the workspace said no: %v", err)
	}
	if err = can(moderator, chat.PermissionRemoveMessages); err != nil {
		t.Fatalf("the row for one permission changed another: %v", err)
	}
	held, err := filters.ManageFiltersRows(ctx, "tenant-a", "")
	if err != nil || len(held) != 1 || held[string(chat.Manager)] {
		t.Fatalf("the workspace's filter rows: %v %v", held, err)
	}
	// The channel's own answer wins there, and only there.
	if err = s.removalTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_permission(tenant_id,conversation_id,role,permission,allowed) VALUES('tenant-a','room','MANAGER','Manage filters',true)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = can(moderator, chat.PermissionManageFilters); err != nil {
		t.Fatalf("the channel's own answer did not win: %v", err)
	}
	if held, err = filters.ManageFiltersRows(ctx, "tenant-a", "room"); err != nil || !held[string(chat.Manager)] {
		t.Fatalf("the channel's filter rows: %v %v", held, err)
	}
	if held, err = filters.ManageFiltersRows(ctx, "tenant-a", "another-room"); err != nil || held[string(chat.Manager)] {
		t.Fatalf("another channel's filter rows: %v %v", held, err)
	}
	if held, err = filters.ManageFiltersRows(ctx, "tenant-b", "room"); err != nil || len(held) != 0 {
		t.Fatalf("another workspace's filter rows: %v %v", held, err)
	}
	rows, err = s.ModerationPermissionRows(ctx, boss, "tenant-a", "room")
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows for the workspace and the channel: %+v %v", rows, err)
	}
	if rows, err = s.ModerationPermissionRows(ctx, boss, "tenant-a", ""); err != nil || len(rows) != 2 {
		t.Fatalf("rows for the workspace alone: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.ConversationID != "" || !chat.ValidModerationPermission(row.Permission) || row.Role == "" {
			t.Fatalf("row=%+v", row)
		}
	}
}
