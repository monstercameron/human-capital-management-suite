package chatui

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"strings"
	"testing"
	"time"
)

func chatcmd003UIFixture() Model {
	return Model{SelectedID: "room", CurrentUser: "me", Locale: "en-US", State: StateReady, Conversations: []Conversation{{ID: "room", Kind: PublicChannel, Name: "general", Joined: true}}, Members: []Member{{ID: "me", Name: "Author", HomeTenantID: "t"}, {ID: "dana", Name: "Dana", HomeTenantID: "t"}}}
}
func TestTodo_CHATCMD_003(t *testing.T) {
	m := chatcmd003UIFixture()
	posted := 0
	var sent chat.Chatcmd002Card
	var restored string
	m.Chatcmd002.Post = func(conversation, parent string, c chat.Chatcmd002Card, done func(error)) {
		posted++
		sent = c
		done(nil)
	}
	m.Callbacks.DraftChanged = func(_ string, text string) { restored = text }
	local := localStore{box: &localUI{}}
	raw := `/poll "Where?" 1="Here" 2="There"`
	if !chatcmd003Send(m, local, "chat-composer", raw) || !local.get().chatcmd003.Open || posted != 0 {
		t.Fatal("command posted without approval")
	}
	markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	if !strings.Contains(markup, `data-action="chatcmd003-post"`) || !strings.Contains(markup, "Here") || !strings.Contains(markup, "Where?") {
		t.Fatalf("preview %s", markup)
	}
	chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
	if posted != 0 || local.get().chatcmd003.Open || restored != raw {
		t.Fatalf("cancel posted=%d restored=%q", posted, restored)
	}
	chatcmd003Send(m, local, "chat-composer", raw)
	chatcmd003Action(m, local, "chatcmd003-post", "", "")
	if posted != 1 || sent.Validate() != nil || sent.Title != "Where?" || restored != "" || local.get().chatcmd003.Open {
		t.Fatalf("post=%d card=%+v restored=%q", posted, sent, restored)
	}
	if _, ok := defaultComposerCommands().lookup(m, "poll"); !ok {
		t.Fatal("poll absent from registry")
	}
	if _, ok := defaultComposerCommands().lookup(m, "todo"); !ok {
		t.Fatal("todo absent from registry")
	}
}
func TestTodo_CHATCMD_003_Property(t *testing.T) {
	t.Run("cancel in flight", func(t *testing.T) {
		m := chatcmd003UIFixture()
		m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) { t.Fatal("tidy posted") }
		var reply func(chat.Chatcmd003Draft, error)
		m.Chatcmd002.Tidy = func(_ string, _ chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error)) { reply = done }
		local := localStore{box: &localUI{}}
		chatcmd003Send(m, local, "chat-composer", `/poll "Q?" 1="A" 2="B"`)
		draft := local.get().chatcmd003.Draft
		chatcmd003Action(m, local, "chatcmd003-tidy", "", "")
		chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
		reply(draft, nil)
		if local.get().chatcmd003.Open {
			t.Fatal("cancelled preview reopened")
		}
	})
	m := chatcmd003UIFixture()
	posted := 0
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, _ func(error)) { posted++ }
	var reply func(chat.Chatcmd003Draft, error)
	m.Chatcmd002.Tidy = func(_ string, _ chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error)) { reply = done }
	local := localStore{box: &localUI{room: "room"}}
	chatcmd003Send(m, local, "chat-composer", `/poll "Q?" 1="A" 2="B"`)
	draft := local.get().chatcmd003.Draft
	chatcmd003Action(m, local, "chatcmd003-tidy", "", "")
	if posted != 0 || reply == nil || !local.get().chatcmd003.Busy {
		t.Fatal("tidy posted or did not start")
	}
	local.forRoom("different")
	reply(draft, nil)
	if local.get().chatcmd003.Open || local.get().chatcmd003.Busy || posted != 0 {
		t.Fatal("stale model response restored a preview")
	}
}
func TestTodo_CHATCMD_003_Security(t *testing.T) {
	m := chatcmd003UIFixture()
	posted := 0
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, _ func(error)) { posted++ }
	local := localStore{box: &localUI{}}
	chatcmd003Send(m, local, "chat-composer", `/poll "Q?" 1="One option"`)
	chatcmd003Action(m, local, "chatcmd003-post", "", "")
	if posted != 0 {
		t.Fatal("invalid poll posted")
	}
	m.Chatcmd002.Post = nil
	if !chatcmd003Send(m, local, "chat-composer", `/poll Q`) || local.get().composerNotice == "" {
		t.Fatal("unavailable command silently disappeared")
	}
	if _, ok := defaultComposerCommands().lookup(m, "poll"); ok {
		t.Fatal("unavailable command advertised")
	}
	d, _ := chat.Chatcmd003ParsePoll(`"<script>alert(1)</script>?" 1="<img onerror=alert(1)>" 2="Safe"`, time.Now(), nil)
	markup := renderNode(t, Chatcmd002RenderCard(m, "p", chat.Chatcmd002View{Card: d.Card, ResultsVisible: true}, true))
	if strings.Contains(markup, "<script>") || strings.Contains(markup, "<img onerror") {
		t.Fatal("card injected markup")
	}
}
func TestTodo_CHATCMD_003_Accessibility(t *testing.T) {
	t.Run("localized action and history labels", func(t *testing.T) {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			m := chatcmd003UIFixture()
			m.Locale = locale
			for _, key := range []string{"former-member", "cost", "apply-settings", "review-settings", "post", "cancel", "draft", "completed", "due", "assigned"} {
				if text := chatcmd003Text(m, key); text == "" || strings.ContainsAny(text, "⟦⟧") {
					t.Errorf("missing localized %s/%s", locale, key)
				}
			}
		}
	})
	m := chatcmd003UIFixture()
	d, _ := chat.Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, time.Now(), nil)
	markup := renderNode(t, chatcmd003PreviewView(m, chatcmd003Preview{Open: true, Conversation: "room", Draft: d, Editing: true, Error: "parse"}))
	for _, want := range []string{`role="region"`, `aria-labelledby="chatcmd003-heading"`, `for="chatcmd003-source"`, `aria-describedby="chatcmd003-usage chatcmd003-error"`, `aria-invalid="true"`, `id="chatcmd003-error"`, `role="alert"`, `data-action="chatcmd003-cancel"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(chatcmd002Styles, "overflow-wrap:anywhere") || !strings.Contains(chatcmd002Styles, "flex-wrap:wrap") || !strings.Contains(chatcmd002Styles, "var(--hcm-") {
		t.Fatal("card lacks wrapping or tokens")
	}
}
func TestTodo_CHATCMD_004(t *testing.T) {
	m := chatcmd003UIFixture()
	posted := 0
	m.Chatcmd002.Post = func(_, _ string, c chat.Chatcmd002Card, done func(error)) {
		posted++
		if c.Todo.Items[0].AssigneeID != "dana" {
			t.Error("lost assignee")
		}
		done(nil)
	}
	local := localStore{box: &localUI{}}
	m.ThreadParentID = "parent"
	if !chatcmd003Send(m, local, "thread-composer", `/todo "Launch" 1="Send invites @Dana"`) || posted != 0 || local.get().chatcmd003.Parent != "parent" {
		t.Fatal("thread did not preview")
	}
	chatcmd003Action(m, local, "chatcmd003-post", "", "")
	if posted != 1 {
		t.Fatalf("posts %d", posted)
	}
	if !chatcmd003Action(m, local, "composer-add", "chat-composer", "todo") || !local.get().chatcmd003.Editing || len(local.get().chatcmd003.Draft.Card.Todo.Items) != 0 {
		t.Fatal("add did not open empty draft")
	}
}
func TestTodo_CHATCMD_002(t *testing.T) {
	m := chatcmd003UIFixture()
	d, _ := chat.Chatcmd003ParsePoll(`"Q?" 1="A" 2="B" multiple=yes`, time.Now(), nil)
	d.Card.Poll.Options[0].ID = "a"
	d.Card.Poll.Options[1].ID = "b"
	view := chat.Chatcmd002View{Card: d.Card, MyOptions: []string{"a"}, ResultsVisible: true, CanManage: true}
	m.Messages = []Message{{ID: "post", Revision: 7}}
	m.Chatcmd002Views = map[string]Chatcmd002View{"post": view}
	var mutation chat.Chatcmd002Mutation
	revision := uint64(0)
	m.Chatcmd002.Mutate = func(_ string, r uint64, v chat.Chatcmd002Mutation) { revision = r; mutation = v }
	markup := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	if !strings.Contains(markup, `aria-pressed="true"`) || !strings.Contains(markup, "0 · 0%") {
		t.Fatalf("poll %s", markup)
	}
	chatcmd002Action(m, "chatcmd002-vote", "post", "b")
	if revision != 7 || strings.Join(mutation.Options, ",") != "a,b" {
		t.Fatalf("vote %+v rev %d", mutation, revision)
	}
	chatcmd002Action(m, "chatcmd002-close", "post", "CLOSE")
	if mutation.Operation != "CLOSE" {
		t.Fatal("close lost")
	}
	body, _ := d.Card.Body()
	if !strings.Contains(renderNode(t, chatcmd002ProjectedBody(m, Message{ID: "post", Body: body}, nil)), "Q?") {
		t.Fatal("timeline card lost")
	}
}
func TestTodo_CHATCMD_002_Accessibility(t *testing.T) {
	m := chatcmd003UIFixture()
	d, _ := chatcmd003Parse(m, "todo", `"Checklist" 1="Send invites @Dana"`)
	d.Card.Todo.Items[0].ID = "task"
	markup := renderNode(t, Chatcmd002RenderCard(m, "p", chat.Chatcmd002View{Card: d.Card, CanTick: map[string]bool{"task": true}}, false))
	for _, want := range []string{`role="checkbox"`, `aria-checked="false"`, `aria-label="Mark complete: Send invites"`, `Assigned to Dana`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %s: %s", want, markup)
		}
	}
}
