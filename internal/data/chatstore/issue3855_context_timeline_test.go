package chatstore

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// issue3855Room fills the fixture room with a channel message, a thread root,
// two replies in that thread, and a last channel message, all written by
// "author". It returns the posts by label.
func issue3855Room(t *testing.T) (*ModeratedAdapter, chat.Principal, map[string]chat.Post) {
	t.Helper()
	s, p, first := chatremoveDB(t)
	ctx := context.Background()
	posts := map[string]chat.Post{"first": first}
	send := func(label, parent string) {
		post, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: p.TenantID, ConversationID: "room", IdempotencyKey: label},
			chat.Post{AuthorID: "author", AuthorHomeTenantID: p.TenantID, Body: label, ParentID: parent})
		if err != nil {
			t.Fatal(err)
		}
		posts[label] = post
	}
	send("root", "")
	send("reply-one", posts["root"].ID)
	send("reply-two", posts["root"].ID)
	send("moderated", "")
	return s, p, posts
}

// issue3855Context removes and appeals one post, then returns the labels of the
// messages the moderation queue shows around it.
func issue3855Context(t *testing.T, s *ModeratedAdapter, p chat.Principal, posts map[string]chat.Post, label string) []string {
	t.Helper()
	ctx := context.Background()
	chatremoveApply(t, s, p, posts[label], "remove")
	author := chat.Principal{TenantID: p.TenantID, SubjectID: "author"}
	if err := s.AppealRemoval(ctx, author, p.TenantID, "room", posts[label].ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	items, err := s.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for l, post := range posts {
		byID[post.ID] = l
	}
	for _, item := range items {
		if item.Kind != "appeal" {
			continue
		}
		var labels []string
		for _, post := range item.Context {
			labels = append(labels, byID[post.ID])
		}
		sort.Strings(labels)
		return labels
	}
	t.Fatalf("no appeal item in %+v", items)
	return nil
}

// TestIssue3855_ChannelMessageContextSkipsThreadReplies: the messages shown
// around a channel message are channel messages; replies inside another post's
// thread are not mixed into that timeline.
func TestIssue3855_ChannelMessageContextSkipsThreadReplies_Integration(t *testing.T) {
	s, p, posts := issue3855Room(t)
	got := strings.Join(issue3855Context(t, s, p, posts, "moderated"), ",")
	if want := "first,moderated,root"; got != want {
		t.Fatalf("context around a channel message = %q, want %q", got, want)
	}
}

// TestIssue3855_ThreadReplyContextStaysInThread: around a reply, the context is
// the thread: its parent and the other replies, not the channel's messages.
func TestIssue3855_ThreadReplyContextStaysInThread_Integration(t *testing.T) {
	s, p, posts := issue3855Room(t)
	got := strings.Join(issue3855Context(t, s, p, posts, "reply-two"), ",")
	if want := "reply-one,reply-two,root"; got != want {
		t.Fatalf("context around a thread reply = %q, want %q", got, want)
	}
}
