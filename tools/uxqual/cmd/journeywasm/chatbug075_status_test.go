package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// An idle open conversation asks for its status when it opens and then once a
// minute. The test hands the watch its own clock and counts the requests that
// reach the transport over thirty idle seconds, then over the next minute.
func TestTodo_CHATBUG_075(t *testing.T) {
	var requests atomic.Int64
	client := chatstateClient{BaseURL: "http://fixture.invalid", Bearer: "fixture", Tenant: "tenant", HTTP: &http.Client{Transport: chatstateRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		w := httptest.NewRecorder()
		_ = json.NewEncoder(w).Encode(chat.ChannelStatusSnapshot{Status: chat.ChannelStatus{TenantID: "tenant", ConversationID: "room", Revision: 2, Status: chatpolicy.StatusOpen}, CanPost: true})
		return w.Result(), nil
	})}}

	if chatstateFallbackInterval != time.Minute {
		t.Fatalf("the fallback interval is %v, want one minute", chatstateFallbackInterval)
	}
	// The clock of the test: one tick per fallback interval, sent by hand.
	tick := make(chan time.Time)
	var signal chatstateSignal
	applied := make(chan chatui.ChannelStatusView, 8)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- client.watch(ctx, "room", chatui.ChannelStatusView{}, func(view chatui.ChannelStatusView) { applied <- view }, tick, signal.Listen("room"))
	}()
	wait := func(what string) {
		t.Helper()
		select {
		case <-applied:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the status was not read", what)
		}
	}
	wait("on open")
	// Thirty idle seconds: the interval is a minute, so no tick falls in them.
	idle := 30 * time.Second
	for elapsed := time.Duration(0); elapsed < idle; elapsed += time.Second {
		if elapsed > 0 && elapsed%chatstateFallbackInterval == 0 {
			tick <- time.Time{}
			wait("fallback")
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("thirty idle seconds sent %d status requests, want the one made on open", got)
	}
	// The minute passes: one more read, and no more than one.
	tick <- time.Time{}
	wait("after a minute")
	if got := requests.Load(); got != 2 {
		t.Fatalf("after a minute %d status requests were sent, want 2", got)
	}
	// A change reported by the event stream is read at once, without waiting
	// for the minute. A change to another conversation is not this one's.
	signal.Changed("another room")
	select {
	case <-applied:
		t.Fatal("a change to another conversation made this one read its status")
	case <-time.After(50 * time.Millisecond):
	}
	signal.Changed("room")
	wait("after a change on the stream")
	if got := requests.Load(); got != 3 {
		t.Fatalf("after a change on the stream %d status requests were sent, want 3", got)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("the watch outlived its conversation")
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("leaving the conversation sent another request: %d", got)
	}
}

// Several changes before the watch reads are one wake, and a listener for a
// conversation the reader has left is no longer woken.
func TestTodo_CHATBUG_075_Signal(t *testing.T) {
	var signal chatstateSignal
	signal.Changed("room") // nobody listens yet: nothing to do, and no panic
	wake := signal.Listen("room")
	if again := signal.Listen("room"); again != wake {
		t.Fatal("listening twice to one conversation made two channels")
	}
	for i := 0; i < 5; i++ {
		signal.Changed("room")
	}
	if len(wake) != 1 {
		t.Fatalf("five changes queued %d wakes, want one", len(wake))
	}
	<-wake
	other := signal.Listen("other")
	signal.Changed("room")
	if len(wake) != 0 || len(other) != 0 {
		t.Fatalf("a change to the conversation that was left woke somebody: old %d, new %d", len(wake), len(other))
	}
	signal.Changed("other")
	if len(other) != 1 {
		t.Fatal("the open conversation was not woken")
	}
}

// The page's own status watch uses that interval: nothing in the browser file
// polls the status on a shorter clock.
func TestTodo_CHATBUG_075_Browser(t *testing.T) {
	source, err := os.ReadFile("integrate2_browser_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "poll := func(view chatui.ChannelStatusView)")
	if start < 0 {
		t.Fatal("the status poll is no longer where this test looks; update the test")
	}
	poll := text[start:]
	if end := strings.Index(poll, "client.Load("); end > 0 {
		poll = poll[:end]
	}
	if !strings.Contains(poll, "time.NewTicker(chatstateFallbackInterval)") || strings.Contains(poll, "time.Second") {
		t.Fatalf("the status poll does not run on the one-minute fallback interval: %s", poll)
	}
	// The same loop wakes on a change the event stream reports, and the stream
	// reader reports a conversation update of the conversation it is reading.
	if !strings.Contains(poll, "changed := chatstateChanged.Listen(room)") || !strings.Contains(poll, "case <-changed:") {
		t.Fatalf("the status poll does not wake on a change from the event stream: %s", poll)
	}
	stream, err := os.ReadFile("chat_stream_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	updated := string(stream)
	at := strings.Index(updated, "case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_CONVERSATION_UPDATED:")
	if at < 0 {
		t.Fatal("the stream reader does not act on a conversation update")
	}
	updated = updated[at:]
	if end := strings.Index(updated[1:], "case chatv1."); end > 0 {
		updated = updated[:end+1]
	}
	if !strings.Contains(updated, "chatstateChanged.Changed(conversationID)") {
		t.Fatalf("a conversation update on the stream does not wake the status watch: %s", updated)
	}
}
