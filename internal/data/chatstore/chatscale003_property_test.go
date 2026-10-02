package chatstore

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// chatscale003Oracle is the bounded history scan, with no cache and none of the
// fast counting: the statement the cached read must always agree with.
func chatscale003Oracle(t *testing.T, s *Store, rooms []string, limit int) map[string]chatrecipient.Counts {
	t.Helper()
	ctx := context.Background()
	out := map[string]chatrecipient.Counts{}
	err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, chatscaleRebuildSQL, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var room string
			var c chatrecipient.Counts
			if err := rows.Scan(&room, &c.Unread, &c.Mentions); err != nil {
				return err
			}
			out[room] = c
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type chatscale003World struct {
	t     *testing.T
	s     *Store
	rng   *rand.Rand
	rooms []string
	posts map[string][]string // room -> ids still stored
	n     int
	ids   int
	base  time.Time
}

func (w *chatscale003World) at() time.Time { return w.base.Add(time.Duration(w.n) * time.Minute) }

func (w *chatscale003World) exec(sql string, args ...any) {
	w.t.Helper()
	if err := w.s.execTenant(context.Background(), chatscaleTenant, sql, args...); err != nil {
		w.t.Fatalf("%v\n%s", err, sql)
	}
}

func (w *chatscale003World) mention(who string) string {
	return fmt.Sprintf(`[{"Kind":"PERSON_MENTION","TenantID":%q,"ID":%q}]`, chatscaleTenant, who)
}

// send follows the send path: advance the conversation's sequence under its
// row, then insert the post at that sequence.
func (w *chatscale003World) send(room string) {
	w.t.Helper()
	ctx := context.Background()
	author, body, refs := "other-"+fmt.Sprint(w.rng.Intn(3)), "message", "[]"
	switch k := w.rng.Intn(20); {
	case k < 3:
		author = chatscaleReader
	case k < 6:
		refs = w.mention(chatscaleReader)
	case k < 8:
		refs = w.mention("someone-else")
	case k < 10:
		body = chat.MembershipAddedMarker + "other-9"
	}
	err := w.s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		var seq int64
		if err := tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING post_sequence`, chatscaleTenant, room).Scan(&seq); err != nil {
			return err
		}
		w.ids++
		id := fmt.Sprintf("c3-p-%d", w.ids)
		w.posts[room] = append(w.posts[room], id)
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,created_at,updated_at)
 VALUES($1,$2,$3,$4,$2,$5,$6,$7::jsonb,$8,$8)`, id, chatscaleTenant, room, author, seq, body, refs, w.at())
		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *chatscale003World) pick(room string) (string, bool) {
	ids := w.posts[room]
	if len(ids) == 0 {
		return "", false
	}
	return ids[w.rng.Intn(len(ids))], true
}

func (w *chatscale003World) step(room string) string {
	switch op := w.rng.Intn(14); {
	case op < 6:
		w.send(room)
		return "send"
	case op == 6:
		if id, ok := w.pick(room); ok {
			refs := "[]"
			if w.rng.Intn(2) == 0 {
				refs = w.mention(chatscaleReader)
			}
			w.exec(`UPDATE chat_post SET references_json=$3::jsonb,revision=revision+1,updated_at=$4 WHERE tenant_id=$1 AND id=$2`, chatscaleTenant, id, refs, w.at())
		}
		return "edit"
	case op == 7:
		if id, ok := w.pick(room); ok {
			w.exec(`UPDATE chat_post SET tombstoned=NOT tombstoned,revision=revision+1,updated_at=$3 WHERE tenant_id=$1 AND id=$2`, chatscaleTenant, id, w.at())
		}
		return "remove or restore"
	case op == 8:
		if id, ok := w.pick(room); ok {
			w.exec(`DELETE FROM chat_post WHERE tenant_id=$1 AND id=$2`, chatscaleTenant, id)
			ids := w.posts[room]
			for i := range ids {
				if ids[i] == id {
					w.posts[room] = append(ids[:i:i], ids[i+1:]...)
					break
				}
			}
		}
		return "delete"
	case op == 9:
		w.exec(`INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence,updated_at)
 SELECT $1,$1,$2,c.id,c.post_sequence,$4 FROM chat_conversation c WHERE c.tenant_id=$1 AND c.id=$3
 ON CONFLICT (tenant_id,home_tenant_id,member_id,conversation_id) DO UPDATE SET last_sequence=EXCLUDED.last_sequence,revision=chat_cursor.revision+1,updated_at=EXCLUDED.updated_at`, chatscaleTenant, chatscaleReader, room, w.at())
		return "mark read"
	case op == 10:
		w.exec(`INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence,updated_at)
 SELECT $1,$1,$2,c.id,GREATEST(0,c.post_sequence-$5),$4 FROM chat_conversation c WHERE c.tenant_id=$1 AND c.id=$3
 ON CONFLICT (tenant_id,home_tenant_id,member_id,conversation_id) DO UPDATE SET last_sequence=EXCLUDED.last_sequence,revision=chat_cursor.revision+1,updated_at=EXCLUDED.updated_at`, chatscaleTenant, chatscaleReader, room, w.at(), w.rng.Intn(12))
		return "mark unread"
	case op == 11:
		w.exec(`UPDATE chat_membership SET state='removed',revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, room, chatscaleReader)
		return "leave"
	case op == 12:
		visibility := "FULL_HISTORY"
		if w.rng.Intn(2) == 0 {
			visibility = "FROM_JOIN"
		}
		w.exec(`UPDATE chat_membership SET state='active',history_visibility=$4,joined_at=$5,revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, room, chatscaleReader, visibility, w.at().Add(-time.Duration(w.rng.Intn(40))*time.Minute))
		return "join"
	default:
		w.send(room)
		w.send(room)
		return "burst"
	}
}

// CHATSCALE-003 PROPERTY: for any interleaving of sends, edits, removals,
// deletions, reads, mark-unreads, joins and leaves, the sidebar read (cached
// rows advanced by the delta, uncached rows counted without reading bodies)
// equals the bounded history scan, both below the display limit and with
// windows that saturate it.
func TestTodo_CHATSCALE_003_Property_Delta(t *testing.T) {
	for _, limit := range []int{4, 5000} {
		limit := limit
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			s, _ := chatFixture(t)
			ctx := context.Background()
			w := &chatscale003World{t: t, s: s, rng: rand.New(rand.NewSource(int64(46000 + limit))), posts: map[string][]string{}, base: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)}
			for i := 0; i < 3; i++ {
				w.rooms = append(w.rooms, fmt.Sprintf("c3-room-%d", i))
			}
			w.exec(`INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) SELECT r,$1,'PUBLIC_CHANNEL',r,$2 FROM unnest($3::text[]) r`, chatscaleTenant, chatscaleReader, w.rooms)
			w.exec(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,joined_at) SELECT $1,r,$1,$2,'2024-01-01'::timestamptz FROM unnest($3::text[]) r`, chatscaleTenant, chatscaleReader, w.rooms)
			r := NewRecipientStateStore(s)
			served := 0
			steps := 220
			if testing.Short() {
				steps = 60
			}
			for i := 0; i < steps; i++ {
				w.n++
				room := w.rooms[w.rng.Intn(len(w.rooms))]
				op := w.step(room)
				want := chatscale003Oracle(t, s, w.rooms, limit)
				for replay := 0; replay < 2; replay++ {
					got, err := r.chatscaleReadCountsLimit(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, w.rooms, false, limit)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("step %d (%s in %s) replay %d: got %v want %v err=%v", i, op, room, replay, got, want, err)
					}
				}
				if i%7 == 0 {
					recount, err := r.chatscaleReadCountsLimit(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, w.rooms, true, limit)
					if err != nil || !reflect.DeepEqual(recount, want) {
						t.Fatalf("step %d: recount %v want %v err=%v", i, recount, want, err)
					}
				}
				// The conversation head kept by the insert trigger (the single-post fast
				// path included) is the head the history would give, and post_sequence
				// never trails a stored sequence.
				var bad []string
				if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
					rows, err := tx.Query(ctx, `SELECT c.id FROM chat_conversation c
 LEFT JOIN LATERAL (SELECT p.created_at,p.author_id,p.author_home_tenant_id FROM chat_post p
  WHERE p.tenant_id=c.tenant_id AND p.conversation_id=c.id AND NOT p.tombstoned ORDER BY p.created_at DESC,p.id DESC LIMIT 1) h ON true
 WHERE c.tenant_id=$1 AND ((c.chatscale_head_ready AND (c.chatscale_last_activity_at IS DISTINCT FROM h.created_at
  OR c.chatscale_last_author IS DISTINCT FROM h.author_id OR c.chatscale_last_author_home IS DISTINCT FROM h.author_home_tenant_id))
  OR c.post_sequence<COALESCE((SELECT max(sequence) FROM chat_post p WHERE p.tenant_id=c.tenant_id AND p.conversation_id=c.id),0))`, chatscaleTenant)
					if err != nil {
						return err
					}
					defer rows.Close()
					for rows.Next() {
						var id string
						if err := rows.Scan(&id); err != nil {
							return err
						}
						bad = append(bad, id)
					}
					return rows.Err()
				}); err != nil {
					t.Fatal(err)
				}
				if len(bad) > 0 {
					t.Fatalf("step %d (%s in %s): conversation head disagrees with history in %v", i, op, room, bad)
				}
				// A row stamped below the head that still serves is the delta at work.
				var behind int
				if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
					return tx.QueryRow(ctx, `SELECT count(*) FROM chatscale_read_state s JOIN chat_conversation c ON c.tenant_id=s.tenant_id AND c.id=s.conversation_id WHERE s.head_sequence<c.post_sequence`).Scan(&behind)
				}); err != nil {
					t.Fatal(err)
				}
				served += behind
			}
			if served == 0 {
				t.Fatal("no cached row was ever read behind its conversation head: the delta path never ran")
			}
		})
	}
}

// A plain send must leave cached badges valid: the history revision stays, the
// cached row is read again without a recount, and the new post is added.
func TestTodo_CHATSCALE_003_Integration_SendKeepsCache(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	w := &chatscale003World{t: t, s: s, rng: rand.New(rand.NewSource(7)), posts: map[string][]string{}, base: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), rooms: []string{"c3-room-0"}}
	w.exec(`INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('c3-room-0',$1,'PUBLIC_CHANNEL','c3',$2)`, chatscaleTenant, chatscaleReader)
	w.exec(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,joined_at) VALUES($1,'c3-room-0',$1,$2,'2024-01-01')`, chatscaleTenant, chatscaleReader)
	for i := 0; i < 3; i++ {
		w.n++
		w.send("c3-room-0")
	}
	r := NewRecipientStateStore(s)
	read := func() chatrecipient.Counts {
		got, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, w.rooms)
		if err != nil {
			t.Fatal(err)
		}
		return got["c3-room-0"]
	}
	first := read()
	var generation, head, stamped int64
	state := func() {
		if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT c.chatscale_history_revision,c.post_sequence,s.head_sequence FROM chat_conversation c JOIN chatscale_read_state s ON s.tenant_id=c.tenant_id AND s.conversation_id=c.id WHERE c.tenant_id=$1 AND c.id='c3-room-0'`, chatscaleTenant).Scan(&generation, &head, &stamped)
		}); err != nil {
			t.Fatal(err)
		}
	}
	state()
	revision, was := generation, stamped
	w.n++
	w.exec(`UPDATE chat_conversation SET post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id='c3-room-0'`, chatscaleTenant)
	w.exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at,updated_at) SELECT 'c3-late',$1,'c3-room-0','other-1',$1,post_sequence,'late',$2,$2 FROM chat_conversation WHERE tenant_id=$1 AND id='c3-room-0'`, chatscaleTenant, w.at())
	state()
	if generation != revision {
		t.Fatalf("a send moved the history revision %d -> %d", revision, generation)
	}
	if got := read(); got.Unread != first.Unread+1 {
		t.Fatalf("after one send unread=%d want %d", got.Unread, first.Unread+1)
	}
	state()
	if stamped != was || stamped >= head {
		t.Fatalf("the cached row was recounted: head stamp %d -> %d, head %d", was, stamped, head)
	}
	// An edit still invalidates, and the recount stamps the new head.
	w.exec(`UPDATE chat_post SET tombstoned=true,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND id='c3-late'`, chatscaleTenant)
	if got := read(); got.Unread != first.Unread {
		t.Fatalf("after the removal unread=%d want %d", got.Unread, first.Unread)
	}
	state()
	if stamped != head {
		t.Fatalf("recount stamp %d, head %d", stamped, head)
	}
}
