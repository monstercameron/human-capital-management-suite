package chatui

import (
	stdhtml "html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatbug071Page draws the conversation column the way the workspace does:
// the handlers are bound from the mention store by bindHandlers and the
// composer is drawn by timeline from those handlers. The earlier tests handed
// composer() a handlers value they had filled in themselves, which is not the
// path the page takes.
func chatbug071Page(t *testing.T, m Model, box *mentionBox) string {
	t.Helper()
	return stdhtml.UnescapeString(renderNode(t, ui.CreateElement(func(m Model) ui.Node {
		store := mentionStore{box: box, tick: ui.UseState(uint64(0))}
		store.ForConversation(m.SelectedID)
		local := localStore{box: ui.UseRef(&localUI{}).Get(), tick: ui.UseState(uint64(0))}
		local.forRoom(m.SelectedID)
		h := bindHandlers(m, ui.UseRef(newGiphyPickerViews()).Get(), store, local, ui.UseRef(&browserDrafts{}).Get())
		return timeline(m, h)
	}, m)))
}

// chatbug071Composer cuts the composer form out of the page.
func chatbug071Composer(t *testing.T, page string) string {
	t.Helper()
	at := strings.Index(page, `class="chat-composer"`)
	if at < 0 {
		t.Fatal("the page has no composer")
	}
	start := strings.LastIndex(page[:at], "<form")
	return page[start : start+strings.Index(page[start:], "</form>")]
}

// TestTodo_CHATBUG_071_Render goes from the mention menu's row to the page:
// the choice is remembered the way the menu remembers it, and the page is
// drawn through the workspace's own handler binding. A person who is not a
// member is named under the draft, between the text and the tool row, with the
// offer to add them, whether the member list had loaded when they were chosen
// or loaded afterwards.
func TestTodo_CHATBUG_071_Render(t *testing.T) {
	const note, add = "Sofia Beltran is not in this conversation and will not be notified.", ">Add Sofia Beltran<"
	model := func() Model {
		m := chatbug071Model()
		m.SearchDirectory = []SearchPerson{{ID: "worker-sofia", Name: "Sofia Beltran"}, {ID: "worker-loretta", Name: "Loretta Haynes"}}
		m.Callbacks.AddMembers = func([]string) {}
		m.Callbacks.SendMessage = func(string, string) {}
		return m
	}
	underDraft := func(t *testing.T, form string) {
		t.Helper()
		field, line, tools := strings.Index(form, `id="chat-composer"`), strings.Index(form, note), strings.Index(form, `class="composer-toolbar"`)
		if line < 0 || !strings.Contains(form, add) || !strings.Contains(form, `data-action="mention-add-outside"`) {
			t.Fatalf("the page's composer does not say %q with the offer to add: %s", note, form)
		}
		if !(field < line && line < tools) {
			t.Errorf("the note is not between the text and the tool row (text %d, note %d, tools %d)", field, line, tools)
		}
	}

	t.Run("chosen from Not in this conversation", func(t *testing.T) {
		m := model()
		options, _ := mentionOptionsForState(m, "Sofia", "chat-composer", false)
		if len(options) != 1 || options[0].person == nil || options[0].person.Member {
			t.Fatalf("Sofia Beltran is not offered as someone outside the conversation: %+v", options)
		}
		box := &mentionBox{}
		m.Draft = chatbug071Pick(t, m, mentionStore{box: box}, "chat-composer", "hi @Sofia", "Sofia Beltran")
		underDraft(t, chatbug071Composer(t, chatbug071Page(t, m, box)))
		// Another conversation's composer does not carry the line.
		m.SelectedID = "dm"
		if form := chatbug071Composer(t, chatbug071Page(t, m, box)); strings.Contains(form, "composer-outside-note") {
			t.Errorf("the note followed the viewer into another conversation: %s", form)
		}
	})

	t.Run("chosen before the member list had loaded", func(t *testing.T) {
		// Sofia wrote here and has left. With no member list yet, the authors
		// on screen stand in for it and she is offered as a member.
		m := model()
		loaded := m.Members
		m.Members = nil
		m.Messages = []Message{{ID: "old", AuthorID: "sofia", Author: "Sofia Beltran", Body: "See you all"}}
		box := &mentionBox{}
		store := mentionStore{box: box}
		m.Draft = chatbug071Pick(t, m, store, "chat-composer", "hi @Sofia", "Sofia Beltran")
		if refs := store.PersonaReferences("chat-composer", "room", m.Draft); len(refs) != 1 {
			t.Fatalf("an author on screen was not taken for a member before the list loaded: %+v", refs)
		}
		if form := chatbug071Composer(t, chatbug071Page(t, m, box)); strings.Contains(form, "composer-outside-note") {
			t.Fatalf("a note was drawn before anyone knew who is in the conversation: %s", form)
		}
		// The list arrives without her.
		m.Members = loaded
		underDraft(t, chatbug071Composer(t, chatbug071Page(t, m, box)))
		if refs := store.PersonaReferences("chat-composer", "room", m.Draft); len(refs) != 0 {
			t.Errorf("someone who is not a member would still be sent as a mention: %+v", refs)
		}
	})

	t.Run("a member chosen before the list had loaded stays a mention", func(t *testing.T) {
		m := model()
		loaded := m.Members
		m.Members = nil
		m.Messages = []Message{{ID: "new", AuthorID: "loretta", Author: "Loretta Haynes", Body: "Hello"}}
		box := &mentionBox{}
		store := mentionStore{box: box}
		m.Draft = chatbug071Pick(t, m, store, "chat-composer", "hi @Lor", "Loretta Haynes")
		m.Members = loaded
		if form := chatbug071Composer(t, chatbug071Page(t, m, box)); strings.Contains(form, "composer-outside-note") {
			t.Fatalf("a member is named as outside the conversation: %s", form)
		}
		refs := store.PersonaReferences("chat-composer", "room", m.Draft)
		if len(refs) != 1 || refs[0].ID != "loretta" || refs[0].Kind != "PERSON_MENTION" {
			t.Errorf("the member's mention was lost when the list loaded: %+v", refs)
		}
	})

	t.Run("an agent in the draft is never a person outside", func(t *testing.T) {
		m := model()
		box := &mentionBox{}
		store := mentionStore{box: box}
		store.AddPersonaToken("chat-composer", "room", ChatReference{Kind: "PERSONA", TenantID: "tenant", ID: "policy-helper", Display: "Policy Helper", ConversationID: "room"})
		if outside := store.SettleOutsiders(m); len(outside) != 0 || len(box.personas) != 1 {
			t.Errorf("the agent the draft asks was moved out of its references: outside %+v, references %+v", outside, box.personas)
		}
	})
}
