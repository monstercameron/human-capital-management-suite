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

// chatcmd003Refusal is a post the server refused, as the client reports it.
type chatcmd003Refusal string

func (e chatcmd003Refusal) Error() string { return "message card: " + string(e) }
func (e chatcmd003Refusal) Code() string  { return string(e) }

// TestTodo_CHATCMD_003: the composer is the draft. Typing a /poll line draws
// its preview with no press, Enter posts the card the preview shows, and
// Cancel leaves the composer empty.
func TestTodo_CHATCMD_003(t *testing.T) {
	m := chatcmd003UIFixture()
	posted := 0
	var sent chat.Chatcmd002Card
	draft := "unset"
	m.Chatcmd002.Post = func(conversation, parent string, c chat.Chatcmd002Card, done func(error)) {
		posted++
		sent = c
		done(nil)
	}
	m.Callbacks.DraftChanged = func(_ string, text string) { draft = text }
	local := localStore{box: &localUI{}}
	raw := `/poll "Where?" 1="Here" 2="There"`
	// Each keystroke is followed: the preview is there before any Enter.
	if !chatcmd003Follow(m, local, "chat-composer", raw) || !local.get().chatcmd003.Open || posted != 0 {
		t.Fatal("typing a /poll line did not draw its preview, or posted")
	}
	markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	if !strings.Contains(markup, `data-action="chatcmd003-post"`) || !strings.Contains(markup, "Here") || !strings.Contains(markup, "Where?") || strings.Contains(markup, "<textarea") {
		t.Fatalf("preview %s", markup)
	}
	chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
	if posted != 0 || local.get().chatcmd003.Open || draft != "" {
		t.Fatalf("cancel posted=%d open=%v draft=%q; the composer must be left empty", posted, local.get().chatcmd003.Open, draft)
	}
	draft = "unset"
	chatcmd003Follow(m, local, "chat-composer", raw)
	// Enter on the line the preview shows is Post.
	if !chatcmd003Send(m, local, "chat-composer", raw) || posted != 1 || sent.Validate() != nil || sent.Title != "Where?" || draft != "" || local.get().chatcmd003.Open {
		t.Fatalf("post=%d card=%+v draft=%q open=%v", posted, sent, draft, local.get().chatcmd003.Open)
	}
	// A line whose preview was never on the page (a draft from before a
	// reload, in a conversation already typed in) is shown first, not posted.
	if !chatcmd003Send(m, local, "chat-composer", raw) || posted != 1 || !local.get().chatcmd003.Open {
		t.Fatalf("Enter posted a card nobody had seen: posts %d", posted)
	}
	if !chatcmd003Send(m, local, "chat-composer", raw) || posted != 2 {
		t.Fatalf("the second Enter did not post: %d", posted)
	}
	// "/poll" and Enter chooses the command and opens its empty preview.
	if !chatcmd003Send(m, local, "chat-composer", "/poll") || posted != 2 || !local.get().chatcmd003.Open || local.get().chatcmd003.Draft.Raw != "" {
		t.Fatalf("bare /poll: posts %d, preview %+v", posted, local.get().chatcmd003)
	}
	if usage := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); !strings.Contains(usage, "Write the question, then the options") || strings.Contains(usage, "chatcmd002-card") {
		t.Fatalf("empty preview: %s", usage)
	}
	// Text that stops being a card command closes the preview it had opened.
	if chatcmd003Follow(m, local, "chat-composer", "hello") || local.get().chatcmd003.Open {
		t.Fatal("ordinary text kept a preview open")
	}
	if chatcmd003Send(m, local, "chat-composer", "hello") {
		t.Fatal("ordinary text was taken for a card")
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
		chatcmd003Follow(m, local, "chat-composer", `/poll "Q?" 1="A" 2="B"`)
		draft := local.get().chatcmd003.Draft
		chatcmd003Action(m, local, "chatcmd003-tidy", "", "")
		chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
		reply(draft, nil)
		if local.get().chatcmd003.Open {
			t.Fatal("cancelled preview reopened")
		}
	})
	t.Run("typing drops a model answer for the earlier text", func(t *testing.T) {
		m := chatcmd003UIFixture()
		m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
		var reply func(chat.Chatcmd003Draft, error)
		m.Chatcmd002.Tidy = func(_ string, _ chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error)) { reply = done }
		local := localStore{box: &localUI{}}
		chatcmd003Follow(m, local, "chat-composer", `/poll "Q?" 1="A" 2="B"`)
		stale := local.get().chatcmd003.Draft
		chatcmd003Action(m, local, "chatcmd003-tidy", "", "")
		chatcmd003Follow(m, local, "chat-composer", `/poll "Q?" 1="A" 2="B" 3="C"`)
		reply(stale, nil)
		if state := local.get().chatcmd003; state.Busy || len(state.Draft.Card.Poll.Options) != 3 {
			t.Fatalf("a model answer for the earlier text replaced the preview: %+v", state.Draft.Card.Poll)
		}
	})
	m := chatcmd003UIFixture()
	posted := 0
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, _ func(error)) { posted++ }
	var reply func(chat.Chatcmd003Draft, error)
	m.Chatcmd002.Tidy = func(_ string, _ chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error)) { reply = done }
	local := localStore{box: &localUI{room: "room"}}
	chatcmd003Follow(m, local, "chat-composer", `/poll "Q?" 1="A" 2="B"`)
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
	// A poll that cannot be posted is not posted, by Enter or by the button,
	// and its line is not sent as a message either.
	line := `/poll "Q?" 1="One option"`
	chatcmd003Follow(m, local, "chat-composer", line)
	chatcmd003Action(m, local, "chatcmd003-post", "", "")
	if !chatcmd003Send(m, local, "chat-composer", line) || posted != 0 {
		t.Fatal("invalid poll posted, or its line left to be sent as text")
	}
	if markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); !strings.Contains(markup, "A poll needs a question and at least two different options.") || !strings.Contains(markup, "disabled") {
		t.Fatalf("the preview does not say why it cannot post: %s", markup)
	}
	// A quote left open is said, and nothing is posted from it.
	broken := `/poll "Q? 1="A" 2="B"`
	chatcmd003Follow(m, local, "chat-composer", broken)
	if markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); !strings.Contains(markup, "A quote is not closed") || !strings.Contains(markup, `role="alert"`) {
		t.Fatalf("unreadable line: %s", markup)
	}
	if !chatcmd003Send(m, local, "chat-composer", broken) || posted != 0 {
		t.Fatal("an unreadable line posted")
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
			for key := range chatcmd003Copy {
				if text := chatcmd003Text(m, key); text == "" || strings.ContainsAny(text, "⟦⟧") {
					t.Errorf("missing localized %s/%s", locale, key)
				}
			}
		}
	})
	m := chatcmd003UIFixture()
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	d, _ := chatcmd003Parse(m, "poll", `"Q?" 1="A" 2="B" closes=2030-01-02`)
	markup := renderNode(t, chatcmd003PreviewView(m, chatcmd003Preview{Open: true, Conversation: "room", Draft: d, Error: "post-denied"}))
	for _, want := range []string{`role="region"`, `aria-labelledby="chatcmd003-heading"`, `id="chatcmd003-error"`, `role="alert"`, `data-action="chatcmd003-cancel"`,
		// Each setting is a labelled group of buttons that say which is chosen.
		`role="switch"`, `aria-checked="false"`, `role="radiogroup"`, `aria-labelledby="chatcmd003-results-label"`, `role="radio"`, `aria-checked="true"`,
		`data-action="chatcmd003-set"`, `data-id="closes"`, `data-extra="hour"`, "In an hour", "End of day", "Tomorrow", "In a week", "Never", "2030-01-02 23:59",
		"Enter posts", "Esc cancels", "Post poll"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(chatcmd002Styles, "overflow-wrap:anywhere") || !strings.Contains(chatcmd002Styles, "flex-wrap:wrap") || !strings.Contains(chatcmd002Styles, "var(--hcm-") {
		t.Fatal("card lacks wrapping or tokens")
	}
	if !strings.Contains(chatcmd003Styles, ".chatcmd003-choice:focus-visible") || !strings.Contains(chatcmd003Styles, "@media(pointer:coarse){.chat-workspace .chatcmd003-choice{min-block-size:44px") {
		t.Fatal("the preview's controls lack a focus ring or a touch size")
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
	if !chatcmd003Action(m, local, "composer-add", "chat-composer", "todo") || !local.get().chatcmd003.Open || local.get().chatcmd003.Draft.Card.Kind != "todo" || len(local.get().chatcmd003.Draft.Card.Todo.Items) != 0 {
		t.Fatal("add did not open empty draft")
	}
	// The finding of 2026-10-02: two tasks, the date on the first, the
	// assignee only on the task that names them, and what changed said once.
	chatcmd003Follow(m, local, "chat-composer", "/todo Order pizza @Dana friday; Book room")
	state := local.get().chatcmd003
	items := state.Draft.Card.Todo.Items
	if len(items) != 2 || items[0].Text != "Order pizza" || items[0].AssigneeID != "dana" || items[0].DueAt == nil || items[0].DueAt.Weekday() != time.Friday || items[1].Text != "Book room" || items[1].AssigneeID != "" || items[1].DueAt != nil {
		t.Fatalf("tasks %+v", items)
	}
	markup := renderNode(t, chatcmd003PreviewView(m, state))
	for _, want := range []string{"Tidied: 3 changes", "One line became 2 tasks", "Order pizza: assigned to Dana", "Order pizza: due " + items[0].DueAt.Format("2006-01-02"), "Assigned to Dana", "Post list",
		`data-id="tick"`, `data-extra="assignee"`, "The assignee"} {
		if !strings.Contains(markup, want) {
			t.Errorf("list preview lacks %q", want)
		}
	}
	if strings.Contains(markup, "<del>") || strings.Contains(markup, "Use what I typed") {
		t.Errorf("a change with the same text on both sides, or a way back to wording that did not change: %s", markup)
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
	t.Run("the bar under the header lists open cards posted here", func(t *testing.T) {
		list, _ := chatcmd003Parse(m, "todo", `"Launch" 1="Book the room" 2="Send invites"`)
		list.Card.Todo.Items[0].ID, list.Card.Todo.Items[1].ID = "one", "two"
		listBody, _ := list.Card.Body()
		closed := d.Card
		at := time.Now().Add(-time.Hour)
		closed.ClosedAt = &at
		closedBody, _ := closed.Body()
		m.Messages = []Message{{ID: "poll", Body: body}, {ID: "plain", Body: "hello"}, {ID: "closed", Body: closedBody}, {ID: "list", Body: listBody}}
		done := list.Card
		done.Todo.Items[0].Completed = true
		m.Chatcmd002Views = map[string]Chatcmd002View{"list": {Card: done}}
		bar := renderNode(t, channelTray(m, handlers{}, ""))
		if strings.Count(bar, `data-action="chatcmd002-jump"`) != 2 || !strings.Contains(bar, `data-id="poll"`) || !strings.Contains(bar, `data-id="list"`) || strings.Contains(bar, `data-id="closed"`) {
			t.Fatalf("chips for open cards: %s", bar)
		}
		// The server's view decides what a chip says, not the body.
		if !strings.Contains(bar, "Launch · 1 of 2 done") || !strings.Contains(bar, "Q?") || !strings.Contains(bar, "Go to this message") {
			t.Fatalf("chip labels: %s", bar)
		}
		if !chatcmd002Action(m, "chatcmd002-jump", "poll", "") {
			t.Fatal("a chip press was not handled")
		}
	})
	t.Run("a card read as text is its words", func(t *testing.T) {
		if got := Chatcmd002PlainBody(body); got != "Q?\nA\nB" {
			t.Fatalf("plain body %q", got)
		}
		for _, plain := range []string{"hello", "x" + chat.Chatcmd002BodyMarker + "{}"} {
			if got := Chatcmd002PlainBody(plain); got != plain {
				t.Fatalf("an ordinary message changed: %q", got)
			}
		}
	})
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
