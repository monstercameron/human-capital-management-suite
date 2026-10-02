package chatstore

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func chatscaleOracle(t *testing.T, s *Store, rooms []string) map[string]chatrecipient.Counts {
	t.Helper()
	out := make(map[string]chatrecipient.Counts)
	err := s.RunTenantTx(context.Background(), chatscaleTenant, func(tx dbport.Tx) error {
		for _, room := range rooms {
			var u, m uint64
			if err := tx.QueryRow(context.Background(), chatscaleLegacyCounts, chatscaleTenant, room, chatscaleTenant, chatscaleReader, countScanLimit).Scan(&u, &m); err != nil {
				return err
			}
			// The old single-room port returns zero for a revoked member. The batched
			// port omits that member altogether, so only include active memberships.
			var active bool
			if err := tx.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3 AND state='active')`, chatscaleTenant, room, chatscaleReader).Scan(&active); err != nil {
				return err
			}
			if active {
				out[room] = chatrecipient.Counts{Unread: u, Mentions: m}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTodo_CHATSCALE_003(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	r := NewRecipientStateStore(s)
	ctx := context.Background()
	rooms := []string{chatscaleRoom, "chatscale-c-001", "chatscale-c-002"}
	want := chatscaleOracle(t, s, rooms)
	for i := 0; i < 2; i++ {
		got, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("cached counts=%v want %v err=%v", got, want, err)
		}
	}
	if err := s.execTenant(ctx, chatscaleTenant, `UPDATE chatscale_read_state SET unread=0,mentions=0 WHERE tenant_id=$1`, chatscaleTenant); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := r.ChatscaleRebuildCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil || !reflect.DeepEqual(rebuilt, want) {
		t.Fatalf("rebuilt=%v want %v err=%v", rebuilt, want, err)
	}
	repaired, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil || !reflect.DeepEqual(repaired, want) {
		t.Fatalf("repair left stale cached counts=%v want=%v err=%v", repaired, want, err)
	}
	for _, args := range []struct {
		host  string
		rooms []string
	}{{"", rooms}, {chatscaleTenant, make([]string, 301)}} {
		if _, err := r.ChatscaleSidebarCounts(ctx, args.host, chatscaleTenant, chatscaleReader, args.rooms); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("invalid batch error=%v", err)
		}
	}
	empty, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty batch=%v err=%v", empty, err)
	}
}

func TestTodo_CHATSCALE_003_Property(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	r := NewRecipientStateStore(s)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(3003))
	rooms := []string{chatscaleRoom, "chatscale-c-001", "chatscale-c-002"}
	for i := 0; i < 60; i++ {
		room := rooms[rng.Intn(len(rooms))]
		err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			var err error
			switch i % 6 {
			case 0:
				_, err = tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json)
 SELECT $1,$2,$3,'sender',$2,COALESCE(max(sequence),0)+1,'new message',jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$2::text,'ID',$4::text)) FROM chat_post WHERE tenant_id=$2 AND conversation_id=$3`, fmt.Sprintf("chatscale-generated-%d", i), chatscaleTenant, room, chatscaleReader)
			case 1:
				_, err = tx.Exec(ctx, `UPDATE chat_post SET references_json='[]',revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND sequence=1`, chatscaleTenant, room)
			case 2:
				_, err = tx.Exec(ctx, `UPDATE chat_post SET tombstoned=NOT tombstoned,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND sequence=2`, chatscaleTenant, room)
			case 3:
				_, err = tx.Exec(ctx, `UPDATE chat_cursor SET last_sequence=$4,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, room, chatscaleReader, rng.Intn(80))
			case 4:
				_, err = tx.Exec(ctx, `UPDATE chat_membership SET state=CASE WHEN state='active' THEN 'removed' ELSE 'active' END,revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, room, chatscaleReader)
			case 5:
				_, err = tx.Exec(ctx, `UPDATE chat_membership SET history_visibility=CASE WHEN history_visibility='FULL_HISTORY' THEN 'FROM_JOIN' ELSE 'FULL_HISTORY' END,joined_at='2024-01-01 00:10:00+00',revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, room, chatscaleReader)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		want := chatscaleOracle(t, s, rooms)
		for replay := 0; replay < 2; replay++ {
			got, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("operation=%d replay=%d got=%v want=%v err=%v", i, replay, got, want, err)
			}
		}
	}
	if err := s.execTenant(ctx, chatscaleTenant, `UPDATE chat_membership SET state='active',history_visibility='FULL_HISTORY',revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, chatscaleRoom, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	if err := s.execTenant(ctx, chatscaleTenant, `UPDATE chat_cursor SET last_sequence=0,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, chatscaleRoom, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	if err := s.execTenant(ctx, chatscaleTenant, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json)
 SELECT 'chatscale-cap-'||g,$1,$2,'sender',$1,100000+g,'capped mention',jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$1::text,'ID',$3::text)) FROM generate_series(1,6000) g`, chatscaleTenant, chatscaleRoom, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	id := chatrecipient.Identity{HostTenantID: chatscaleTenant, HomeTenantID: chatscaleTenant, SubjectID: chatscaleReader, ConversationID: chatscaleRoom}
	for i := 0; i < 2; i++ {
		got, err := r.Counts(ctx, id)
		if err != nil || got.Unread != countScanLimit || got.Mentions != countScanLimit {
			t.Fatalf("display cap counts=%v err=%v", got, err)
		}
		if i == 0 {
			if err := s.execTenant(ctx, chatscaleTenant, `DELETE FROM chat_post WHERE tenant_id=$1 AND id='chatscale-cap-6000'`, chatscaleTenant); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestTodo_CHATSCALE_003_Fault(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	r := NewRecipientStateStore(s)
	ctx := context.Background()
	id := chatrecipient.Identity{HostTenantID: chatscaleTenant, HomeTenantID: chatscaleTenant, SubjectID: chatscaleReader, ConversationID: chatscaleRoom}
	before, err := r.Counts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	aborted := errors.New("crash before commit")
	err = s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true WHERE tenant_id=$1 AND conversation_id=$2`, chatscaleTenant, chatscaleRoom)
		if err != nil {
			return err
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	got, err := r.Counts(ctx, id)
	if err != nil || got != before {
		t.Fatalf("rolled back counts=%v want=%v err=%v", got, before, err)
	}
	if err := s.execTenant(ctx, chatscaleTenant, `UPDATE chat_post SET tombstoned=true WHERE tenant_id=$1 AND conversation_id=$2`, chatscaleTenant, chatscaleRoom); err != nil {
		t.Fatal(err)
	}
	// No outbox consumer has run: the same-transaction fence must already repair
	// the badge, and retrying the read cannot apply a removal twice.
	for i := 0; i < 2; i++ {
		got, err := r.Counts(ctx, id)
		if err != nil || got != (chatrecipient.Counts{}) {
			t.Fatalf("resume=%d counts=%v err=%v", i, got, err)
		}
	}
}

func TestTodo_CHATSCALE_003_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "heads")
	ctx := context.Background()
	if n, err := s.ChatscaleBackfillHeads(ctx, "heads", 1); err != nil || n != 1 {
		t.Fatalf("first batch=%d err=%v", n, err)
	}
	if n, err := s.ChatscaleBackfillHeads(ctx, "heads", 1); err != nil || n != 0 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	if _, err := s.ChatscaleBackfillHeads(ctx, "heads", 501); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal(err)
	}
	a := NewAdapter(s)
	for _, p := range []struct {
		id  string
		seq int
		at  string
	}{{"older", 1, "2024-01-01"}, {"newer", 2, "2024-02-01"}} {
		if err := s.execTenant(ctx, "heads", `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,sequence,body,created_at) VALUES($1,$2,'c-1','u-1',$3,'head',$4::timestamptz)`, p.id, "heads", p.seq, p.at); err != nil {
			t.Fatal(err)
		}
	}
	c, err := a.GetConversation(ctx, "heads", "c-1")
	if err != nil || c.LastActivityAt == nil || c.LastActivityAt.Month() != 2 {
		t.Fatalf("head=%+v err=%v", c, err)
	}
	if err := s.execTenant(ctx, "heads", `UPDATE chat_post SET tombstoned=true WHERE tenant_id=$1 AND id='newer'`, "heads"); err != nil {
		t.Fatal(err)
	}
	c, err = a.GetConversation(ctx, "heads", "c-1")
	if err != nil || c.LastActivityAt == nil || c.LastActivityAt.Month() != 1 {
		t.Fatalf("removed head=%+v err=%v", c, err)
	}
	if err := s.execTenant(ctx, "heads", `DELETE FROM chat_post WHERE tenant_id=$1`, "heads"); err != nil {
		t.Fatal(err)
	}
	c, err = a.GetConversation(ctx, "heads", "c-1")
	if err != nil || c.LastActivityAt != nil {
		t.Fatalf("empty head=%+v err=%v", c, err)
	}
}

func TestTodo_CHATSCALE_003_Integration_RollingSchema(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	ctx := context.Background()
	rooms := []string{chatscaleRoom}
	want := chatscaleOracle(t, s, rooms)
	if err := s.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `DROP TABLE chatscale_read_state`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `ALTER TABLE chat_conversation DROP COLUMN chatscale_history_revision,
 DROP COLUMN chatscale_head_ready,DROP COLUMN chatscale_last_activity_at,
 DROP COLUMN chatscale_last_author,DROP COLUMN chatscale_last_author_home`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, err := NewRecipientStateStore(s).ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("pre-migration counts=%v want=%v err=%v", got, want, err)
	}
	a := NewAdapter(s)
	c, err := a.GetConversation(ctx, chatscaleTenant, chatscaleRoom)
	if err != nil || c.LastActivityAt == nil {
		t.Fatalf("pre-migration conversation=%+v err=%v", c, err)
	}
	list, err := a.ListConversations(ctx, chat.Principal{TenantID: chatscaleTenant, SubjectID: chatscaleReader}, chatscaleTenant, chat.Page{PageSize: 200}, chat.ConversationScope{})
	if err != nil || len(list.Conversations) != 200 {
		t.Fatalf("pre-migration list=%d err=%v", len(list.Conversations), err)
	}
	if _, err := s.ChatscaleBackfillHeads(ctx, chatscaleTenant, 100); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("pre-migration backfill=%v", err)
	}
}
