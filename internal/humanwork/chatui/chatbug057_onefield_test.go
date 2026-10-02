package chatui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	xhtml "golang.org/x/net/html"
)

// chatbug057FieldPath is where a composer's text field stands in its form: the
// position of each ancestor among its parent's children, from the form down
// to the field.
func chatbug057FieldPath(t *testing.T, markup, form, field string) []int {
	t.Helper()
	nodes := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "textarea" && chatPolishAttr(n, "id") == field })
	if len(nodes) != 1 {
		t.Fatalf("%d fields named %s", len(nodes), field)
	}
	var path []int
	for n := nodes[0]; n != nil && !(n.Data == "form" && chatPolishHasClass(n, form)); n = n.Parent {
		index := 0
		for sibling := n.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
			if sibling.Type == xhtml.ElementNode {
				index++
			}
		}
		path = append([]int{index}, path...)
	}
	return path
}

// TestTodo_CHATBUG_057_OneField is the page finding of 2026-10-02: Add, Poll
// opened a preview with its own "Write your draft" box while the composer under
// it stayed live, so the poll typed into the composer was sent as an ordinary
// message. There is one field now. Add, Poll writes "/poll " into the composer
// and the composer is the draft: the preview is drawn above it from what it
// holds, Enter posts the card, Escape or Cancel leaves it empty, and a line that
// is the draft of a card is never sent as text.
func TestTodo_CHATBUG_057_OneField(t *testing.T) {
	m := composerToolsModel("en-US", false)
	var posted []chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, card chat.Chatcmd002Card, done func(error)) {
		posted = append(posted, card)
		done(nil)
	}
	draft := "unset"
	m.Callbacks.DraftChanged = func(_ string, text string) { draft = text }
	local := localStore{box: &localUI{}}

	closed := composerToolsMarkup(t, m, local.get())
	if !strings.Contains(closed, `class="chatcmd003-slot"`) || strings.Contains(closed, "chatcmd003-preview") {
		t.Fatalf("a closed preview must keep its empty place in the composer: %s", closed)
	}
	if !chatcmd003Action(m, local, "composer-add", "chat-composer", "poll") {
		t.Fatal("Add, Poll was not taken")
	}
	open := composerToolsMarkup(t, m, local.get())
	// The preview brings no box of its own to write in: the composer is the
	// only field of the draft.
	inPreview := chatPolishNodes(t, open, func(n *xhtml.Node) bool {
		return (n.Data == "textarea" || n.Data == "input" || n.Data == "select") && chatPolishAncestor(n, "chatcmd003-slot")
	})
	if len(inPreview) != 0 || strings.Count(open, "<textarea") != strings.Count(closed, "<textarea") || strings.Contains(open, "Write your draft") {
		t.Fatalf("the preview has %d fields of its own", len(inPreview))
	}
	for _, want := range []string{"Poll preview", "Write the question, then the options", "Where for lunch? Tacos, pho or pizza", `class="chatcmd003-slot"`} {
		if !strings.Contains(open, want) {
			t.Errorf("the composer as a poll draft lacks %q", want)
		}
	}
	// The field is the same child of the same parents open or closed, so the
	// page does not make it again and the caret stays in it.
	if before, after := chatbug057FieldPath(t, closed, "chat-composer", "chat-composer"), chatbug057FieldPath(t, open, "chat-composer", "chat-composer"); len(before) == 0 || !chatbug057SamePath(before, after) {
		t.Fatalf("the field moved from %v to %v when the preview opened", before, after)
	}

	// What is typed is drawn at once, with no Preview press.
	line := "/poll Where for lunch? Tacos, Pho, Salad"
	if !chatcmd003Follow(m, local, "chat-composer", line) {
		t.Fatal("typing did not keep the preview")
	}
	typed := composerToolsMarkup(t, m, local.get())
	for _, want := range []string{"Where for lunch?", "Tacos", "Pho", "Salad", "Post poll", "Enter posts"} {
		if !strings.Contains(typed, want) {
			t.Errorf("the preview drawn from the composer lacks %q", want)
		}
	}
	if strings.Contains(typed, `data-action="chatcmd003-preview"`) || strings.Contains(typed, `data-action="chatcmd003-edit"`) {
		t.Error("the preview still asks for a Preview or Edit press")
	}

	// Enter posts the card. The line is never left to be sent as a message:
	// not when the card is ready, and not when it cannot be posted yet.
	short := "/poll Where for lunch?"
	chatcmd003Follow(m, local, "chat-composer", short)
	if !chatcmd003Send(m, local, "chat-composer", short) || len(posted) != 0 {
		t.Fatalf("a question with no options: posted %d cards, or left to be sent as text", len(posted))
	}
	if note := composerToolsMarkup(t, m, local.get()); !strings.Contains(note, "A poll needs a question and at least two different options.") {
		t.Fatal("a poll that cannot be posted does not say why")
	}
	chatcmd003Follow(m, local, "chat-composer", line)
	if !chatcmd003Send(m, local, "chat-composer", line) || len(posted) != 1 {
		t.Fatalf("Enter posted %d cards", len(posted))
	}
	if posted[0].Title != "Where for lunch?" || len(posted[0].Poll.Options) != 3 || posted[0].Poll.Options[2].Text != "Salad" {
		t.Fatalf("posted card %+v", posted[0])
	}
	if state := local.get().chatcmd003; state.Open || draft != "" {
		t.Fatalf("after Post the preview is open=%v and the draft is %q; the composer must be empty", state.Open, draft)
	}

	// What the composer already holds when the menu is used becomes the draft.
	if value := chatcmd003AddLine(m, "chat-composer", "poll", "  Lunch on Friday?  "); value != "/poll Lunch on Friday?" {
		t.Fatalf("Add, Poll over ordinary text wrote %q", value)
	}
	if value := chatcmd003AddLine(m, "chat-composer", "todo", "/poll Where? Here, There"); value != "/todo Where? Here, There" {
		t.Fatalf("Add, To-do list over a /poll line wrote %q", value)
	}
	if value := chatcmd003AddLine(m, "chat-composer", "todo", "/poll"); value != "/todo " {
		t.Fatalf("Add, To-do list over a bare /poll wrote %q", value)
	}

	// Cancel, and Escape, close the preview and leave the composer empty.
	draft = "unset"
	chatcmd003Action(m, local, "composer-add", "chat-composer", "todo")
	chatcmd003Follow(m, local, "chat-composer", "/todo Order pizza; Book room")
	chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
	if state := local.get().chatcmd003; state.Open || draft != "" || len(posted) != 1 {
		t.Fatalf("after Cancel the preview is open=%v, the draft %q, posts %d", state.Open, draft, len(posted))
	}
	if chatcmd003Send(m, local, "chat-composer", "hello") {
		t.Fatal("after Cancel ordinary text is taken for a card")
	}
}

// chatbug057Text is the text a node shows.
func chatbug057Text(n *xhtml.Node) string {
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(chatbug057Text(child))
	}
	return text.String()
}

func chatbug057SamePath(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTodo_CHATBUG_057_Refused: a post the server refuses leaves the draft
// where it is and says why beside the preview, in words for each refusal.
func TestTodo_CHATBUG_057_Refused(t *testing.T) {
	for refusal, want := range map[error]string{
		chatcmd003Refusal("permission_denied"): "Not posted: you cannot post here, or a person a task names is not a member of this conversation. Your draft is kept.",
		chatcmd003Refusal("invalid_argument"):  "Not posted: the server did not accept this card.",
		chatcmd003Refusal("not_found"):         "Not posted: this conversation is no longer available.",
		chatcmd003Refusal("unavailable"):       "Could not post: the server did not answer. Your draft is kept; press Enter to try again.",
		errors.New("anything else"):            "Could not post: the server did not answer.",
	} {
		m := composerToolsModel("en-US", false)
		attempts := 0
		var answer func(error)
		m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, done func(error)) { attempts++; answer = done }
		cleared := 0
		m.Callbacks.DraftChanged = func(_ string, text string) {
			if text == "" {
				cleared++
			}
		}
		local := localStore{box: &localUI{}}
		line := "/todo Order pizza @Loretta friday; Book room"
		chatcmd003Follow(m, local, "chat-composer", line)
		if !chatcmd003Send(m, local, "chat-composer", line) || attempts != 1 {
			t.Fatalf("%v: Enter made %d posts", refusal, attempts)
		}
		// While the answer is on its way the preview says so, takes no second
		// press, and does not follow the composer.
		waiting := composerToolsMarkup(t, m, local.get())
		if !strings.Contains(waiting, "Posting…") || !chatcmd003Send(m, local, "chat-composer", line) || attempts != 1 {
			t.Fatalf("%v: a second Enter while posting made %d posts", refusal, attempts)
		}
		answer(refusal)
		state := local.get().chatcmd003
		if !state.Open || state.Busy || state.Draft.Raw != "Order pizza @Loretta friday; Book room" || cleared != 0 {
			t.Fatalf("%v: the draft was not kept: open=%v busy=%v raw=%q cleared=%d", refusal, state.Open, state.Busy, state.Draft.Raw, cleared)
		}
		markup := composerToolsMarkup(t, m, local.get())
		alerts := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return chatPolishAttr(n, "id") == "chatcmd003-error" && chatPolishAttr(n, "role") == "alert" && chatPolishAncestor(n, "chatcmd003-preview")
		})
		if len(alerts) != 1 || !strings.Contains(chatbug057Text(alerts[0]), want) {
			t.Fatalf("%v: the preview does not say why: %s", refusal, markup)
		}
		// Enter again is the same post, and when it is accepted the composer is emptied.
		if !chatcmd003Send(m, local, "chat-composer", line) || attempts != 2 {
			t.Fatalf("%v: Enter after a refusal made %d posts", refusal, attempts)
		}
		answer(nil)
		if local.get().chatcmd003.Open || cleared != 1 {
			t.Fatalf("%v: an accepted post left the preview open or the draft in place", refusal)
		}
	}
}

// TestTodo_CHATBUG_057_Settings: the preview's controls are buttons, so a
// choice reaches the card on the press, holds while the text changes, and is
// what is posted.
func TestTodo_CHATBUG_057_Settings(t *testing.T) {
	m := composerToolsModel("en-US", false)
	m.Chatcmd002TimeZone = "UTC"
	var posted []chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, card chat.Chatcmd002Card, done func(error)) {
		posted = append(posted, card)
		done(nil)
	}
	local := localStore{box: &localUI{}}
	line := "/poll Where? Here, There"
	chatcmd003Follow(m, local, "chat-composer", line)
	for _, press := range [][2]string{{"multiple", "yes"}, {"anonymous", "yes"}, {"results", "after-closing"}, {"closes", "hour"}} {
		if !chatcmd003Action(m, local, "chatcmd003-set", press[0], press[1]) {
			t.Fatalf("%v was not taken", press)
		}
	}
	card := local.get().chatcmd003.Draft.Card
	if !card.Poll.Multiple || !card.Poll.Anonymous || card.Poll.Results != "after-closing" || card.Poll.ClosesAt == nil {
		t.Fatalf("the choices did not reach the card: %+v", card.Poll)
	}
	markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	for _, want := range []string{"Several choices", "Anonymous", "Closes "} {
		if !strings.Contains(markup, want) {
			t.Errorf("the card in the preview does not show %q", want)
		}
	}
	// More typing reads the text again and keeps what was chosen.
	line += ", Elsewhere"
	chatcmd003Follow(m, local, "chat-composer", line)
	if card = local.get().chatcmd003.Draft.Card; len(card.Poll.Options) != 3 || !card.Poll.Multiple || !card.Poll.Anonymous || card.Poll.Results != "after-closing" || card.Poll.ClosesAt == nil {
		t.Fatalf("typing dropped the choices: %+v", card.Poll)
	}
	chatcmd003Action(m, local, "chatcmd003-set", "multiple", "no")
	before := time.Now()
	chatcmd003Send(m, local, "chat-composer", line)
	if len(posted) != 1 || posted[0].Poll.Multiple || !posted[0].Poll.Anonymous || posted[0].Poll.Results != "after-closing" {
		t.Fatalf("posted %+v", posted)
	}
	if closes := posted[0].Poll.ClosesAt; closes == nil || closes.Before(before.Add(59*time.Minute)) || closes.After(time.Now().Add(61*time.Minute)) {
		t.Fatalf("\"In an hour\" posted as %v", closes)
	}
	// A date typed in the text is one more choice, and Never drops it.
	chatcmd003Follow(m, local, "chat-composer", "/poll Where? Here, There closes=2031-05-06")
	typed := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	if !strings.Contains(typed, "2031-05-06 23:59") || !strings.Contains(typed, `data-extra="typed"`) {
		t.Fatalf("the typed closing date is not offered: %s", typed)
	}
	chatcmd003Action(m, local, "chatcmd003-set", "closes", "never")
	if local.get().chatcmd003.Draft.Card.Poll.ClosesAt != nil {
		t.Fatal("Never left a closing time")
	}
	chatcmd003Action(m, local, "chatcmd003-set", "closes", "typed")
	if at := local.get().chatcmd003.Draft.Card.Poll.ClosesAt; at == nil || at.Format("2006-01-02") != "2031-05-06" {
		t.Fatalf("the typed date did not come back: %v", at)
	}
	// A list's one setting.
	chatcmd003Follow(m, local, "chat-composer", "/todo Ship it")
	chatcmd003Action(m, local, "chatcmd003-set", "tick", "author")
	if local.get().chatcmd003.Draft.Card.Todo.Tick != "author" {
		t.Fatal("who may complete items was not set")
	}
}

// TestTodo_CHATBUG_057_Reload is page finding 4: a /todo line left in the
// composer came back after Post and after a reload, and typing landed in the
// middle of it. The saved draft of a conversation the person has not typed in
// yet shows its preview again, Enter posts what that preview shows, and after
// Post or Cancel the draft is filed as empty.
func TestTodo_CHATBUG_057_Reload(t *testing.T) {
	m := composerToolsModel("en-US", false)
	m.Draft = "/todo Order pizza; Book room"
	var posted []chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, card chat.Chatcmd002Card, done func(error)) {
		posted = append(posted, card)
		done(nil)
	}
	draft := "unset"
	m.Callbacks.DraftChanged = func(_ string, text string) { draft = text }
	local := localStore{box: &localUI{}}
	restored := composerToolsMarkup(t, m, local.get())
	if !strings.Contains(restored, "To-do list preview") || !strings.Contains(restored, "Order pizza") || !strings.Contains(restored, "Book room") {
		t.Fatalf("the saved /todo draft does not show its preview after a reload: %s", restored)
	}
	if !chatcmd003Send(m, local, "chat-composer", m.Draft) || len(posted) != 1 || len(posted[0].Todo.Items) != 2 || draft != "" {
		t.Fatalf("Enter on the restored draft: posts %d, draft %q", len(posted), draft)
	}
	// The conversation has been typed in: the model's older copy of the draft
	// does not bring the preview back.
	if again := composerToolsMarkup(t, m, local.get()); strings.Contains(again, "chatcmd003-preview") {
		t.Fatal("the preview came back from the model's stale draft after Post")
	}
	// Another conversation starts untouched; Escape there cancels its draft.
	local.forRoom("elsewhere")
	if state := local.get().chatcmd003; state.Open || state.Touched {
		t.Fatalf("the preview state crossed into another conversation: %+v", state)
	}
}
