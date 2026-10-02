//go:build !(js && wasm)

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_014_ChannelStatusBatchClient: the channel statuses of the
// list are read in one request, a refusal is kept apart from a read that got no
// answer, and nothing the server was not asked for is taken from its answer.
func TestTodo_CHATBUG_014_ChannelStatusBatchClient(t *testing.T) {
	requests := 0
	snapshot := func(tenant, id string, status chatpolicy.ChannelStatus) *chat.ChannelStatusSnapshot {
		return &chat.ChannelStatusSnapshot{Status: chat.ChannelStatus{TenantID: tenant, ConversationID: id, Revision: 4, Status: status}, CanPost: status == chatpolicy.StatusOpen}
	}
	client := chatstateClient{BaseURL: "http://fixture.invalid/", Bearer: "fixture", Tenant: "tenant", HTTP: &http.Client{Transport: chatstateRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture" || r.URL.Path != "/api/chat/v1/channel-status/" {
			t.Fatalf("batch went to %s %s with authorization %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if got := r.URL.Query().Get("conversations"); got != "general,engineering,secret,forged,quiet" {
			t.Fatalf("batch asked for %q", got)
		}
		w := httptest.NewRecorder()
		_ = json.NewEncoder(w).Encode([]chatstateBatchItem{
			{ConversationID: "general", Snapshot: snapshot("tenant", "general", chatpolicy.StatusOpen)},
			{ConversationID: "engineering", Snapshot: snapshot("tenant", "engineering", chatpolicy.StatusLocked)},
			{ConversationID: "secret", Code: "permission_denied"},
			// A snapshot of another tenant, one that is not this conversation's,
			// one nobody asked for, and a second answer for a conversation.
			{ConversationID: "forged", Snapshot: snapshot("other-tenant", "forged", chatpolicy.StatusOpen)},
			{ConversationID: "uninvited", Snapshot: snapshot("tenant", "uninvited", chatpolicy.StatusOpen)},
			{ConversationID: "general", Snapshot: snapshot("tenant", "general", chatpolicy.StatusLocked)},
		})
		return w.Result(), nil
	})}}

	previous := map[string]chatui.ChannelStatusView{"engineering": {Status: chat.ChannelStatus{Revision: 1}, CanPost: true}}
	views, refused := client.LoadMany(t.Context(), []string{"general", "engineering", "secret", "forged", "quiet"}, previous)
	if requests != 1 {
		t.Fatalf("five statuses took %d requests, want one", requests)
	}
	if len(views) != 2 || views["general"].Status.Status != chatpolicy.StatusOpen || !views["general"].CanPost {
		t.Fatalf("views = %+v", views)
	}
	if locked := views["engineering"]; locked.Status.Status != chatpolicy.StatusLocked || locked.CanPost || !locked.Updated || locked.Unavailable {
		t.Fatalf("engineering = %+v, want the locked status on top of the view it had", locked)
	}
	if len(refused) != 1 || !chatux012StatusFinal(refused["secret"]) {
		t.Fatalf("refused = %+v, want the one final refusal", refused)
	}
	// "forged" and "quiet" were not answered: the caller reads them itself.
	for _, id := range []string{"forged", "quiet", "uninvited"} {
		if _, ok := views[id]; ok {
			t.Fatalf("%q was taken from the batch", id)
		}
		if _, ok := refused[id]; ok {
			t.Fatalf("%q was recorded as refused", id)
		}
	}

	// A batch that fails answers for nothing, so every channel is read singly.
	client.HTTP.Transport = chatstateRoundTrip(func(*http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_argument"}`))
		return w.Result(), nil
	})
	if views, refused := client.LoadMany(t.Context(), []string{"general", "engineering"}, nil); len(views) != 0 || len(refused) != 0 {
		t.Fatalf("a failed batch answered %+v / %+v", views, refused)
	}

	// A long list is asked for in pieces the server accepts.
	var sizes []int
	client.HTTP.Transport = chatstateRoundTrip(func(r *http.Request) (*http.Response, error) {
		sizes = append(sizes, len(strings.Split(r.URL.Query().Get("conversations"), ",")))
		w := httptest.NewRecorder()
		_, _ = w.Write([]byte(`[]`))
		return w.Result(), nil
	})
	many := make([]string, chatstateBatchLimit+7)
	for i := range many {
		many[i] = "room-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	client.LoadMany(t.Context(), many, nil)
	if !reflect.DeepEqual(sizes, []int{chatstateBatchLimit, 7}) {
		t.Fatalf("a list of %d was asked for in pieces of %v", len(many), sizes)
	}

	// Which channels a batch asks for: channels only, not the open one, not one
	// already read, and each once.
	rooms := []chatui.Conversation{
		{ID: "general", Kind: chatui.PublicChannel}, {ID: "open", Kind: chatui.PublicChannel}, {ID: "dm", Kind: chatui.DirectMessage},
		{ID: "read", Kind: chatui.PrivateChannel}, {ID: "private", Kind: chatui.PrivateChannel}, {ID: "general", Kind: chatui.PublicChannel}, {Kind: chatui.PublicChannel},
	}
	if got := chatperf2StatusRooms(rooms, map[string]chatui.ChannelStatusView{"read": {}}, "open"); !reflect.DeepEqual(got, []string{"general", "private"}) {
		t.Fatalf("channels to read = %v", got)
	}

	// The page uses the batch before the single reads.
	directory := chatperfBody(t, "integrate2_browser_wasm.go", "func integrate2SyncStatusDirectory(")
	chatperfInOrder(t, "integrate2SyncStatusDirectory", directory,
		"chatperf2StatusRooms(rooms, views, m.SelectedID)",
		"client.LoadMany(call, wanted, m.ChannelStatuses)",
		"batchViews[room.ID]",
		"batchRefused[room.ID]",
		"client.Load(call, room.ID, m.ChannelStatuses[room.ID])",
	)
}

// TestTodo_CHATBUG_014_OnePersonaRead: the agent directory of the open
// conversation is read once when Chat opens. The first load leaves the agent
// reads to the open that follows it, the activity watch's first answer asks for
// nothing, and the requests of a stream's replay share one read.
func TestTodo_CHATBUG_014_OnePersonaRead(t *testing.T) {
	// The watch asks for the directory when the set of answer posts changes,
	// and not for the first set it sees.
	for name, tc := range map[string]struct {
		first       bool
		known, next string
		want        bool
	}{
		"the first answer, with posts":  {true, "", "post-1", false},
		"the first answer, with none":   {true, "", "", false},
		"the same posts again":          {false, "post-1", "post-1", false},
		"a new answer post":             {false, "post-1", "post-1\x00post-2", true},
		"the first post after an empty": {false, "", "post-1", true},
	} {
		if got := chatperf2DirectoryStale(tc.first, tc.known, tc.next); got != tc.want {
			t.Errorf("%s: stale = %v, want %v", name, got, tc.want)
		}
	}

	// A replay's requests (18 memberships and 85 posts over two seconds on the
	// review data) are one read, made when they have paused.
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	clock := &chatperfClock{now: start}
	var reads []time.Duration
	refresh := newChatperfRenderCoalescer(clock.schedule, func() time.Time { return clock.now }, chatperf2DirectoryPace,
		func() { reads = append(reads, clock.now.Sub(start)) })
	for i := 0; i < 103; i++ {
		clock.advance(start.Add(time.Duration(i) * 20 * time.Millisecond))
		refresh.Schedule()
	}
	clock.advance(start.Add(10 * time.Second))
	if len(reads) != 1 {
		t.Fatalf("a replay of 103 events over two seconds read the directory %d times at %v, want once", len(reads), reads)
	}
	if last := 102 * 20 * time.Millisecond; reads[0] < last || reads[0] > last+chatperf2DirectoryQuiet+10*time.Millisecond {
		t.Fatalf("the read came at %v; the last request was at %v and is answered within the pause after it", reads[0], last)
	}
	// One request on its own waits the half second it always did, and a burst
	// that never pauses is still read within the limit.
	reads = nil
	late := start.Add(time.Minute)
	clock.advance(late)
	refresh.Schedule()
	clock.advance(late.Add(chatperf2DirectoryQuiet))
	if len(reads) != 1 {
		t.Fatalf("a single request read the directory %d times within %v", len(reads), chatperf2DirectoryQuiet)
	}
	reads = nil
	steady := start.Add(2 * time.Minute)
	for i := 0; i < 60; i++ {
		clock.advance(steady.Add(time.Duration(i) * 100 * time.Millisecond))
		refresh.Schedule()
	}
	if len(reads) != 1 || reads[0] > steady.Add(chatperf2DirectoryLimit+10*time.Millisecond).Sub(start) {
		t.Fatalf("six seconds of steady requests read the directory at %v, want one read by the %v limit", reads, chatperf2DirectoryLimit)
	}

	// The browser half, which no native test can run.
	projection := chatperfBody(t, "chat_wasm.go", "func loadChatProjection(")
	chatperfInOrder(t, "loadChatProjection", projection,
		"if !deferTimeline {",
		"startPersonaChat(cfg, model.SelectedID)",
		"startChatDMPeers(cfg, conversations)",
		"chatux009OpenAhead()",
	)
	if strings.Count(projection, "startPersonaChat(") != 1 {
		t.Fatal("the first load starts the agent reads in more than one place")
	}
	open := chatperfBody(t, "chat_wasm.go", "func openChatConversationAt(")
	chatperfInOrder(t, "openChatConversationAt", open, "chatBrowser.selectChatConversation(id)", "startPersonaChat(cfg, id)")
	watch := chatperfBody(t, "persona_chat_wasm.go", "func watchPersonaChat(")
	chatperfInOrder(t, "watchPersonaChat", watch,
		`directoryPosts, firstAnswer := "", true`,
		"chatperf2DirectoryStale(firstAnswer, directoryPosts, nextPosts)",
		"personaDirectoryRefresh.Schedule()",
		"directoryPosts, firstAnswer = nextPosts, false",
	)
	raw := chatperfBody(t, "persona_chat_wasm.go", "var personaDirectoryRefresh = ")
	if !strings.Contains(raw, "newChatperfRenderCoalescer(browserDebounceScheduler, time.Now, chatperf2DirectoryPace,") {
		t.Fatal("the directory read is no longer paced by chatperf2DirectoryPace")
	}
}

// TestTodo_CHATBUG_014_ReplayEventCost: a stream event that cannot sound (a
// reaction, a deletion, a join: most of a replay) does not copy the whole model
// to decide whether it should.
func TestTodo_CHATBUG_014_ReplayEventCost(t *testing.T) {
	receive := chatperfBody(t, "chat_stream_wasm.go", "func receiveChatEvents(")
	chatperfInOrder(t, "receiveChatEvents", receive,
		"soundCandidate = false",
		"if event.GetKind() == chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED {",
		"soundModel = chatBrowser.snapshot()",
		"soundCandidate = chatPostNewerThanLoadedTimeline(soundModel, event.GetPost())",
		"chatBrowser.applyStreamEvent(",
	)
	if strings.Count(receive, "chatBrowser.snapshot()") != 1 {
		t.Fatal("the stream copies the model in more than one place per event")
	}
}
