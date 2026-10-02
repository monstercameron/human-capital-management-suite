package chatrecipient

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// batchRepo reads a sidebar's counts in one query and records how it was asked.
type batchRepo struct {
	repo
	batches       int
	host, home    string
	subject       string
	conversations []string
	rows          map[string]Counts
	fail          error
}

func (r *batchRepo) ChatscaleSidebarCounts(_ context.Context, host, home, subject string, conversations []string) (map[string]Counts, error) {
	r.batches++
	r.host, r.home, r.subject = host, home, subject
	r.conversations = append([]string(nil), conversations...)
	return r.rows, r.fail
}

// refusing answers "not found" for the conversations it names and stops the
// whole read for the one it is told is broken.
type refusing struct {
	conversations
	missing map[string]bool
	broken  string
	asked   int
}

func (c *refusing) GetConversation(ctx context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	c.asked++
	if r.ConversationID == c.broken {
		return chat.Conversation{}, chat.ErrUnavailable
	}
	if c.missing[r.ConversationID] {
		return chat.Conversation{}, chat.ErrNotFound
	}
	return c.conversations.GetConversation(ctx, r)
}

// TestTodo_CHATBUG_014_SidebarCounts: a sidebar's counts are one read of the
// store, for the conversations the reader is admitted to and no others.
func TestTodo_CHATBUG_014_SidebarCounts(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	db := &batchRepo{rows: map[string]Counts{"general": {Unread: 4, Mentions: 1}, "secret": {Unread: 9}}}
	admission := &refusing{conversations: conversations{allowed: true, denied: map[string]bool{"host/secret": true}}, missing: map[string]bool{"gone": true}}
	s := &Service{Conversations: admission, Repo: db}

	got, err := s.SidebarCounts(ctx, p, "host", []string{"general", "quiet", "secret", "gone", "general"})
	if err != nil {
		t.Fatal(err)
	}
	if db.batches != 1 || db.calls != 0 {
		t.Fatalf("five conversations cost %d batch reads and %d single reads, want one batch read", db.batches, db.calls)
	}
	if db.host != "host" || db.home != "home" || db.subject != "alice" {
		t.Fatalf("the store was asked as %s/%s on %s", db.home, db.subject, db.host)
	}
	if !reflect.DeepEqual(db.conversations, []string{"general", "quiet"}) {
		t.Fatalf("the store was asked for %v: a refused or missing conversation must not reach it, and none twice", db.conversations)
	}
	want := map[string]Counts{"general": {Unread: 4, Mentions: 1}, "quiet": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts=%v want %v (a conversation with nothing unread still has an answer; a refused one has none)", got, want)
	}
	if admission.asked != 4 {
		t.Fatalf("admission was checked %d times for four distinct conversations", admission.asked)
	}

	// Bounds and identity are checked before anything is read.
	tooMany := make([]string, SidebarCountsLimit+1)
	for i := range tooMany {
		tooMany[i] = "c" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	db.batches = 0
	for name, call := range map[string]func() error{
		"too many conversations": func() error { _, err := s.SidebarCounts(ctx, p, "host", tooMany); return err },
		"an empty conversation":  func() error { _, err := s.SidebarCounts(ctx, p, "host", []string{"general", ""}); return err },
		"no host":                func() error { _, err := s.SidebarCounts(ctx, p, "", []string{"general"}); return err },
		"no principal": func() error {
			_, err := s.SidebarCounts(ctx, chat.Principal{}, "host", []string{"general"})
			return err
		},
	} {
		if err := call(); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if db.batches != 0 {
		t.Fatalf("a refused request reached the store %d times", db.batches)
	}
	if _, err := (&Service{}).SidebarCounts(ctx, p, "host", []string{"general"}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("an unwired service: %v", err)
	}

	// Nothing admitted is an empty answer without a read; a failure that is not
	// a refusal fails the read instead of passing for "nothing unread".
	if got, err := s.SidebarCounts(ctx, p, "host", []string{"secret", "gone"}); err != nil || len(got) != 0 || db.batches != 0 {
		t.Fatalf("nothing admitted: counts=%v err=%v reads=%d", got, err, db.batches)
	}
	admission.broken = "general"
	if _, err := s.SidebarCounts(ctx, p, "host", []string{"general"}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a failed admission read was swallowed: %v", err)
	}
	admission.broken = ""
	db.fail = chat.ErrUnavailable
	if _, err := s.SidebarCounts(ctx, p, "host", []string{"general"}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a failed store read was swallowed: %v", err)
	}

	// A repository without the batch read is asked one conversation at a time.
	plain := &repo{counts: Counts{Unread: 2}}
	s = &Service{Conversations: conversations{allowed: true}, Repo: plain}
	got, err = s.SidebarCounts(ctx, p, "host", []string{"general", "quiet"})
	if err != nil || plain.calls != 2 || got["general"].Unread != 2 || got["quiet"].Unread != 2 {
		t.Fatalf("fallback: counts=%v calls=%d err=%v", got, plain.calls, err)
	}
}
