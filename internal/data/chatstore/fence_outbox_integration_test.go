package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// leased returns a context carrying the write lease a routed caller would hold.
func leased(ctx context.Context, tenant, conversation, shard string, epoch uint64) context.Context {
	return chatrouting.WithWriteLease(ctx, chatrouting.WriteLease{
		Route:     chatrouting.Route{ConversationID: conversation, HostTenantID: tenant, ShardID: shard, Epoch: epoch, State: chatrouting.StateActive},
		ExpiresAt: time.Now().Add(time.Hour),
	})
}

// TestTodo_CHAT_007_PlacedConversationRejectsUnleasedWrites proves the fence is
// no longer opt-in: once the route authority has placed a conversation on a
// shard, a caller that omits its lease is refused on every write path instead of
// silently skipping the epoch and shard comparison.
func TestTodo_CHAT_007_PlacedConversationRejectsUnleasedWrites(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "placed", TenantID: "host", Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(leased(ctx, c.TenantID, c.ID, "chat-local", 1), c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	send := chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "unleased"}
	if _, err := s.SendPost(ctx, send, chat.Post{AuthorID: "alice", Body: "no lease"}); !errors.Is(err, ErrNoRouteLease) {
		t.Fatalf("unleased send=%v want ErrNoRouteLease", err)
	}
	good := leased(ctx, c.TenantID, c.ID, "chat-local", 1)
	p, err := s.SendPost(good, send, chat.Post{AuthorID: "alice", Body: "leased"})
	if err != nil {
		t.Fatal(err)
	}
	edit := chat.EditPostRequest{Principal: send.Principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "edited", ExpectedRevision: 1}
	if _, err = s.EditPost(ctx, edit); !errors.Is(err, ErrNoRouteLease) {
		t.Fatalf("unleased edit=%v want ErrNoRouteLease", err)
	}
	if _, err = s.PutReaction(ctx, chat.Reaction{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, SubjectID: "alice", Emoji: "+1"}); !errors.Is(err, ErrNoRouteLease) {
		t.Fatalf("unleased reaction=%v want ErrNoRouteLease", err)
	}
	if _, err = s.PutPin(ctx, chat.Pin{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, PinnedBy: "alice"}); !errors.Is(err, ErrNoRouteLease) {
		t.Fatalf("unleased pin=%v want ErrNoRouteLease", err)
	}
	bob := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory}
	if _, err = s.PutMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, bob); !errors.Is(err, ErrNoRouteLease) {
		t.Fatalf("unleased membership=%v want ErrNoRouteLease", err)
	}
	// A stale lease is still rejected, and a current lease still commits.
	if _, err = s.EditPost(leased(ctx, c.TenantID, c.ID, "chat-local", 9), edit); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("stale epoch edit=%v", err)
	}
	if _, err = s.EditPost(good, edit); err != nil {
		t.Fatalf("leased edit=%v", err)
	}
}

// TestTodo_CHAT_007_MovingRouteRejectsEveryWrite proves the route state check is
// unconditional: a fenced conversation refuses writes even from a caller that
// carries no lease at all and so used to skip the check entirely.
func TestTodo_CHAT_007_MovingRouteRejectsEveryWrite(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, s, "tenant-a")
	if _, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "before", Body: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FenceWrites(ctx, chatrouting.MovePlan{ConversationID: "c-1", HostTenantID: "tenant-a", SourceEpoch: 1, MoveEpoch: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "during", Body: "blocked"}); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("unleased write to moving route=%v", err)
	}
	a := NewAdapter(s)
	if _, err := a.PutMembership(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "u-1"}, chat.Membership{TenantID: "tenant-a", ConversationID: "c-1", HomeTenantID: "tenant-a", SubjectID: "u-2", Role: chat.Member, HistoryVisibility: chat.FullHistory}); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("membership write to moving route=%v", err)
	}
}

// TestTodo_CHAT_017_SequenceSurvivesRetentionPurge proves the per-conversation
// sequence comes from a counter rather than max(sequence) over surviving rows,
// so a retention purge cannot make the next post reuse a number a cursor or an
// idempotency record already used.
func TestTodo_CHAT_017_SequenceSurvivesRetentionPurge(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, s, "tenant-a")
	var last int64
	for _, key := range []string{"one", "two", "three"} {
		p, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: key, Body: key})
		if err != nil {
			t.Fatal(err)
		}
		last = p.Sequence
	}
	if last != 3 {
		t.Fatalf("sequence=%d want 3", last)
	}
	// Retention removes the tail of the conversation.
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND sequence>=2`, "tenant-a", "c-1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "after-purge", Body: "after"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Sequence != 4 {
		t.Fatalf("sequence after purge=%d want 4 (no reuse)", p.Sequence)
	}
}

// TestTodo_CHAT_003_OutboxCursorAndPrune proves a consumer can resume from its
// own cursor and that retention can bound the outbox without dropping an event
// a consumer has not reached.
func TestTodo_CHAT_003_OutboxCursorAndPrune(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, s, "tenant-a")
	for _, key := range []string{"a", "b", "c"} {
		if _, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: key, Body: key}); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.PendingOutbox(ctx, "tenant-a", 100)
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending=%d %v", len(pending), err)
	}
	start, err := s.OutboxCursor(ctx, "tenant-a", "bridge")
	if err != nil || start != 0 {
		t.Fatalf("initial cursor=%d %v", start, err)
	}
	if err = s.AdvanceOutboxCursor(ctx, "tenant-a", "bridge", pending[0].ID); err != nil {
		t.Fatal(err)
	}
	// A cursor never moves backwards.
	if err = s.AdvanceOutboxCursor(ctx, "tenant-a", "bridge", 0); err != nil {
		t.Fatal(err)
	}
	cursor, err := s.OutboxCursor(ctx, "tenant-a", "bridge")
	if err != nil || cursor != pending[0].ID {
		t.Fatalf("cursor=%d want %d %v", cursor, pending[0].ID, err)
	}
	after, err := s.PendingOutboxAfter(ctx, "tenant-a", cursor, 100)
	if err != nil || len(after) != 2 || after[0].ID != pending[1].ID {
		t.Fatalf("resume=%+v %v", after, err)
	}
	horizon := time.Now().Add(time.Hour)
	// Nothing is published yet, so nothing may be pruned.
	if n, pruneErr := s.PruneOutbox(ctx, "tenant-a", horizon, 100); pruneErr != nil || n != 0 {
		t.Fatalf("prune unpublished=%d %v", n, pruneErr)
	}
	for _, e := range pending {
		if err = s.MarkOutboxPublished(ctx, "tenant-a", e.ID); err != nil {
			t.Fatal(err)
		}
	}
	// The slowest consumer cursor still sits on the first event, so only that
	// event may go.
	n, err := s.PruneOutbox(ctx, "tenant-a", horizon, 100)
	if err != nil || n != 1 {
		t.Fatalf("prune to cursor=%d %v", n, err)
	}
	if err = s.AdvanceOutboxCursor(ctx, "tenant-a", "bridge", pending[2].ID); err != nil {
		t.Fatal(err)
	}
	n, err = s.PruneOutbox(ctx, "tenant-a", horizon, 100)
	if err != nil || n != 2 {
		t.Fatalf("prune drained=%d %v", n, err)
	}
	var remaining int64
	if err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1`, "tenant-a").Scan(&remaining)
	}); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("outbox rows remaining=%d", remaining)
	}
	// Rows remain immutable while they exist.
	if err = s.execTenant(ctx, "tenant-a", `UPDATE chat_outbox SET event_type='tampered' WHERE tenant_id=$1`, "tenant-a"); err != nil {
		t.Fatalf("empty update should be a no-op, not an error: %v", err)
	}
	if _, err = s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "immutable", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if err = s.execTenant(ctx, "tenant-a", `UPDATE chat_outbox SET event_type='tampered' WHERE tenant_id=$1`, "tenant-a"); err == nil {
		t.Fatal("outbox row was rewritten")
	}
}

// TestTodo_CHAT_025_OutboxPayloadHasOneSpelling proves every producer in this
// package writes the same envelope keys and that watchPage's filter reads that
// same spelling, so a post event and an adapter event cannot disagree.
func TestTodo_CHAT_025_OutboxPayloadHasOneSpelling(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "spelling", TenantID: "tenant-a", Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	send := chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p1"}
	p, err := s.SendPost(ctx, send, chat.Post{AuthorID: "alice", Body: "one spelling"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.EditPost(ctx, chat.EditPostRequest{Principal: send.Principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "edited", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutReaction(ctx, chat.Reaction{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, SubjectID: "alice", Emoji: "+1"}); err != nil {
		t.Fatal(err)
	}
	if err = s.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		rows, queryErr := tx.Query(ctx, `SELECT event_type,payload FROM chat_outbox WHERE tenant_id=$1 ORDER BY id`, c.TenantID)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		seen := 0
		for rows.Next() {
			var kind string
			var payload []byte
			if scanErr := rows.Scan(&kind, &payload); scanErr != nil {
				return scanErr
			}
			var envelope OutboxEnvelope
			if unmarshalErr := json.Unmarshal(payload, &envelope); unmarshalErr != nil {
				return unmarshalErr
			}
			if envelope.ConversationID != c.ID || envelope.SchemaVersion != OutboxSchemaVersion || envelope.EventSequence == 0 || envelope.CorrelationID == "" || len(envelope.Value) == 0 {
				t.Fatalf("event %s payload=%s", kind, payload)
			}
			var loose map[string]json.RawMessage
			if unmarshalErr := json.Unmarshal(payload, &loose); unmarshalErr != nil {
				return unmarshalErr
			}
			for _, legacy := range []string{"schema_version", "event_sequence", "actor_id", "correlation_id", "policy_revision", "target_id"} {
				if _, bad := loose[legacy]; bad {
					t.Fatalf("event %s still writes snake_case key %q", kind, legacy)
				}
			}
			seen++
		}
		if rows.Err() != nil {
			return rows.Err()
		}
		if seen != 5 {
			t.Fatalf("outbox events=%d want conversation, membership, post, edit and reaction", seen)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The same spelling has to reach the watch reader, including the reaction
	// event written by the adapter path.
	page, err := s.ReadConversationEvents(ctx, chat.WatchConversationRequest{Principal: send.Principal, TenantID: c.TenantID, ConversationID: c.ID}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[chat.ConversationEventKind]bool{}
	for _, e := range page.Events {
		kinds[e.Event.Kind] = true
		if e.Event.Kind == chat.PostCreated && (e.Event.Post == nil || e.Event.Post.ID != p.ID) {
			t.Fatalf("post.created did not round trip: %+v", e)
		}
	}
	for _, want := range []chat.ConversationEventKind{chat.PostCreated, chat.PostEdited, chat.ReactionChanged, chat.ConversationUpdated, chat.MembershipChanged} {
		if !kinds[want] {
			t.Fatalf("watch page missing kind %v: %+v", want, kinds)
		}
	}
}

// TestTodo_CHAT_015_SearchUsesFullTextIndex proves search matches through the
// tsvector expression the chat_post_search index is built on, and that the
// planner picks that index rather than a sequential scan.
func TestTodo_CHAT_015_SearchUsesFullTextIndex(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "search", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "Benefits planning", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	principal := chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}
	want, err := s.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "hit"}, chat.Post{AuthorID: "alice", Body: "quarterly headcount plan"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "miss"}, chat.Post{AuthorID: "alice", Body: "lunch order"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Search(ctx, chat.SearchRequest{Principal: principal, TenantID: c.TenantID, Query: "headcount"})
	if err != nil || len(got.Results) != 1 || got.Results[0].Post.ID != want.ID {
		t.Fatalf("search=%+v %v", got, err)
	}
	// A substring that is not a lexeme no longer matches; the index expression
	// defines the contract now.
	if got, err = s.Search(ctx, chat.SearchRequest{Principal: principal, TenantID: c.TenantID, Query: "headcoun"}); err != nil || len(got.Results) != 0 {
		t.Fatalf("partial lexeme=%+v %v", got, err)
	}
	if got, err = s.Search(ctx, chat.SearchRequest{Principal: principal, TenantID: c.TenantID, Query: "plan quarterly"}); err != nil || len(got.Results) != 1 {
		t.Fatalf("multi term=%+v %v", got, err)
	}
	if got, err = s.Search(ctx, chat.SearchRequest{Principal: principal, TenantID: c.TenantID, Query: "bene"}); err != nil || len(got.Channels) != 1 || got.Channels[0].Name != c.Name || !got.Channels[0].Joined {
		t.Fatalf("partial channel prefix=%+v %v", got, err)
	}
	olderAt := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	newerAt := olderAt.Add(time.Hour)
	older, err := s.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "older"}, chat.Post{AuthorID: "alice", Body: "chronology needle", CreatedAt: olderAt})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "newer"}, chat.Post{AuthorID: "alice", Body: "chronology needle", CreatedAt: newerAt})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Search(ctx, chat.SearchRequest{Principal: principal, TenantID: c.TenantID, Query: "chronology", Page: chat.Page{PageSize: 1}})
	if err != nil || len(first.Results) != 1 || first.Results[0].Post.ID != newer.ID || first.NextCursor == "" {
		t.Fatalf("newest search page=%+v %v", first, err)
	}
	second, err := s.Search(ctx, chat.SearchRequest{Principal: principal, TenantID: c.TenantID, Query: "chronology", Page: chat.Page{PageSize: 1, Cursor: first.NextCursor}})
	if err != nil || len(second.Results) != 1 || second.Results[0].Post.ID != older.ID || second.NextCursor != "" {
		t.Fatalf("older search page=%+v %v", second, err)
	}
	var plan strings.Builder
	if err = s.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		if _, e := tx.Exec(ctx, `SET LOCAL enable_seqscan=off`); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `EXPLAIN SELECT id FROM chat_post WHERE to_tsvector('simple', body) @@ plainto_tsquery('simple', $1)`, "headcount")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var line string
			if e = rows.Scan(&line); e != nil {
				return e
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "chat_post_search") {
		t.Fatalf("search predicate does not reach the GIN index: %s", plan.String())
	}
}

// TestTodo_CHAT_025_WatchSurfacesPollFailure proves a failed poll is reported
// rather than being indistinguishable from end of stream. Watch can only close
// its channel, so the reason now arrives on the WatchWithErrors error channel.
func TestTodo_CHAT_025_WatchSurfacesPollFailure(t *testing.T) {
	s := adapterDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := chat.Conversation{ID: "watch-errors", TenantID: "tenant-a", Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	events, fail, err := s.WatchWithErrors(ctx, chat.WatchConversationRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.execTenant(ctx, c.TenantID, `DROP TABLE chat_outbox CASCADE`); err != nil {
		t.Fatal(err)
	}
	select {
	case pollErr, open := <-fail:
		if !open || pollErr == nil {
			t.Fatalf("poll failure was swallowed: err=%v open=%t", pollErr, open)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not report the poll failure")
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("watch did not close after the failure")
		}
	}
}
