package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

func TestTodo_CHAT_022_Integration_Recipient(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, s, "host")
	// The second member is the recipient; a distinct home tenant proves that
	// identical subject IDs in different companies cannot share counts.
	err := s.execTenant(ctx, "host", `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,state,history_visibility) VALUES('host','c-1','home','alice','active','FULL_HISTORY')`)
	if err != nil {
		t.Fatal(err)
	}
	err = s.execTenant(ctx, "host", `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json) VALUES('p1','host','c-1','u-1','host',1,'hello','[{"Kind":"PERSON_MENTION","TenantID":"home","ID":"alice"}]')`)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRecipientStateStore(s)
	id := chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "home", SubjectID: "alice", ConversationID: "c-1"}
	got, err := r.Counts(ctx, id)
	if err != nil || got.Unread != 1 || got.Mentions != 1 {
		t.Fatalf("counts=%+v err=%v", got, err)
	}
	if err := s.execTenant(ctx, "host", `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence) VALUES('host','home','alice','c-1',1)`); err != nil {
		t.Fatal(err)
	}
	got, err = r.Counts(ctx, id)
	if err != nil || got.Unread != 0 || got.Mentions != 0 {
		t.Fatalf("read counts=%+v err=%v", got, err)
	}
	if err := s.execTenant(ctx, "host", `UPDATE chat_post SET revision=2,updated_at=now()+interval '1 second' WHERE tenant_id='host' AND id='p1'`); err != nil {
		t.Fatal(err)
	}
	got, err = r.Counts(ctx, id)
	if err != nil || got.Unread != 0 || got.Mentions != 1 {
		t.Fatalf("edited mention counts=%+v err=%v", got, err)
	}
	err = s.execTenant(ctx, "host", `UPDATE chat_post SET tombstoned=true WHERE tenant_id='host' AND id='p1'`)
	if err != nil {
		t.Fatal(err)
	}
	got, err = r.Counts(ctx, id)
	if err != nil || got.Unread != 0 || got.Mentions != 0 {
		t.Fatalf("tombstone counts=%+v err=%v", got, err)
	}
	err = s.execTenant(ctx, "host", `UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND home_tenant_id='home' AND member_id='alice'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Follow(ctx, id, "p1"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked follow read: %v", err)
	}
}

func TestTodo_CHAT_023_Integration_Recipient(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, s, "host")
	if err := s.execTenant(ctx, "host", `UPDATE chat_membership SET home_tenant_id='host' WHERE tenant_id='host' AND conversation_id='c-1' AND member_id='u-1'`); err != nil {
		t.Fatal(err)
	}
	err := s.execTenant(ctx, "host", `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('root','host','c-1','u-1','host',1,'hello')`)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRecipientStateStore(s)
	id := chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "host", SubjectID: "u-1", ConversationID: "c-1"}
	f, err := r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root", Followed: true}, 1)
	if err != nil || !f.Followed || f.Revision != 2 {
		t.Fatalf("follow=%+v err=%v", f, err)
	}
	if _, err = r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root", Followed: false}, 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale follow: %v", err)
	}
	f, err = r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root", Followed: false}, 2)
	if err != nil || f.Followed || f.Revision != 3 {
		t.Fatalf("unfollow=%+v err=%v", f, err)
	}
	if err := s.execTenant(ctx, "host", `UPDATE chat_membership SET history_visibility='NONE' WHERE tenant_id='host' AND home_tenant_id='host' AND member_id='u-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root", Followed: true}, 3); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("hidden root follow: %v", err)
	}
	if _, err := r.Follow(ctx, id, "root"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("hidden root follow read: %v", err)
	}
}
func TestTodo_CHAT_032_Integration_Recipient(t *testing.T) {
	s, _ := chatFixture(t)
	r := NewRecipientStateStore(s)
	ctx := context.Background()
	a, err := r.PutSidebar(ctx, "home", "alice", chatrecipient.Sidebar{Layout: []byte(`{"sections":[{"id":"team"}]}`)}, 1)
	if err != nil || a.Revision != 2 {
		t.Fatalf("save=%+v err=%v", a, err)
	}
	b, err := r.Sidebar(ctx, "home", "bob")
	if err != nil || b.Revision != 1 {
		t.Fatalf("another user=%+v err=%v", b, err)
	}
	c, err := r.Sidebar(ctx, "home", "alice")
	var left, right any
	if err := json.Unmarshal(c.Layout, &left); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(a.Layout, &right); err != nil {
		t.Fatal(err)
	}
	if err != nil || !reflect.DeepEqual(left, right) {
		t.Fatalf("reload=%+v err=%v", c, err)
	}
}
func TestTodo_CHAT_034_Integration_Recipient(t *testing.T) {
	s, _ := chatFixture(t)
	r := NewRecipientStateStore(s)
	ctx := context.Background()
	x, err := r.PutQuietHours(ctx, "home", "alice", chatrecipient.QuietHours{Timezone: "UTC", StartMinute: 120, EndMinute: 360, Enabled: true}, 1)
	if err != nil || x.Revision != 2 {
		t.Fatalf("save=%+v err=%v", x, err)
	}
	y, err := r.QuietHours(ctx, "home", "alice")
	if err != nil || !y.Enabled || y.StartMinute != 120 {
		t.Fatalf("reload=%+v err=%v", y, err)
	}
}
func TestRecipientSidebarOptimisticRace(t *testing.T) {
	s, _ := chatFixture(t)
	r := NewRecipientStateStore(s)
	ctx := context.Background()
	const n = 8
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.PutSidebar(ctx, "home", "alice", chatrecipient.Sidebar{Layout: []byte(`{"sections":[]}`)}, 1)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, chat.ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("successful concurrent revisions=%d want 1", success)
	}
}
