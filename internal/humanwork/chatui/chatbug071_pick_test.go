package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

// chatbug071Pick does what the mention menu's choice does, with the draft text
// in hand instead of in a text area: the row the menu offers for the query is
// applied to the draft and remembered, and the input event the replaced text
// raises reconciles the store against the new text. It returns the new draft.
func chatbug071Pick(t *testing.T, m Model, store mentionStore, target, draft, name string) string {
	t.Helper()
	caret := len(draft)
	query, start, found := mentionTokenAt(draft, caret)
	if !found {
		t.Fatalf("the draft %q has no @ token at its end", draft)
	}
	options, _ := mentionOptionsForState(m, query, target, false)
	for _, option := range options {
		if option.person == nil || option.person.Name != name {
			continue
		}
		updated, next := applyMention(draft, start, caret, option.person.Name)
		store.AddPerson(target, m.SelectedID, start, next-1, *option.person)
		store.ReconcilePersonas(target, updated)
		return updated
	}
	t.Fatalf("the mention menu does not offer %q for %q: %+v", name, query, options)
	return ""
}

// TestTodo_CHATBUG_071_Pick goes from the menu's row to the line under the
// draft. Someone who wrote in the conversation and is no longer in it was
// offered as a member, so choosing them left no note and sent a mention the
// service refuses; the page check of 2026-10-02 found the draft with nothing
// under it.
func TestTodo_CHATBUG_071_Pick(t *testing.T) {
	model := func() Model {
		m := chatbug071Model()
		m.SearchDirectory = []SearchPerson{{ID: "worker-sofia", Name: "Sofia Beltran"}, {ID: "worker-loretta", Name: "Loretta Haynes"}}
		m.Callbacks.AddMembers = func([]string) {}
		m.Callbacks.SendMessage = func(string, string) {}
		return m
	}

	for name, messages := range map[string][]Message{
		"someone who was never in the conversation":           nil,
		"someone who wrote here and has since left":           {{ID: "old", AuthorID: "sofia", Author: "Sofia Beltran", Body: "Sofia Beltran left the channel"}},
		"someone who replied in the open thread and has left": {{ID: "old", AuthorID: "walt", Author: "Walt Brennan", Body: "Parent"}},
	} {
		m := model()
		m.Messages = messages
		if strings.Contains(name, "thread") {
			m.ThreadMessages = []Message{{ID: "reply", AuthorID: "sofia", Author: "Sofia Beltran", Body: "A reply"}}
		}
		store := mentionStore{box: &mentionBox{conversationID: "room"}}
		draft := chatbug071Pick(t, m, store, "chat-composer", "hi @Sofia", "Sofia Beltran")
		if draft != "hi @Sofia Beltran " {
			t.Fatalf("%s: the draft reads %q", name, draft)
		}
		if refs := store.PersonaReferences("chat-composer", "room", draft); len(refs) != 0 {
			t.Errorf("%s: a mention of a non-member would be sent: %+v", name, refs)
		}
		m.Draft = draft
		form := stdhtml.UnescapeString(renderNode(t, composer(m, handlers{mentionOutside: store.SettleOutsiders(m)})))
		for _, want := range []string{`class="composer-outside-note"`, "Sofia Beltran is not in this conversation and will not be notified.", ">Add Sofia Beltran<", `data-action="mention-add-outside"`} {
			if !strings.Contains(form, want) {
				t.Errorf("%s: the composer lacks %q under the draft", name, want)
			}
		}
		// Typing on keeps the line; taking the name out of the draft ends it.
		store.ReconcilePersonas("chat-composer", draft+"are you free?")
		if outside := store.SettleOutsiders(m); len(outside) != 1 {
			t.Errorf("%s: typing after the name dropped the note", name)
		}
		store.ReconcilePersonas("chat-composer", "hi ")
		if outside := store.SettleOutsiders(m); len(outside) != 0 {
			t.Errorf("%s: the note outlived the name: %+v", name, outside)
		}
	}

	t.Run("a member is a reference and has no note", func(t *testing.T) {
		m := model()
		store := mentionStore{box: &mentionBox{conversationID: "room"}}
		draft := chatbug071Pick(t, m, store, "chat-composer", "hi @Lor", "Loretta Haynes")
		refs := store.PersonaReferences("chat-composer", "room", draft)
		if len(refs) != 1 || refs[0].Kind != "PERSON_MENTION" || refs[0].ID != "loretta" || refs[0].TenantID != "tenant" {
			t.Fatalf("a member's mention is not kept for the send under the member's own identifier: %+v", refs)
		}
		m.Draft = draft
		if form := renderNode(t, composer(m, handlers{mentionOutside: store.SettleOutsiders(m)})); strings.Contains(form, "composer-outside-note") {
			t.Fatalf("a member is named as outside the conversation: %s", form)
		}
	})

	t.Run("until the member list has loaded, the authors on screen stand in for it", func(t *testing.T) {
		m := model()
		m.Members = nil
		m.Messages = []Message{{ID: "new", AuthorID: "loretta", Author: "Loretta Haynes", Body: "Hello"}}
		options, _ := mentionOptionsForState(m, "Lor", "chat-composer", false)
		if len(options) != 1 || options[0].person == nil || !options[0].person.Member || options[0].person.ID != "loretta" {
			t.Fatalf("an author on screen is not offered as a member before the list loads: %+v", options)
		}
	})
}
