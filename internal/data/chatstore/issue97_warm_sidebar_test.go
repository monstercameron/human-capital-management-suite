package chatstore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// chatPostProbes lists every chat_post plan node and how many times it ran.
func chatPostProbes(n chatscaleNode) (nodes int, executed float64) {
	if n.Relation == "chat_post" {
		nodes++
		executed += n.Loops
	}
	for _, c := range n.Children {
		cn, ce := chatPostProbes(c)
		nodes += cn
		executed += ce
	}
	return nodes, executed
}

func issue97Plan(t *testing.T, s *Store, rooms []string) (string, chatscaleNode) {
	t.Helper()
	ctx := context.Background()
	var raw string
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+chatscaleCachedSQL, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms, countScanLimit).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	var plans []chatscalePlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("explain: %v", err)
	}
	return raw, plans[0].Plan
}

// Issue #97: with every cached row current (no post after the cached head), the
// sidebar read must not execute the chat_post history probe at all.
func TestIssue97_WarmSidebarExecutesNoHistoryProbe_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 5000)
	r := NewRecipientStateStore(s)
	rooms := chatscaleRooms(300)
	ctx := context.Background()
	// The served budget shape: 200 busy rooms plus 100 empty ones the reader belongs to.
	if err := s.execTenant(ctx, chatscaleTenant, `INSERT INTO chat_conversation(id,tenant_id,kind,owner_id) SELECT 'chatscale-c-'||g,$1,'PUBLIC_CHANNEL',$2 FROM generate_series(200,299) g ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	if err := s.execTenant(ctx, chatscaleTenant, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id) SELECT $1,id,$1,$2 FROM chat_conversation WHERE tenant_id=$1 ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	first, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil || len(first) != len(rooms) {
		t.Fatalf("cold read rows=%d err=%v", len(first), err)
	}
	// Warm: the cold read saved every row at its current head.
	raw, plan := issue97Plan(t, s, rooms)
	if nodes, executed := chatPostProbes(plan); executed > 0 {
		t.Fatalf("warm sidebar executes %v chat_post probes over %d nodes: %s", executed, nodes, raw)
	}
	warm, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil {
		t.Fatal(err)
	}
	for room, want := range first {
		if warm[room] != want {
			t.Fatalf("warm counts for %s = %+v, cold %+v", room, warm[room], want)
		}
	}
}

// A room with a new post after the cached head still probes and still counts it.
func TestIssue97_NewPostIsStillCountedAfterWarmRead_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 2000)
	r := NewRecipientStateStore(s)
	rooms := chatscaleRooms(20)
	ctx := context.Background()
	before, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil {
		t.Fatal(err)
	}
	post, err := s.sendPostRaw(ctx, SendRequest{TenantID: chatscaleTenant, ConversationID: rooms[3], AuthorID: "chatscale-u-31", ClientKey: "issue97-new", Body: "Fresh payroll note", References: []byte("[]")})
	if err != nil || post.Sequence == 0 {
		t.Fatalf("send: %+v %v", post, err)
	}
	after, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil {
		t.Fatal(err)
	}
	if after[rooms[3]].Unread != before[rooms[3]].Unread+1 {
		t.Fatalf("unread %d -> %d, want +1", before[rooms[3]].Unread, after[rooms[3]].Unread)
	}
	for i, room := range rooms {
		if i != 3 && after[room] != before[room] {
			t.Fatalf("untouched %s changed %+v -> %+v", room, before[room], after[room])
		}
	}
}

// The reader's own sends move the conversation head but never add to their
// unread count. Once the sidebar has read them, the cached row must be current
// again, so the next read executes no history probe.
func TestIssue97_OwnSendDoesNotLeaveASidebarProbeBehind_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 2000)
	r := NewRecipientStateStore(s)
	rooms := chatscaleRooms(20)
	ctx := context.Background()
	before, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.sendPostRaw(ctx, SendRequest{TenantID: chatscaleTenant, ConversationID: rooms[2], AuthorID: chatscaleReader, ClientKey: "issue97-own-" + string(rune('a'+i)), Body: "My own follow-up", References: []byte("[]")}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
	if err != nil {
		t.Fatal(err)
	}
	for _, room := range rooms {
		if got[room] != before[room] {
			t.Fatalf("own sends changed counts for %s: %+v -> %+v", room, before[room], got[room])
		}
	}
	raw, plan := issue97Plan(t, s, rooms)
	if _, executed := chatPostProbes(plan); executed > 0 {
		t.Fatalf("sidebar executes %v chat_post probes after own sends were read: %s", executed, raw)
	}
}
