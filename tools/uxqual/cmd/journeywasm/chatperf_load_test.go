//go:build !(js && wasm)

package main

import (
	"os"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_014_FirstOpen: the first load's conversation is opened once,
// by the loader ahead of the paint when it can, by the page when it mounts
// otherwise, and never by both.
func TestTodo_CHATBUG_014_FirstOpen(t *testing.T) {
	var open chatperfFirstOpen
	if _, ok := open.ahead(); ok {
		t.Fatal("an open was started with nothing recorded")
	}
	if _, owed, start := open.mounted(); owed || start {
		t.Fatal("a page with nothing deferred was told to open a conversation")
	}

	// The loader starts it; the page then has nothing to start.
	open.record("room")
	id, ok := open.ahead()
	if !ok || id != "room" {
		t.Fatalf("the loader could not start the recorded open: %q, %v", id, ok)
	}
	if _, again := open.ahead(); again {
		t.Fatal("the open was started twice ahead of the paint")
	}
	id, owed, start := open.mounted()
	if id != "room" || !owed || start {
		t.Fatalf("after an open started ahead, mount reported id=%q owed=%v start=%v; the page must not start it again", id, owed, start)
	}
	if _, owed, _ := open.mounted(); owed {
		t.Fatal("the debt was not settled by the first mount")
	}

	// The loader did not start it (a page that mounted first): the page does.
	open.record("other")
	id, owed, start = open.mounted()
	if id != "other" || !owed || !start {
		t.Fatalf("an open nobody started was not handed to the page: id=%q owed=%v start=%v", id, owed, start)
	}
	if _, ok := open.ahead(); ok {
		t.Fatal("an open handed to the page was started again by the loader")
	}

	// A change committed between the first render and the mount is repainted.
	drawn := chatperfDrawn{State: "loading", Selected: "room"}
	if chatperfBehind(drawn, drawn) {
		t.Fatal("a page that drew what the model holds was told to repaint")
	}
	if !chatperfBehind(drawn, chatperfDrawn{State: "ready", Selected: "room", Messages: 37}) {
		t.Fatal("messages committed after the first render would not be drawn")
	}
}

// TestTodo_CHATBUG_014_Gate: work the first paint does not need waits for it,
// runs in the order it was asked for, and passes straight through afterwards.
func TestTodo_CHATBUG_014_Gate(t *testing.T) {
	var gate chatperfGate
	var ran []string
	for _, name := range []string{"reactions", "members", "directory"} {
		name := name
		if !gate.hold(func() { ran = append(ran, name) }) {
			t.Fatalf("%s ran before the first paint", name)
		}
	}
	if gate.isOpen() || len(ran) != 0 {
		t.Fatalf("the gate opened by itself: open=%v ran=%v", gate.isOpen(), ran)
	}
	for _, work := range gate.release() {
		work()
	}
	if got := strings.Join(ran, ","); got != "reactions,members,directory" {
		t.Fatalf("held work ran as %q, want the order it was asked for", got)
	}
	if again := gate.release(); len(again) != 0 {
		t.Fatalf("a second release ran %d pieces of work again", len(again))
	}
	if gate.hold(func() { t.Fatal("work queued on an open gate") }) {
		t.Fatal("work asked for after the first paint was held; a conversation switch would wait for nothing")
	}

	if gate.shownAt().IsZero() {
		t.Fatal("the gate does not know when the first timeline was drawn")
	}

	// The stream waits for the reaction read of its own conversation, and for
	// nothing else.
	closed := func(wait <-chan struct{}) bool {
		select {
		case <-wait:
			return true
		default:
			return false
		}
	}
	var turn chatperfTurn
	if !closed(turn.wait("room")) {
		t.Fatal("with no read under way the stream would wait")
	}
	turn.begin("room")
	waiting := turn.wait("room")
	if closed(waiting) {
		t.Fatal("the stream did not wait for the read that is under way")
	}
	if !closed(turn.wait("other")) {
		t.Fatal("the stream of one conversation waits for another's read")
	}
	turn.end("other")
	if closed(waiting) {
		t.Fatal("another conversation's read ended this one's wait")
	}
	turn.end("room")
	if !closed(waiting) || !closed(turn.wait("room")) {
		t.Fatal("the stream is still waiting after the read finished")
	}
	turn.end("room")
	// Opening another conversation releases whoever waited on the one left.
	turn.begin("room")
	left := turn.wait("room")
	turn.begin("next")
	if !closed(left) || closed(turn.wait("next")) {
		t.Fatal("a read for a conversation that was left still holds its stream, or the new one does not")
	}

	// Leaving the page closes the gate and forgets what the old view queued.
	gate.reset()
	if !gate.shownAt().IsZero() {
		t.Fatal("a page that was left still counts as shown")
	}
	if !gate.hold(func() { ran = append(ran, "stale") }) {
		t.Fatal("the gate stayed open for the next page")
	}
	gate.reset()
	if waiting := gate.release(); len(waiting) != 0 {
		t.Fatalf("work queued by a page that was left ran on the next one: %d", len(waiting))
	}
}

// TestTodo_CHATBUG_014_TraceIsOff: an ordinary build carries no load trace. The
// trace hooks do nothing and the connection is handed back as it was, so the
// served client is not measuring itself.
func TestTodo_CHATBUG_014_TraceIsOff(t *testing.T) {
	chatperfTrace("chat-render")
	chatperfTraceCaller("refresh")
	chatperfTraceEvent(1, true)
	if conn := chatperfConn(nil); conn != nil {
		t.Fatalf("an untraced build wrapped the connection: %T", conn)
	}
	raw, err := os.ReadFile("chatperf_trace_on_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "//go:build js && wasm && chatperf\n") {
		t.Fatal("the load trace is no longer behind the chatperf build tag")
	}
	if strings.Contains(wasmBuildTags, "chatperf") {
		t.Fatalf("the served client is built with the trace on: %q", wasmBuildTags)
	}
}

// chatperfBody returns the source of one top-level function of a file.
func chatperfBody(t *testing.T, file, signature string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("%s no longer declares %q", file, signature)
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("%s: %q has no end", file, signature)
	}
	return source[start : start+end]
}

// chatperfInOrder fails unless every piece appears in body, in the given order.
func chatperfInOrder(t *testing.T, where, body string, pieces ...string) {
	t.Helper()
	at := 0
	for _, piece := range pieces {
		next := strings.Index(body[at:], piece)
		if next < 0 {
			t.Fatalf("%s: %q is missing, or no longer comes after the step before it", where, piece)
		}
		at += next + len(piece)
	}
}

// TestTodo_CHATBUG_014_LoadSequence pins the order of the first load in the
// browser half, which no native test can run: the messages are asked for when
// the list is adopted, beside their pins, and everything the first paint does
// not need is behind the gate.
func TestTodo_CHATBUG_014_LoadSequence(t *testing.T) {
	projection := chatperfBody(t, "chat_wasm.go", "func loadChatProjection(")
	chatperfInOrder(t, "loadChatProjection", projection,
		"client.ListConversations(",
		"chatBrowser.adoptLoadedChatProjection(",
		"if deferTimeline {",
		"chatux009Defer(cfg, model.SelectedID)",
		"chatux009OpenAhead()",
		"return chatBrowser.snapshot(), nil",
	)

	posts := chatperfBody(t, "chat_wasm.go", "func loadChatPosts(")
	chatperfInOrder(t, "loadChatPosts", posts,
		"chatperfReadPins(",
		"client.ListPosts(",
		"applyChatPins(model, <-pins)",
	)
	for _, held := range []string{"loadChatReactionsLater(", "markChatRead(", "resolveChatMedia("} {
		if !strings.Contains(posts, "chatperfAfterFirstPaint(func() { "+held) {
			t.Errorf("loadChatPosts starts %s without waiting for the first paint", strings.TrimSuffix(held, "("))
		}
		if strings.Contains(posts, "go "+held) {
			t.Errorf("loadChatPosts starts %s on its own goroutine, ahead of the first paint", strings.TrimSuffix(held, "("))
		}
	}
	if strings.Contains(posts, "client.ListPins(") {
		t.Error("loadChatPosts reads the pins itself again: the open waits two round trips instead of one")
	}

	open := chatperfBody(t, "chat_wasm.go", "func openChatConversationAt(")
	for _, held := range []string{"loadChatDirectory(cfg)", "loadChannelTodo(cfg, id)", "loadChannelWidgets(cfg, id)", "loadChannelPoll(cfg, id)", "loadChatMembers(cfg)"} {
		if !strings.Contains(open, "chatperfAfterFirstPaint(func() { "+held+" })") {
			t.Errorf("openChatConversationAt starts %s without waiting for the first paint", held)
		}
	}
	// The stream starts after the first paint and after the reaction read.
	if !strings.Contains(open, "chatperfAfterFirstPaint(func() { chatperfSubscribeAfterReactions(id, generation) })") || strings.Contains(open, "subscribeChatConversation(") {
		t.Error("openChatConversationAt starts the stream itself: its replay runs ahead of the first paint or of the reaction read")
	}
	chatperfInOrder(t, "loadChatPosts", posts,
		"chatperfReactionRead.begin(room)",
		"loadChatReactionsLater(cfg, room, reactionPosts); chatperfReactionRead.end(room)",
	)
	if strings.Count(open, "loadChatMembers(cfg)") != 1 {
		t.Errorf("openChatConversationAt reads the member list %d times for one open", strings.Count(open, "loadChatMembers(cfg)"))
	}

	// A burst of membership events reads the member list once.
	events := chatperfBody(t, "chat_stream_wasm.go", "func receiveChatEvents(")
	if strings.Contains(events, "go loadChatMembers(") || !strings.Contains(events, "chatStreamMembers.Schedule()") {
		t.Error("each membership event reads the member list again instead of sharing one read")
	}

	// The page repaints for a change committed before it mounted, starts the
	// sound poll only after the first timeline, and opens the gate from a render
	// that drew a settled timeline.
	page := chatperfBody(t, "chat_page_wasm.go", "func renderChatPage(")
	chatperfInOrder(t, "renderChatPage", page,
		"chatux009Mounted()",
		"chatRerender = func()",
		"chatperfCatchUp()",
		"chatperfStartAfterFirstPaint(startChatSoundPolling)",
		"chatperfLastDrawn = chatperfDrawnOf(model)",
		"chatperfTimelineShown()",
	)

	// While the skeleton is on screen a change is gathered with the others; once
	// the messages are in the model the repaint is immediate.
	refresh := chatperfBody(t, "chat_wasm.go", "func refreshChatRoute(")
	chatperfInOrder(t, "refreshChatRoute", refresh,
		"if chatRerender != nil {",
		"if chatperfSkeletonOnScreen() {",
		"chatperfSkeletonRender.Schedule()",
		"return",
		"chatRerender()",
	)
	gate := chatperfBody(t, "chatperf_gate_wasm.go", "func chatperfSkeletonOnScreen(")
	if !strings.Contains(gate, "!chatperfFirstPaint.isOpen()") || !strings.Contains(gate, "chatui.StateLoading") {
		t.Error("repaints are held for something other than the first skeleton: a settled page or a later open would lag")
	}
}
