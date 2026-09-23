package chatstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func todoAllow(context.Context) error { return nil }

func TestChannelTodoMutationValidation(t *testing.T) {
	for _, m := range []ChannelTodoMutation{{Operation: "ADD", Text: " "}, {Operation: "ADD", Text: string(make([]byte, 501))}, {Operation: "ADD", Text: "hostile\x00payload"}, {Operation: "ADD", Text: "\x1b[31m"}, {Operation: "DELETE"}, {Operation: "SET_COMPLETED", ItemID: "x", Text: "smuggled"}, {Operation: "other"}} {
		if err := validateChannelTodoMutation(m); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("mutation %+v: got %v", m, err)
		}
	}
	out := &ChannelTodoList{Items: []ChannelTodoItem{{ID: "x", Text: "one"}}}
	if err := applyChannelTodoMutation(out, "home", "u", ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: "x", Completed: true}); err != nil || !out.Items[0].Completed || out.Items[0].CompletedBySubjectID != "u" || out.Items[0].CompletedByHomeTenantID != "home" || out.Items[0].CompletedAtUnix == 0 {
		t.Fatalf("completion = %+v, %v", out, err)
	}
	if err := applyChannelTodoMutation(out, "home", "u", ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: "x", Completed: false}); err != nil || out.Items[0].Completed || out.Items[0].CompletedBySubjectID != "" || out.Items[0].CompletedByHomeTenantID != "" || out.Items[0].CompletedAtUnix != 0 {
		t.Fatalf("reopen = %+v, %v", out, err)
	}
	if err := applyChannelTodoMutation(out, "home", "u", ChannelTodoMutation{Operation: "DELETE", ItemID: "missing"}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("missing item: %v", err)
	}
}

func TestChannelTodoIntegration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "todos", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}})
	ctx := context.Background()
	initial, err := s.ChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "worker", todoAllow)
	if err != nil || initial.Revision != 1 || len(initial.Items) != 0 {
		t.Fatalf("initial = %+v, %v", initial, err)
	}
	if _, err := s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "worker", 1, ChannelTodoMutation{Operation: "SET_PINNED", Pinned: true}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member pin: %v", err)
	}
	list, err := s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "worker", 1, ChannelTodoMutation{Operation: "ADD", Text: "  Ship release  "}, todoAllow)
	if err != nil || list.Revision != 2 || len(list.Items) != 1 || list.Items[0].Text != "Ship release" || list.Items[0].CreatedBy != "worker" || list.Items[0].CreatedByHomeTenantID != "tenant-a" {
		t.Fatalf("add = %+v, %v", list, err)
	}
	if _, err := s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "worker", 1, ChannelTodoMutation{Operation: "DELETE", ItemID: list.Items[0].ID}, todoAllow); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	list, err = s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "owner", 2, ChannelTodoMutation{Operation: "SET_PINNED", Pinned: true}, todoAllow)
	if err != nil || !list.Pinned || list.Revision != 3 {
		t.Fatalf("pin = %+v, %v", list, err)
	}
	list, err = s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "worker", 3, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: list.Items[0].ID, Completed: true}, todoAllow)
	if err != nil || !list.Items[0].Completed || list.Items[0].CompletedBySubjectID != "worker" || list.Items[0].CompletedByHomeTenantID != "tenant-a" || list.Items[0].CompletedAtUnix == 0 {
		t.Fatalf("complete = %+v, %v", list, err)
	}
	list, err = s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "owner", 4, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: list.Items[0].ID, Completed: false}, todoAllow)
	if err != nil || list.Items[0].Completed || list.Items[0].CompletedBySubjectID != "" || list.Items[0].CompletedByHomeTenantID != "" || list.Items[0].CompletedAtUnix != 0 {
		t.Fatalf("reopen = %+v, %v", list, err)
	}
	list, err = s.ChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "owner", todoAllow)
	if err != nil || list.Items[0].CompletedBySubjectID != "" || list.Items[0].CompletedAtUnix != 0 {
		t.Fatalf("reopen persisted = %+v, %v", list, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now() WHERE tenant_id='tenant-a' AND conversation_id='todos' AND member_id='worker'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "worker", todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked read: %v", err)
	}
	if _, err := s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "todos", "worker", 5, ChannelTodoMutation{Operation: "ADD", Text: "after revoke"}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked write: %v", err)
	}
	if _, err := s.ChannelTodo(ctx, "tenant-b", "tenant-b", "todos", "owner", todoAllow); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("other tenant read: %v", err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var actor, operation string
		var before, after []byte
		if err := tx.QueryRow(ctx, `SELECT actor_id,operation,prior_items_json,items_json FROM chat_channel_todo_revision WHERE tenant_id='tenant-a' AND conversation_id='todos' AND revision=2`).Scan(&actor, &operation, &before, &after); err != nil {
			return err
		}
		if actor != "worker" || operation != "ADD" || string(before) != "[]" || !strings.Contains(string(after), "Ship release") {
			t.Errorf("audit row = %q %q %s %s", actor, operation, before, after)
		}
		var kind string
		if err := tx.QueryRow(ctx, `SELECT kind FROM chat_record_inventory WHERE tenant_id='tenant-a' AND record_id='todo:todos:2'`).Scan(&kind); err != nil || kind != "CHANNEL_TODO" {
			t.Errorf("record inventory kind = %q, %v", kind, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_channel_todo_revision WHERE tenant_id='tenant-a' AND conversation_id='todos' AND revision=2`)
		return err
	}); err == nil {
		t.Fatal("append-only audit row deleted")
	}
}

func TestChannelTodoConcurrentRevision(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	var wg sync.WaitGroup
	var successes, conflicts int
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.MutateChannelTodo(context.Background(), "tenant-a", "tenant-a", "c-1", "u-1", 1, ChannelTodoMutation{Operation: "ADD", Text: "task"}, todoAllow)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, chat.ErrConflict):
				conflicts++
			default:
				t.Errorf("unexpected mutation error: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success/conflict = %d/%d", successes, conflicts)
	}
}

func TestChannelTodoGuestGrantAndPinnedLink(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "shared", TenantID: "host", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "member", State: "active"}, {HomeTenantID: "guest", MemberID: "visitor", Role: "member", State: "active"}})
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO chat_share_grant(id,tenant_id,conversation_id,consumer_tenant,version,scope,classification,residency,proposed_by,accepted_by,accepted_at,expires_at)
			VALUES('grant','host','shared','guest',1,'conversation','','','owner','visitor',now(),now()+interval '1 day')`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,sequence,body) VALUES('post','host','shared','owner',1,'pinned body')`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_pin(tenant_id,conversation_id,post_id,member_id) VALUES('host','shared','post','owner')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.MutateChannelTodo(ctx, "host", "guest", "shared", "visitor", 1, ChannelTodoMutation{Operation: "ADD", Text: "Follow up", SourcePostID: "post"}, todoAllow)
	if err != nil || len(list.Items) != 1 || list.Items[0].SourcePostID != "post" || list.Items[0].CreatedBy != "visitor" || list.Items[0].CreatedByHomeTenantID != "guest" {
		t.Fatalf("guest linked add = %+v, %v", list, err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "host", "shared", "owner", 2, ChannelTodoMutation{Operation: "SET_PINNED", Pinned: true}, todoAllow)
	if err != nil || !list.Pinned {
		t.Fatalf("owner pin with member role = %+v, %v", list, err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET history_visibility='FROM_JOIN',joined_at=now()+interval '1 hour' WHERE tenant_id='host' AND conversation_id='shared' AND home_tenant_id='guest' AND member_id='visitor'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ChannelTodo(ctx, "host", "guest", "shared", "visitor", todoAllow)
	if err != nil || list.Items[0].SourcePostID != "" {
		t.Fatalf("from-join source leaked = %+v, %v", list, err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "guest", "shared", "visitor", 3, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: list.Items[0].ID, Completed: true}, todoAllow)
	if err != nil || list.Items[0].SourcePostID != "" || !list.Items[0].Completed || list.Items[0].CompletedBySubjectID != "visitor" || list.Items[0].CompletedByHomeTenantID != "guest" || list.Items[0].CompletedAtUnix == 0 {
		t.Fatalf("from-join mutation source leaked = %+v, %v", list, err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		var actorHome, actor string
		var items []byte
		if err := tx.QueryRow(ctx, `SELECT actor_home_tenant_id,actor_id,items_json FROM chat_channel_todo_revision WHERE tenant_id='host' AND conversation_id='shared' AND revision=4`).Scan(&actorHome, &actor, &items); err != nil {
			return err
		}
		if actorHome != "guest" || actor != "visitor" || !strings.Contains(string(items), `"completed_by_subject_id": "visitor"`) && !strings.Contains(string(items), `"completed_by_subject_id":"visitor"`) {
			t.Errorf("guest completion audit = %q %q %s", actorHome, actor, items)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET history_visibility='FULL_HISTORY',joined_at=now() WHERE tenant_id='host' AND conversation_id='shared' AND home_tenant_id='guest' AND member_id='visitor'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_pin WHERE tenant_id='host' AND conversation_id='shared' AND post_id='post'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ChannelTodo(ctx, "host", "guest", "shared", "visitor", todoAllow)
	if err != nil || list.Items[0].SourcePostID != "" {
		t.Fatalf("unpin read = %+v, %v", list, err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "guest", "shared", "visitor", 4, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: list.Items[0].ID, Completed: true}, todoAllow)
	if err != nil || list.Items[0].SourcePostID != "" || !list.Items[0].Completed {
		t.Fatalf("unpin mutation response = %+v, %v", list, err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "guest", "shared", "visitor", 5, ChannelTodoMutation{Operation: "ADD", Text: "bad link", SourcePostID: "post"}, todoAllow); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("unpinned source accepted: %v", err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_share_grant SET revoked_at=now(),revoked_by='owner' WHERE id='grant'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChannelTodo(ctx, "host", "guest", "shared", "visitor", todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked grant read: %v", err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "guest", "shared", "visitor", 5, ChannelTodoMutation{Operation: "ADD", Text: "after revoke"}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked grant write: %v", err)
	}
}

func TestChannelTodoPolicyRecheckAfterAdmission(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,role_mode) VALUES('tenant-a','c-1',1,1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	authorize := func(ctx context.Context) error {
		return s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			var revision int64
			if err := tx.QueryRow(ctx, `SELECT revision FROM chat_channel_policy WHERE tenant_id='tenant-a' AND conversation_id='c-1'`).Scan(&revision); err != nil {
				return err
			}
			if revision != 1 {
				return chat.ErrPermissionDenied
			}
			return nil
		})
	}
	if err := authorize(ctx); err != nil {
		t.Fatalf("precheck: %v", err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_channel_policy SET revision=2 WHERE tenant_id='tenant-a' AND conversation_id='c-1'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "c-1", "u-1", 1, ChannelTodoMutation{Operation: "ADD", Text: "stale authority"}, authorize); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("stale admission mutation: %v", err)
	}
	if _, err := s.ChannelTodo(ctx, "tenant-a", "tenant-a", "c-1", "u-1", authorize); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("stale admission read: %v", err)
	}
	if _, err := s.ChannelTodo(ctx, "tenant-a", "tenant-a", "c-1", "u-1", nil); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("missing authority callback: %v", err)
	}
	list, err := s.ChannelTodo(ctx, "tenant-a", "tenant-a", "c-1", "u-1", todoAllow)
	if err != nil || list.Revision != 1 || len(list.Items) != 0 {
		t.Fatalf("denied mutation persisted: %+v, %v", list, err)
	}
}

func TestChannelTodoLegacyCreatorTenant(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_channel_todo(tenant_id,conversation_id,revision,pinned,items_json)
			VALUES('tenant-a','c-1',2,false,'[{"id":"old","text":"Legacy","completed":false,"created_by":"u-1","created_at_unix":1}]'::jsonb)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ChannelTodo(ctx, "tenant-a", "tenant-a", "c-1", "u-1", todoAllow)
	if err != nil || len(list.Items) != 1 || list.Items[0].CreatedByHomeTenantID != "tenant-a" {
		t.Fatalf("legacy creator tenant = %+v, %v", list, err)
	}
	list, err = s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "c-1", "u-1", 2, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: "old", Completed: true}, todoAllow)
	if err != nil || list.Items[0].CreatedByHomeTenantID != "tenant-a" || list.Items[0].CompletedByHomeTenantID != "tenant-a" {
		t.Fatalf("legacy mutation attribution = %+v, %v", list, err)
	}
}

func TestChannelTodoPerItemCompletionPolicy(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "policy", TenantID: "host", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}, {MemberID: "other", Role: "member", State: "active"}})
	ctx := context.Background()
	list, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "owner", 1, ChannelTodoMutation{Operation: "ADD", Text: "Approve budget"}, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	id := list.Items[0].ID
	if list.Items[0].CompletionMode != "EVERYONE" {
		t.Fatalf("default mode = %q", list.Items[0].CompletionMode)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "worker", 2, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME"}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("noncreator policy edit: %v", err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "host", "policy", "owner", 2, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME"}, todoAllow)
	if err != nil || !list.Items[0].CanToggle || !list.Items[0].CanManageCompletionPolicy {
		t.Fatalf("me policy = %+v, %v", list, err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "worker", 3, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: true}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("me unauthorized toggle: %v", err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "owner", 3, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "host", SubjectID: "not-a-member"}}}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("nonmember selected: %v", err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "owner", 3, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "host", SubjectID: "worker"}, {HomeTenantID: "host", SubjectID: "worker"}}}, todoAllow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("duplicate selection: %v", err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "host", "policy", "owner", 3, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "host", SubjectID: "worker"}}}, todoAllow)
	if err != nil || len(list.Items[0].SelectedCompleters) != 1 {
		t.Fatalf("selected policy = %+v, %v", list, err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		var before, after []byte
		if err := tx.QueryRow(ctx, `SELECT prior_items_json,items_json FROM chat_channel_todo_revision WHERE tenant_id='host' AND conversation_id='policy' AND revision=4`).Scan(&before, &after); err != nil {
			return err
		}
		if !strings.Contains(string(before), `"completion_mode": "ME"`) || !strings.Contains(string(after), `"completion_mode": "ME_AND_SELECTED"`) || !strings.Contains(string(after), `"membership_revision"`) {
			t.Errorf("policy audit before/after = %s / %s", before, after)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	workerList, err := s.ChannelTodo(ctx, "host", "host", "policy", "worker", todoAllow)
	if err != nil || !workerList.Items[0].CanToggle || workerList.Items[0].CanManageCompletionPolicy || len(workerList.Items[0].SelectedCompleters) != 0 {
		t.Fatalf("worker projection = %+v, %v", workerList, err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "other", 4, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: true}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("unselected toggle: %v", err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "host", "policy", "worker", 4, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: true}, todoAllow)
	if err != nil || list.Items[0].CompletedBySubjectID != "worker" {
		t.Fatalf("selected toggle = %+v, %v", list, err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now(),revision=revision+1 WHERE tenant_id='host' AND conversation_id='policy' AND member_id='worker'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "worker", 5, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: false}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked toggle: %v", err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='active',left_at=NULL,joined_at=now()+interval '1 second',revision=revision+1 WHERE tenant_id='host' AND conversation_id='policy' AND member_id='worker'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	workerList, err = s.ChannelTodo(ctx, "host", "host", "policy", "worker", todoAllow)
	if err != nil || workerList.Items[0].CanToggle {
		t.Fatalf("rejoined selection resurrected = %+v, %v", workerList, err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "policy", "worker", 5, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: false}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("rejoined toggle: %v", err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "host", "policy", "owner", 5, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "host", SubjectID: "worker"}}}, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "host", "policy", "worker", 6, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: false}, todoAllow)
	if err != nil || list.Items[0].Completed || list.Items[0].CompletedBySubjectID != "" {
		t.Fatalf("reselected reopen = %+v, %v", list, err)
	}
}

func TestChannelTodoGuestSelectionGrantEpoch(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "guest-policy", TenantID: "host", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "visitor", Role: "member", State: "active"}, {HomeTenantID: "guest", MemberID: "visitor", Role: "member", State: "active"}})
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_share_grant(id,tenant_id,conversation_id,consumer_tenant,version,scope,classification,residency,proposed_by,accepted_by,accepted_at,expires_at) VALUES('grant-a','host','guest-policy','guest',1,'conversation','','','owner','visitor',now(),now()+interval '1 day')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.MutateChannelTodo(ctx, "host", "host", "guest-policy", "visitor", 1, ChannelTodoMutation{Operation: "ADD", Text: "Same subject"}, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	id := list.Items[0].ID
	list, err = s.MutateChannelTodo(ctx, "host", "host", "guest-policy", "visitor", 2, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME"}, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "guest", "guest-policy", "visitor", 3, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: true}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("guest same subject bypass: %v", err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "host", "guest-policy", "visitor", 3, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "guest", SubjectID: "visitor"}}}, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	list, err = s.MutateChannelTodo(ctx, "host", "guest", "guest-policy", "visitor", 4, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: true}, todoAllow)
	if err != nil || list.Items[0].CompletedByHomeTenantID != "guest" {
		t.Fatalf("guest selected toggle = %+v, %v", list, err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_share_grant SET revoked_at=now(),revoked_by='owner',version=version+1 WHERE id='grant-a'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "guest", "guest-policy", "visitor", 5, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: false}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked guest toggle: %v", err)
	}
	ownerView, err := s.ChannelTodo(ctx, "host", "host", "guest-policy", "visitor", todoAllow)
	if err != nil || len(ownerView.Items[0].SelectedCompleters) != 0 {
		t.Fatalf("revoked selected identity leaked = %+v, %v", ownerView, err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_share_grant(id,tenant_id,conversation_id,consumer_tenant,version,scope,classification,residency,proposed_by,accepted_by,accepted_at,expires_at) VALUES('grant-b','host','guest-policy','guest',1,'conversation','','','owner','visitor',now(),now()+interval '1 day')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	guestList, err := s.ChannelTodo(ctx, "host", "guest", "guest-policy", "visitor", todoAllow)
	if err != nil || guestList.Items[0].CanToggle {
		t.Fatalf("renewed grant resurrected selection = %+v, %v", guestList, err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "guest", "guest-policy", "visitor", 5, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: false}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("renewed grant toggle: %v", err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "guest-policy", "visitor", 5, ChannelTodoMutation{Operation: "SET_COMPLETION_POLICY", ItemID: id, CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "guest", SubjectID: "visitor"}}}, todoAllow); err != nil {
		t.Fatalf("explicit reselection: %v", err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "guest", "guest-policy", "visitor", 6, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: false}, todoAllow); err != nil {
		t.Fatalf("reselected guest reopen: %v", err)
	}
}

func TestChannelTodoAtomicAddPolicy(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "atomic", TenantID: "host", Kind: "PRIVATE_CHANNEL", OwnerID: "creator", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "creator", Role: "manager", State: "active"}, {MemberID: "selected", Role: "member", State: "active"}, {MemberID: "other", Role: "member", State: "active"}})
	ctx := context.Background()
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "atomic", "creator", 1, ChannelTodoMutation{Operation: "ADD", Text: "invalid selected", CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "host", SubjectID: "missing"}}}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("invalid selected add: %v", err)
	}
	list, err := s.MutateChannelTodo(ctx, "host", "host", "atomic", "creator", 1, ChannelTodoMutation{Operation: "ADD", Text: "restricted at birth", CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []ChannelTodoSelectedMember{{HomeTenantID: "host", SubjectID: "selected"}}}, todoAllow)
	if err != nil || list.Revision != 2 || list.Items[0].CompletionMode != "ME_AND_SELECTED" || len(list.Items[0].SelectedCompleters) != 1 {
		t.Fatalf("atomic add = %+v, %v", list, err)
	}
	id := list.Items[0].ID
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "atomic", "other", 2, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: true}, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("initial unrestricted window: %v", err)
	}
	if _, err := s.MutateChannelTodo(ctx, "host", "host", "atomic", "selected", 2, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: id, Completed: true}, todoAllow); err != nil {
		t.Fatalf("selected initial toggle: %v", err)
	}
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		var payload []byte
		if err := tx.QueryRow(ctx, `SELECT items_json FROM chat_channel_todo_revision WHERE tenant_id='host' AND conversation_id='atomic' AND revision=2`).Scan(&payload); err != nil {
			return err
		}
		if !strings.Contains(string(payload), `"completion_mode": "ME_AND_SELECTED"`) {
			t.Errorf("atomic policy absent from first revision: %s", payload)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
