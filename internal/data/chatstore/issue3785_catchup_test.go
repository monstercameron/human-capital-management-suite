package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3785Seed(t *testing.T) (*Store, *RecipientStateStore) {
	t.Helper()
	s, r := issue3733Seed(t)
	// u-1 has read up to the second message of ops.
	if err := s.execTenant(context.Background(), "host", `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence) VALUES('host','host','u-1','c-1',2)`); err != nil {
		t.Fatal(err)
	}
	return s, r
}

func issue3785IDs(rng chatrecipient.CatchUpRange) []string {
	ids := []string{}
	for _, p := range rng.Posts {
		ids = append(ids, p.ID)
	}
	return ids
}

func issue3785Equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestIssue3785_CatchUpRange_ReadsOnlyWhatWasMissed_Integration(t *testing.T) {
	_, r := issue3785Seed(t)
	rng, err := r.CatchUpRange(context.Background(), "host", "host", "u-1", "c-1", "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	// After the read position (2): a deleted post (5), a post held back by its
	// audience (10) and system lines are not part of what u-1 can read.
	want := []string{"mention-of-other", "own", "root-f", "reply-f", "root-n", "reply-n", "ancient"}
	if got := issue3785IDs(rng); !issue3785Equal(got, want) {
		t.Fatalf("posts = %v, want %v", got, want)
	}
	if rng.Cursor != 2 || rng.After != 2 || rng.Total != 7 || rng.Head != 11 || rng.ConversationName != "ops" {
		t.Fatalf("range = cursor %d after %d total %d head %d name %q", rng.Cursor, rng.After, rng.Total, rng.Head, rng.ConversationName)
	}
	for _, p := range rng.Posts {
		if p.ID == "reply-f" && p.ThreadRootID != "root-f" {
			t.Fatalf("a reply names its thread: %+v", p)
		}
		if p.Body == "" || p.AuthorID == "" || p.Sequence <= 2 {
			t.Fatalf("incomplete post %+v", p)
		}
	}
}

func TestIssue3785_CatchUpRange_FlagsMentionsOfTheReader_Integration(t *testing.T) {
	_, r := issue3785Seed(t)
	rng, err := r.CatchUpRange(context.Background(), "host", "host", "u-1", "c-1", "", "mention", 50)
	if err != nil {
		t.Fatal(err)
	}
	flags := map[string]bool{}
	for _, p := range rng.Posts {
		flags[p.ID] = p.Mentioned
	}
	if !flags["mention"] || flags["plain"] || flags["mention-of-other"] {
		t.Fatalf("mentioned flags = %v", flags)
	}
}

func TestIssue3785_CatchUpRange_BoundsToTheNewestAndCountsTheRest_Integration(t *testing.T) {
	_, r := issue3785Seed(t)
	rng, err := r.CatchUpRange(context.Background(), "host", "host", "u-1", "c-1", "", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := issue3785IDs(rng); !issue3785Equal(got, []string{"root-n", "reply-n", "ancient"}) {
		t.Fatalf("posts = %v, want the newest three, oldest first", got)
	}
	if rng.Total != 7 || rng.Head != 11 {
		t.Fatalf("total %d head %d: the cap must not hide how much was missed", rng.Total, rng.Head)
	}
}

func TestIssue3785_CatchUpRange_AnchorAndThread_Integration(t *testing.T) {
	_, r := issue3785Seed(t)
	ctx := context.Background()
	// Starting from a post the reader names: that post and everything after it.
	rng, err := r.CatchUpRange(ctx, "host", "host", "u-1", "c-1", "", "root-f", 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := issue3785IDs(rng); !issue3785Equal(got, []string{"root-f", "reply-f", "root-n", "reply-n", "ancient"}) || rng.After != 5 || rng.Cursor != 2 {
		t.Fatalf("anchored = %v after %d cursor %d", got, rng.After, rng.Cursor)
	}
	// A thread: its root and replies only, from the start when no anchor is given.
	rng, err = r.CatchUpRange(ctx, "host", "host", "u-1", "c-1", "root-f", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := issue3785IDs(rng); !issue3785Equal(got, []string{"root-f", "reply-f"}) || rng.After != 0 || rng.Head != 7 {
		t.Fatalf("thread = %v after %d head %d", got, rng.After, rng.Head)
	}
	// From a reply onward.
	rng, err = r.CatchUpRange(ctx, "host", "host", "u-1", "c-1", "root-f", "reply-f", 50)
	if err != nil || !issue3785Equal(issue3785IDs(rng), []string{"reply-f"}) {
		t.Fatalf("thread from reply = %v err=%v", issue3785IDs(rng), err)
	}
}

func TestIssue3785_CatchUpRange_RefusesWhatTheReaderCannotOpen_Security(t *testing.T) {
	_, r := issue3785Seed(t)
	ctx := context.Background()
	// Not a member, a member who left, and another company.
	for _, c := range []struct{ host, home, who, conv string }{
		{"host", "host", "u-3", "c-1"},
		{"host", "host", "u-1", "c-left"},
		{"host", "host", "u-1", "c-away"},
		{"other-tenant", "host", "u-1", "c-1"},
		{"host", "other-tenant", "u-1", "c-1"},
	} {
		rng, err := r.CatchUpRange(ctx, c.host, c.home, c.who, c.conv, "", "", 50)
		if !errors.Is(err, chat.ErrPermissionDenied) || len(rng.Posts) != 0 || rng.Total != 0 || rng.Head != 0 {
			t.Fatalf("%+v: range=%+v err=%v, want a refusal that reveals nothing", c, rng, err)
		}
	}
	// A post held back by its audience cannot be an anchor, a thread, or any
	// part of a range: its existence is not confirmed.
	for _, c := range []struct{ thread, anchor string }{{"", "held"}, {"held", ""}, {"", "away"}, {"", "no-such-post"}} {
		if _, err := r.CatchUpRange(ctx, "host", "host", "u-1", "c-1", c.thread, c.anchor, 50); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("%+v: err=%v, want not found", c, err)
		}
	}
	rng, err := r.CatchUpRange(ctx, "host", "host", "u-2", "c-1", "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	// u-2 is the audience of the held post, so u-2 may read it.
	found := false
	for _, p := range rng.Posts {
		found = found || p.ID == "held"
	}
	if !found {
		t.Fatalf("the audience of a restricted post reads it: %v", issue3785IDs(rng))
	}
}

func TestIssue3785_CatchUpRange_NeverMovesTheReadPosition_Integration(t *testing.T) {
	s, r := issue3785Seed(t)
	ctx := context.Background()
	read := func() (int64, int64) {
		var seq, rev int64
		if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT last_sequence,revision FROM chat_cursor WHERE tenant_id='host' AND member_id='u-1' AND conversation_id='c-1'`).Scan(&seq, &rev)
		}); err != nil {
			t.Fatal(err)
		}
		return seq, rev
	}
	seq0, rev0 := read()
	for i := 0; i < 2; i++ {
		if _, err := r.CatchUpRange(ctx, "host", "host", "u-1", "c-1", "", "", 50); err != nil {
			t.Fatal(err)
		}
	}
	if seq, rev := read(); seq != seq0 || rev != rev0 {
		t.Fatalf("cursor moved from %d/%d to %d/%d by reading a brief", seq0, rev0, seq, rev)
	}
}
