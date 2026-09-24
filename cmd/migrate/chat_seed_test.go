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
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

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
	if err := runChatSeedCommand(ctx, store, opts, &out); err != nil {
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
	if err := runChatSeedCommand(ctx, store, opts, &bytes.Buffer{}); err == nil {
		t.Fatal("a second seed without -reset succeeded")
	}
	opts.Reset = true
	if err := runChatSeedCommand(ctx, store, opts, &bytes.Buffer{}); err != nil {
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
