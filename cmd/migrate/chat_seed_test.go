package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatroutingadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatroutestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestChatSeedCommand_RegistersRoutesForLiveReactionsAndReadState pins the
// CHAT-04 root cause: a seeded room only exists on the bare chat store
// adapter this seeder writes with, but the live server's ConversationService
// is chatroutingadapter-wrapped, and its AddReaction/UpdateReadState (and
// every other lease-guarded write) refuse any conversation the core route
// directory has never placed. Before runChatSeedCommand registered a route
// for each room, that refusal surfaced as a raw chatrouting.ErrNotFound the
// transport layer has no mapping for -- logged as an unclassified
// INTERNAL_FAILURE for every reaction or read-state update against a seeded
// room, which is exactly what the live audit's server.log recorded.
func TestChatSeedCommand_RegistersRoutesForLiveReactionsAndReadState(t *testing.T) {
	store, db := chatSeedStore(t)
	ctx := context.Background()
	tenant := "harborcare-demo"

	routeConn := pgtest.NewEmpty(t).NewConn(t)
	routes, err := chatroutestore.New(routeConn)
	if err != nil {
		t.Fatal(err)
	}
	if err = routes.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	opts := chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), AssetDir: "../../internal/humanwork/workspace/assets", Now: time.Now().UTC()}
	if err := runChatSeedCommand(ctx, store, routes, opts, &bytes.Buffer{}); err != nil {
		t.Fatalf("chat seed: %v", err)
	}

	var conversationID string
	if err := db.SQL.QueryRowContext(ctx, `SELECT id FROM chat_conversation WHERE kind='GROUP' LIMIT 1`).Scan(&conversationID); err != nil {
		t.Fatalf("find a seeded group room: %v", err)
	}
	if _, err := routes.Lookup(ctx, conversationID, tenant); err != nil {
		t.Fatalf("seeded room was not registered in the route directory: %v", err)
	}
	var memberID, homeTenantID string
	if err := db.SQL.QueryRowContext(ctx, `SELECT member_id, home_tenant_id FROM chat_membership WHERE conversation_id=$1 LIMIT 1`, conversationID).Scan(&memberID, &homeTenantID); err != nil {
		t.Fatalf("find a seeded member: %v", err)
	}
	var postID string
	if err := db.SQL.QueryRowContext(ctx, `SELECT id FROM chat_post WHERE conversation_id=$1 ORDER BY sequence LIMIT 1`, conversationID).Scan(&postID); err != nil {
		t.Fatalf("find a seeded post: %v", err)
	}

	adapter := chatstore.NewAdapter(store)
	service := chatcore.NewService(adapter, func() time.Time { return time.Now().UTC() })
	service.SetAuthority(seedTestAuthority{store: adapter})
	routed, err := chatroutingadapter.New(service, chatroutingadapter.Options{
		Directory: routes, DefaultShard: chatSeedRouteShard,
		PlacementPolicy: chatSeedRoutePlacementPolicy, PlacementPolicyVersion: chatSeedRoutePolicyVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	principal := chatcore.Principal{TenantID: homeTenantID, SubjectID: memberID}

	if _, err := routed.AddReaction(ctx, chatcore.AddReactionRequest{
		Principal: principal,
		Reaction:  chatcore.Reaction{TenantID: tenant, ConversationID: conversationID, PostID: postID, Emoji: "tada"},
	}); err != nil {
		t.Fatalf("AddReaction on a seeded, routed room = %v, want success", err)
	}
	if _, err := routed.UpdateReadState(ctx, chatcore.UpdateReadStateRequest{
		Principal:        principal,
		ReadState:        chatcore.ReadState{TenantID: tenant, ConversationID: conversationID},
		ExpectedRevision: 1,
	}); err != nil {
		t.Fatalf("UpdateReadState on a seeded, routed room = %v, want success", err)
	}
}

// seedTestAuthority is the same membership-derived chatpolicy.Input the
// CHAT-030 forwarding authority in internal/data/chatstore builds, copied
// here (unexported there) so this package's routing test can authorize a
// real chatcore.Service without a fake store.
type seedTestAuthority struct{ store *chatstore.Adapter }

func (a seedTestAuthority) Authorize(ctx context.Context, p chatcore.Principal, c chatcore.Conversation, _ chatpolicy.Action, at time.Time) (chatpolicy.Input, error) {
	in := chatpolicy.Input{
		Principal: chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:   chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Private: c.Kind != chatcore.PublicChannel, Enabled: true, Revision: c.Revision},
		Now:       at,
	}
	m, err := a.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	if err == nil && m.LeftAt == nil {
		in.HasMembership = true
		in.Membership = chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: m.Revision, JoinedAt: at.Add(-time.Hour)}
	}
	return in, nil
}

func chatSeedStore(t *testing.T) (*chatstore.Store, *pgtest.DB) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := runChatMigrateCommand(ctx, "up", db.SQL, &bytes.Buffer{}); err != nil {
		t.Fatalf("chat migrations: %v", err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	store, err := chatstore.New(ctx, chatstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatalf("open chat store: %v", err)
	}
	t.Cleanup(store.Close)
	return store, db
}

// TestChatSeedCommand_Integration seeds the small profile against a real chat
// database and asserts what a demo is actually judged on: rooms of every kind,
// two weeks of history in order, threads with real depth, reactions, pins, read
// state, and media that is referenced rather than embedded.
func TestChatSeedCommand_Integration(t *testing.T) {
	store, db := chatSeedStore(t)
	ctx := context.Background()
	tenant := "harborcare-demo"
	now := time.Now().UTC()
	var out bytes.Buffer
	opts := chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), AssetDir: "../../internal/humanwork/workspace/assets", Now: now}
	if err := runChatSeedCommand(ctx, store, nil, opts, &out); err != nil {
		t.Fatalf("chat seed: %v", err)
	}
	if !strings.Contains(out.String(), "seeded chat demo for "+tenant) {
		t.Fatalf("receipt = %q", out.String())
	}

	var conversations, posts, replies, reactions, pins, cursors int
	row := func(query string, into *int) {
		if err := db.SQL.QueryRowContext(ctx, query).Scan(into); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	row(`SELECT count(*) FROM chat_conversation`, &conversations)
	row(`SELECT count(*) FROM chat_post`, &posts)
	row(`SELECT count(*) FROM chat_post WHERE parent_id <> ''`, &replies)
	row(`SELECT count(*) FROM chat_reaction`, &reactions)
	row(`SELECT count(*) FROM chat_pin`, &pins)
	row(`SELECT count(*) FROM chat_cursor`, &cursors)
	if conversations != 6 {
		t.Fatalf("conversations = %d, want the six small-profile rooms", conversations)
	}
	if posts < 60 || replies < 8 {
		t.Fatalf("posts = %d with %d replies, want history with threads", posts, replies)
	}
	if reactions < 10 || pins < 4 {
		t.Fatalf("reactions = %d pins = %d", reactions, pins)
	}
	if cursors == 0 {
		t.Fatal("no read state was seeded for the admin persona")
	}

	// Every kind of room is present, which is what makes the sidebar look real.
	kinds := map[string]int{}
	rows, err := db.SQL.QueryContext(ctx, `SELECT kind, count(*) FROM chat_conversation GROUP BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			t.Fatal(err)
		}
		kinds[kind] = n
	}
	for _, want := range []string{"PUBLIC_CHANNEL", "PRIVATE_CHANNEL", "DIRECT", "GROUP"} {
		if kinds[want] == 0 {
			t.Fatalf("no %s room was seeded: %v", want, kinds)
		}
	}

	// Two weeks of history, ending at the run's clock rather than at now().
	var earliest, latest time.Time
	if err := db.SQL.QueryRowContext(ctx, `SELECT min(created_at), max(created_at) FROM chat_post`).Scan(&earliest, &latest); err != nil {
		t.Fatal(err)
	}
	span := latest.Sub(earliest)
	if span < 11*24*time.Hour {
		t.Fatalf("history spans %v, want about two weeks", span)
	}
	if latest.After(now.Add(time.Minute)) {
		t.Fatalf("history ends in the future: %v", latest)
	}

	// Thread depth: at least one root with several replies.
	var deepest int
	if err := db.SQL.QueryRowContext(ctx, `SELECT COALESCE(max(n),0) FROM (SELECT count(*) AS n FROM chat_post WHERE parent_id <> '' GROUP BY parent_id) t`).Scan(&deepest); err != nil {
		t.Fatal(err)
	}
	if deepest < 2 {
		t.Fatalf("deepest thread has %d replies, want at least two", deepest)
	}

	// Media references resolve: every attachment names an artifact with a
	// renderable description, in its own conversation, and no post carries bytes.
	attachments := 0
	refRows, err := db.SQL.QueryContext(ctx, `SELECT conversation_id, references_json FROM chat_post WHERE references_json::text LIKE '%MEDIA%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer refRows.Close()
	type attachment struct {
		conversation string
		refs         []chatcore.Reference
	}
	var found []attachment
	for refRows.Next() {
		var conversation string
		var raw []byte
		if err := refRows.Scan(&conversation, &raw); err != nil {
			t.Fatal(err)
		}
		var refs []chatcore.Reference
		if err := json.Unmarshal(raw, &refs); err != nil {
			t.Fatal(err)
		}
		found = append(found, attachment{conversation: conversation, refs: refs})
	}
	for _, a := range found {
		for _, ref := range a.refs {
			if ref.Kind != chatcore.MediaAttachment {
				continue
			}
			attachments++
			if ref.ID == "" || ref.ContentType == "" || ref.ByteSize == 0 {
				t.Fatalf("media reference is not renderable: %+v", ref)
			}
			if ref.ConversationID != a.conversation {
				t.Fatalf("media reference crossed conversations: %+v in %s", ref, a.conversation)
			}
			if ref.ContentType != "image/png" && ref.ContentType != "image/gif" {
				t.Fatalf("unexpected attachment type %q", ref.ContentType)
			}
		}
	}
	if attachments < 6 {
		t.Fatalf("media attachments = %d, want the seeded images and loops", attachments)
	}

	// Re-running refuses, and -reset makes it idempotent rather than doubling.
	if err := runChatSeedCommand(ctx, store, nil, opts, &bytes.Buffer{}); err == nil {
		t.Fatal("a second seed without -reset succeeded")
	}
	opts.Reset = true
	if err := runChatSeedCommand(ctx, store, nil, opts, &bytes.Buffer{}); err != nil {
		t.Fatalf("reseed with reset: %v", err)
	}
	var afterConversations, afterPosts int
	row(`SELECT count(*) FROM chat_conversation`, &afterConversations)
	row(`SELECT count(*) FROM chat_post`, &afterPosts)
	if afterConversations != conversations {
		t.Fatalf("reseed left %d conversations, want %d", afterConversations, conversations)
	}
	if afterPosts != posts {
		t.Fatalf("reseed left %d posts, want %d", afterPosts, posts)
	}
}

func TestSeedLineSaysEachTopicOnceThenVariesChatter(t *testing.T) {
	topics := []string{"alpha", "beta"}
	if seedLine(topics, 0, 0) != "alpha" || seedLine(topics, 0, 1) != "beta" {
		t.Fatal("topic lines are not said first, in order")
	}
	seen := map[string]int{}
	for i := 2; i < 2+len(chatterLines); i++ {
		line := seedLine(topics, 3, i)
		if line == "alpha" || line == "beta" {
			t.Fatalf("topic repeated at index %d", i)
		}
		seen[line]++
	}
	if len(seen) != len(chatterLines) {
		t.Fatalf("one pass of chatter reused lines: %d distinct of %d", len(seen), len(chatterLines))
	}
	if seedLine(topics, 3, 2) == seedLine(topics, 4, 2) {
		t.Fatal("two rooms start their chatter on the same line")
	}
}
