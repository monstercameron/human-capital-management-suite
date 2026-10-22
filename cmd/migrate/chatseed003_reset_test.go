package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

type seededPostRow struct {
	key, body string
	at        time.Time
}

// seededPosts reads every post in a stable order, which is what a second seed
// has to reproduce exactly.
func seededPosts(t *testing.T, db *sql.DB) []seededPostRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT client_key, body, created_at FROM chat_post ORDER BY client_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []seededPostRow
	for rows.Next() {
		var row seededPostRow
		if err := rows.Scan(&row.key, &row.body, &row.at); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func samePosts(a, b []seededPostRow) string {
	if len(a) != len(b) {
		return fmt.Sprintf("%d posts against %d", len(a), len(b))
	}
	for i := range a {
		if a[i].key != b[i].key || a[i].body != b[i].body || !a[i].at.Equal(b[i].at) {
			return fmt.Sprintf("post %s differs: %s against %s", a[i].key, a[i].at, b[i].at)
		}
	}
	return ""
}

// TestTodo_CHATSEED_003_Integration seeds a real chat database, resets it and
// seeds again, which stopped at a foreign key before the purge knew every table.
// It then judges the times a demo is looked at by: office hours on weekdays,
// replies after their parent a few minutes apart, and the same history every
// time the same run is repeated.
func TestTodo_CHATSEED_003_Integration(t *testing.T) {
	store, db := chatSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	office := chatSeedOfficeZone(tenant)
	// The run starts in the small hours of a Sunday, the case that used to put
	// the newest messages at night and on a weekend.
	now := time.Date(2026, time.September, 27, 0, 40, 0, 0, office).UTC()
	opts := chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), AssetDir: "../../internal/humanwork/workspace/assets", Now: now}
	if err := runChatSeedCommand(ctx, store, nil, opts, &bytes.Buffer{}); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first := seededPosts(t, db.SQL)
	if len(first) < 60 {
		t.Fatalf("seeded %d posts, want a fortnight of history", len(first))
	}

	// Without a reset a repeat run changes nothing.
	if err := runChatSeedCommand(ctx, store, nil, opts, &bytes.Buffer{}); err == nil {
		t.Fatal("a repeat seed without -reset succeeded")
	}
	if diff := samePosts(first, seededPosts(t, db.SQL)); diff != "" {
		t.Fatalf("a repeat seed without -reset changed the history: %s", diff)
	}

	// Reset then seed succeeds on the current schema and rebuilds the same
	// history, moderation items included.
	opts.Reset = true
	if err := runChatSeedCommand(ctx, store, nil, opts, &bytes.Buffer{}); err != nil {
		t.Fatalf("reset then seed: %v", err)
	}
	if diff := samePosts(first, seededPosts(t, db.SQL)); diff != "" {
		t.Fatalf("reset then seed did not rebuild the same history: %s", diff)
	}

	for _, post := range first {
		local := post.at.In(office)
		if !isWorkingMoment(post.at, office) {
			t.Errorf("%s is dated %s, outside 08:00 to 18:00 on a weekday", post.key, local.Format("Mon 2 Jan 15:04"))
		}
		if post.at.After(now) {
			t.Errorf("%s is dated %s, after the run", post.key, post.at)
		}
	}

	// Within a conversation the sequence order is the time order.
	var disordered int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_post a JOIN chat_post b ON a.conversation_id=b.conversation_id AND a.sequence<b.sequence AND a.created_at>b.created_at`).Scan(&disordered); err != nil {
		t.Fatal(err)
	}
	if disordered != 0 {
		t.Fatalf("%d pairs of posts are in the wrong time order for their sequence", disordered)
	}

	// A reply is after its parent and after the reply before it, a few minutes
	// later unless the thread carried over to the next morning.
	rows, err := db.SQL.QueryContext(ctx, `SELECT r.client_key, r.created_at, p.created_at, p.id,
			(SELECT max(s.created_at) FROM chat_post s WHERE s.parent_id=p.id AND s.sequence<r.sequence)
		FROM chat_post r JOIN chat_post p ON p.id=r.parent_id AND p.conversation_id=r.conversation_id ORDER BY r.conversation_id, r.sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	replies := 0
	end := latestOfficeMoment(now, office)
	for rows.Next() {
		var key, parentID string
		var at, parentAt time.Time
		var previous sql.NullTime
		if err := rows.Scan(&key, &at, &parentAt, &parentID, &previous); err != nil {
			t.Fatal(err)
		}
		replies++
		before := parentAt
		if previous.Valid {
			before = previous.Time
		}
		gap := at.Sub(before)
		sameDay := at.In(office).YearDay() == before.In(office).YearDay()
		switch {
		case !at.After(before) && !at.Equal(end):
			t.Errorf("reply %s (%s) is not after the message before it (%s)", key, at.In(office).Format("Mon 15:04"), before.In(office).Format("Mon 15:04"))
		case sameDay && !at.Equal(end) && (gap < 2*time.Minute || gap > 9*time.Minute):
			t.Errorf("reply %s follows the message before it by %s, want two to nine minutes", key, gap)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if replies < 8 {
		t.Fatalf("checked %d replies, want threads in the history", replies)
	}
}
