package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// Thursday 1 October 2026, half past eight in the morning, in a fixed zone: the
// reader's clock for every test below.
var chatsave002Now = time.Date(2026, time.October, 1, 8, 30, 0, 0, time.UTC)

func chatsave002Rows() []SavedMessageRow {
	body := "@Ben Whitaker can you check the guide doc:" + chatbug034Doc + " and this " + chatbug037Address + "\n**bold** line two"
	return []SavedMessageRow{
		{TenantID: "tenant", ConversationID: "general", PostID: "p1", AuthorID: "ben", Author: "Walt Brennan", Channel: "general", InChannel: true, SentAt: chatsave002Now.Add(-90 * time.Minute), Body: body, Availability: "readable", Sequence: 7, Revision: 1, Attachments: 2,
			References: []ChatReference{{Kind: "PERSON_MENTION", TenantID: "tenant", ID: "ben", Display: "Ben Whitaker"}},
			DueAt:      time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC), Note: "ask Ben on Friday"},
		{TenantID: "tenant", ConversationID: "design", PostID: "p2", AuthorID: "ana", Author: "Ana Flores", Channel: "design", InChannel: true, SentAt: chatsave002Now.Add(-30 * time.Hour), Body: "Second message", Availability: "readable", Sequence: 3,
			DueAt: time.Date(2026, time.September, 30, 17, 0, 0, 0, time.UTC)},
		{TenantID: "tenant", ConversationID: "general", PostID: "p3", AuthorID: "ana", Author: "Ana Flores", Channel: "general", InChannel: true, SentAt: chatsave002Now.Add(-72 * time.Hour), Body: "Third, finished", Availability: "readable", Sequence: 2, Done: true, DoneAt: chatsave002Now.Add(-time.Hour)},
		{TenantID: "tenant", ConversationID: "gone", PostID: "p4", Availability: "no_access"},
	}
}

func chatsave002View(tab string, rows []SavedMessageRow) SavedMessagesView {
	m := chatbug037Model("ready")
	m.Members = []Member{{ID: "ben", HomeTenantID: "tenant", Name: "Ben Whitaker"}}
	return SavedMessagesView{Locale: "en-US", Tab: tab, Rows: rows, Now: chatsave002Now, Model: m, TodoCount: 2, DoneCount: 1, AllCount: 3,
		DocPreviews: map[string]DocPreview{chatbug034Doc: {ID: chatbug034Doc, Title: "Open enrollment guide", Readable: true, State: "ready"}}}
}

func chatsave002Find(t *testing.T, markup string, match func(*xhtml.Node) bool) []*xhtml.Node {
	t.Helper()
	return chatPolishNodes(t, markup, match)
}

func chatsave002Class(n *xhtml.Node, class string) bool { return chatPolishHasClass(n, class) }

func chatsave002Descendants(n *xhtml.Node, tag string) int {
	count := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == tag {
			count++
		}
		count += chatsave002Descendants(c, tag)
	}
	return count
}

func chatsave002NodeText(n *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			out.WriteString(n.Data)
			out.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(out.String()), " ")
}

// TestTodo_CHATSAVE_002 pins the Saved panel's design: an item is the message it
// is (its author, its conversation, its time, its text through the
// conversation's own renderer), the four actions are icons in one bar, a reminder
// is a chip, a note is a line, and a done item has one action.
func TestTodo_CHATSAVE_002(t *testing.T) {
	view := chatsave002View("todo", chatsave002Rows()[:2])
	markup := chatPolishMarkup(t, RenderSavedMessages(view), 1280, "light")

	items := chatsave002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "li" && chatsave002Class(n, "chatsave-item") })
	if len(items) != 2 {
		t.Fatalf("want two items, got %d: %s", len(items), markup)
	}
	// The overdue reminder sorts first.
	if chatPolishAttr(items[0], "data-saved-post") != "p2" || chatPolishAttr(items[1], "data-saved-post") != "p1" {
		t.Fatalf("an overdue item must come first: %s %s", chatPolishAttr(items[0], "data-saved-post"), chatPolishAttr(items[1], "data-saved-post"))
	}
	first := chatsave002NodeText(items[1])
	for _, want := range []string{"Walt Brennan", "in #general", "7:00 AM", "Ben Whitaker", "Open enrollment guide", "a message in #design", "bold", "line two", "ask Ben on Friday", "Tomorrow 9:00 AM"} {
		if !strings.Contains(first, want) {
			t.Errorf("the item does not read %q: %s", want, first)
		}
	}
	// No identifier, address or document token reaches the page.
	for _, leak := range []string{"doc:", "47892b80", chatbug037Token, "aXJvbnJpZGdl", "|"} {
		if strings.Contains(first, leak) {
			t.Errorf("the item prints %q: %s", leak, first)
		}
	}
	// The message is drawn by the conversation's own renderer: its body is the very
	// markup the conversation draws for the same message.
	row := chatsave002Rows()[0]
	m := chatsave002Model(view)
	conversation := message(m, handlers{}, chatsave002Message(row), false)
	bodyOf := func(markup string) string {
		for _, body := range regexp.MustCompile(`(?s)<div class="message-body"[^>]*>.*?</div>`).FindAllString(markup, -1) {
			if strings.Contains(body, "guide") {
				return body
			}
		}
		return ""
	}
	want := bodyOf(renderNode(t, conversation))
	got := bodyOf(markup)
	if want == "" || got == "" || want != got {
		t.Fatalf("the saved text is not the conversation's rendering:\nconversation: %s\nsaved:        %s", want, got)
	}
	if !strings.Contains(got, `class="mention-chip`) || !strings.Contains(got, `class="chat-doc-reference"`) || !strings.Contains(got, `class="chat-share-link"`) {
		t.Fatalf("the mention, the document and the share link are not drawn as in the conversation: %s", got)
	}
	// The author line: an avatar, the name in bold, the conversation as a link, a time.
	for _, want := range []string{`class="avatar small chatsave-avatar"`, `<strong class="chatsave-author" dir="auto">Walt Brennan</strong>`, `class="chatsave-where"`, `href="/workspace/app/chat#channel=general"`, `data-saved-action="open-channel"`, `<time class="chatsave-time">`} {
		if !strings.Contains(markup, want) {
			t.Errorf("the author line lacks %s", want)
		}
	}
	// The attachment indicator is the conversation's.
	if !strings.Contains(markup, `class="chat-embed-attachments"`) || !strings.Contains(markup, "2 attachments") {
		t.Errorf("the attachment indicator is missing")
	}
	// The whole item is one target: it opens the message, from a click or Enter.
	if chatPolishAttr(items[1], "data-saved-action") != "open" || chatPolishAttr(items[1], "tabindex") != "0" || chatPolishAttr(items[1], "data-saved-sequence") != "7" {
		t.Errorf("the item is not one focusable target that opens the message")
	}
	// Four icon buttons, in order, each named; no text buttons, no disclosure rows.
	buttons := chatsave002Find(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "button" && chatsave002Class(n, "chatsave-act") && chatPolishAttr(n, "data-saved-post") == "p1"
	})
	if len(buttons) != 4 {
		t.Fatalf("want four action buttons, got %d", len(buttons))
	}
	for i, action := range []struct{ act, name string }{{"done", "Mark done"}, {"remind", "Remind me"}, {"note", "Edit note"}, {"remove", "Remove from Saved"}} {
		b := buttons[i]
		if chatPolishAttr(b, "data-saved-action") != action.act || chatPolishAttr(b, "aria-label") != action.name || chatPolishAttr(b, "title") != action.name || chatsave002NodeText(b) != "" {
			t.Errorf("action %d: want icon button %q named %q, got %s %q %q", i, action.act, action.name, chatPolishAttr(b, "data-saved-action"), chatPolishAttr(b, "aria-label"), chatsave002NodeText(b))
		}
		if chatsave002Descendants(b, "svg") != 1 {
			t.Errorf("action %d draws no single icon", i)
		}
	}
	for _, forbidden := range []string{"chatsave-row", "<textarea", "chat-disclosure", "chatsave-button", "chatsave-open", "chatsave-private", `type="datetime-local"`} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("the old design's %q is still drawn", forbidden)
		}
	}
	// A note is one muted line with the pencil, naming what it does.
	if !strings.Contains(markup, `class="chatsave-note"`) || !strings.Contains(markup, "icon-edit") {
		t.Errorf("the note is not a line with the pencil")
	}
	// A reminder is a chip with a clear control; an overdue one says so.
	if !strings.Contains(markup, `class="chatsave-chip"`) || !strings.Contains(markup, `aria-label="Remove reminder"`) || !strings.Contains(markup, `class="chatsave-chip is-overdue"`) || !strings.Contains(markup, "Overdue · Yesterday 5:00 PM") {
		t.Errorf("the reminder chips are wrong: %s", markup)
	}
}

func TestTodo_CHATSAVE_002_Done(t *testing.T) {
	view := chatsave002View("done", chatsave002Rows()[2:3])
	markup := chatPolishMarkup(t, RenderSavedMessages(view), 1280, "light")
	items := chatsave002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "li" && chatsave002Class(n, "chatsave-item") })
	if len(items) != 1 || !chatsave002Class(items[0], "is-done") {
		t.Fatalf("a done item is drawn muted: %s", markup)
	}
	text := chatsave002NodeText(items[0])
	if !strings.Contains(text, "Done Today") || !strings.Contains(text, "Reopen") {
		t.Errorf("a done item says when it was done and offers Reopen: %s", text)
	}
	// One action only.
	for _, gone := range []string{`data-saved-action="done"`, `data-saved-action="remind"`, `data-saved-action="note"`, `data-saved-action="remove"`, "chatsave-actions"} {
		if strings.Contains(markup, gone) {
			t.Errorf("a done item still offers %s", gone)
		}
	}
	if strings.Count(markup, `data-saved-action="reopen"`) != 1 {
		t.Errorf("a done item has exactly one action, Reopen")
	}
}

func TestTodo_CHATSAVE_002_Unavailable(t *testing.T) {
	copy := SavedMessagesCopy("en-US")
	markup := renderNode(t, RenderSavedMessages(chatsave002View("todo", chatsave002Rows()[3:])))
	if !strings.Contains(markup, copy.NoAccess) || strings.Count(markup, `data-saved-action="remove"`) != 1 || strings.Contains(markup, `data-saved-action="open"`) {
		t.Errorf("an item the reader lost access to says so and can only be removed: %s", markup)
	}
}

func TestTodo_CHATSAVE_002_Panel(t *testing.T) {
	copy := SavedMessagesCopy("en-US")
	view := chatsave002View("todo", chatsave002Rows()[:2])
	markup := renderNode(t, RenderSavedMessages(view))
	// The heading is the thread and details panels' own, with the count still to do.
	for _, want := range []string{`class="side-heading chatsave-header"`, `<h2>Saved</h2>`, "2 to do", `data-saved-action="close"`, "icon-close"} {
		if !strings.Contains(markup, want) {
			t.Errorf("the heading lacks %s", want)
		}
	}
	// One segmented control, three segments, each with its count, one chosen.
	tabs := chatsave002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "role") == "tab" })
	if len(tabs) != 3 || strings.Count(markup, `aria-selected="true"`) != 1 {
		t.Fatalf("want a three-segment control with one chosen: %d", len(tabs))
	}
	for i, want := range []string{copy.Todo + " 2", copy.Done + " 1", copy.All + " 3"} {
		if got := chatsave002NodeText(tabs[i]); got != want {
			t.Errorf("segment %d reads %q, want %q", i, got, want)
		}
	}
	if !strings.Contains(markup, `class="chatsave-seg"`) || strings.Contains(markup, "chatsave-tabs") {
		t.Errorf("the segmented control is not one track")
	}
	// The search field appears only when the list has more than eight items.
	for count, want := range map[int]bool{0: false, 8: false, 9: true} {
		v := view
		v.AllCount = count
		if got := strings.Contains(renderNode(t, RenderSavedMessages(v)), `id="chatsave-search"`); got != want {
			t.Errorf("%d items: search shown %v, want %v", count, got, want)
		}
	}
	v := view
	v.Query = "guide"
	if !strings.Contains(renderNode(t, RenderSavedMessages(v)), `id="chatsave-search"`) {
		t.Errorf("a search in use must stay on screen")
	}
	// Empty, loading, failed.
	for tab, want := range map[string]string{"todo": copy.EmptyTodo, "done": copy.EmptyDone} {
		empty := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: tab}))
		if !strings.Contains(empty, want) || !strings.Contains(empty, `class="chatsave-empty"`) || !strings.Contains(empty, "icon-") {
			t.Errorf("%s empty: %s", tab, empty)
		}
	}
	if copy.EmptyTodo != "Hover a message and press the bookmark to keep it here." || copy.EmptyDone != "Things you tick off appear here." {
		t.Errorf("the empty sentences are not the designed ones")
	}
	loading := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Loading: true}))
	if strings.Count(loading, `chatsave-item chatsave-skeleton`) != 3 || !strings.Contains(loading, copy.Loading) {
		t.Errorf("loading is three skeleton rows: %s", loading)
	}
	failed := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Error: "unavailable"}))
	if !strings.Contains(failed, copy.Failed) || strings.Count(failed, `data-saved-action="retry"`) != 1 || strings.Contains(failed, "<li") {
		t.Errorf("a failed load is one line and Retry: %s", failed)
	}
	// The Undo line.
	for kind, want := range map[string]string{"done": "Marked done", "remove": "Removed from Saved"} {
		v := view
		v.Undo = kind
		undo := renderNode(t, RenderSavedMessages(v))
		if !strings.Contains(undo, want) || !strings.Contains(undo, `data-saved-action="undo"`) || !strings.Contains(undo, ">Undo<") {
			t.Errorf("%s: the Undo line is missing: %s", kind, undo)
		}
	}
}

func TestTodo_CHATSAVE_002_Reminder(t *testing.T) {
	view := chatsave002View("todo", chatsave002Rows()[:1])
	key := SavedItemKey("tenant", "general", "p1")
	view.ReminderMenu = key
	menu := renderNode(t, RenderSavedMessages(view))
	for _, want := range []string{`role="menu"`, "In 1 hour", "This afternoon", "Tomorrow at 9:00 AM", "Next Monday at 9:00 AM", "Pick a date and time", `data-saved-preset="hour"`, `data-saved-preset="afternoon"`, `data-saved-preset="tomorrow"`, `data-saved-preset="monday"`, `data-saved-action="remind-pick"`, `aria-expanded="true"`} {
		if !strings.Contains(menu, want) {
			t.Errorf("the reminder menu lacks %q", want)
		}
	}
	if strings.Contains(menu, `type="datetime-local"`) {
		t.Errorf("the date field opens only when a date is asked for")
	}
	// The menu is the bell's own child, inside the item's bar, and is not the
	// conversation's message menu (which the page positions from the top).
	menus := chatsave002Find(t, menu, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "menu" })
	if len(menus) != 1 || !chatPolishAncestor(menus[0], "chatsave-anchor") || !chatPolishAncestor(menus[0], "chatsave-actions") || chatsave002Class(menus[0], "message-menu") || strings.Contains(menu, "message-menu") {
		t.Errorf("the reminder menu is not the bell's own child: %s", menu)
	}
	if bells := chatsave002Find(t, menu, func(n *xhtml.Node) bool {
		return n.Data == "button" && chatPolishAttr(n, "data-saved-action") == "remind" && chatPolishAncestor(n, "chatsave-anchor")
	}); len(bells) != 1 {
		t.Errorf("the bell is not in the menu's anchor")
	}
	view.PickingDate = true
	picking := renderNode(t, RenderSavedMessages(view))
	for _, want := range []string{`type="datetime-local"`, `for="chatsave-due-p1"`, `id="chatsave-due-p1"`, `data-saved-form="due"`, "Set reminder", `min="2026-10-01T08:30"`} {
		if !strings.Contains(picking, want) {
			t.Errorf("the date field lacks %q", want)
		}
	}
	// This afternoon is offered only in the morning.
	view.Now = time.Date(2026, time.October, 1, 13, 0, 0, 0, time.UTC)
	view.PickingDate = false
	if strings.Contains(renderNode(t, RenderSavedMessages(view)), "This afternoon") {
		t.Errorf("This afternoon is offered after noon")
	}
	// What each choice means, from Thursday 08:30.
	for key, want := range map[string]time.Time{
		"hour":      chatsave002Now.Add(time.Hour),
		"afternoon": time.Date(2026, time.October, 1, 15, 0, 0, 0, time.UTC),
		"tomorrow":  time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC),
		"monday":    time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC),
	} {
		if got, ok := SavedReminderAt(key, chatsave002Now); !ok || !got.Equal(want) {
			t.Errorf("%s: got %v %v, want %v", key, got, ok, want)
		}
	}
	// On a Monday, "next Monday" is a week away; on a Sunday, tomorrow.
	if got, _ := SavedReminderAt("monday", time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)); got.Day() != 12 {
		t.Errorf("next Monday from a Monday is %v", got)
	}
	if got, _ := SavedReminderAt("monday", time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)); got.Day() != 5 {
		t.Errorf("next Monday from a Sunday is %v", got)
	}
	if _, ok := SavedReminderAt("pick", chatsave002Now); ok {
		t.Errorf("the date picker has no time of its own")
	}
	// Labels in the three languages.
	due := time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC)
	for locale, want := range map[string]string{"en-US": "Tomorrow 9:00 AM", "de-DE": "Morgen 09:00", "ar": "غدًا ٠٩:٠٠"} {
		if got := SavedDueLabel(locale, due, chatsave002Now); got != want {
			t.Errorf("%s: %q, want %q", locale, got, want)
		}
	}
	if got := SavedDueLabel("en-US", time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC), chatsave002Now); got != "Monday 9:00 AM" {
		t.Errorf("within the week a reminder names its weekday: %q", got)
	}
	if got := SavedDueLabel("en-US", time.Date(2026, time.October, 20, 9, 0, 0, 0, time.UTC), chatsave002Now); got != "Oct 20 9:00 AM" {
		t.Errorf("later it names its date: %q", got)
	}
	if got := SavedTimeLabel("en-US", chatsave002Now.Add(-time.Hour), chatsave002Now); got != "7:30 AM" {
		t.Errorf("today's message shows its clock: %q", got)
	}
	if got := SavedTimeLabel("en-US", chatsave002Now.Add(-30*time.Hour), chatsave002Now); got != "Yesterday, 2:30 AM" {
		t.Errorf("yesterday's message shows its day: %q", got)
	}
}

func TestTodo_CHATSAVE_002_Note(t *testing.T) {
	view := chatsave002View("todo", chatsave002Rows()[:1])
	view.EditingNote = SavedItemKey("tenant", "general", "p1")
	markup := renderNode(t, RenderSavedMessages(view))
	for _, want := range []string{`<form class="chatsave-note-form"`, `data-saved-form="note"`, `type="text"`, `id="chatsave-note-field-p1"`, `for="chatsave-note-field-p1"`, `data-chat-value="ask Ben on Friday"`, "Enter to save, Esc to cancel"} {
		if !strings.Contains(markup, want) {
			t.Errorf("the note field lacks %q", want)
		}
	}
	if strings.Contains(markup, "<textarea") || strings.Contains(markup, `class="chatsave-note"`) {
		t.Errorf("a note being written is a single line field, not the note line")
	}
}

func TestTodo_CHATSAVE_002_Clamp(t *testing.T) {
	rows := chatsave002Rows()[:2]
	rows[0].Body = strings.Repeat("a long line of words ", 20)
	view := chatsave002View("todo", rows)
	markup := renderNode(t, RenderSavedMessages(view))
	more := chatsave002Find(t, markup, func(n *xhtml.Node) bool { return chatsave002Class(n, "chatsave-more") })
	if len(more) != 2 {
		t.Fatalf("every item has a Show more control")
	}
	shown := 0
	for _, m := range more {
		hidden := false
		for _, a := range m.Attr {
			if a.Key == "hidden" {
				hidden = true
			}
		}
		if !hidden {
			shown++
		}
	}
	if shown != 1 {
		t.Errorf("only the long text offers Show more, %d do", shown)
	}
	view.Expanded = map[string]bool{"p1": true}
	expanded := renderNode(t, RenderSavedMessages(view))
	if !strings.Contains(expanded, `chatsave-text is-expanded`) || !strings.Contains(expanded, "Show less") {
		t.Errorf("an expanded text reads Show less")
	}
}

func TestTodo_CHATSAVE_002_Accessibility(t *testing.T) {
	view := chatsave002View("todo", chatsave002Rows()[:2])
	view.ReminderMenu = SavedItemKey("tenant", "general", "p1")
	markup := chatPolishMarkup(t, RenderSavedMessages(view), 390, "dark")
	// Every control has a name; the bars are toolbars; the segments are tabs that
	// name the panel they show; the status is live.
	for _, b := range chatsave002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" }) {
		if chatsave002NodeText(b) == "" && chatPolishAttr(b, "aria-label") == "" {
			t.Errorf("a button without a name: %s", chatPolishAttr(b, "id"))
		}
	}
	for _, want := range []string{`role="toolbar"`, `role="tablist"`, `role="tabpanel"`, `aria-controls="chatsave-items"`, `aria-labelledby="chatsave-tab-todo"`, `role="status"`, `aria-live="polite"`, `aria-haspopup="menu"`, `role="menuitem"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessibility: missing %s", want)
		}
	}
	// The segment that is chosen is in the tab order and the others are reached by
	// arrow keys; every item is in the tab order.
	inOrder := 0
	for _, tab := range chatsave002Find(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "tab" }) {
		if chatPolishAttr(tab, "tabindex") == "0" {
			inOrder++
		}
	}
	if inOrder != 1 {
		t.Errorf("%d segments are in the tab order, want 1", inOrder)
	}
	for _, item := range chatsave002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "li" && chatsave002Class(n, "chatsave-item") }) {
		if chatPolishAttr(item, "tabindex") != "0" {
			t.Errorf("an item is not in the tab order")
		}
	}
	// Style contract: touch targets, focus, reduced motion, right-to-left, tokens only.
	// The menu opens under its bell and flips above it; the bar is shown only for the
	// item the pointer is over; the header line never wraps; the bar's lower half
	// lies in the item's top padding, so it covers no text.
	for _, want := range []string{".chatsave-anchor{position:relative", ".chatsave-menu{position:absolute;top:calc(100% + 6px)", ".chatsave-menu.is-above{top:auto;bottom:calc(100% + 6px)}", "var(--chatsave-menu-shift,0px)", ".chatsave-item:not(:hover):not(.has-menu)>.chatsave-actions{opacity:0", ".chatsave-meta{display:flex;align-items:baseline;flex-wrap:nowrap", ".chatsave-author{flex:0 0 auto", ".chatsave-where{flex:0 1 auto", ".chatsave-time{flex:none", "padding:14px 16px", "top:-12px", "z-index:4"} {
		if !strings.Contains(Chatsave002Styles, want) {
			t.Errorf("style contract: %s", want)
		}
	}
	if strings.Contains(Chatsave002Styles, ":focus-within>.chatsave-actions") {
		t.Errorf("a click must not leave a bar on the item that took focus")
	}
	for _, want := range []string{"@media(max-width:760px),(pointer:coarse)", "width:44px", "height:44px", "min-height:44px", ".chatsave-item:focus-visible", "inset-inline-end:12px", "margin-inline:-16px", "var(--hcm-radius-control)", "var(--hcm-shadow-raised)", "var(--line)", "height:28px"} {
		if !strings.Contains(Chatsave002Styles, want) {
			t.Errorf("style contract: %s", want)
		}
	}
	if regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgb\(|hsl\(|font-family|\b\d*\.?\d+m?s\b`).MatchString(Chatsave002Styles) {
		t.Errorf("the panel's styles use a literal colour, font or duration")
	}
	// The panel keeps its dock under the page header at the inline end.
	if !strings.Contains(ChatPolishStyles, ".chatsave-panel{inset-block:var(--chatsave-top,82px) 0;inset-inline:auto 0;") {
		t.Errorf("the panel lost its dock")
	}
	if !strings.Contains(ScopedStylesheet(), ".chatsave-seg-button") {
		t.Errorf("the panel's styles are not in the workspace stylesheet")
	}
}
