package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATMOD_004_Quick: one decision on one message is the ordinary count
// and apply commands in a row, the apply carrying the confirmation the count
// returned; a count other than one, or a refused count, stops before anything
// is applied.
func TestTodo_CHATMOD_004_Quick(t *testing.T) {
	var paths []string
	var applied chat.RemovalRequest
	count := 1
	client := &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Errorf("no credential on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/chat/moderation/preview":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"Count":` + string(rune('0'+count)) + `,"Confirmation":"digest"}`))}, nil
		case "/api/chat/moderation/apply":
			var body chatremoveClientInput
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			applied = body.Removal
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"Count":1}`))}, nil
		}
		t.Errorf("unexpected %s", r.URL.Path)
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	cfg := journeyclient.Config{TunnelURL: "https://fixture.invalid", Bearer: "fixture"}
	input, err := chatremoveFormInput("quick", map[string]string{"conversation": "room", "post": "post-1", "reason": "harassment", "note": "targeted", "removal_action": "remove"}, []string{"post-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if input.Removal.Action != "remove" || len(input.Removal.Selection.PostIDs) != 1 || input.Removal.ReasonCode != "harassment" {
		t.Fatalf("quick input=%+v", input.Removal)
	}
	if _, err = chatmod004QuickWith(context.Background(), client, cfg, input); err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "POST /api/chat/moderation/preview,POST /api/chat/moderation/apply" {
		t.Fatalf("calls=%v", paths)
	}
	if applied.Confirmation != "digest" || applied.ConfirmedCount != 1 || applied.Selection.PostIDs[0] != "post-1" || applied.Note != "targeted" {
		t.Fatalf("apply=%+v", applied)
	}
	// A restore needs no reason.
	restore, err := chatremoveFormInput("quick", map[string]string{"conversation": "room", "post": "post-1", "removal_action": "restore"}, []string{"post-1"}, nil)
	if err != nil || restore.Removal.Action != "restore" || restore.Removal.ReasonCode != "" {
		t.Fatalf("restore=%+v err=%v", restore.Removal, err)
	}
	// More than one message, or none, is not a quick decision.
	paths, count = nil, 2
	if _, err = chatmod004QuickWith(context.Background(), client, cfg, input); !errors.Is(err, chat.ErrConflict) || len(paths) != 1 {
		t.Fatalf("a count of two: err=%v calls=%v", err, paths)
	}
	// A refused count never reaches apply.
	denied := &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})}
	paths = nil
	if _, err = chatmod004QuickWith(context.Background(), denied, cfg, input); !errors.Is(err, chat.ErrPermissionDenied) || len(paths) != 1 {
		t.Fatalf("a refused count: err=%v calls=%v", err, paths)
	}
}

func TestTodo_CHATMOD_005_Summary(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "https://fixture.invalid", Bearer: "fixture"}
	client := &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/moderation/summary" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"Open":4,"Moderator":true,"Removable":["room"],"Reviewable":[],"Notices":[{"ID":"outcome:report:1","PostID":"p1","Reason":"spam","Outcome":"remove"},{"ID":"remove:p1:1","ConversationID":"room","PostID":"p1","Reason":"spam","Outcome":"remove","CanAppeal":true},{"ID":"message:x","PostID":"p2","Reason":"hello","Outcome":"message_author"}],"NoticeCount":3}`))}, nil
	})}
	summary, err := chatmod005SummaryWith(context.Background(), client, cfg)
	if err != nil || summary.Open != 4 || !summary.Moderator || summary.NoticeCount != 3 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	state := chatui.ModerationStateFromSummary(summary)
	if !state.Ready || !state.CanRemoveIn("room") || state.CanRemoveIn("other") || state.CanRestoreIn("room") || state.Attention() != 4 || !state.Visible() {
		t.Fatalf("state=%+v", state)
	}
	// The notice that tells the author about the removal is the one that offers
	// the review, not the outcome sent to a person who reported their own message.
	if n := state.Notices["p1"]; n.PostID != "p1" || !n.CanAppeal || n.Reason != "spam" {
		t.Fatalf("notices=%+v", state.Notices)
	}
	if _, ok := state.Notices["p2"]; ok {
		t.Fatal("a moderator's message is a removal notice")
	}
	bad := &http.Client{Transport: chatremoveHTTPTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`not json`))}, nil
	})}
	if _, err = chatmod005SummaryWith(context.Background(), bad, cfg); err == nil {
		t.Fatal("a broken summary was accepted")
	}
}

func TestTodo_CHATMOD_004_Projection(t *testing.T) {
	removed := &chatv1.Post{Id: "p1", Deleted: true, Body: chat.RemovedByAdministrator, Revision: 4}
	authorDeleted := &chatv1.Post{Id: "p2", Deleted: true, Body: ""}
	live := &chatv1.Post{Id: "p3", Body: "text"}
	if !chatmod004Removed(removed) || chatmod004Gone(removed) {
		t.Fatal("an administrator's removal leaves the timeline")
	}
	if chatmod004Removed(authorDeleted) || !chatmod004Gone(authorDeleted) {
		t.Fatal("an author's own delete stays in the timeline")
	}
	if chatmod004Removed(live) || chatmod004Gone(live) || chatmod004Removed(nil) || chatmod004Gone(nil) {
		t.Fatal("a live post is neither")
	}
	// The list read keeps the removed post and the reply under it, drops the
	// author's own delete.
	posts := []*chatv1.Post{{Id: "p1", Sequence: 1, Deleted: true, Body: chat.RemovedByAdministrator, AuthorId: "a"}, {Id: "p2", Sequence: 2, Deleted: true, AuthorId: "a"}, {Id: "p4", Sequence: 3, ParentId: "p1", Body: "a reply", AuthorId: "b"}}
	messages := chatMessages(posts, "en-US", nil, nil, time.Now())
	if len(messages) != 1 || messages[0].ID != "p1" || messages[0].Body != chat.RemovedByAdministrator || messages[0].Replies != 1 {
		t.Fatalf("messages=%+v", messages)
	}
	thread := chatThreadMessages(posts, "p1", "en-US", nil, time.Now())
	if len(thread) != 1 || thread[0].ID != "p4" {
		t.Fatalf("thread=%+v", thread)
	}
	// A live removal event hides the text, files, reactions and pin in place and
	// keeps the thread.
	model := chatui.Model{
		Messages:       []chatui.Message{{ID: "p1", Body: "text", Replies: 2, Reactions: 3, Reacted: true, Pinned: true, Chips: []chatui.ReactionChip{{Emoji: "x"}}, Attachments: []chatui.Attachment{{Name: "a.pdf"}}, Revision: 1}},
		ThreadMessages: []chatui.Message{{ID: "p1", Body: "text"}},
		ThreadParent:   &chatui.Message{ID: "p1", Body: "text"},
	}
	applyChatPostDeleted(&model, removed)
	got := model.Messages[0]
	if got.Body != chat.RemovedByAdministrator || got.Replies != 2 || got.Reactions != 0 || got.Reacted || got.Pinned || len(got.Chips) != 0 || len(got.Attachments) != 0 || got.Revision != 4 {
		t.Fatalf("message=%+v", got)
	}
	if model.ThreadMessages[0].Body != chat.RemovedByAdministrator || model.ThreadParent == nil || model.ThreadParent.Body != chat.RemovedByAdministrator {
		t.Fatalf("thread copies=%+v %+v", model.ThreadMessages, model.ThreadParent)
	}
	// An author's own delete still removes the message.
	applyChatPostDeleted(&model, &chatv1.Post{Id: "p1", Deleted: true})
	if len(model.Messages) != 0 {
		t.Fatalf("an author's delete kept the message: %+v", model.Messages)
	}
	// Restoring is an edit: the original text and its files come back.
	model.Messages = []chatui.Message{{ID: "p1", Body: chat.RemovedByAdministrator, Revision: 4}}
	applyChatPostEdited(&model, &chatv1.Post{Id: "p1", Body: "text", Revision: 5}, "en-US", nil, time.Now())
	if model.Messages[0].Body != "text" || model.Messages[0].Revision != 5 {
		t.Fatalf("restored=%+v", model.Messages[0])
	}
}

func TestTodo_CHATMOD_005_ErrorCopy(t *testing.T) {
	for err, key := range map[error]string{chat.ErrConflict: "conflict", chat.ErrPermissionDenied: "forbidden", chat.ErrInvalidArgument: "failed", chat.ErrUnavailable: "error", errChatmod005NoteRequired: "note_required"} {
		if got := chatremoveErrorKey(err); got != key {
			t.Errorf("%v: %q, want %q", err, got, key)
		}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if chatui.ModerationText(locale, key) == "" {
				t.Errorf("%s: %q has no words", locale, key)
			}
		}
	}
}
