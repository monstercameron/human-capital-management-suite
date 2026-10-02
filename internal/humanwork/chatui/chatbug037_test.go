package chatui

import (
	"strings"
	"testing"
)

const (
	chatbug037Origin  = "http://localhost:8290"
	chatbug037Token   = "aXJvbnJpZGdlLWRlbW8AODZjZmNjYWltNWM2Mi01MTBhLTg3NjItYjRkMzIzNjgwMDNiAGJjNWZmYTZkLTcwNmUt"
	chatbug037Address = chatbug037Origin + "/workspace/app/chat#share=" + chatbug037Token
)

func chatbug037Model(state string) Model {
	m := Model{
		State: StateReady, Locale: "en-US", SelectedID: "general", EmbedOrigin: chatbug037Origin,
		CurrentTenantID: "tenant", CurrentUser: "cam",
		Conversations: []Conversation{
			{ID: "general", Name: "general", Kind: PublicChannel, Joined: true},
			{ID: "design", Name: "design", Kind: PublicChannel, Joined: true},
		},
	}
	switch state {
	case "ready":
		m.Embeds = map[string]LinkEmbed{chatbug037Token: {Token: chatbug037Token, State: "ready", SourceRoom: "design", SourcePost: "p1", Channel: "design", Author: "Eddie Ramirez", TimeLabel: "11:59 AM", Body: "Confirmed with legal.", AttachmentCount: 1}}
	case "anywhere":
		m.Embeds = map[string]LinkEmbed{chatbug037Token: {Token: chatbug037Token, State: "ready", SourcePost: "p1", Author: "Eddie Ramirez", Body: "Confirmed with legal."}}
	case "unavailable":
		m.Embeds = map[string]LinkEmbed{chatbug037Token: {Token: chatbug037Token, State: "unavailable"}}
	case "loading":
		m.Embeds = map[string]LinkEmbed{chatbug037Token: {Token: chatbug037Token, State: "loading"}}
	}
	return m
}

func chatbug037Body(t *testing.T, m Model, body string) string {
	t.Helper()
	return renderNode(t, spanOf(mentionReferenceBody(m, body)))
}

// TestTodo_CHATBUG_037: a share address in a message is a short link when it
// resolves to the card below it, and a shortened, titled link when it does not;
// the raw token is never printed, in a sentence or alone.
func TestTodo_CHATBUG_037(t *testing.T) {
	for name, body := range map[string]string{"in a sentence": "did y'all see this " + chatbug037Address, "alone": chatbug037Address, "with a full stop": "did y'all see this " + chatbug037Address + "."} {
		resolved := chatbug037Body(t, chatbug037Model("ready"), body)
		if !strings.Contains(resolved, ">a message in #design</a>") || !strings.Contains(resolved, `href="`+chatbug037Address+`"`) {
			t.Errorf("%s, resolvable: want a link reading \"a message in #design\" pointing at the address: %s", name, resolved)
		}
		if strings.Contains(strings.ReplaceAll(resolved, `href="`+chatbug037Address+`"`, ""), chatbug037Token) {
			t.Errorf("%s, resolvable: the raw token is printed: %s", name, resolved)
		}
		if strings.HasPrefix(body, "did") && !strings.Contains(resolved, "did y&#39;all see this ") && !strings.Contains(resolved, "did y'all see this ") {
			t.Errorf("%s, resolvable: the sentence was lost: %s", name, resolved)
		}
		if strings.HasSuffix(body, ".") && !strings.Contains(resolved, "</a>.") {
			t.Errorf("%s, resolvable: the full stop moved into the link: %s", name, resolved)
		}

		anywhere := chatbug037Body(t, chatbug037Model("anywhere"), body)
		if !strings.Contains(anywhere, ">a message</a>") {
			t.Errorf("%s: a reader who cannot see where the message is wants \"a message\": %s", name, anywhere)
		}

		for _, state := range []string{"unavailable", "loading", "none"} {
			unresolved := chatbug037Body(t, chatbug037Model(state), body)
			if !strings.Contains(unresolved, `href="`+chatbug037Address+`"`) || !strings.Contains(unresolved, `title="`+chatbug037Address+`"`) {
				t.Errorf("%s, %s: the address must stay a link with the whole address as its title: %s", name, state, unresolved)
			}
			if !strings.Contains(unresolved, "…</a>") || strings.Contains(strings.ReplaceAll(strings.ReplaceAll(unresolved, `href="`+chatbug037Address+`"`, ""), `title="`+chatbug037Address+`"`, ""), chatbug037Token) {
				t.Errorf("%s, %s: the visible address must be shortened with an ellipsis: %s", name, state, unresolved)
			}
		}
	}

	// A foreign address and a short one are left as they are.
	if got := chatbug037Body(t, chatbug037Model("ready"), "see https://example.com/a/b"); strings.Contains(got, "<a") {
		t.Errorf("a foreign address became a link: %s", got)
	}

	// Several links, each named for its own state.
	m := chatbug037Model("ready")
	other := chatbug037Origin + "/chat/share/otherToken"
	got := chatbug037Body(t, m, chatbug037Address+" and "+other)
	if strings.Count(got, "<a ") != 2 || !strings.Contains(got, ">a message in #design</a>") || !strings.Contains(got, `href="`+other+`"`) {
		t.Errorf("two addresses: %s", got)
	}

	// Attachments: an icon and the count, named in the reader's language.
	for locale, want := range map[string]map[int]string{
		"en-US": {1: "1 attachment", 2: "2 attachments", 5: "5 attachments"},
		"de-DE": {1: "1 Anhang", 2: "2 Anhänge", 5: "5 Anhänge"},
		"ar":    {1: "مرفق واحد", 2: "مرفقان", 5: "5 مرفقات", 12: "12 مرفقاً"},
	} {
		for count, label := range want {
			m := Model{Locale: locale}
			markup := renderNode(t, chatbug037AttachmentBadge(m, count))
			if !strings.Contains(markup, `aria-label="`+label+`"`) || !strings.Contains(markup, "<svg") || strings.Contains(markup, "⟦") {
				t.Errorf("%s %d: want an icon named %q: %s", locale, count, label, markup)
			}
		}
	}
}

// TestTodo_CHATBUG_038_Composer: what a person sent or cleared does not come
// back into the composer, with its previews, from a render made with the
// model's older copy of the draft.
func TestTodo_CHATBUG_038_Composer(t *testing.T) {
	text := "did y'all see this " + chatbug037Address
	drafts := &browserDrafts{storage: &memoryDraftPersistence{}}
	model := chatbug037Model("ready")
	model.Draft = text
	drafts.prepare(&model)
	if model.Draft != text {
		t.Fatalf("the draft was not shown: %q", model.Draft)
	}
	if markup := renderNode(t, composer(model, handlers{})); !strings.Contains(markup, "chat-embed") {
		t.Fatalf("the draft's preview is not shown before the send: %s", markup)
	}

	// The send clears the store; the next render is made from the old props.
	drafts.set("general", "")
	stale := chatbug037Model("ready")
	stale.Draft = text
	drafts.prepare(&stale)
	if stale.Draft != "" {
		t.Fatalf("a sent draft came back from the model's older copy: %q", stale.Draft)
	}
	if markup := renderNode(t, composer(stale, handlers{})); strings.Contains(markup, "chat-embed") || strings.Contains(markup, "Linked message") {
		t.Fatalf("a sent draft's preview stayed in the composer: %s", markup)
	}

	// An older copy (the text at the page's last render) is just as stale.
	drafts = &browserDrafts{storage: &memoryDraftPersistence{}}
	earlier := chatbug037Model("ready")
	earlier.Draft = chatbug037Address
	drafts.prepare(&earlier)
	drafts.set("general", text)
	drafts.set("general", "")
	again := chatbug037Model("ready")
	again.Draft = chatbug037Address
	drafts.prepare(&again)
	if again.Draft != "" {
		t.Fatalf("the text of the page's last render came back after a send: %q", again.Draft)
	}

	// A render that shows the draft empty ends it, and typing again works.
	fresh := chatbug037Model("ready")
	drafts.prepare(&fresh)
	typed := chatbug037Model("ready")
	typed.Draft = chatbug037Address
	drafts.set("general", chatbug037Address)
	drafts.prepare(&typed)
	if typed.Draft != chatbug037Address {
		t.Fatalf("a new draft with the same text was refused: %q", typed.Draft)
	}

	// Clearing the link out of the draft removes its preview.
	drafts.set("general", "did y'all see this")
	edited := chatbug037Model("ready")
	edited.Draft = "did y'all see this"
	drafts.prepare(&edited)
	if markup := renderNode(t, composer(edited, handlers{})); strings.Contains(markup, "chat-embed") {
		t.Fatalf("a preview stayed after its link left the draft: %s", markup)
	}

	// A send that failed puts its text back on purpose.
	drafts.set("general", "")
	ForgetClearedDraft("general")
	back := chatbug037Model("ready")
	back.Draft = "did y'all see this"
	drafts.prepare(&back)
	if back.Draft != "did y'all see this" {
		t.Fatalf("a failed send could not restore its text: %q", back.Draft)
	}
}

// TestTodo_CHATBUG_037_OtherHostName: the stored message names the server as
// localhost while the reader loaded the page from 127.0.0.1. The token is what
// identifies the message, so the address is still a link and its card still
// resolves; a different server is still not interpreted.
func TestTodo_CHATBUG_037_OtherHostName(t *testing.T) {
	body := "did y'all see this " + chatbug037Address
	for _, origin := range []string{"http://127.0.0.1:8290", "http://[::1]:8290", "http://localhost:8290"} {
		if got := ShareLocators(body, origin); len(got) != 1 || got[0].Token != chatbug037Token {
			t.Errorf("reader on %s: the stored address is not recognized: %+v", origin, got)
		}
		m := chatbug037Model("ready")
		m.EmbedOrigin = origin
		if got := chatbug037Body(t, m, body); !strings.Contains(got, ">a message in #design</a>") || !strings.Contains(got, `href="`+chatbug037Address+`"`) {
			t.Errorf("reader on %s, card ready: %s", origin, got)
		}
		m = chatbug037Model("none")
		m.EmbedOrigin = origin
		if got := chatbug037Body(t, m, body); !strings.Contains(got, `title="`+chatbug037Address+`"`) || !strings.Contains(got, "…</a>") {
			t.Errorf("reader on %s, card not ready: %s", origin, got)
		}
	}
	for _, origin := range []string{"http://127.0.0.1:9999", "https://127.0.0.1:8290", "http://hcm.example:8290"} {
		if got := ShareLocators(body, origin); len(got) != 0 {
			t.Errorf("reader on %s: another server's address was interpreted: %+v", origin, got)
		}
	}
	if got := ShareLocators("http://evil.example/workspace/app/chat#share="+chatbug037Token, "http://127.0.0.1:8290"); len(got) != 0 {
		t.Errorf("a foreign host was interpreted: %+v", got)
	}
}
