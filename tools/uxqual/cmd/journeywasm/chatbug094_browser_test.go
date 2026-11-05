package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_094_Browser: a channel whose rendering reads are still on
// their way draws every message as written at once, in English, German and
// Arabic, and a message the batch leaves out is never asked about again. (The
// retry bound and the backoff rhythm are pinned by TestChattoneY2Reads and
// TestTodo_CHATUX_012; the open-time comparison is a live measurement and is
// not encoded here. What this test pins is its structural cause: no rendering
// read can hold a loading row.)
func TestTodo_CHATBUG_094_Browser(t *testing.T) {
	// A batch answers what it can and leaves out what is gone: the omitted
	// message settles (it is not asked about again) while a message still
	// pending is asked about again.
	messages := []chatui.Message{{ID: "m1", Revision: 1}, {ID: "gone", Revision: 1}}
	answered := map[string]chatui.ReaderSelection{}
	integrate2ApplyReaderAnswer(answered, messages, map[string]chatui.ReaderSelection{
		"m1": {Revision: 1, Mark: chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Translate}, CanShowOriginal: true}},
	}, nil)
	if unsettled := readerUnsettled(answered); len(unsettled) != 0 {
		t.Fatalf("a message the batch left out is asked about again: %v", unsettled)
	}
	pending := map[string]chatui.ReaderSelection{}
	integrate2ApplyReaderAnswer(pending, messages, map[string]chatui.ReaderSelection{
		"m1": {Revision: 1, Mark: chatrender.Mark{State: "pending", CanShowOriginal: true, Wanted: []chatrender.Kind{chatrender.Translate}}},
	}, nil)
	if unsettled := readerUnsettled(pending); len(unsettled) != 1 || unsettled[0] != "m1" {
		t.Fatalf("a pending rendering is not asked about again: %v", unsettled)
	}

	// Seven messages with every rendering still pending: every body is drawn
	// as written at once, with no loading rows, and a rendering that arrives
	// swaps its text in.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			bodies := []string{"Standup moved to ten", "The quarterly report is ready", "Lunch at noon", "Deploy at three", "Review the draft", "Call at five", "Ship it today"}
			var list []chatui.Message
			selections := map[string]chatui.ReaderSelection{}
			for i, body := range bodies {
				id := string(rune('a' + i))
				list = append(list, chatui.Message{ID: id, Revision: 1, AuthorID: "hans", Author: "Hans", Body: body, Sequence: uint64(i + 1)})
				selections[id] = chatui.ReaderSelection{Revision: 1, Mark: chatrender.Mark{State: "pending", SourceLanguage: "en", CanShowOriginal: true, Wanted: []chatrender.Kind{chatrender.Translate}}}
			}
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "room", CurrentUser: "eve", CurrentTenantID: "tenant",
				Text:             func(key string) string { return ctx.Text(key) },
				ChatFeatures:     &chatui.ChatFeatures{Renderings: true, Translating: true},
				Conversations:    []chatui.Conversation{{ID: "room", Kind: chatui.PublicChannel, Name: "general", Joined: true, MemberCount: 5}},
				Messages:         list,
				ReaderSelections: selections,
				Callbacks:        chatui.Callbacks{SendMessage: func(string, string) {}}}
			page, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			for _, body := range bodies {
				if !strings.Contains(page, body) {
					t.Fatalf("%s: the list does not draw %q while its rendering is pending", locale, body)
				}
			}
			for _, loading := range []string{"chatux009-skel-row", "chatux009-skeleton"} {
				if strings.Contains(page, loading) {
					t.Fatalf("%s: a rendering read holds a loading row: %s", locale, loading)
				}
			}
			arrived := map[string]chatui.ReaderSelection{}
			for id, selection := range selections {
				arrived[id] = selection
			}
			arrived["a"] = chatui.ReaderSelection{Revision: 1,
				Rendering: chatrender.Rendering{Message: "a", Revision: 1, Tone: chatrender.AsWritten, Language: chatrender.Language(ctx.Resolved), SourceLanguage: "en", Text: "TRANSLATED", Kinds: []chatrender.Kind{chatrender.Translate}},
				Mark:      chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Translate}, SourceLanguage: "en", CanShowOriginal: true, CanShowAsWritten: true}}
			m.ReaderSelections = arrived
			swapped, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(swapped, "TRANSLATED") || !strings.Contains(swapped, bodies[1]) {
				t.Fatal("an arrived rendering did not swap in beside the messages as written")
			}
		})
	}
}
