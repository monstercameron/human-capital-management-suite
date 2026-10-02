package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
	"unicode/utf16"
)

// chatbug071Field is the browser's text area as far as the mention menu can
// tell: a value and a caret in UTF-16 units. Writing the value raises the input
// event the page raises, which runs the composer's own input steps before the
// writer goes on (the page runs them inside dispatchEvent).
type chatbug071Field struct {
	value string
	caret int
	input func(value string)
}

func (f *chatbug071Field) selection(string) (string, int, bool) { return f.value, f.caret, true }

func (f *chatbug071Field) replace(_ string, value string, caret int) {
	f.value = value
	f.caret = len(utf16.Encode([]rune(value)))
	f.input(value)
	f.caret = caret
}

func (f *chatbug071Field) typeText(text string) {
	for _, r := range text {
		f.value += string(r)
		f.caret = len(utf16.Encode([]rune(f.value)))
		f.input(f.value)
	}
}

// chatbug071Page types into a composer the way the page does: every character
// raises the input event, which reconciles the draft's people and opens or
// closes the mention menu, and Enter with the menu open picks the highlighted
// row.
type chatbug071Typist struct {
	t     *testing.T
	m     Model
	store mentionStore
	field *chatbug071Field
}

func newChatbug071Typist(t *testing.T, m Model) *chatbug071Typist {
	typist := &chatbug071Typist{t: t, m: m, store: mentionStore{box: &mentionBox{conversationID: m.SelectedID}}}
	typist.field = &chatbug071Field{}
	typist.field.input = func(value string) {
		// The composerInput handler: reconcile first, then follow the caret.
		typist.store.ReconcilePersonas("chat-composer", value)
		query, start, found := mentionTokenAt(value, typist.field.caret)
		if !found {
			typist.store.Set(mentionState{})
			return
		}
		typist.store.Set(mentionState{Target: "chat-composer", Query: query, Start: start, End: typist.field.caret, Open: true})
	}
	return typist
}

func (p *chatbug071Typist) enter() {
	state := liveComposerMention(p.store.Get(), "chat-composer", p.field.value, p.field.caret)
	if !state.Open {
		return
	}
	pickMention(p.m, p.store, composerField{selection: p.field.selection, replace: p.field.replace}, state, state.Active)
}

func (p *chatbug071Typist) page() string {
	m := p.m
	m.Draft = p.field.value
	return stdhtml.UnescapeString(renderNode(p.t, composer(m, handlers{mentionOutside: p.store.SettleOutsiders(m)})))
}

// TestTodo_CHATBUG_071_PageSequence walks the page's sequence: "hi @Sofia" typed
// one character at a time, Enter on the menu's row, then more typing. Sofia
// Beltran is in the directory and not in the conversation, whose members
// are many. The line under the draft must be there right after Enter, stay while
// the person types after the name and before it, and go when the name goes.
func TestTodo_CHATBUG_071_PageSequence(t *testing.T) {

	members := []Member{{ID: "walt", HomeTenantID: "tenant", Name: "Walt Brennan"}}
	for _, name := range []string{"Ana Ruiz", "Ben Okafor", "Cleo Park", "Dev Rao", "Eli Stone", "Fay Moss", "Gus Hale", "Hana Lee"} {
		members = append(members, Member{ID: strings.ToLower(strings.ReplaceAll(name, " ", "-")), HomeTenantID: "tenant", Name: name})
	}
	model := func(loaded bool) Model {
		m := chatbug071Model()
		m.SelectedID = "room"
		if loaded {
			m.Members = members
		}
		m.SearchDirectory = []SearchPerson{{ID: "worker-sofia", Name: "Sofia Beltran"}, {ID: "worker-ana", Name: "Ana Ruiz"}}
		m.Callbacks.AddMembers = func([]string) {}
		m.Callbacks.SendMessage = func(string, string) {}
		return m
	}
	for name, m := range map[string]Model{"members loaded": model(true)} {
		typist := newChatbug071Typist(t, m)
		typist.field.typeText("hi @Sofia")
		if st := typist.store.Get(); !st.Open || st.Query != "Sofia" {
			t.Fatalf("%s: the menu is not open on the query: %+v", name, st)
		}
		typist.enter()
		if typist.field.value != "hi @Sofia Beltran " {
			t.Fatalf("%s: the draft reads %q", name, typist.field.value)
		}
		check := func(step string, wantNote bool) {
			t.Helper()
			page := typist.page()
			has := strings.Contains(page, `class="composer-outside-note"`) && strings.Contains(page, "Sofia Beltran is not in this conversation and will not be notified.") && strings.Contains(page, `data-action="mention-add-outside"`)
			if has != wantNote {
				t.Fatalf("%s, %s: note present = %v, want %v (draft %q)", name, step, has, wantNote, typist.field.value)
			}
		}
		check("right after Enter", true)
		if refs := typist.store.PersonaReferences("chat-composer", "room", typist.field.value); len(refs) != 0 {
			t.Fatalf("%s: a mention of someone outside the conversation would be sent: %+v", name, refs)
		}
		typist.field.typeText("are you free?")
		check("after typing on", true)
		// Typing in front of the name moves it; the line stays.
		typist.field.value = "Hello! " + typist.field.value
		typist.field.caret = len(utf16.Encode([]rune(typist.field.value)))
		typist.field.input(typist.field.value)
		check("after typing before the name", true)
		typist.field.value = "Hello! hi "
		typist.field.caret = len(typist.field.value)
		typist.field.input(typist.field.value)
		check("after the name was deleted", false)
	}

	t.Run("a member is one reference whichever way the text moves", func(t *testing.T) {
		m := model(true)
		m.SearchDirectory = nil
		typist := newChatbug071Typist(t, m)
		typist.field.typeText("ping @Ben")
		typist.enter()
		if typist.field.value != "ping @Ben Okafor " {
			t.Fatalf("the draft reads %q", typist.field.value)
		}
		typist.field.value = "Hi, " + typist.field.value + "please look"
		typist.field.caret = len(typist.field.value)
		typist.field.input(typist.field.value)
		refs := typist.store.PersonaReferences("chat-composer", "room", typist.field.value)
		if len(refs) != 1 || refs[0].Kind != "PERSON_MENTION" || refs[0].ID != "ben-okafor" {
			t.Fatalf("the mention was lost when text was typed before it: %+v", refs)
		}
		if strings.Contains(typist.page(), "composer-outside-note") {
			t.Fatal("a member is named as outside the conversation")
		}
	})
}
