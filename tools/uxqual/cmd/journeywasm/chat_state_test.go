package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newChatStateForTest is a configured, empty chat state. The tests use their
// own value rather than the package global so they neither depend on nor
// disturb each other.
func newChatStateForTest(t *testing.T) *chatState {
	t.Helper()
	state := &chatState{}
	state.reset(nil, journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}, nil)
	return state
}

func TestTodo_CHAT_032_LateListingKeepsGroups(t *testing.T) {
	state := newChatStateForTest(t)
	sales := chatui.Conversation{ID: "sales", Name: "Sales", Kind: chatui.PublicChannel}
	state.mutate(func(m *chatui.Model) {
		m.SelectedID = "sales"
		m.Conversations = []chatui.Conversation{sales}
		m.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels"}, {ID: "direct", Name: "Direct messages"}, {ID: "custom", Name: "Projects", Collapsed: true, Chats: []chatui.Conversation{sales}}}
		m.RailMenuID = "sales"
	})
	before := state.snapshot()
	loaded := before
	loaded.Sections = nil
	loaded.RailMenuID = ""
	loaded.Conversations = []chatui.Conversation{{ID: "sales", Name: "Sales updated", Kind: chatui.PublicChannel}, {ID: "new", Name: "New", Kind: chatui.PublicChannel}}
	if !state.adoptLoadedChatProjection(state.generation, loaded, chatCursor{}, false) {
		t.Fatal("projection rejected")
	}
	after := state.snapshot()
	if len(after.Sections) != 3 || !after.Sections[2].Collapsed || len(after.Sections[2].Chats) != 1 || after.Sections[2].Chats[0].Name != "Sales updated" || len(after.Sections[0].Chats) != 1 || after.Sections[0].Chats[0].ID != "new" || after.RailMenuID != "sales" {
		t.Fatalf("late listing replaced group state: %+v", after.Sections)
	}
}

func TestChatPhotosFollowAuthorizedWorkerDirectory(t *testing.T) {
	state := newChatStateForTest(t)
	people := chatPhotos([]productui.Person{{ID: "ref", WorkerID: "id", PhotoURL: "/people.jpg"}})
	state.mergePhotos(people, false)
	if got := state.photoSnapshot()["id"]; got != "/people.jpg" {
		t.Fatalf("people fallback = %q", got)
	}
	workers := chatPhotosFromWorkers([]*journeyv1.Worker{{WorkerRef: "ref", WorkerId: "id", SubjectId: "ref", ProfilePhotoUrl: "/worker.jpg"}})
	state.mergePhotos(workers, true)
	state.mergePhotos(people, false)
	if got := state.snapshot().PhotoURLs["ref"]; got != "/worker.jpg" {
		t.Fatalf("worker photo = %q", got)
	}
	state.mergePhotos(chatPhotosFromWorkers([]*journeyv1.Worker{{WorkerRef: "ref", WorkerId: "id", SubjectId: "ref"}}), true)
	state.mergePhotos(people, false)
	if _, ok := state.photoSnapshot()["ref"]; ok {
		t.Fatal("removed worker photo survived")
	}
	// The directory keys photos by chat subject only; the entity id is the
	// people page's own key and the directory does not govern it.
}

func TestChatPhotoArrivingDuringProjectionReadSurvivesAdoption(t *testing.T) {
	state := newChatStateForTest(t)
	stale := state.snapshot()
	state.mergePhotos(map[string]string{"author": "/workspace/assets/author.jpg"}, true)
	if !state.adoptLoadedChatProjection(state.currentGeneration(), stale, chatCursor{}, false) {
		t.Fatal("projection not adopted")
	}
	if got := state.snapshot().PhotoURLs["author"]; got != "/workspace/assets/author.jpg" {
		t.Fatalf("late photo lost: %q", got)
	}
}

func chatTestPost(id string, sequence uint64, author, body, parent string, when time.Time) *chatv1.Post {
	return &chatv1.Post{
		Id: id, ConversationId: "c1", TenantId: "northwind", AuthorId: author, Body: body,
		Sequence: sequence, Revision: 1, ParentId: parent, CreatedAt: timestamppb.New(when),
	}
}

func TestChatProjectionResolvesNamesCountsRepliesAndNeverLabelsTheReaderYou(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	directory := chatDirectory([]productui.Person{
		{ID: "avery", Name: "Avery Okafor"},
		{WorkerID: "blake", Name: "Blake Idris"},
	})
	posts := []*chatv1.Post{
		chatTestPost("p1", 1, "avery", "kickoff", "", now),
		chatTestPost("p2", 2, "blake", "reply one", "p1", now),
		chatTestPost("p3", 3, "blake", "reply two", "p1", now),
		chatTestPost("p4", 4, "casey", "standalone", "", now),
		func() *chatv1.Post {
			post := chatTestPost("p5", 5, "blake", "gone", "", now)
			post.Deleted = true
			return post
		}(),
	}
	messages := chatMessages(posts, "en-US", directory, map[string][]chatui.ReactionChip{"p1": {{Emoji: "👍", Count: 2}, {Emoji: "🎉", Count: 1, Mine: true}}}, now)
	if len(messages) != 2 {
		t.Fatalf("timeline entries = %d, want 2 (replies and deleted posts are not entries): %+v", len(messages), messages)
	}
	first := messages[0]
	if first.ID != "p1" || first.AuthorID != "avery" {
		t.Fatalf("first message identity = %q/%q, want p1/avery", first.ID, first.AuthorID)
	}
	if first.Author != "Avery Okafor" {
		t.Fatalf("author = %q, want the resolved display name; the reader is never labelled \"You\"", first.Author)
	}
	if first.Replies != 2 {
		t.Fatalf("replies = %d, want 2 counted from parent_id", first.Replies)
	}
	if first.Reactions != 3 {
		t.Fatalf("reactions = %d, want the 3 the reaction read supplied", first.Reactions)
	}
	if messages[1].Author != "Casey" {
		t.Fatalf("unresolved author = %q, want the reference read as a name rather than a raw slug", messages[1].Author)
	}
	if messages[1].AuthorID != "casey" {
		t.Fatalf("AuthorID = %q, want the identifier untouched", messages[1].AuthorID)
	}
	if got := chatThreadMessages(posts, "p1", "en-US", directory, now); len(got) != 2 || got[0].Body != "reply one" {
		t.Fatalf("thread messages = %+v, want the two replies under p1", got)
	}
}

func TestChatTimeLabelFollowsLocaleAndDatesOlderMessages(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC).Local()
	today := now
	older := now.AddDate(0, 0, -3)
	if got := chatTimeLabel(today, now, "en-US"); got != today.Format("3:04 PM") {
		t.Fatalf("en-US today = %q, want a twelve-hour clock", got)
	}
	if got := chatTimeLabel(today, now, "de-DE"); got != today.Format("15:04") {
		t.Fatalf("de-DE today = %q, want a twenty-four-hour clock", got)
	}
	if got := chatTimeLabel(older, now, "de-DE"); got != older.Format("15:04") {
		t.Fatalf("older message = %q, want the date as well as the clock", got)
	}
	if got := chatTimeLabel(time.Time{}, now, "en-US"); got != "" {
		t.Fatalf("zero timestamp = %q, want an empty label", got)
	}
}

func TestChatSendKeyBelongsToTheBodySoAnEditedRetryCannotReplayTheFirstPost(t *testing.T) {
	state := newChatStateForTest(t)
	minted := 0
	mint := func() string { minted++; return fmt.Sprintf("key-%d", minted) }

	first := state.sendKey("c1", "hello", mint)
	if again := state.sendKey("c1", "hello", mint); again != first {
		t.Fatalf("identical retry key = %q, want the pending %q reused", again, first)
	}
	// The bug this replaces: the key was cached per conversation and cleared
	// only on success, so a timed-out send retried with corrected text
	// replayed the original body under the original key.
	edited := state.sendKey("c1", "hello there", mint)
	if edited == first {
		t.Fatalf("edited retry reused key %q; a different body must mint a new key", edited)
	}
	if other := state.sendKey("c2", "hello", mint); other == first || other == edited {
		t.Fatalf("second conversation reused key %q", other)
	}
	state.clearSendKey("c1")
	if after := state.sendKey("c1", "hello there", mint); after == edited {
		t.Fatalf("key %q survived the commit that cleared it", after)
	}
	if minted != 4 {
		t.Fatalf("minted %d keys, want 4 (one per distinct attempt)", minted)
	}
}

func TestChatActionNoticeIsTransientAndOnlyItsOwnTokenClearsIt(t *testing.T) {
	state := newChatStateForTest(t)
	failure := state.setNotice(actionFailureNotice("pin this message", status.Error(codes.PermissionDenied, "chat.permission_denied")), false)
	if got := state.currentNotice(); got == "" || got == "permission denied" {
		t.Fatalf("notice = %q, want text naming the action that failed", got)
	}
	success := state.setNotice("Message pinned", false)
	if state.clearNotice(failure) {
		t.Fatal("the superseded failure's timer cleared a newer notice")
	}
	if state.currentNotice() != "Message pinned" {
		t.Fatalf("notice = %q, want the newer one intact", state.currentNotice())
	}
	if !state.clearNotice(success) {
		t.Fatal("the current notice's own token did not clear it")
	}
	if state.currentNotice() != "" || state.snapshot().Notice != "" {
		t.Fatalf("notice survived its clear: %q / %q", state.currentNotice(), state.snapshot().Notice)
	}
	// A conversation switch is the other thing that clears it: a notice
	// belongs to the conversation it was raised in.
	state.setNotice("Reaction added", false)
	if model, _ := state.selectChatConversation("c2"); model.Notice != "" {
		t.Fatalf("notice %q survived the conversation switch", model.Notice)
	}
}

func TestChatLoadErrorClearsOnTheNextSuccessfulLoad(t *testing.T) {
	state := newChatStateForTest(t)
	state.setLoadError("list conversations: unavailable")
	if state.loadError() == "" {
		t.Fatal("load error was not recorded")
	}
	state.setLoadError("")
	if got := state.loadError(); got != "" {
		t.Fatalf("load error = %q after a successful load, want it cleared; the latched actionErr this replaces was re-applied by every load", got)
	}
}

func TestSearchFocusClearsWhenConversationChanges(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "room-a"
		model.FocusMessageID = "search-hit"
	})
	model, _ := state.selectChatConversation("room-b")
	if model.FocusMessageID != "" {
		t.Fatalf("search highlight %q survived navigation to another conversation", model.FocusMessageID)
	}
}

func TestChatGenerationGuardDiscardsAnAnswerForAConversationTheReaderLeft(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1" })
	stale := state.currentGeneration()
	state.selectChatConversation("c2")
	if state.generationActive(stale) {
		t.Fatal("the conversation switch did not claim a new generation")
	}
	applied := state.commit(stale, func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = []chatui.Message{{ID: "p1"}}
	})
	if applied {
		t.Fatal("a stale snapshot committed over the newer selection")
	}
	if model := state.snapshot(); model.SelectedID != "c2" || len(model.Messages) != 0 {
		t.Fatalf("model = %q with %d messages, want c2 and an empty timeline", model.SelectedID, len(model.Messages))
	}
	if !state.commit(state.currentGeneration(), func(model *chatui.Model) { model.State = chatui.StateReady }) {
		t.Fatal("the current generation could not commit")
	}
}

func TestClassifyChatSequenceOrdersAppliesDuplicatesAndGaps(t *testing.T) {
	for _, tc := range []struct {
		name          string
		last, arrived uint64
		baselined     bool
		want          chatEventOutcome
	}{
		{"next", 7, 8, true, chatEventApplied},
		{"first", 0, 1, true, chatEventApplied},
		{"replayed", 7, 7, true, chatEventDuplicate},
		{"older", 7, 3, true, chatEventDuplicate},
		{"skipped", 7, 9, true, chatEventGap},
		{"from nothing", 0, 5, true, chatEventGap},
		{"unsequenced", 7, 0, true, chatEventUnordered},
		// The live defect: the watch position lives in the server's outbox
		// space, so the first event of a room with history arrives thousands
		// above anything this client knows. A stream with no baseline yet has
		// nothing to measure against, so it adopts the position instead of
		// declaring a gap.
		{"first event of a stream", 0, 91437, false, chatEventApplied},
		{"first event below a stale position", 7, 3, false, chatEventApplied},
	} {
		if got := classifyChatSequence(tc.last, tc.arrived, tc.baselined); got != tc.want {
			t.Errorf("%s: last=%d arrived=%d baselined=%v outcome=%d, want %d", tc.name, tc.last, tc.arrived, tc.baselined, got, tc.want)
		}
	}
	cursor := chatCursor{StreamSequence: 3, Baselined: true, Seen: map[string]uint64{"p9": 9}}
	if got := classifyChatEvent(cursor, "p9", 9, 4181, true); got != chatEventDuplicate {
		t.Errorf("a post already applied is a duplicate whatever watch position it carries; got %d", got)
	}
}

func TestApplyChatEventBuildsTheTimelineDedupesAndReportsAGap(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	model := chatui.Model{State: chatui.StateEmpty}
	cursor := chatCursor{ConversationID: "c1", Seen: map[string]uint64{}}
	created := func(post *chatv1.Post, sequence uint64) *chatv1.ConversationEvent {
		return &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: sequence, Post: post}
	}

	if got := applyChatEvent(&model, &cursor, created(chatTestPost("p1", 1, "blake", "first", "", now), 1), "en-US", nil, now); got != chatEventApplied {
		t.Fatalf("first event outcome = %d, want applied", got)
	}
	if len(model.Messages) != 1 || model.State != chatui.StateReady {
		t.Fatalf("model = %d messages / state %q, want one message and ready", len(model.Messages), model.State)
	}
	// At-least-once delivery: the same event again changes nothing.
	if got := applyChatEvent(&model, &cursor, created(chatTestPost("p1", 1, "blake", "first", "", now), 1), "en-US", nil, now); got != chatEventDuplicate {
		t.Fatalf("replayed event outcome = %d, want duplicate", got)
	}
	if len(model.Messages) != 1 {
		t.Fatalf("a duplicate was appended: %d messages", len(model.Messages))
	}
	// A reply is a thread entry and a count on its parent, not a timeline row.
	if got := applyChatEvent(&model, &cursor, created(chatTestPost("p2", 2, "avery", "reply", "p1", now), 2), "en-US", nil, now); got != chatEventApplied {
		t.Fatalf("reply outcome = %d, want applied", got)
	}
	if len(model.Messages) != 1 || model.Messages[0].Replies != 1 {
		t.Fatalf("reply handling: %d messages, parent replies = %d", len(model.Messages), model.Messages[0].Replies)
	}
	// A skipped sequence is never rendered: it is a catch-up.
	if got := applyChatEvent(&model, &cursor, created(chatTestPost("p9", 9, "avery", "missed", "", now), 9), "en-US", nil, now); got != chatEventGap {
		t.Fatalf("gap outcome = %d, want gap", got)
	}
	if len(model.Messages) != 1 || cursor.StreamSequence != 2 {
		t.Fatalf("the gap event mutated state: %d messages, watch position %d", len(model.Messages), cursor.StreamSequence)
	}

	edited := chatTestPost("p1", 1, "blake", "corrected", "", now)
	edited.Revision = 2
	if got := applyChatEvent(&model, &cursor, &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_EDITED, Sequence: 3, Post: edited}, "en-US", nil, now); got != chatEventApplied {
		t.Fatalf("edit outcome = %d, want applied", got)
	}
	if model.Messages[0].Body != "corrected" || !model.Messages[0].Edited || model.Messages[0].Replies != 1 {
		t.Fatalf("edit lost body, edited flag or reply count: %+v", model.Messages[0])
	}
	if got := applyChatEvent(&model, &cursor, &chatv1.ConversationEvent{
		Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED, Sequence: 4,
		Reaction: &chatv1.Reaction{PostId: "p1", Emoji: "👍"},
	}, "en-US", nil, now); got != chatEventApplied || model.Messages[0].Reactions != 1 {
		t.Fatalf("reaction event: outcome %d, reactions %d", got, model.Messages[0].Reactions)
	}
	if got := applyChatEvent(&model, &cursor, &chatv1.ConversationEvent{
		Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_PIN_CHANGED, Sequence: 5,
		Pin: &chatv1.Pin{PostId: "p1"},
	}, "en-US", nil, now); got != chatEventApplied || !model.Messages[0].Pinned {
		t.Fatalf("pin event: outcome %d, pinned %v", got, model.Messages[0].Pinned)
	}
	deleted := chatTestPost("p1", 1, "blake", "corrected", "", now)
	if got := applyChatEvent(&model, &cursor, &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_DELETED, Sequence: 6, Post: deleted}, "en-US", nil, now); got != chatEventApplied {
		t.Fatalf("delete outcome = %d, want applied", got)
	}
	if len(model.Messages) != 0 || model.State != chatui.StateEmpty {
		t.Fatalf("delete left %d messages / state %q", len(model.Messages), model.State)
	}
	if cursor.StreamSequence != 6 {
		t.Fatalf("watch position = %d, want 6", cursor.StreamSequence)
	}
	if !cursor.Baselined {
		t.Fatal("the stream never took a baseline")
	}
}

func TestChatCatchupFoldsForwardHistoryWithoutDuplicatingWhatIsAlreadyApplied(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1"; model.State = chatui.StateEmpty })
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", LastSequence: 1, StreamSequence: 1, Baselined: true, Seen: map[string]uint64{"p1": 1}}
	state.model.Messages = []chatui.Message{{ID: "p1", Body: "first", SentAt: now}}
	state.mu.Unlock()
	generation := state.currentGeneration()

	posts := []*chatv1.Post{
		chatTestPost("p1", 1, "blake", "first", "", now),
		chatTestPost("p2", 2, "avery", "second", "", now.Add(time.Minute)),
		chatTestPost("p3", 3, "avery", "third", "", now.Add(2*time.Minute)),
	}
	if !state.applyCatchup(generation, "c1", posts, "en-US", nil, now) {
		t.Fatal("catch-up applied nothing")
	}
	model := state.snapshot()
	if len(model.Messages) != 3 {
		t.Fatalf("timeline = %d messages, want 3 with no duplicate of p1: %+v", len(model.Messages), model.Messages)
	}
	if got := state.streamCursor(); got.LastSequence != 3 {
		t.Fatalf("cursor = %d, want 3 so the resubscribe starts after the catch-up", got.LastSequence)
	}
	if state.applyCatchup(generation, "c1", posts, "en-US", nil, now) {
		t.Fatal("re-applying the same page applied again")
	}
	if state.applyCatchup(generation+1, "c1", posts, "en-US", nil, now) {
		t.Fatal("a catch-up for a superseded generation was applied")
	}
}

func TestChatDraftSwapKeepsEachConversationsOwnText(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1" })
	state.setDraft("c1", "half a sentence")
	if got := state.snapshot().Draft; got != "half a sentence" {
		t.Fatalf("composer = %q, want the typed text", got)
	}
	model, _ := state.selectChatConversation("c2")
	if model.Draft != "" {
		t.Fatalf("composer = %q on switching to a conversation with no draft, want empty", model.Draft)
	}
	state.setDraft("c2", "other thread")
	back, _ := state.selectChatConversation("c1")
	if back.Draft != "half a sentence" {
		t.Fatalf("composer = %q on returning, want the kept draft", back.Draft)
	}
	if got := state.draft("c2"); got != "other thread" {
		t.Fatalf("stored draft for c2 = %q", got)
	}
	if drafts := state.allDrafts(); len(drafts) != 2 {
		t.Fatalf("stored drafts = %+v, want both conversations", drafts)
	}
	// A persisted copy never overwrites what this session has typed: it is
	// always at least as old, because it is a debounced write of it.
	state.loadDrafts(map[string]string{"c1": "stale server copy", "c3": "  ", "c4": "kept from last time"})
	if got := state.snapshot().Draft; got != "half a sentence" {
		t.Fatalf("composer after a reload = %q, want the text still being typed", got)
	}
	if got := state.draft("c4"); got != "kept from last time" {
		t.Fatalf("an untouched conversation did not adopt its persisted draft: %q", got)
	}
	if got := state.draft("c3"); got != "" {
		t.Fatalf("whitespace-only draft %q was restored", got)
	}
	state.setDraft("c1", "")
	if got := state.draft("c1"); got != "" {
		t.Fatalf("a cleared draft is still stored: %q", got)
	}
}

func TestTodo_CHAT_035_ConflictRebaseKeepsLocalTextAndServerClear(t *testing.T) {
	state := newChatStateForTest(t)
	state.loadDrafts(map[string]string{"local": "old", "cleared-elsewhere": "sent elsewhere"})
	state.mutate(func(model *chatui.Model) { model.SelectedID = "local" })
	state.setDraft("local", "current typing")
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery"}
	_, _, epoch := state.draftsForWrite()
	drafts, revision, current := state.rebaseDrafts(map[string]string{"local": "other tab's text", "new": "other tab's draft"}, cfg, epoch)
	if !current {
		t.Fatal("current principal was rejected")
	}
	if drafts["local"] != "current typing" || drafts["new"] != "other tab's draft" || drafts["cleared-elsewhere"] != "" || state.snapshot().Draft != "current typing" {
		t.Fatalf("conflict rebase lost or restored a draft: %+v", drafts)
	}
	state.markDraftsPersisted(revision, cfg, epoch)
	if state.draftRevision("local") != 0 {
		t.Fatal("persisted local revision still masks future server updates")
	}
	state.setDraft("local", "")
	drafts, _, current = state.rebaseDrafts(map[string]string{"local": "stale server text"}, cfg, epoch)
	if !current {
		t.Fatal("current principal was rejected after send")
	}
	if drafts["local"] != "" || state.snapshot().Draft != "" {
		t.Fatalf("send tombstone was overwritten: %+v", drafts)
	}
	foreign := journeyclient.Config{Tenant: "northwind", Subject: "someone-else"}
	if _, _, current := state.rebaseDrafts(map[string]string{"local": "foreign draft"}, foreign, epoch); current || state.draft("local") != "" {
		t.Fatal("a stale principal applied a draft after identity changed")
	}
	state.reset(nil, cfg, nil)
	state.setDraft("local", "new session draft")
	if _, _, current := state.rebaseDrafts(map[string]string{"local": "old session draft"}, cfg, epoch); current || state.draft("local") != "new session draft" {
		t.Fatal("an old write rebased a new session")
	}
	state.markDraftsPersisted(revision, cfg, epoch)
	if state.draftRevision("local") == 0 {
		t.Fatal("an old write acknowledged a new session draft")
	}
}

func TestBoundChatMessagesKeepsTheNewestAndReportsOlderHistory(t *testing.T) {
	messages := make([]chatui.Message, 0, 12)
	for i := 0; i < 12; i++ {
		messages = append(messages, chatui.Message{ID: fmt.Sprintf("p%02d", i)})
	}
	bounded, hasOlder := boundChatMessages(messages, 5, false)
	if len(bounded) != 5 || bounded[0].ID != "p07" || bounded[4].ID != "p11" {
		t.Fatalf("bounded = %+v, want the newest five", bounded)
	}
	if !hasOlder {
		t.Fatal("dropping the front did not report older history")
	}
	if kept, flag := boundChatMessages(messages, 50, false); len(kept) != 12 || flag {
		t.Fatalf("a window larger than the timeline changed it: %d messages, hasOlder %v", len(kept), flag)
	}
	if _, flag := boundChatMessages(messages, 50, true); !flag {
		t.Fatal("an already-known older page was forgotten")
	}
}

func TestJoinableChatConversationsIsTheDiscoverableRowsNotYetJoined(t *testing.T) {
	all := []*chatv1.Conversation{
		{Id: "c1", Name: "General", Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, Joined: true},
		{Id: "c2", Name: "Announcements", Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL},
		{Id: "c4", Name: "Retired", Kind: chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, Archived: true},
		nil,
	}
	browse := joinableChatConversations(all)
	if len(browse) != 1 || browse[0].ID != "c2" {
		t.Fatalf("browse list = %+v, want only the discoverable row the reader has not joined and that is not archived", browse)
	}
	if browse[0].Kind != chatui.PublicChannel || browse[0].Joined {
		t.Fatalf("browse row = %+v, want an unjoined public channel", browse[0])
	}
	// The rail's own rows are rooms the caller is in, whatever the wire says.
	if got := chatConversation(all[0]); !got.Joined {
		t.Fatalf("rail row = %+v, want Joined", got)
	}
}

func TestChatDirectoryResolvesBothSubjectAndWorkerIdentifiers(t *testing.T) {
	directory := chatDirectory([]productui.Person{
		{ID: "subject-1", WorkerID: "worker-1", Name: "Avery Okafor"},
		{ID: "subject-2", Name: ""},
	})
	if got := chatDisplayName(directory, "subject-1"); got != "Avery Okafor" {
		t.Fatalf("subject lookup = %q", got)
	}
	if got := chatDisplayName(directory, "worker-1"); got != "Avery Okafor" {
		t.Fatalf("worker-ref lookup = %q", got)
	}
	if got := chatDisplayName(directory, "9f2a4c1e-7b33-4d90-a1f2-0c5e8b6d4417"); got != "9f2a4c1e-7b33-4d90-a1f2-0c5e8b6d4417" {
		t.Fatalf("an opaque id resolved to %q, want it left exactly as it came", got)
	}
	if got := chatDisplayName(nil, "hc-050-rafael-torres"); got != "Rafael Torres" {
		t.Fatalf("empty directory = %q, want the reference read as a name", got)
	}
}

func TestChatConversationAndKindRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		wire chatv1.ConversationKind
		ui   chatui.ConversationKind
	}{
		{chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, chatui.PublicChannel},
		{chatv1.ConversationKind_CONVERSATION_KIND_PRIVATE_CHANNEL, chatui.PrivateChannel},
		{chatv1.ConversationKind_CONVERSATION_KIND_DIRECT, chatui.DirectMessage},
		{chatv1.ConversationKind_CONVERSATION_KIND_GROUP, chatui.GroupChat},
	} {
		projected := chatConversation(&chatv1.Conversation{Id: "c1", Name: "Team", Kind: tc.wire})
		if projected.Kind != tc.ui {
			t.Errorf("wire %v projected to %q, want %q", tc.wire, projected.Kind, tc.ui)
		}
		if back := chatKind(tc.ui); back != tc.wire {
			t.Errorf("%q mapped back to %v, want %v", tc.ui, back, tc.wire)
		}
	}
	if got := chatConversation(&chatv1.Conversation{Id: "c9"}).Name; got != "c9" {
		t.Fatalf("unnamed conversation = %q, want its id so the rail is never blank", got)
	}
}

func TestChatPostWindowIsTheNewestBoundedSlice(t *testing.T) {
	messages := []chatui.Message{{ID: "a"}, {ID: "b"}, {ID: ""}, {ID: "c"}}
	if got := chatPostWindow(messages, 3); len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("window = %+v, want the newest three minus the entry with no id", got)
	}
	if got := chatPostWindow(messages, 0); got != nil {
		t.Fatalf("zero limit = %+v, want nothing read", got)
	}
	if got := chatLastSequence([]*chatv1.Post{{Sequence: 4}, nil, {Sequence: 11}, {Sequence: 2}}); got != 11 {
		t.Fatalf("last sequence = %d, want 11", got)
	}
}

func TestChatForwardPagingAdvancesAcrossReplyOnlyPage(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = []chatui.Message{{ID: "root-10", Sequence: 10}}
		model.HasNewer = true
	})
	if !state.appendNewerChatMessages("c1", nil, "more", 210) {
		t.Fatal("reply-only page did not update paging state")
	}
	if got := state.newestHeldSequence(); got != 210 {
		t.Fatalf("forward edge after reply-only page = %d, want 210", got)
	}
	if !state.appendNewerChatMessages("c1", []chatui.Message{{ID: "root-211", Sequence: 211}}, "", 211) {
		t.Fatal("root after reply-only page was not admitted")
	}
	if model := state.snapshot(); len(model.Messages) != 2 || model.HasNewer {
		t.Fatalf("forward paging result = %+v", model)
	}
}

func TestChatLiveThreadBoundPreservesOlderRecoveryAndExcludesFuture(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.ShowThread = true
		model.ThreadParentID = "root"
		model.ThreadMessages = make([]chatui.Message, chatThreadWindow)
		for i := range model.ThreadMessages {
			model.ThreadMessages[i] = chatui.Message{ID: fmt.Sprintf("reply-%d", i+1), Sequence: uint64(i + 1)}
		}
	})
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", Seen: map[string]uint64{}}
	state.mu.Unlock()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	post := chatTestPost("reply-151", 151, "avery", "reply", "root", now)
	state.applySentChatPost("c1", post, "en-US", nil, now)
	model := state.snapshot()
	if len(model.ThreadMessages) != chatThreadWindow || model.ThreadMessages[0].Sequence != 2 || !model.ThreadHasOlder || state.threadBeforeSequence != 2 {
		t.Fatalf("live thread trimming lost older edge: len=%d first=%d older=%v edge=%d", len(model.ThreadMessages), model.ThreadMessages[0].Sequence, model.ThreadHasOlder, state.threadBeforeSequence)
	}
	state.mutate(func(model *chatui.Model) { model.ThreadHasNewer = true })
	state.applySentChatPost("c1", chatTestPost("reply-300", 300, "avery", "future", "root", now), "en-US", nil, now)
	if got := state.snapshot().ThreadMessages[len(model.ThreadMessages)-1].Sequence; got != 151 {
		t.Fatalf("future reply spliced into older thread window: %d", got)
	}
}

func TestChatReactionLookupPrunesEvictedPosts(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = []chatui.Message{{ID: "current", Sequence: 100}}
	})
	state.mu.Lock()
	state.reactions = make(map[string][]chatui.ReactionChip)
	for i := 0; i < 1000; i++ {
		state.reactions[fmt.Sprintf("evicted-%d", i)] = []chatui.ReactionChip{{Emoji: "👍", Count: 1}}
	}
	state.reactions["current"] = []chatui.ReactionChip{{Emoji: "👍", Count: 2}}
	state.pruneRetainedReactions()
	got := len(state.reactions)
	_, current := state.reactions["current"]
	state.mu.Unlock()
	if got != 1 || !current {
		t.Fatalf("reaction lookup retained %d entries, current=%v", got, current)
	}
}

func TestChatPagedReactionReadBatchesAndFencesRoom(t *testing.T) {
	state := newChatStateForTest(t)
	page := make([]chatui.Message, 120)
	for i := range page {
		page[i] = chatui.Message{ID: fmt.Sprintf("post-%d", i+1), Sequence: uint64(i + 1)}
	}
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = append([]chatui.Message(nil), page...)
	})
	gen := state.currentGeneration()
	if !state.queuePagedReactionRead(gen, "c1", page) {
		t.Fatal("first page did not start bounded reader")
	}
	first := state.takePagedReactionRead(gen, "c1")
	if len(first) != chatReactionWindow {
		t.Fatalf("first reaction batch = %d", len(first))
	}
	secondPage := make([]chatui.Message, 100)
	for i := range secondPage {
		secondPage[i] = chatui.Message{ID: fmt.Sprintf("post-%d", i+121), Sequence: uint64(i + 121)}
	}
	state.mutate(func(model *chatui.Model) { model.Messages = append(model.Messages, secondPage...) })
	if state.queuePagedReactionRead(gen, "c1", secondPage) {
		t.Fatal("second page started an overlapping reader")
	}
	state.completePagedReactionBatch(gen, first)
	seen := map[string]bool{}
	for _, id := range first {
		seen[id] = true
	}
	var recovered int
	for {
		batch := state.takePagedReactionRead(gen, "c1")
		if len(batch) == 0 {
			break
		}
		for _, id := range batch {
			if seen[id] {
				t.Fatalf("reaction ID %s fetched twice", id)
			}
			seen[id] = true
			recovered++
		}
		state.completePagedReactionBatch(gen, batch)
	}
	if recovered != 170 {
		t.Fatalf("rapid second page recovered %d pending IDs, want 170", recovered)
	}
	if !state.applyPagedReactionChips(gen, "c1", map[string][]chatui.ReactionChip{"post-1": {{Emoji: "👍", Count: 2}}}) {
		t.Fatal("retained post reaction was not applied")
	}
	if got := state.snapshot().Messages[0].Reactions; got != 2 {
		t.Fatalf("retained reaction count = %d", got)
	}
	state.selectChatConversation("c2")
	if state.applyPagedReactionChips(gen, "c1", map[string][]chatui.ReactionChip{"post-1": {{Emoji: "👍", Count: 99}}}) {
		t.Fatal("stale room reaction applied")
	}
}

func TestChatPinRevisionsAndRetainedWindowSurviveTheirWrites(t *testing.T) {
	state := newChatStateForTest(t)
	state.setPinRevisions(map[string]uint64{"p1": 4})
	if got := state.pinRevision("p1"); got != 4 {
		t.Fatalf("pin revision = %d, want the one ListPins published; UnpinPost refuses a zero", got)
	}
	state.setPinRevision("p2", 7)
	if got := state.pinRevision("p2"); got != 7 {
		t.Fatalf("pin revision after a pin = %d, want 7", got)
	}
	state.clearPinRevision("p1")
	if got := state.pinRevision("p1"); got != 0 {
		t.Fatalf("unpinned post still carries revision %d", got)
	}
	if got := state.retainedWindow(); got != chatMessageWindow {
		t.Fatalf("retained window = %d, want the default %d", got, chatMessageWindow)
	}
	if got := state.growWindow(chatPageSize); got != chatMessageWindow {
		t.Fatalf("window exceeded memory bound: %d", got)
	}
	if got := state.retainedWindow(); got != chatMessageWindow {
		t.Fatalf("retained window changed: %d", got)
	}
}

func TestChatEditDraftsAreLocalStateOnly(t *testing.T) {
	state := newChatStateForTest(t)
	state.setEditDraft("p1", "corrected text")
	if got := state.editDraftSnapshot()["p1"]; got != "corrected text" {
		t.Fatalf("edit draft = %q", got)
	}
	if got := state.snapshot().EditDrafts["p1"]; got != "corrected text" {
		t.Fatalf("the model did not carry the edit draft: %q", got)
	}
	state.clearEditDraft("p1")
	if got := state.editDraftSnapshot(); len(got) != 0 {
		t.Fatalf("edit draft survived the commit: %+v", got)
	}
}

func TestChatSubscriptionIsIdempotentSoARevalidationNeverReopensTheStream(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1" })
	cancelledFirst := false
	generation := state.currentGeneration()
	previous, started := state.swapSubscription(generation, "c1", func() { cancelledFirst = true })
	if !started || previous != nil {
		t.Fatalf("first swap: started=%v previous=%v", started, previous != nil)
	}
	if !state.streamLive("c1") {
		t.Fatal("the stream was not recorded as live")
	}
	// The live run opened 702 WatchConversation streams for one tab because
	// every projection load reopened the stream. Ten revalidations in a row
	// must tear down nothing and start nothing.
	for i := 0; i < 10; i++ {
		previous, started = state.swapSubscription(state.currentGeneration(), "c1", func() {})
		if started || previous != nil {
			t.Fatalf("revalidation %d rebuilt a healthy stream: started=%v previous=%v", i, started, previous != nil)
		}
	}
	if cancelledFirst {
		t.Fatal("a revalidation cancelled the live stream")
	}
	stale := generation
	state.beginGeneration()
	if previous, started = state.swapSubscription(stale, "c1", func() {}); started || previous != nil {
		t.Fatalf("a superseded generation touched the live stream: started=%v previous=%v", started, previous != nil)
	}
	// Its generation moved, so the registration no longer keeps the loader
	// from re-reading, and the next subscribe is allowed to replace it.
	if state.streamLive("c1") {
		t.Fatal("a stream from a superseded generation still counted as live")
	}
	// A real conversation switch does hand the open stream back to be ended.
	previous, started = state.swapSubscription(state.currentGeneration(), "c2", func() {})
	if !started || previous == nil {
		t.Fatalf("switching conversation: started=%v previous=%v", started, previous != nil)
	}
	previous()
	if !cancelledFirst {
		t.Fatal("the stream for the conversation the reader left was not cancelled")
	}
	if got := state.takeSubscription(); got == nil {
		t.Fatal("takeSubscription returned nothing while a stream was recorded")
	}
	if got := state.takeSubscription(); got != nil {
		t.Fatal("takeSubscription handed the same cancel out twice")
	}
}

func TestChatSubscriptionIsReleasedOnlyByTheWatchThatOwnsIt(t *testing.T) {
	state := newChatStateForTest(t)
	generation := state.currentGeneration()
	if _, started := state.swapSubscription(generation, "c1", func() {}); !started {
		t.Fatal("first swap did not start")
	}
	if stop := state.releaseSubscription(generation, "c2"); stop != nil {
		t.Fatal("a watch for another conversation released this registration")
	}
	if stop := state.releaseSubscription(generation+7, "c1"); stop != nil {
		t.Fatal("a watch from another generation released this registration")
	}
	if !state.streamLive("c1") {
		t.Fatal("the foreign releases cleared the live registration")
	}
	if stop := state.releaseSubscription(generation, "c1"); stop == nil {
		t.Fatal("the owning watch could not release its own registration")
	}
	if state.streamLive("c1") {
		t.Fatal("the registration outlived the watch that owned it; the loader would trust a dead stream")
	}
}

func TestChatWatchStartSendsACursorOrASequenceButNeverBoth(t *testing.T) {
	// The transport refuses a request carrying both with INVALID_ARGUMENT.
	for _, tc := range []struct {
		name         string
		cursor       chatCursor
		wantSequence uint64
		wantResume   string
	}{
		{"first subscription", chatCursor{}, 0, ""},
		// A post read teaches the timeline, not the watch: the post sequence is
		// never sent as a watch position.
		{"after a plain post read", chatCursor{LastSequence: 12}, 0, ""},
		{"after the stream reported its position", chatCursor{LastSequence: 12, StreamSequence: 4181}, 4181, ""},
		{"resuming a stream", chatCursor{StreamSequence: 4181, Resume: "opaque-12"}, 0, "opaque-12"},
		{"blank cursor is not a cursor", chatCursor{StreamSequence: 4, Resume: "   "}, 4, ""},
	} {
		sequence, resume := chatWatchStart(tc.cursor)
		if sequence != tc.wantSequence || resume != tc.wantResume {
			t.Errorf("%s: after_sequence=%d resume_cursor=%q, want %d/%q", tc.name, sequence, resume, tc.wantSequence, tc.wantResume)
		}
		if sequence != 0 && resume != "" {
			t.Errorf("%s: sent both after_sequence=%d and resume_cursor=%q", tc.name, sequence, resume)
		}
	}
}

func TestApplySentChatPostShowsTheReadersOwnMessageOnceWithoutAReload(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1"; model.State = chatui.StateEmpty })
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", LastSequence: 4, Seen: map[string]uint64{}}
	state.mu.Unlock()

	sent := chatTestPost("p5", 5, "avery", "posted", "", now)
	applied, gapped := state.applySentChatPost("c1", sent, "en-US", nil, now)
	if !applied || gapped {
		t.Fatalf("the committed post: applied=%v gapped=%v", applied, gapped)
	}
	model := state.snapshot()
	if len(model.Messages) != 1 || model.Messages[0].ID != "p5" {
		t.Fatalf("timeline = %+v", model.Messages)
	}
	// A send never takes an open conversation through a loading render: that
	// is what disabled the composer and dropped the keystrokes typed during it.
	if model.State != chatui.StateReady || model.Error != "" {
		t.Fatalf("state = %q / %q, want ready with no error", model.State, model.Error)
	}
	if got := state.streamCursor().LastSequence; got != 5 {
		t.Fatalf("cursor = %d, want 5 so the stream does not redeliver it", got)
	}
	// The stream delivers the same post a moment later, at whatever watch
	// position the server gives it. It is a duplicate on the post's identity.
	if got := classifyChatEvent(state.streamCursor(), "p5", 5, 91437, true); got != chatEventDuplicate {
		t.Fatalf("the stream copy of the reader's own post classified as %d, want duplicate", got)
	}
	if applied, _ = state.applySentChatPost("c1", sent, "en-US", nil, now); applied {
		t.Fatal("the same post applied twice")
	}
	// A post that jumped the sequence is still shown, and the gap is reported
	// so the caller reads the missing posts by cursor instead of reloading.
	applied, gapped = state.applySentChatPost("c1", chatTestPost("p9", 9, "avery", "jumped", "", now.Add(time.Minute)), "en-US", nil, now)
	if !applied || !gapped {
		t.Fatalf("a post past the cursor: applied=%v gapped=%v, want both true", applied, gapped)
	}
	if got := state.streamCursor().LastSequence; got != 5 {
		t.Fatalf("cursor = %d, want 5: advancing past a gap would hide what was missed", got)
	}
	// A reply counts against its parent and never becomes a timeline row.
	if applied, _ = state.applySentChatPost("c1", chatTestPost("p6", 6, "avery", "reply", "p5", now), "en-US", nil, now); !applied {
		t.Fatal("a reply was not applied")
	}
	model = state.snapshot()
	if len(model.Messages) != 2 || model.Messages[0].Replies != 1 {
		t.Fatalf("reply handling: %d rows, parent replies %d", len(model.Messages), model.Messages[0].Replies)
	}
	// Nothing is applied for a conversation the reader has left.
	if applied, _ = state.applySentChatPost("c2", chatTestPost("p7", 7, "avery", "elsewhere", "", now), "en-US", nil, now); applied {
		t.Fatal("a post for another conversation reached this timeline")
	}
}

// chatTestScheduler is a debounceScheduler whose timers fire only when the
// test says so, so debounce ordering is decided rather than waited for.
type chatTestScheduler struct {
	pending []*chatTestTimer
}

type chatTestTimer struct {
	fire    func()
	stopped bool
}

func (t *chatTestTimer) Stop() bool { t.stopped = true; return true }

func (s *chatTestScheduler) schedule(_ time.Duration, fire func()) debounceTimer {
	timer := &chatTestTimer{fire: fire}
	s.pending = append(s.pending, timer)
	return timer
}

// runAll fires every timer that was not stopped, newest last.
func (s *chatTestScheduler) runAll() {
	pending := s.pending
	s.pending = nil
	for _, timer := range pending {
		if !timer.stopped {
			timer.fire()
		}
	}
}

func TestChatDraftWriterFlushCancelsTheWriteOfTheTextJustSent(t *testing.T) {
	scheduler := &chatTestScheduler{}
	writes := 0
	writer := newChatDraftWriter(scheduler.schedule, time.Second, func() { writes++ })

	writer.Schedule()
	writer.Schedule()
	if !writer.Pending() {
		t.Fatal("no write was scheduled")
	}
	// A send flushes: the pending write is cancelled and the current state is
	// written at once, so no older write can land after it.
	writer.Flush()
	if writes != 1 {
		t.Fatalf("writes after the flush = %d, want 1", writes)
	}
	if writer.Pending() {
		t.Fatal("a write is still pending after the flush")
	}
	scheduler.runAll()
	if writes != 1 {
		t.Fatalf("writes after the cancelled timers ran = %d, want 1; a stale write landed over the cleared draft", writes)
	}
	// Typing again schedules again, and only the newest scheduled write runs.
	writer.Schedule()
	writer.Schedule()
	scheduler.runAll()
	if writes != 2 {
		t.Fatalf("writes after typing again = %d, want 2", writes)
	}
}

func TestChatProjectionNeverRestoresADraftThisSessionHasCleared(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1" })
	// The reader typed and the debounced write persisted this much.
	state.setDraft("c1", "Thanks. I will chase them today and post here when the last one la")
	persisted := state.allDrafts()
	// They pressed Enter: the draft is cleared locally.
	state.setDraft("c1", "")
	if state.draftRevision("c1") == 0 {
		t.Fatal("clearing the draft left no local revision to defend it")
	}
	// A projection reload reads the server's older copy.
	state.loadDrafts(persisted)
	if got := state.snapshot().Draft; got != "" {
		t.Fatalf("composer = %q after a reload, want empty; the sent text was restored and merged into the next message", got)
	}
	if got := state.draft("c1"); got != "" {
		t.Fatalf("stored draft = %q, want the cleared one kept", got)
	}
	// A conversation this session has not typed in still adopts its draft.
	state.loadDrafts(map[string]string{"c2": "kept from last time"})
	if got := state.draft("c2"); got != "kept from last time" {
		t.Fatalf("untouched conversation draft = %q, want the persisted one", got)
	}
}

func TestChatStreamRolloverDoesNotCountATunnelRotationOrOurOwnCancel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		lifetime time.Duration
		alive    bool
		want     bool
	}{
		{"ceiling reached after a quiet hour", io.EOF, 15 * time.Minute, true, true},
		{"clean end with no error", nil, 20 * time.Minute, true, true},
		{"transport deadline after a long stream", context.DeadlineExceeded, 11 * time.Second, true, true},
		{"grpc deadline after a long stream", status.Error(codes.DeadlineExceeded, "ceiling"), time.Minute, true, true},
		// The live run: the tunnel's socket rotated inside the ten-second
		// window, five times, and each rotation was counted as a refusal.
		{"tunnel rotation seconds after opening", status.Error(codes.Canceled, "context canceled"), 900 * time.Millisecond, true, true},
		{"rotation reported as context.Canceled", context.Canceled, time.Second, true, true},
		// Our own cancel -- a conversation switch, a page leaving.
		{"we cancelled it", status.Error(codes.Canceled, "context canceled"), time.Second, false, true},
		{"we cancelled a refused stream", status.Error(codes.Internal, "boom"), time.Second, false, true},
		// Still failures.
		{"refused immediately", status.Error(codes.Internal, "boom"), 500 * time.Microsecond, true, false},
		{"clean end immediately", io.EOF, time.Second, true, false},
		{"permission lost mid-stream", status.Error(codes.PermissionDenied, "revoked"), time.Minute, true, false},
	} {
		if got := chatStreamRollover(tc.err, tc.lifetime, tc.alive); got != tc.want {
			t.Errorf("%s: rollover = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestChatReopeningTheRoomAlreadyOpenDoesNotCancelItsStream(t *testing.T) {
	state := newChatStateForTest(t)
	state.selectChatConversation("people-ops")
	if state.alreadyOpen("people-ops") {
		t.Fatal("a room whose first read is still in flight counted as open")
	}
	state.finishChatOpen("people-ops", state.currentGeneration())
	if state.alreadyOpen("people-ops") {
		t.Fatal("a room with no stream counted as open")
	}

	cancelled := 0
	if _, started := state.swapSubscription(state.currentGeneration(), "people-ops", func() { cancelled++ }); !started {
		t.Fatal("the subscription did not start")
	}
	if !state.alreadyOpen("people-ops") {
		t.Fatal("the open room with a live stream did not count as open")
	}
	if state.alreadyOpen("comp-review") {
		t.Fatal("another room counted as open")
	}
	// This is the guard the live run needed: a re-open of the same room is not
	// a switch, and treating it as one cancelled a healthy stream five times in
	// forty seconds until the feed gave up.
	for i := 0; i < 5; i++ {
		if _, started := state.swapSubscription(state.currentGeneration(), "people-ops", func() {}); started {
			t.Fatalf("re-open %d started a second stream", i)
		}
	}
	if cancelled != 0 {
		t.Fatalf("the live stream was cancelled %d times by re-opens", cancelled)
	}
	// A load failure means there is something to recover, so a re-open is allowed.
	state.setLoadError("We couldn't load this conversation. Try again.")
	if state.alreadyOpen("people-ops") {
		t.Fatal("a room with a load error refused the re-open that would recover it")
	}
}

func TestChatRepeatedSelectionDuringOpenKeepsThePendingRead(t *testing.T) {
	state := newChatStateForTest(t)
	_, generation := state.selectChatConversation("people-ops")
	if !state.alreadyOpening("people-ops") || state.alreadyOpening("comp-review") {
		t.Fatal("only the selected room with an in-flight read should be held")
	}
	if state.currentGeneration() != generation || state.snapshot().State != chatui.StateLoading {
		t.Fatal("the first open did not retain its generation and loading state")
	}
	state.finishChatOpen("people-ops", generation)
	if state.alreadyOpening("people-ops") {
		t.Fatal("a completed or failed read must allow an explicit retry")
	}
	state.selectChatConversation("comp-review")
	if !state.alreadyOpening("comp-review") || state.alreadyOpening("people-ops") {
		t.Fatal("switching to another room must start its own read")
	}
}

func TestChatOldReadCannotFinishNewOpenOfTheSameRoom(t *testing.T) {
	state := newChatStateForTest(t)
	_, oldA := state.selectChatConversation("room-a")
	state.selectChatConversation("room-b")
	_, newA := state.selectChatConversation("room-a")
	if _, queued := state.resolveSendTarget("room-a", "new message"); !queued {
		t.Fatal("a send during the new open was not queued")
	}
	if drained := state.finishChatOpen("room-a", oldA); len(drained) != 0 {
		t.Fatalf("stale read drained the new open's send: %+v", drained)
	}
	if !state.alreadyOpening("room-a") || state.currentGeneration() != newA {
		t.Fatal("stale read cleared the new open's latch or generation")
	}
	if drained := state.finishChatOpen("room-a", newA); len(drained) != 1 || drained[0] != "new message" {
		t.Fatalf("new read drained %+v, want its queued send", drained)
	}
	if state.alreadyOpening("room-a") {
		t.Fatal("completed new read left its opening latch set")
	}
}

func TestChatStreamLogKeepsTheLastEntriesForALiveRun(t *testing.T) {
	ring := newChatStreamRing(3)
	if got := ring.Lines(); len(got) != 0 {
		t.Fatalf("a fresh log is not empty: %+v", got)
	}
	at := time.Date(2026, 9, 22, 0, 31, 47, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		ring.Add(chatStreamEvent{At: at, Conversation: "people-ops", Event: "open", Attempt: i})
	}
	lines := ring.Lines()
	if len(lines) != 3 {
		t.Fatalf("log held %d entries, want the last 3", len(lines))
	}
	if !strings.Contains(lines[0], "attempt=3") || !strings.Contains(lines[2], "attempt=5") {
		t.Fatalf("log is not oldest-first over the last three: %+v", lines)
	}
	ring.Add(chatStreamEvent{At: at, Conversation: "people-ops", Event: "ended", Cause: "Canceled", Backoff: 2 * time.Second, Rollover: true})
	last := ring.Lines()[2]
	for _, want := range []string{"00:31:47.000", "ended", "people-ops", "cause=Canceled", "backoff=2s", "rollover"} {
		if !strings.Contains(last, want) {
			t.Fatalf("log line %q is missing %q", last, want)
		}
	}
	if text := ring.Text(); strings.Count(text, "\n") != 2 {
		t.Fatalf("log text is not one entry per line: %q", text)
	}
}

func TestChatSendClearsTheComposerBeforeTheRPCAndOnlyAFailureRestoresIt(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1" })
	body := "Thanks. I will chase them today"
	state.setDraft("c1", body)

	// Pressing Enter empties the composer before the send leaves, so any render
	// between then and the response shows an empty composer.
	state.setDraft("c1", "")
	if got := state.snapshot().Draft; got != "" {
		t.Fatalf("composer = %q at the moment the send starts, want empty", got)
	}

	// A projection read that began BEFORE the send commits afterwards. Its
	// snapshot still carries the old draft; adopting it must not put the sent
	// text back. This is the 200-500 ms revert the live run saw.
	stale := state.snapshot()
	stale.Draft = body
	stale.Preferences.Drafts = map[string]string{"c1": body}
	stale.Messages = []chatui.Message{{ID: "p1", Body: "earlier"}}
	if !state.adoptLoadedChatProjection(state.currentGeneration(), stale, chatCursor{ConversationID: "c1", Seen: map[string]uint64{"p1": 1}}, true) {
		t.Fatal("the projection was not adopted")
	}
	model := state.snapshot()
	if model.Draft != "" {
		t.Fatalf("composer = %q after a projection commit, want it still empty", model.Draft)
	}
	if got := model.Preferences.Drafts["c1"]; got != "" {
		t.Fatalf("stored draft = %q after a projection commit", got)
	}
	if len(model.Messages) != 1 {
		t.Fatalf("the projection's own fields were dropped: %+v", model.Messages)
	}
	// A superseded read is discarded whole.
	if state.adoptLoadedChatProjection(state.currentGeneration()+1, stale, chatCursor{}, true) {
		t.Fatal("a stale generation adopted its projection")
	}

	// Only a failed send puts the text back, and it reaches the composer.
	state.restoreDraft("c1", body)
	if got := state.snapshot().Draft; got != body {
		t.Fatalf("composer = %q after a failed send, want the text back to retry", got)
	}
	if got := state.draft("c1"); got != body {
		t.Fatalf("stored draft = %q after a failed send", got)
	}
	// And a restore for a room the reader has left never reaches the composer.
	state.setDraft("c1", "")
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c2" })
	state.restoreDraft("c1", body)
	if got := state.snapshot().Draft; got != "" {
		t.Fatalf("composer = %q, want the other room's restore kept out of it", got)
	}
}

func TestChatProjectionRefreshPreservesThreadOpenedDuringListingRead(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1" })
	stale := state.snapshot() // The room listing starts before the thread opens.
	stale.Messages = []chatui.Message{{ID: "root", Sequence: 10}}

	state.mutate(func(model *chatui.Model) {
		model.ShowThread = true
		model.ThreadParentID = "root"
		model.ThreadLoading = false
		model.ThreadHasOlder = true
		for i := 0; i < 5; i++ {
			model.ThreadMessages = append(model.ThreadMessages, chatui.Message{ID: fmt.Sprintf("reply-%d", i), Sequence: uint64(11 + i)})
		}
	})
	state.mu.Lock()
	state.threadBeforeSequence = 8 // A scan edge may precede the first retained reply.
	state.threadAfterSequence = 16
	state.mu.Unlock()

	if !state.adoptLoadedChatProjection(state.currentGeneration(), stale, chatCursor{ConversationID: "c1"}, true) {
		t.Fatal("listing refresh was not adopted")
	}
	model := state.snapshot()
	if !model.ShowThread || model.ThreadParentID != "root" || len(model.ThreadMessages) != 5 || !model.ThreadHasOlder {
		t.Fatalf("in-flight listing erased opened thread: %+v", model)
	}
	if state.threadBeforeSequence != 8 || state.threadAfterSequence != 16 {
		t.Fatalf("in-flight listing changed thread scan edges: before=%d after=%d", state.threadBeforeSequence, state.threadAfterSequence)
	}
	// Even a listing that began on the same root cannot replace a newer
	// dedicated thread result with its stale copy.
	stale = state.snapshot()
	stale.ThreadMessages = []chatui.Message{{ID: "stale-reply", Sequence: 9}}
	stale.ThreadHasOlder = false
	state.mutate(func(model *chatui.Model) { model.ThreadLoading = true })
	if !state.adoptLoadedChatProjection(state.currentGeneration(), stale, chatCursor{ConversationID: "c1"}, true) {
		t.Fatal("second listing refresh was not adopted")
	}
	model = state.snapshot()
	if len(model.ThreadMessages) != 5 || model.ThreadMessages[0].ID != "reply-0" || !model.ThreadLoading || !model.ThreadHasOlder {
		t.Fatalf("stale same-root listing replaced current thread: %+v", model.ThreadMessages)
	}
}

func TestChatErrorCopyNeverShowsAGRPCStatusString(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"denied", status.Error(codes.PermissionDenied, "chat.permission_denied"), "You do not have access to it."},
		{"expired", status.Error(codes.Unauthenticated, "chat.no_principal"), "Your sign-in has expired. Sign in again."},
		{"gone", status.Error(codes.NotFound, "chat.not_found"), "It is no longer there."},
		{"conflict", status.Error(codes.Aborted, "chat.conflict"), "Someone else changed it first. Try again."},
		{"down", status.Error(codes.Unavailable, "transport.unclassified_failure"), "The service did not answer. Try again."},
		{"not built", status.Error(codes.Unimplemented, "unknown service"), "This server does not offer it yet."},
		{"plain error", errors.New("boom"), "Something went wrong. Try again."},
	} {
		if got := chatFriendlyError(tc.err); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := chatFriendlyError(nil); got != "" {
		t.Errorf("no error produced copy: %q", got)
	}
	// Nothing the reader sees may carry the transport's own words.
	raw := status.Error(codes.Unavailable, "transport.unclassified_failure")
	for _, text := range []string{
		chatFriendlyError(raw),
		actionFailureNotice("pin this message", raw),
		chatLoadFailureMessage("this conversation", raw),
	} {
		if strings.Contains(text, "rpc error") || strings.Contains(text, "code =") || strings.Contains(text, "transport.") {
			t.Errorf("reader-facing copy leaked the status string: %q", text)
		}
	}
	if got := chatLoadFailureMessage("this conversation", raw); got != "We couldn't load this conversation. The service did not answer. Try again." {
		t.Fatalf("load failure copy = %q", got)
	}
}

func TestChatNoticeRetryIsSetForTheStoppedFeedAndClearedWithIt(t *testing.T) {
	state := newChatStateForTest(t)
	plain := state.setNotice("Message pinned", false)
	if state.snapshot().NoticeRetry {
		t.Fatal("an outcome notice offered a retry control")
	}
	state.clearNotice(plain)

	token := state.setNotice("This conversation is no longer updating live.", true)
	model := state.snapshot()
	if !model.NoticeRetry || model.Notice == "" {
		t.Fatalf("stopped-feed notice = %q retry=%v, want both set", model.Notice, model.NoticeRetry)
	}
	if !state.clearNotice(token) {
		t.Fatal("the notice did not clear")
	}
	if model = state.snapshot(); model.Notice != "" || model.NoticeRetry {
		t.Fatalf("notice %q / retry %v survived the clear", model.Notice, model.NoticeRetry)
	}
}

// assertChatTimelineAscending fails when the timeline is not in conversation
// order. The live build rendered it newest-first, so every path that touches
// Model.Messages is checked against the same assertion.
func assertChatTimelineAscending(t *testing.T, where string, messages []chatui.Message, sequences map[string]uint64) {
	t.Helper()
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	for i := 1; i < len(messages); i++ {
		previous, current := sequences[messages[i-1].ID], sequences[messages[i].ID]
		if previous == 0 || current == 0 {
			t.Fatalf("%s: message %q or %q is not in the sequence index", where, messages[i-1].ID, messages[i].ID)
		}
		if previous > current {
			t.Fatalf("%s: timeline is not ascending by sequence: %v (%d then %d)", where, ids, previous, current)
		}
	}
}

func TestChatTimelineIsAscendingAfterOpenSendAndLoadOlder(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	sequences := map[string]uint64{"p5": 5, "p6": 6, "p7": 7, "p8": 8, "p9": 9, "p10": 10}

	// The store hands a backward page back oldest-first already, and a client
	// that reverses it is what put "Reminder..." above "Morning all...".
	// chatInConversationOrder sorts, so either arrival order reads the same.
	page := []*chatv1.Post{
		chatTestPost("p7", 7, "avery", "Morning all", "", now),
		chatTestPost("p8", 8, "blake", "middle", "", now),
		nil,
		chatTestPost("p9", 9, "avery", "Reminder", "", now),
	}
	ordered := chatInConversationOrder(page)
	if len(ordered) != 3 || ordered[0].GetId() != "p7" || ordered[2].GetId() != "p9" {
		t.Fatalf("reading order = %+v, want p7,p8,p9", ordered)
	}
	reversed := []*chatv1.Post{page[3], page[1], page[0]}
	if got := chatInConversationOrder(reversed); got[0].GetId() != "p7" || got[2].GetId() != "p9" {
		t.Fatalf("a newest-first page was not put in reading order: %+v", got)
	}
	// Posts sharing a timestamp -- which a seed run produces -- still order by
	// sequence, so the clock is never the authority.
	opened := chatMessages(ordered, "en-US", nil, nil, now)
	assertChatTimelineAscending(t, "after open", opened, sequences)

	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = opened
	})
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", LastSequence: 9, Seen: map[string]uint64{"p7": 7, "p8": 8, "p9": 9}}
	state.mu.Unlock()
	state.setOlderCursor("page-2")
	if !state.snapshot().HasOlder {
		t.Fatal("a next cursor did not offer older history")
	}

	// A send appends at the end, not wherever its timestamp lands.
	if applied, _ := state.applySentChatPost("c1", chatTestPost("p10", 10, "avery", "sent", "", now), "en-US", nil, now); !applied {
		t.Fatal("the sent post was not applied")
	}
	assertChatTimelineAscending(t, "after a send", state.snapshot().Messages, sequences)
	if last := state.snapshot().Messages; last[len(last)-1].ID != "p10" {
		t.Fatalf("the sent post is not at the end: %+v", last)
	}

	// A stream event lands in sequence position too.
	state.mu.Lock()
	delete(state.cursor.Seen, "p10")
	state.cursor.LastSequence = 9
	state.model.Messages = opened
	state.mu.Unlock()
	if outcome, _ := state.applyStreamEvent(state.currentGeneration(), "c1", &chatv1.ConversationEvent{
		Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: 10,
		Post: chatTestPost("p10", 10, "blake", "streamed", "", now),
	}, "en-US", nil, now, ""); outcome != chatEventApplied {
		t.Fatalf("stream event outcome = %d, want applied", outcome)
	}
	assertChatTimelineAscending(t, "after a stream event", state.snapshot().Messages, sequences)

	// An older page prepends and the whole timeline stays ascending.
	older := chatMessages(chatInConversationOrder([]*chatv1.Post{
		chatTestPost("p6", 6, "blake", "earlier", "", now),
		chatTestPost("p5", 5, "blake", "earliest", "", now),
		chatTestPost("p7", 7, "avery", "Morning all", "", now),
	}), "en-US", nil, nil, now)
	state.mu.Lock()
	state.cursor.Seen["p5"], state.cursor.Seen["p6"] = 5, 6
	state.mu.Unlock()
	if !state.prependOlderChatMessages("c1", older, "") {
		t.Fatal("the older page was not prepended")
	}
	model := state.snapshot()
	if len(model.Messages) != 6 {
		t.Fatalf("timeline = %d messages, want 6 with p7 not duplicated: %+v", len(model.Messages), model.Messages)
	}
	assertChatTimelineAscending(t, "after LoadOlder", model.Messages, sequences)
	if model.Messages[0].ID != "p5" || model.Messages[5].ID != "p10" {
		t.Fatalf("order = %s..%s, want p5..p10", model.Messages[0].ID, model.Messages[5].ID)
	}
	if model.HasOlder {
		t.Fatal("an empty next cursor still offered older history")
	}
	if got := state.retainedWindow(); got != chatMessageWindow {
		t.Fatalf("retained window = %d, want fixed bound", got)
	}
	if state.prependOlderChatMessages("c2", older, "") {
		t.Fatal("an older page reached a conversation the reader had left")
	}
}

func TestChatSendTypedWhileARoomIsOpeningIsQueuedNotDropped(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "comp-review"
		model.Conversations = []chatui.Conversation{{ID: "comp-review"}, {ID: "people-ops"}}
	})

	// A room was just created and is opening. chatui still holds the previous
	// render, so its send carries the previous room's id.
	_, generation := state.selectChatConversation("people-ops")
	if got := state.snapshot().SelectedID; got != "people-ops" {
		t.Fatalf("SelectedID = %q, want the new room set before the read starts", got)
	}
	target, queued := state.resolveSendTarget("comp-review", "Kicking this off")
	if !queued || target != "" {
		t.Fatalf("mid-open send: target=%q queued=%v, want it held", target, queued)
	}
	if _, queued = state.resolveSendTarget("", "And a second line"); !queued {
		t.Fatal("a second mid-open send was not held")
	}

	// A conversation the reader has left never receives them.
	if got := state.finishChatOpen("comp-review", generation); got != nil {
		t.Fatalf("another conversation's open drained the queue: %+v", got)
	}
	pending := state.finishChatOpen("people-ops", generation)
	if len(pending) != 2 || pending[0] != "Kicking this off" || pending[1] != "And a second line" {
		t.Fatalf("queued sends = %+v, want both in the order they were typed", pending)
	}
	if got := state.finishChatOpen("people-ops", generation); got != nil {
		t.Fatalf("the queue was drained twice: %+v", got)
	}

	// With nothing opening, the composer the message was typed into decides.
	// The model is the fallback, not the preference: a stale projection commit
	// can leave SelectedID naming the room before this one.
	target, queued = state.resolveSendTarget("people-ops", "later")
	if queued || target != "people-ops" {
		t.Fatalf("settled send: target=%q queued=%v, want people-ops", target, queued)
	}
	if target, queued = state.resolveSendTarget("", "no id from the caller"); queued || target != "people-ops" {
		t.Fatalf("send with no caller id: target=%q queued=%v, want the selected room", target, queued)
	}
	// Nothing selected at all still holds the message rather than losing it.
	state.mutate(func(model *chatui.Model) { model.SelectedID = "" })
	if _, queued = state.resolveSendTarget("", "before any room"); !queued {
		t.Fatal("a send with no room selected was dropped")
	}
}

func TestChatResumeCursorIsDroppedWhenTheServerRefusesIt(t *testing.T) {
	state := newChatStateForTest(t)
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", LastSequence: 12, StreamSequence: 4181, Resume: "stale-token", Baselined: true, Seen: map[string]uint64{}}
	state.mu.Unlock()

	if _, resume := chatWatchStart(state.streamCursor()); resume != "stale-token" {
		t.Fatalf("resume = %q, want the cursor preferred while it is good", resume)
	}
	if !chatCursorRejected(status.Error(codes.InvalidArgument, "chat.invalid_argument")) {
		t.Fatal("an INVALID_ARGUMENT refusal was not recognised as a cursor rejection")
	}
	if chatCursorRejected(status.Error(codes.Unavailable, "down")) {
		t.Fatal("an outage was treated as a cursor rejection")
	}
	state.dropResumeCursor("c1")
	sequence, resume := chatWatchStart(state.streamCursor())
	// The watch position, not the post sequence: they are different spaces.
	if resume != "" || sequence != 4181 {
		t.Fatalf("after dropping the cursor: after_sequence=%d resume=%q, want the watch position and nothing", sequence, resume)
	}
	state.dropResumeCursor("c2")
	if got := state.streamCursor().LastSequence; got != 12 {
		t.Fatalf("a drop for another conversation moved this cursor to %d", got)
	}
}

func TestChatMemberCountReachesTheConversationHeader(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Conversations = []chatui.Conversation{{ID: "c1", Name: "People ops"}, {ID: "c2"}}
	})
	state.setChatMemberCount("c1", 4)
	model := state.snapshot()
	if model.Conversations[0].MemberCount != 4 {
		t.Fatalf("member count = %d, want 4", model.Conversations[0].MemberCount)
	}
	if model.Conversations[1].MemberCount != 0 {
		t.Fatalf("another conversation picked up the count: %d", model.Conversations[1].MemberCount)
	}
}

func TestChatResolvesAuthorNamesWithoutThePeoplePage(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	state := newChatStateForTest(t)
	post := chatTestPost("p1", 1, "hc-050-rafael-torres", "Morning all", "", now)

	// A fresh session: nothing has populated the product shell's People
	// projection, which is what an Arabic session and every second-persona
	// session looked like. Until the directory read lands the id renders as
	// itself -- never an invented name.
	// Before the read the reference is read as a name -- never the raw slug the
	// hiring-manager session was showing.
	if got := chatMessage(post, "ar", state.directorySnapshot(), now).Author; got != "Rafael Torres" {
		t.Fatalf("author before the directory read = %q, want the humanized reference", got)
	}
	if !state.claimDirectoryRead() {
		t.Fatal("a fresh session did not owe itself a directory read")
	}
	if state.claimDirectoryRead() {
		t.Fatal("the directory was read twice in one session")
	}

	// The session's own worker read lands.
	state.mergeDirectory(chatDirectoryFromWorkers([]*journeyv1.Worker{
		{WorkerRef: "rafael-8f1b9c7d", WorkerId: "hc-050", SubjectId: "hc-050-rafael-torres", PreferredName: "Rafael", LegalName: "Rafael Torres"},
		{WorkerRef: "dana-2c4e6a80", SubjectId: "hc-051-dana-whitfield", LegalName: "Dana Whitfield"},
		{WorkerRef: "worker-4e2a", SubjectId: "hc-052-8f1b9c7d4e2a"},
		nil,
	}))
	directory := state.directorySnapshot()
	if got := chatMessage(post, "ar", directory, now).Author; got != "Rafael Torres" {
		t.Fatalf("author after the directory read = %q, want the preferred name with the surname", got)
	}
	if got := chatDisplayName(directory, "rafael-8f1b9c7d"); got == "Rafael Torres" {
		t.Fatalf("the display slug resolved as a chat identity: %q", got)
	}
	if got := chatDisplayName(directory, "hc-051-dana-whitfield"); got != "Dana Whitfield" {
		t.Fatalf("legal-name fallback = %q", got)
	}
	if got := chatDisplayName(directory, "hc-052-8f1b9c7d4e2a"); got != "hc-052-8f1b9c7d4e2a" {
		t.Fatalf("a worker the directory does not know resolved to %q, want the opaque id unchanged", got)
	}
	// AuthorID stays the identifier whatever the name resolves to.
	if got := chatMessage(post, "ar", directory, now).AuthorID; got != "hc-050-rafael-torres" {
		t.Fatalf("AuthorID = %q, want the subject id", got)
	}

	// A later empty source never erases what is resolved. Replacing rather
	// than merging is what left every author an id.
	state.mergeDirectory(nil)
	state.mergeDirectory(map[string]string{})
	state.mergeDirectory(map[string]string{"hc-050-rafael-torres": ""})
	if got := chatDisplayName(state.directorySnapshot(), "hc-050-rafael-torres"); got != "Rafael Torres" {
		t.Fatalf("name after an empty merge = %q, want it kept", got)
	}
	// The People projection, when this session has one, adds to the same map.
	state.mergeDirectory(chatDirectory([]productui.Person{{ID: "hc-060", Name: "Priya Raman"}}))
	if got := chatDisplayName(state.directorySnapshot(), "hc-060"); got != "Priya Raman" {
		t.Fatalf("People projection name = %q", got)
	}
	// A directory that never arrives is not a page failure: chat stays usable,
	// with humanized references, and nothing sets the load error.
	if got := state.loadError(); got != "" {
		t.Fatalf("the directory work set a load error: %q", got)
	}
	// Names already on screen are re-resolved in place: the read lands after
	// the first render, and Members is not re-projected by a route read at all.
	state.mutate(func(model *chatui.Model) {
		model.Messages = []chatui.Message{{ID: "p1", AuthorID: "hc-050-rafael-torres", Author: "hc-050-rafael-torres"}}
		model.ThreadMessages = []chatui.Message{{ID: "p2", AuthorID: "hc-051-dana-whitfield", Author: "hc-051-dana-whitfield"}}
		model.Members = []chatui.Member{{ID: "hc-050-rafael-torres", Name: "hc-050-rafael-torres"}, {ID: "hc-099-4b7e2f", Name: "hc-099-4b7e2f"}}
	})
	if !state.applyChatDirectory(state.directorySnapshot()) {
		t.Fatal("the directory did not re-resolve what was already rendered")
	}
	model := state.snapshot()
	if model.Messages[0].Author != "Rafael Torres" || model.ThreadMessages[0].Author != "Dana Whitfield" {
		t.Fatalf("rendered authors = %q / %q", model.Messages[0].Author, model.ThreadMessages[0].Author)
	}
	if model.Members[0].Name != "Rafael Torres" {
		t.Fatalf("member name = %q, want the resolved one", model.Members[0].Name)
	}
	if model.Members[1].Name != "hc-099-4b7e2f" {
		t.Fatalf("an id the directory does not know became %q", model.Members[1].Name)
	}
	if state.applyChatDirectory(state.directorySnapshot()) {
		t.Fatal("a second pass reported a change with nothing left to resolve")
	}

	// A failed read is owed again rather than leaving the session on ids.
	state.releaseDirectoryRead()
	if !state.claimDirectoryRead() {
		t.Fatal("a failed directory read was not retried")
	}
}

func TestStaleChatDirectoryReadCannotCrossSessionReset(t *testing.T) {
	state := newChatStateForTest(t)
	oldConfig := journeyclient.Config{Tenant: "northwind", Subject: "avery", Bearer: "old-session-token", Locale: "en-US"}
	state.reset(nil, oldConfig, nil)
	oldEpoch, claimed := state.claimDirectoryReadFor(oldConfig)
	if !claimed {
		t.Fatal("fresh session did not claim directory read")
	}
	newConfig := journeyclient.Config{Tenant: "northwind", Subject: "avery", Bearer: "refreshed-session-token", Locale: "en-US"}
	state.reset(nil, newConfig, nil)
	if _, claimed := state.claimDirectoryReadFor(oldConfig); claimed {
		t.Fatal("an old request claimed the new session's directory latch")
	}
	if _, claimed := state.claimDirectoryReadFor(newConfig); !claimed {
		t.Fatal("new session did not claim its own directory read")
	}
	staleDirectory := map[string]string{"hc-050-rafael": "Rafael Torres"}
	if state.completeDirectoryRead(oldEpoch, oldConfig, staleDirectory, map[string]string{"hc-050": "https://old.example/photo"}, []chatui.SearchPerson{{ID: "hc-050-rafael", Name: "Rafael Torres"}}) {
		t.Fatal("stale directory response was accepted")
	}
	state.releaseDirectoryReadAt(oldEpoch, oldConfig)
	model := state.snapshot()
	if len(model.SearchDirectory) != 0 || len(model.PhotoURLs) != 0 || len(state.directorySnapshot()) != 0 {
		t.Fatalf("stale worker data crossed reset: people=%v photos=%v names=%v", model.SearchDirectory, model.PhotoURLs, state.directorySnapshot())
	}
	if state.claimDirectoryRead() {
		t.Fatal("stale completion cleared the new session's in-flight directory latch")
	}
}

func TestLoadedChatProjectionKeepsDirectoryThatCompletedDuringItsRPC(t *testing.T) {
	state := newChatStateForTest(t)
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Bearer: "session", Locale: "en-US"}
	state.reset(nil, cfg, nil)
	loadedBeforeDirectory := state.snapshot()
	generation := state.currentGeneration()
	epoch, claimed := state.claimDirectoryReadFor(cfg)
	if !claimed {
		t.Fatal("directory read was not claimed")
	}
	workers := []*journeyv1.Worker{{WorkerRef: "worker-anika-desai", WorkerId: "anika", SubjectId: "worker-anika-desai", PreferredName: "Anika", LegalName: "Desai"}}
	if !state.completeDirectoryRead(epoch, cfg, chatDirectoryFromWorkers(workers), nil, chatSearchDirectoryFromWorkers(workers)) {
		t.Fatal("directory completion was rejected")
	}
	if !state.adoptLoadedChatProjection(generation, loadedBeforeDirectory, chatCursor{}, false) {
		t.Fatal("in-flight chat listing was rejected")
	}
	people := chatSearchVisibleWorkers(state.snapshot().SearchDirectory, "Anika", 20)
	if len(people) != 1 || people[0].Name != "Anika" {
		t.Fatalf("directory completion was lost behind the older listing: %+v", people)
	}
}

func TestHumanizeChatSubjectIDReadsANameOrLeavesTheIdentifierAlone(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		// The live run's slug.
		{"hc-050-rafael-torres", "Rafael Torres"},
		{"hc-050-rafael-torres-jr", "Rafael Torres Jr"},
		{"rafael-torres", "Rafael Torres"},
		{"050-rafael", "Rafael"},
		{"RAFAEL-torres", "Rafael Torres"},
		{"avery.okafor@northwind.example", "Avery Okafor"},
		{"casey", "Casey"},
		// Nothing a name can be read out of is left exactly as it came: a
		// guessed name belongs to nobody.
		{"9f2a4c1e-7b33-4d90-a1f2-0c5e8b6d4417", "9f2a4c1e-7b33-4d90-a1f2-0c5e8b6d4417"},
		{"hc-050", "hc-050"},
		{"hc-050-8f1b9c", "hc-050-8f1b9c"},
		{"one-two-three-four-five", "one-two-three-four-five"},
		{"", ""},
		{"   ", "   "},
	} {
		if got := humanizeChatSubjectID(tc.id); got != tc.want {
			t.Errorf("%q humanized to %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestChatRailPutsTheRoomWithTheNewestMessageFirst(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	state := newChatStateForTest(t)
	state.noteChatActivity("comp-review", now.Add(-time.Hour))
	state.noteChatActivity("people-ops", now)
	// An older note never moves a room back down.
	state.noteChatActivity("people-ops", now.Add(-2*time.Hour))
	state.noteChatActivity("", now)
	state.noteChatActivity("ignored", time.Time{})

	rail := []chatui.Conversation{
		{ID: "zulu-quiet", Name: "Zulu"},
		{ID: "comp-review", Name: "Comp review"},
		{ID: "alpha-quiet", Name: "Alpha"},
		{ID: "people-ops", Name: "People ops"},
	}
	sortChatRail(rail, state.activitySnapshot())
	order := []string{rail[0].ID, rail[1].ID, rail[2].ID, rail[3].ID}
	want := []string{"people-ops", "comp-review", "alpha-quiet", "zulu-quiet"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("rail order = %v, want %v (newest message first, then the quiet rooms by name)", order, want)
		}
	}
	if _, ok := state.activitySnapshot()["ignored"]; ok {
		t.Fatal("a zero activity time was recorded")
	}
	// A delivered message moves its room to the top.
	state.noteChatActivity("zulu-quiet", now.Add(time.Minute))
	sortChatRail(rail, state.activitySnapshot())
	if rail[0].ID != "zulu-quiet" {
		t.Fatalf("after a new message the rail leads with %q, want zulu-quiet", rail[0].ID)
	}
}

func TestChatSendIntoANewRoomSurvivesAnotherRoomOpeningBehindIt(t *testing.T) {
	state := newChatStateForTest(t)

	// Room A is open and settled: five sends go straight out.
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "comp-review"
		model.Conversations = []chatui.Conversation{{ID: "comp-review", Name: "Comp review"}}
	})
	for i := 0; i < 5; i++ {
		target, queued := state.resolveSendTarget("comp-review", fmt.Sprintf("line %d", i))
		if queued || target != "comp-review" {
			t.Fatalf("settled send %d: target=%q queued=%v", i, target, queued)
		}
	}

	// Create B. Its open is in flight, and the reader types into it before it
	// resolves -- chatui still addressing the send from an older render.
	_, peopleGeneration := state.selectChatConversation("people-ops")
	target, queued := state.resolveSendTarget("comp-review", "Kicking off people ops")
	if !queued || target != "" {
		t.Fatalf("mid-open send: target=%q queued=%v, want it held for people-ops", target, queued)
	}

	// The seed creates a third room immediately, so another open claims the
	// marker before B's read comes back. This is what dropped the message: one
	// shared queue, and B's drain found it no longer its own.
	_, leadershipGeneration := state.selectChatConversation("leadership-private")
	if _, queued = state.resolveSendTarget("leadership-private", "Board prep"); !queued {
		t.Fatalf("the third room's send was not held")
	}

	// B resolves. It takes its own message and only its own, exactly once.
	pending := state.finishChatOpen("people-ops", peopleGeneration)
	if len(pending) != 1 || pending[0] != "Kicking off people ops" {
		t.Fatalf("people-ops drained %+v, want exactly its own message", pending)
	}
	if again := state.finishChatOpen("people-ops", peopleGeneration); again != nil {
		t.Fatalf("people-ops drained twice: %+v", again)
	}

	// The third room still has its own message waiting, untouched.
	pending = state.finishChatOpen("leadership-private", leadershipGeneration)
	if len(pending) != 1 || pending[0] != "Board prep" {
		t.Fatalf("leadership-private drained %+v, want exactly its own message", pending)
	}
	if got := state.discardQueuedChatSends(); got != nil {
		t.Fatalf("messages were left waiting after every room resolved: %+v", got)
	}
}

func TestChatQueuedSendComesBackToTheComposerWhenTheRoomNeverOpens(t *testing.T) {
	state := newChatStateForTest(t)

	// Typed before anything was selected at all: the first room to resolve
	// takes it rather than the message being lost.
	if _, queued := state.resolveSendTarget("", "before any room"); !queued {
		t.Fatal("a send with no room was not held")
	}
	state.mutate(func(model *chatui.Model) { model.SelectedID = "comp-review" })
	if pending := state.finishChatOpen("comp-review", state.currentGeneration()); len(pending) != 1 || pending[0] != "before any room" {
		t.Fatalf("the first room to resolve drained %+v", pending)
	}

	// A Create that fails leaves nothing to open, so the text goes back to the
	// composer rather than waiting for a room that will never exist.
	state.selectChatConversation("people-ops")
	if _, queued := state.resolveSendTarget("people-ops", "Kicking off people ops"); !queued {
		t.Fatal("the mid-open send was not held")
	}
	waiting := state.discardQueuedChatSends()
	if len(waiting) != 1 || waiting[0] != "Kicking off people ops" {
		t.Fatalf("discarded %+v, want the one held message", waiting)
	}
	state.restoreDraft("people-ops", waiting[0])
	if got := state.snapshot().Draft; got != "Kicking off people ops" {
		t.Fatalf("composer = %q, want the text back to retry", got)
	}
	if got := state.discardQueuedChatSends(); got != nil {
		t.Fatalf("the queue was drained twice: %+v", got)
	}
}

// chatReplayStream is a WatchConversation stream that replays a backlog and
// then goes quiet, which is what a room with history does.
type chatReplayStream struct {
	replay  []*chatv1.WatchConversationResponse
	next    int
	silence chan struct{}
	calls   int
}

func (s *chatReplayStream) Recv() (*chatv1.WatchConversationResponse, error) {
	s.calls++
	if s.next < len(s.replay) {
		message := s.replay[s.next]
		s.next++
		return message, nil
	}
	// Silence. A healthy server holds the stream open here; the client must
	// still be receiving.
	<-s.silence
	return nil, io.EOF
}

func TestDrainChatStreamKeepsReceivingAfterAReplayGoesQuiet(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	cursor := chatCursor{ConversationID: "comp-review", LastSequence: 5, Seen: map[string]uint64{}}
	for i := 1; i <= 5; i++ {
		cursor.Seen[fmt.Sprintf("p%d", i)] = uint64(i)
	}
	model := chatui.Model{State: chatui.StateReady}

	// The replay the live run saw: the room's five existing posts, delivered
	// with watch positions from the server's outbox counter -- nowhere near the
	// post sequences the client had read.
	replay := make([]*chatv1.WatchConversationResponse, 0, 6)
	for i := 1; i <= 5; i++ {
		replay = append(replay, &chatv1.WatchConversationResponse{
			Event: &chatv1.ConversationEvent{
				Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: uint64(91430 + i),
				Post: chatTestPost(fmt.Sprintf("p%d", i), uint64(i), "avery", "replayed", "", now),
			},
			ResumeCursor: fmt.Sprintf("cursor-%d", i),
		})
	}
	// A message with no event at all: a resume-cursor heartbeat is not an end.
	replay = append(replay, &chatv1.WatchConversationResponse{ResumeCursor: "cursor-5"})

	stream := &chatReplayStream{replay: replay, silence: make(chan struct{})}
	done := make(chan chatStreamEnd, 1)
	go func() {
		_, reason, _ := drainChatStream(context.Background(), stream, chatStreamHooks{
			Apply: func(event *chatv1.ConversationEvent, resume string) (chatEventOutcome, bool) {
				outcome := applyChatEvent(&model, &cursor, event, "en-US", nil, now)
				if outcome == chatEventApplied && resume != "" {
					cursor.Resume = resume
				}
				return outcome, true
			},
		})
		done <- reason
	}()

	select {
	case reason := <-done:
		t.Fatalf("the drain returned %q instead of waiting on a quiet stream; this is the give-up loop", reason)
	case <-time.After(2 * time.Second):
		// Still receiving, which is the whole assertion.
	}
	if stream.calls != len(replay)+1 {
		t.Fatalf("Recv called %d times, want every replayed message plus the one it is blocked on", stream.calls)
	}
	// Every replayed creation was already on screen, so nothing was applied --
	// but the watch position still moved past them, or every reconnect would
	// replay the whole backlog again.
	if cursor.StreamSequence != 91435 || !cursor.Baselined {
		t.Fatalf("watch position = %d baselined=%v, want the server's own position adopted", cursor.StreamSequence, cursor.Baselined)
	}
	if cursor.LastSequence != 5 {
		t.Fatalf("post sequence = %d, want 5: the two spaces must not be conflated", cursor.LastSequence)
	}
	if len(model.Messages) != 0 {
		t.Fatalf("the replay duplicated posts already on screen: %+v", model.Messages)
	}

	// Ending the silence ends the drain, as a transport end and nothing else.
	close(stream.silence)
	select {
	case reason := <-done:
		if reason != chatStreamEndTransport {
			t.Fatalf("end reason = %q, want transport", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the drain did not return when the stream ended")
	}
}

func TestDrainChatStreamStopsOnlyForAGapOrASupersededSubscription(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	message := &chatv1.WatchConversationResponse{Event: &chatv1.ConversationEvent{
		Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: 9,
		Post: chatTestPost("p9", 9, "avery", "next", "", now),
	}}
	for _, tc := range []struct {
		name    string
		outcome chatEventOutcome
		active  bool
		want    chatStreamEnd
	}{
		{"gap", chatEventGap, true, chatStreamEndGap},
		{"superseded", chatEventApplied, false, chatStreamEndSuperseded},
	} {
		stream := &chatReplayStream{replay: []*chatv1.WatchConversationResponse{message}, silence: make(chan struct{})}
		close(stream.silence)
		_, reason, _ := drainChatStream(context.Background(), stream, chatStreamHooks{
			Apply: func(*chatv1.ConversationEvent, string) (chatEventOutcome, bool) { return tc.outcome, tc.active },
		})
		if reason != tc.want {
			t.Errorf("%s: end reason = %q, want %q", tc.name, reason, tc.want)
		}
	}
	// An applied event runs its work and keeps going.
	applied := 0
	stream := &chatReplayStream{replay: []*chatv1.WatchConversationResponse{message, message}, silence: make(chan struct{})}
	close(stream.silence)
	delivered, reason, _ := drainChatStream(context.Background(), stream, chatStreamHooks{
		Apply:   func(*chatv1.ConversationEvent, string) (chatEventOutcome, bool) { return chatEventApplied, true },
		Applied: func(*chatv1.ConversationEvent) { applied++ },
	})
	if !delivered || reason != chatStreamEndTransport || applied != 2 {
		t.Fatalf("delivered=%v reason=%q applied=%d, want both events applied and a transport end", delivered, reason, applied)
	}
}

func TestChatStreamGiveUpNeedsBothTheAttemptsAndThirtySeconds(t *testing.T) {
	start := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	if chatStreamShouldGiveUp(chatStreamMaxAttempts, start, start.Add(time.Second)) {
		t.Fatal("five failures in a second showed the notice; a blip must never flash it")
	}
	if chatStreamShouldGiveUp(chatStreamMaxAttempts-1, start, start.Add(time.Hour)) {
		t.Fatal("too few attempts gave up")
	}
	if chatStreamShouldGiveUp(chatStreamMaxAttempts, time.Time{}, start.Add(time.Hour)) {
		t.Fatal("gave up with no run of failures recorded")
	}
	if !chatStreamShouldGiveUp(chatStreamMaxAttempts, start, start.Add(chatStreamGiveUpFloor)) {
		t.Fatal("a sustained outage never reached the notice")
	}
}

func TestFilterChatBrowseNarrowsTheListingWithoutAskingTheServer(t *testing.T) {
	all := []chatui.Conversation{
		{ID: "c1", Name: "People ops", Topic: "Hiring and onboarding"},
		{ID: "c2", Name: "Announcements"},
		{ID: "c3", Name: "Comp review", Topic: "People decisions"},
	}
	if got := filterChatBrowse(all, ""); len(got) != 3 {
		t.Fatalf("an empty query returned %d rows, want the whole listing", len(got))
	}
	if got := filterChatBrowse(all, "  "); len(got) != 3 {
		t.Fatalf("a blank query returned %d rows, want the whole listing", len(got))
	}
	got := filterChatBrowse(all, "PEOPLE")
	if len(got) != 2 || got[0].ID != "c1" || got[1].ID != "c3" {
		t.Fatalf("query matched %+v, want the name and the topic hit, case-insensitively", got)
	}
	if got := filterChatBrowse(all, "nothing here"); len(got) != 0 {
		t.Fatalf("a query with no matches returned %+v", got)
	}

	state := newChatStateForTest(t)
	state.setChatBrowse(all)
	if model := state.snapshot(); len(model.Browse) != 3 || model.BrowseQuery != "" {
		t.Fatalf("browse = %d rows query=%q, want the whole listing", len(model.Browse), model.BrowseQuery)
	}
	model := state.filterChatBrowseQuery("announce")
	if len(model.Browse) != 1 || model.Browse[0].ID != "c2" || model.BrowseQuery != "announce" {
		t.Fatalf("filtered browse = %+v query=%q", model.Browse, model.BrowseQuery)
	}
	// The full listing is kept, so clearing the query restores it with no RPC.
	if model = state.filterChatBrowseQuery(""); len(model.Browse) != 3 {
		t.Fatalf("clearing the query left %d rows, want the whole listing back", len(model.Browse))
	}
	// A listing that arrives while a query is typed shows only what matches.
	state.filterChatBrowseQuery("comp")
	if model = state.setChatBrowse(all); len(model.Browse) != 1 || model.Browse[0].ID != "c3" {
		t.Fatalf("a new listing under a live query showed %+v", model.Browse)
	}
}

func TestChatIdentityResolvesToOneNameEverywhere(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	state := newChatStateForTest(t)
	ref := "hc-050-rafael-torres"

	// Before the directory: everything reads the humanized reference, and it is
	// the same string in every place.
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = []chatui.Message{{ID: "p1", AuthorID: ref, Author: chatDisplayName(nil, ref)}}
		model.ThreadMessages = []chatui.Message{{ID: "p2", AuthorID: ref, Author: chatDisplayName(nil, ref)}}
		model.Members = []chatui.Member{{ID: ref, Name: chatDisplayName(nil, ref)}}
		model.Conversations = []chatui.Conversation{{ID: ref, Name: chatDisplayName(nil, ref)}}
	})
	state.setChatBrowse([]chatui.Conversation{{ID: ref, Name: chatDisplayName(nil, ref)}})
	before := state.snapshot()
	if before.Messages[0].Author != "Rafael Torres" {
		t.Fatalf("pre-directory author = %q", before.Messages[0].Author)
	}

	// The directory arrives with a preferred name that differs from the
	// humanized reference. Every surface moves to it together: the critic saw
	// "Rafael Torres" on a message and "Rafa Torres" in the member list because
	// only some of them were re-resolved.
	directory := chatDirectoryFromWorkers([]*journeyv1.Worker{
		{WorkerRef: ref, SubjectId: ref, PreferredName: "Rafa", LegalName: "Rafael Torres"},
	})
	state.mergeDirectory(directory)
	if !state.applyChatDirectory(state.directorySnapshot()) {
		t.Fatal("the directory did not re-resolve the rendered names")
	}
	after := state.snapshot()
	names := map[string]string{
		"message author": after.Messages[0].Author,
		"thread author":  after.ThreadMessages[0].Author,
		"member row":     after.Members[0].Name,
		"rail row":       after.Conversations[0].Name,
		"browse row":     after.Browse[0].Name,
	}
	for where, got := range names {
		if got != "Rafa Torres" {
			t.Errorf("%s = %q, want the one resolved name %q", where, got, "Rafa Torres")
		}
	}
	// And the resolver a fresh projection would use agrees with all of them.
	if got := chatMessage(chatTestPost("p3", 3, ref, "later", "", now), "en-US", state.directorySnapshot(), now).Author; got != "Rafa Torres" {
		t.Fatalf("a newly projected message reads %q, want the same name", got)
	}
	if got := chatDisplayName(state.directorySnapshot(), ref); got != "Rafa Torres" {
		t.Fatalf("the resolver itself returns %q", got)
	}
}

func TestChatMediaReferencesBecomeAttachmentsWithNoURLYet(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	post := chatTestPost("p1", 1, "avery", "Here is the offsite clip", "", now)
	post.References = []*chatv1.Reference{
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "art-1", Display: "offsite.gif"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION, Id: "hc-050", Display: "Rafael"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "art-2", Display: "handbook.pdf"},
		// A repeat of the same artifact is one attachment.
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "art-1", Display: "offsite.gif"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "   "},
		nil,
	}

	attachments := chatMediaAttachments(post)
	if len(attachments) != 2 {
		t.Fatalf("attachments = %+v, want the two distinct artifacts", attachments)
	}
	if attachments[0].ID != "art-1" || attachments[0].Name != "offsite.gif" {
		t.Fatalf("first attachment = %+v", attachments[0])
	}
	if !attachments[0].IsImage() || !attachments[0].IsGIF() {
		t.Fatalf("a .gif is not renderable inline: %+v", attachments[0])
	}
	if attachments[1].IsImage() || attachments[1].ContentType != "application/pdf" {
		t.Fatalf("a .pdf rendered as an image: %+v", attachments[1])
	}
	// The URL is the grant's job. A timeline that waited for it would not draw.
	for _, attachment := range attachments {
		if attachment.URL != "" {
			t.Fatalf("attachment %q arrived with a URL before any grant", attachment.ID)
		}
	}
	// A reference with no display falls back to the artifact id, and an
	// unknown extension renders as a chip rather than a broken image.
	post.References = []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "art-3"}}
	only := chatMediaAttachments(post)
	if len(only) != 1 || only[0].Name != "art-3" || only[0].ContentType != "" || only[0].IsImage() {
		t.Fatalf("unnamed attachment = %+v", only)
	}
	// The projection carries them, and a post with no media carries none.
	if got := chatMessage(post, "en-US", nil, now).Attachments; len(got) != 1 {
		t.Fatalf("the projection dropped the attachment: %+v", got)
	}
	if got := chatMessage(chatTestPost("p2", 2, "avery", "text only", "", now), "en-US", nil, now).Attachments; got != nil {
		t.Fatalf("a post with no media carries %+v", got)
	}
}

func TestChatMediaGrantsAreCachedPerArtifactUntilTheyExpireOrAreRefused(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 0, 0, time.UTC)
	cache := newChatMediaGrants()

	if _, ok := cache.Get("art-1", now); ok {
		t.Fatal("an empty cache produced a grant")
	}
	// The first render claims the fetch; a second render of the same timeline
	// must not fetch the same artifact again.
	if !cache.Claim("art-1", now) {
		t.Fatal("the first render did not claim the fetch")
	}
	if cache.Claim("art-1", now) {
		t.Fatal("a second render fetched the same artifact again")
	}
	if cache.Claim("", now) {
		t.Fatal("an empty artifact id was claimed")
	}

	cache.Put("art-1", chatMediaGrant{Token: "g1", ExpiresAt: now.Add(5 * time.Minute), URL: "blob:one"})
	grant, ok := cache.Get("art-1", now)
	if !ok || grant.Token != "g1" || grant.URL != "blob:one" {
		t.Fatalf("cached grant = %+v ok=%v", grant, ok)
	}
	// With a grant in hand nothing fetches again: the same image in three
	// conversations is one artifact and one grant.
	if cache.Claim("art-1", now) {
		t.Fatal("a cached grant was fetched again")
	}

	// It is spent before its stated expiry, by the skew: the server minted
	// against a different clock and a grant that dies mid-fetch costs a 403.
	if _, ok = cache.Get("art-1", now.Add(5*time.Minute-chatGrantSkew)); ok {
		t.Fatal("a grant inside the skew window was still offered")
	}
	if !cache.Claim("art-1", now.Add(5*time.Minute)) {
		t.Fatal("an expired grant was not re-fetched")
	}

	// A 403 drops it whatever the stated expiry said: the server may revoke
	// access before then, and the expiry is the only thing the client could
	// otherwise believe.
	cache.Put("art-1", chatMediaGrant{Token: "g2", ExpiresAt: now.Add(time.Hour), URL: "blob:two"})
	cache.Invalidate("art-1")
	if _, ok = cache.Get("art-1", now); ok {
		t.Fatal("a refused grant survived")
	}
	if !cache.Claim("art-1", now) {
		t.Fatal("a refused artifact could not be re-fetched")
	}
	// A claim that produced nothing is released so the next render retries.
	cache.Release("art-1")
	if !cache.Claim("art-1", now) {
		t.Fatal("a released claim blocked the retry")
	}
	// Artifacts are independent.
	cache.Put("art-2", chatMediaGrant{Token: "g3", ExpiresAt: now.Add(time.Hour), URL: "blob:three"})
	if grant, ok = cache.Get("art-2", now); !ok || grant.URL != "blob:three" {
		t.Fatalf("second artifact = %+v ok=%v", grant, ok)
	}
}

func TestChatMediaCacheClearsURLsAndRejectsLateFetches(t *testing.T) {
	now := time.Now()
	cache := newChatMediaGrants()
	var revoked []string
	cache.SetRevoker(func(raw string) { revoked = append(revoked, raw) })
	epoch, claimed := cache.ClaimEpoch("old", now)
	if !claimed || !cache.PutIfEpoch("old", chatMediaGrant{Token: "grant", URL: "blob:old", ExpiresAt: now.Add(time.Hour)}, epoch) {
		t.Fatal("first fetch did not land")
	}
	cache.Clear()
	if len(revoked) != 1 || revoked[0] != "blob:old" {
		t.Fatalf("revoked = %v", revoked)
	}
	if cache.PutIfEpoch("late", chatMediaGrant{Token: "stale", URL: "blob:late"}, epoch) || cache.ApplyIfEpoch("late", epoch, func() bool { t.Fatal("stale apply ran"); return true }) {
		t.Fatal("late fetch survived clear")
	}
	newEpoch, claimed := cache.ClaimEpoch("new", now)
	if !claimed || newEpoch == epoch || !cache.PutIfEpoch("new", chatMediaGrant{Token: "fresh", URL: "blob:new", ExpiresAt: now.Add(time.Hour)}, newEpoch) {
		t.Fatal("new fetch was blocked")
	}
	cache.Invalidate("new")
	if len(revoked) != 2 || revoked[1] != "blob:new" {
		t.Fatalf("revoked after invalidation = %v", revoked)
	}
}

func TestChatMediaCacheBoundsVisibleBytesAndEntries(t *testing.T) {
	now := time.Now()
	cache := newChatMediaGrants()
	var revoked []string
	cache.SetRevoker(func(raw string) { revoked = append(revoked, raw) })
	ids := make([]string, 40)
	for i := range ids {
		ids[i] = fmt.Sprintf("artifact-%d", i)
	}
	cache.SetWanted(ids)
	for i, id := range ids {
		epoch, claimed := cache.ClaimEpoch(id, now)
		if i >= chatMediaCacheMaxItems {
			if claimed {
				t.Fatalf("off-window artifact %s fetched", id)
			}
			continue
		}
		if !claimed || !cache.PutIfEpoch(id, chatMediaGrant{Token: "grant", URL: "blob:" + id, Bytes: 3 << 20, ExpiresAt: now.Add(time.Hour)}, epoch) {
			t.Fatalf("visible artifact %s not cached", id)
		}
	}
	if len(cache.grants) > chatMediaCacheMaxItems || cache.byteSize() > chatMediaCacheMaxBytes {
		t.Fatalf("cache grew to %d entries / %d bytes", len(cache.grants), cache.byteSize())
	}
	if len(revoked) == 0 {
		t.Fatal("byte bound evicted no object URLs")
	}
	cache.SetWanted([]string{ids[0]})
	if len(cache.grants) > 1 {
		t.Fatalf("off-window grants retained: %d", len(cache.grants))
	}
}

func TestChatMediaURLsLandOnEveryMessageThatReferencesTheArtifact(t *testing.T) {
	messages := []chatui.Message{
		{ID: "p1", Attachments: []chatui.Attachment{{ID: "art-1", Name: "offsite.gif"}}},
		{ID: "p2", Attachments: []chatui.Attachment{{ID: "art-2"}, {ID: "art-1"}}},
		{ID: "p3"},
	}
	if got := chatAttachmentsNeedingGrants(messages); len(got) != 2 || got[0] != "art-1" || got[1] != "art-2" {
		t.Fatalf("pending artifacts = %+v, want each distinct one once", got)
	}
	if !applyChatMediaURL(messages, "art-1", "blob:one") {
		t.Fatal("the URL was not applied")
	}
	if messages[0].Attachments[0].URL != "blob:one" || messages[1].Attachments[1].URL != "blob:one" {
		t.Fatalf("the URL did not reach every message: %+v", messages)
	}
	if messages[1].Attachments[0].URL != "" {
		t.Fatal("another artifact picked up the URL")
	}
	if applyChatMediaURL(messages, "art-1", "blob:one") {
		t.Fatal("re-applying the same URL reported a change")
	}
	if applyChatMediaURL(messages, "art-1", "") || applyChatMediaURL(messages, "", "blob:one") {
		t.Fatal("an empty id or URL was applied")
	}
	// Only what is still missing is fetched next time.
	if got := chatAttachmentsNeedingGrants(messages); len(got) != 1 || got[0] != "art-2" {
		t.Fatalf("pending after one resolved = %+v", got)
	}
}

// A replayed creation for a post the loaded page never returned, at a
// sequence the page already covered, is purged history (the outbox outlives
// the post): it must be dropped, not appended as a second copy.
func TestClassifyChatEventDropsCreationsBelowLoadedHistoryWithUnknownIDs(t *testing.T) {
	cursor := chatCursor{LastSequence: 34, Seen: map[string]uint64{"real-1": 1, "real-34": 34}}
	if got := classifyChatEvent(cursor, "ghost-1", 1, 900, true); got != chatEventDuplicate {
		t.Fatalf("unknown creation below loaded history = %v, want duplicate", got)
	}
	if got := classifyChatEvent(cursor, "real-1", 1, 901, true); got != chatEventDuplicate {
		t.Fatalf("known creation = %v, want duplicate", got)
	}
	// Above the loaded history the event is genuinely new and follows the
	// stream's own ordering rules.
	if got := classifyChatEvent(cursor, "new-35", 35, 902, true); got == chatEventDuplicate {
		t.Fatalf("creation above loaded history was dropped")
	}
	// Edits and deletes are never about a creation and keep their meaning.
	if got := classifyChatEvent(cursor, "real-1", 1, 903, false); got == chatEventDuplicate {
		t.Fatalf("edit below loaded history was dropped")
	}
}

func TestChatWorkerDisplayNameCompletesAFirstNameWithTheSurname(t *testing.T) {
	cases := map[[2]string]string{
		{"Rafael", "Rafael Torres"}:      "Rafael Torres",
		{"Bob", "Robert Smith"}:          "Bob Smith",
		{"Anika Desai", "Anika R Desai"}: "Anika Desai",
		{"", "Evelyn Morgan"}:            "Evelyn Morgan",
		{"Cher", ""}:                     "Cher",
		{"Madonna", "Madonna"}:           "Madonna",
		{"Torres", "Rafael Torres"}:      "Torres",
	}
	for in, want := range cases {
		if got := chatWorkerDisplayName(in[0], in[1]); got != want {
			t.Errorf("chatWorkerDisplayName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestReplayedPostKeepsChipsViewerFlagAndAttachmentURLs(t *testing.T) {
	current := chatui.Message{ID: "p1", Body: "old", Revision: 1, Replies: 2, Reactions: 3, Reacted: true, Pinned: true,
		Chips:       []chatui.ReactionChip{{Emoji: "👍", Count: 2, Mine: true}, {Emoji: "🎉", Count: 1}},
		Attachments: []chatui.Attachment{{ID: "art-1", URL: "blob:one"}}}
	next := chatui.Message{ID: "p1", Body: "old", Revision: 1, Attachments: []chatui.Attachment{{ID: "art-1"}}}
	got := carryChatLocalState(current, next)
	if len(got.Chips) != 2 || !got.Reacted || got.Reactions != 3 || got.Replies != 2 || !got.Pinned {
		t.Fatalf("local state dropped on replay: %+v", got)
	}
	if got.Attachments[0].URL != "blob:one" {
		t.Fatalf("attachment URL dropped on replay: %+v", got.Attachments)
	}
	edited := chatui.Message{ID: "p1", Body: "new", Revision: 2}
	if got := carryChatLocalState(current, edited); got.Body != "new" || len(got.Chips) != 2 {
		t.Fatalf("edit lost the body or the chips: %+v", got)
	}
	stale := chatui.Message{ID: "p1", Body: "older", Revision: 0}
	if got := carryChatLocalState(current, stale); got.Body != "old" {
		t.Fatalf("a stale revision replaced the newer message: %+v", got)
	}
}

func TestChatOwnReactionRPCAndStreamCountOnceInEitherOrder(t *testing.T) {
	for _, streamFirst := range []bool{false, true} {
		for _, added := range []bool{true, false} {
			name := fmt.Sprintf("streamFirst=%t/added=%t", streamFirst, added)
			t.Run(name, func(t *testing.T) {
				initial := []chatui.ReactionChip{{Emoji: "🎉", Count: 2, Mine: !added}}
				if added {
					initial[0].Count = 1 // another viewer's reaction remains
				}
				s := &chatState{model: chatui.Model{CurrentUser: "me", Messages: []chatui.Message{{ID: "p1", Chips: initial}}}}
				delta := -1
				if added {
					delta = 1
				}
				event := &chatv1.ConversationEvent{Reaction: &chatv1.Reaction{PostId: "p1", SubjectId: "me", Emoji: "🎉"}, Removed: !added}
				rpc := func() { s.adjustReaction("p1", "🎉", delta, true) }
				stream := func() { applyChatReactionChanged(&s.model, event, "me") }
				if streamFirst {
					stream()
					rpc()
				} else {
					rpc()
					stream()
				}
				// A repeated completion or replay must also leave the count alone.
				rpc()
				stream()
				got := s.model.Messages[0].Chips
				wantCount := 1
				if added {
					wantCount = 2
				}
				if len(got) != 1 || got[0].Count != wantCount || got[0].Mine != added {
					t.Fatalf("own reaction counted more than once: %+v", got)
				}
				cached := s.reactionSnapshot()["p1"]
				if len(cached) != 1 || cached[0].Count != wantCount || cached[0].Mine != added {
					t.Fatalf("reaction cache diverged from stream and RPC state: %+v", cached)
				}
			})
		}
	}
}

func TestChatOtherViewerReactionStillChangesCount(t *testing.T) {
	model := chatui.Model{Messages: []chatui.Message{{ID: "p1", Chips: []chatui.ReactionChip{{Emoji: "🎉", Count: 2, Mine: true}}}}}
	applyChatReactionChanged(&model, &chatv1.ConversationEvent{Reaction: &chatv1.Reaction{PostId: "p1", SubjectId: "other", Emoji: "🎉"}}, "me")
	if got := model.Messages[0].Chips[0]; got.Count != 3 || !got.Mine {
		t.Fatalf("other viewer's reaction did not increase count while preserving mine: %+v", got)
	}
	applyChatReactionChanged(&model, &chatv1.ConversationEvent{Reaction: &chatv1.Reaction{PostId: "p1", SubjectId: "other", Emoji: "🎉"}, Removed: true}, "me")
	if got := model.Messages[0].Chips[0]; got.Count != 2 || !got.Mine {
		t.Fatalf("other viewer's removal changed own reaction: %+v", got)
	}
}

func TestChatReactionStreamReplayMatchesLoadedMembership(t *testing.T) {
	state := &chatState{
		cfg:        journeyclient.Config{Tenant: "home-a"},
		generation: 4,
		cursor:     chatCursor{ConversationID: "c1", Seen: map[string]uint64{}},
		model: chatui.Model{
			CurrentUser: "me",
			SelectedID:  "c1",
			Messages: []chatui.Message{{ID: "p1", Chips: []chatui.ReactionChip{
				{Emoji: "❤️", Count: 1}, {Emoji: "🎉", Count: 1}, {Emoji: "👀", Count: 1},
			}}},
		},
		reactionMembers: map[string]map[chatReactionIdentity]struct{}{
			"p1": {
				{homeTenantID: "home-a", subjectID: "mateo", emoji: "❤️"}: {},
				{homeTenantID: "home-a", subjectID: "owen", emoji: "🎉"}:   {},
				{homeTenantID: "home-a", subjectID: "micah", emoji: "👀"}:  {},
			},
		},
	}
	for sequence, item := range []struct{ subject, emoji string }{{"mateo", "❤️"}, {"owen", "🎉"}, {"micah", "👀"}} {
		event := &chatv1.ConversationEvent{
			Kind:     chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED,
			Sequence: uint64(sequence + 1),
			Reaction: &chatv1.Reaction{PostId: "p1", HomeTenantId: "home-a", SubjectId: item.subject, Emoji: item.emoji},
		}
		if outcome, active := state.applyStreamEvent(state.generation, "c1", event, "en-US", nil, time.Now(), "resume"); outcome != chatEventApplied || !active {
			t.Fatalf("stream replay outcome = %v, active = %t", outcome, active)
		}
	}
	chips := state.model.Messages[0].Chips
	if len(chips) != 3 {
		t.Fatalf("replay changed reaction chips: %+v", chips)
	}
	for _, chip := range chips {
		if chip.Count != 1 {
			t.Errorf("replayed %s reaction count = %d, want 1", chip.Emoji, chip.Count)
		}
	}
	if got := state.model.Messages[0].Reactions; got != 3 {
		t.Fatalf("total after replay = %d, want 3", got)
	}

	newEvent := &chatv1.ConversationEvent{
		Kind:     chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED,
		Sequence: 4,
		Reaction: &chatv1.Reaction{PostId: "p1", HomeTenantId: "home-a", SubjectId: "new-person", Emoji: "❤️"},
	}
	if outcome, _ := state.applyStreamEvent(state.generation, "c1", newEvent, "en-US", nil, time.Now(), "resume"); outcome != chatEventApplied {
		t.Fatalf("new reaction outcome = %v, want applied", outcome)
	}
	if got := state.model.Messages[0].Chips[0].Count; got != 2 {
		t.Fatalf("new member reaction count = %d, want 2", got)
	}
}
func TestChatMembershipDisplayNameKeepsTenantIdentity(t *testing.T) {
	directory := map[string]string{"shared-subject": "Host Worker"}
	if got := chatMembershipDisplayName(directory, "shared-subject", "guest-tenant", "host-tenant"); got != "" {
		t.Fatalf("foreign subject collision resolved to %q", got)
	}
	if got := chatMembershipDisplayName(directory, "shared-subject", "host-tenant", "host-tenant"); got != "Host Worker" {
		t.Fatalf("same-tenant member name = %q", got)
	}
	if got := chatMembershipDisplayName(nil, "9f2a4c1e-7b33-4d90-a1f2-0c5e8b6d4417", "host-tenant", "host-tenant"); got != "" {
		t.Fatalf("opaque ID presented as name: %q", got)
	}
}
