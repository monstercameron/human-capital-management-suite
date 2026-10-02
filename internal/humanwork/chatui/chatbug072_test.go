package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatbug072Rooms() Model {
	return Model{Conversations: []Conversation{
		{ID: "c-gen", Name: "General Chat!", Kind: PublicChannel},
		{ID: "c-gen2", Name: "General", Kind: PublicChannel},
		{ID: "c-inc", Name: "incident-review", Kind: PublicChannel},
		{ID: "c-sales", Name: "sales", Kind: PublicChannel},
	}}
}

// The in: filter reads a quoted name, and a name with spaces typed without
// quotes, as the one channel; before, "in:#General Chat!" was channel
// "#General" plus the word "Chat!" and found nothing.
func TestTodo_CHATBUG_072_SearchFilterReadsNamesWithSpaces(t *testing.T) {
	m := chatbug072Rooms()
	for _, tc := range []struct{ query, text, id string }{
		{`item in:"General Chat!"`, "item", "c-gen"},
		{`in:#"General Chat!" item`, "item", "c-gen"},
		{`item in:#General Chat!`, "item", "c-gen"},
		{`in:#General Chat! item`, "item", "c-gen"},
		{`in:#general item`, "item", "c-gen2"},
		{`in:#GENERAL`, "in:#GENERAL", "c-gen2"},
		{`budget in:#incident-review review`, "budget review", "c-inc"},
		{`budget in:#nowhere`, "budget in:#nowhere", ""},
		{`begin:#sales plan`, "begin:#sales plan", ""},
		{`in:"nowhere" plan`, `in:"nowhere" plan`, ""},
	} {
		text, id, _ := SearchFilters(m, tc.query)
		if text != tc.text || id != tc.id {
			t.Errorf("SearchFilters(%q) = %q, %q; want %q, %q", tc.query, text, id, tc.text, tc.id)
		}
	}
}

// The scope chip is an offer until pressed, and what it appends names the
// channel the way the filter reads back.
func TestTodo_CHATBUG_072_ScopeChipIsAnOffer(t *testing.T) {
	m := chatbug072Rooms()
	m.SelectedID = "c-gen"
	m.Callbacks.Search = func(string) {}
	markup := renderNode(t, searchFilterBar(m, "item"))
	if !strings.Contains(markup, "Search only in #General Chat!") || strings.Contains(markup, ">In #General") {
		t.Fatalf("the chip does not read as an offer: %s", markup)
	}
	if !strings.Contains(markup, `data-extra="&#34;General Chat!&#34;"`) && !strings.Contains(markup, `data-extra="&quot;General Chat!&quot;"`) {
		t.Fatalf("the chip does not quote the name: %s", markup)
	}
	text, id, _ := SearchFilters(m, `item in:"General Chat!"`)
	if text != "item" || id != "c-gen" {
		t.Fatalf("what the chip appends does not read back: %q %q", text, id)
	}
	if strings.Contains(renderNode(t, searchFilterBar(m, `item in:"General Chat!"`)), "Search only in") {
		t.Fatal("the offer stays after the filter is in the query")
	}
	m.SelectedID = "c-sales"
	if got := renderNode(t, searchFilterBar(m, "item")); !strings.Contains(got, `data-extra="#sales"`) {
		t.Fatalf("a one-word name stays bare: %s", got)
	}
	for _, loc := range []string{"de-DE", "ar"} {
		m.Locale = loc
		if got := renderNode(t, searchFilterBar(m, "item")); strings.Contains(got, "Search only in") || strings.Contains(got, "chat.bug072") {
			t.Fatalf("%s: offer not localised: %s", loc, got)
		}
	}
}

func TestTodo_CHATBUG_072(t *testing.T) {
	m := Model{Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}}}
	plain := renderNode(t, createDialog(m, handlers{}))
	if strings.Contains(plain, "field-error") || strings.Contains(plain, `aria-invalid`) {
		t.Fatalf("an untouched name box already shows an error: %s", plain)
	}
	refused := renderNode(t, createDialog(m, handlers{local: localUI{nameRefused: true}}))
	for _, want := range []string{"field-error", `role="alert"`, "Use letters, numbers, dashes and underscores, up to 80 characters.", `aria-invalid="true"`, `aria-describedby="new-chat-name-hint"`} {
		if !strings.Contains(refused, want) {
			t.Errorf("refused name: %q missing from %s", want, refused)
		}
	}
	// The service's answer is shown for the name it refused, and goes away when a
	// different name is typed.
	m.NewNameRefused = "ab-"
	if got := renderNode(t, createDialog(m, handlers{local: localUI{nameTyped: "ab-"}})); !strings.Contains(got, "Use letters, numbers") {
		t.Fatalf("the service's refusal is not shown: %s", got)
	}
	if got := renderNode(t, createDialog(m, handlers{local: localUI{nameTyped: "abc"}})); strings.Contains(got, "field-error") {
		t.Fatalf("the refusal outlives the name it was for: %s", got)
	}
	// A group is named for its people: no rule, no hint.
	group := renderNode(t, createDialog(m, handlers{local: localUI{createKind: GroupChat, nameRefused: true}}))
	if strings.Contains(group, "field-error") || strings.Contains(group, "new-chat-name-hint") {
		t.Fatalf("a group name shows the channel rule: %s", group)
	}
}

func TestTodo_CHATBUG_072_SubmitNameFollowsTheRule(t *testing.T) {
	local := localStore{box: &localUI{}}
	for _, tc := range []struct {
		kind   ConversationKind
		typed  string
		name   string
		accept bool
	}{
		{PublicChannel, "  General Chat! ", "general-chat", true},
		{PrivateChannel, "design-reviews", "design-reviews", true},
		{PublicChannel, "!!!", "", false},
		{PublicChannel, "--", "", false},
		{GroupChat, "Walt, Sam & Loretta", "Walt, Sam & Loretta", true},
		{GroupChat, "  ", "", false},
	} {
		name, ok := createNameForSubmit(local, tc.kind, tc.typed)
		if name != tc.name || ok != tc.accept {
			t.Errorf("%s %q = %q, %v; want %q, %v", tc.kind, tc.typed, name, ok, tc.name, tc.accept)
		}
	}
	if !local.get().nameRefused {
		t.Error("a refused submit leaves no reason on screen")
	}
}

func TestTodo_CHATBUG_072_NameInputTracksRefusal(t *testing.T) {
	local := localStore{box: &localUI{}}
	m := Model{}
	createNameInput(m, local, "General Chat!")
	if st := local.get(); !st.nameRefused || st.nameTyped != "general-chat" {
		t.Fatalf("after a refused character: %+v", st)
	}
	createNameInput(m, local, "general-chat")
	if st := local.get(); st.nameRefused || st.nameTyped != "general-chat" {
		t.Fatalf("after the name is clean: %+v", st)
	}
	local.box.createKind = GroupChat
	createNameInput(m, local, "A!")
	if local.get().nameRefused {
		t.Fatal("a group name was judged by the channel rule")
	}
}

// The rule's sentence and the scope offer exist in every language the page
// ships, so neither the dialog nor the search bar can print a copy key.
func TestTodo_CHATBUG_072_Languages(t *testing.T) {
	for locale, words := range map[string][2]string{
		"en-US": {"Use letters, numbers, dashes and underscores", "Search only in #sales"},
		"de-DE": {"Buchstaben, Ziffern, Bindestriche und Unterstriche", "Nur in #sales suchen"},
		"ar":    {"استخدم الأحرف والأرقام", "البحث في #sales فقط"},
	} {
		m := Model{Locale: locale, NewNameRefused: "x", Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}}}
		dialog := renderNode(t, createDialog(m, handlers{local: localUI{nameRefused: true}}))
		if !strings.Contains(dialog, words[0]) || strings.Contains(dialog, "chat.bug072") {
			t.Errorf("%s: the dialog's rule sentence: %s", locale, dialog)
		}
		m.Conversations = []Conversation{{ID: "c-sales", Name: "sales", Kind: PublicChannel}}
		m.SelectedID = "c-sales"
		m.Callbacks.Search = func(string) {}
		if bar := renderNode(t, searchFilterBar(m, "item")); !strings.Contains(bar, words[1]) || strings.Contains(bar, "chat.bug072") {
			t.Errorf("%s: the scope offer: %s", locale, bar)
		}
	}
}

// Moving a name through the rule twice changes nothing: what the box lets
// through is what the service accepts, for any text.
func TestTodo_CHATBUG_072_RuleIsIdempotentAndAccepted(t *testing.T) {
	for _, typed := range []string{"General Chat!", "  spaces  ", "UPPER_case-1", "emoji 😀 name", `a/b\c`, "日本語 チャンネル", "💥"} {
		name, _ := chat.NormalizeChannelName(typed)
		again, refused := chat.NormalizeChannelName(name)
		if again != name || refused {
			t.Errorf("%q -> %q -> %q (refused %v)", typed, name, again, refused)
		}
		if name != "" && strings.ContainsAny(name, " !/"+`\`+"😀💥") {
			t.Errorf("%q keeps a refused character: %q", typed, name)
		}
	}
}
