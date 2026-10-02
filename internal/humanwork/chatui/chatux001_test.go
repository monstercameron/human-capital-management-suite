package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// chatux001Fixture is a public channel of 18 members with one agent. open is the
// number of open tasks, done the number of finished ones, pins the number of
// pinned posts.
func chatux001Fixture(locale string, open, done, pins int) Model {
	m := chat4Fixture(locale, "sent", false)
	m.ShowDetails = false
	m.Callbacks.Search = func(string) {}
	m.Callbacks.OpenChannelTodo = func() {}
	m.Callbacks.OpenChannelPoll = func(string) {}
	for i := 0; i < open; i++ {
		m.ChannelTodo.Items = append(m.ChannelTodo.Items, ChannelTodoItem{ID: "open" + strconv.Itoa(i), Text: "Open task"})
	}
	for i := 0; i < done; i++ {
		m.ChannelTodo.Items = append(m.ChannelTodo.Items, ChannelTodoItem{ID: "done" + strconv.Itoa(i), Text: "Done task", Completed: true})
	}
	for i := 0; i < pins; i++ {
		m.ChannelPins = append(m.ChannelPins, ChannelPin{PostID: "pin" + strconv.Itoa(i), Author: "Alice", Body: "Pinned", Sequence: uint64(i + 1)})
	}
	return m
}

func chatux001Header(t *testing.T, m Model, h handlers, width int, theme string) (header *xhtml.Node, markup string) {
	t.Helper()
	markup = chatPolishMarkup(t, timeline(m, h), width, theme)
	found := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "header" && chatPolishHasClass(n, "conversation-header") })
	if len(found) != 1 {
		t.Fatalf("%d conversation headers", len(found))
	}
	return found[0], markup
}

// chatux001Actions lists the data-action of each button in the header's action
// group, in reading order.
func chatux001Actions(t *testing.T, header *xhtml.Node) []string {
	t.Helper()
	groups := chatPolishNodesIn(header, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "conversation-actions") })
	if len(groups) != 1 {
		t.Fatalf("%d action groups", len(groups))
	}
	var out []string
	for _, b := range chatPolishNodesIn(groups[0], func(n *xhtml.Node) bool { return n.Data == "button" }) {
		out = append(out, chatPolishAttr(b, "data-action"))
	}
	return out
}

func chatux001Button(t *testing.T, header *xhtml.Node, action string) *xhtml.Node {
	t.Helper()
	found := chatPolishNodesIn(header, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "data-action") == action })
	if len(found) != 1 {
		t.Fatalf("%d buttons for %q", len(found), action)
	}
	return found[0]
}

func chatux001Join(parts []string) string { return strings.Join(parts, ",") }

func TestTodo_CHATUX_001(t *testing.T) {
	// Nothing open, nothing pinned: Search, Members, Details. No poll, no to-do list.
	header, markup := chatux001Header(t, chatux001Fixture("en-US", 0, 0, 0), handlers{}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); got != "chat-search-open,header-members,details" {
		t.Fatalf("quiet channel header = %s", got)
	}
	for _, gone := range []string{"channel-poll-trigger", "channel-todo-trigger", `data-action="open-poll"`, `data-action="open-todo"`} {
		if strings.Contains(markup, gone) {
			t.Fatalf("the header still carries %q", gone)
		}
	}

	// Open tasks bring the to-do list back, first, with the count of open tasks only.
	header, _ = chatux001Header(t, chatux001Fixture("en-US", 2, 3, 0), handlers{}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); got != "open-todo,chat-search-open,header-members,details" {
		t.Fatalf("header with open tasks = %s", got)
	}
	todo := chatux001Button(t, header, "open-todo")
	if count := chatPolishNodesIn(todo, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "channel-todo-count") }); len(count) != 1 || chatbug030Text(count[0]) != "2" {
		t.Fatalf("the to-do button does not state 2 open tasks: %q", chatbug030Text(todo))
	}
	// Finished tasks alone do not.
	header, _ = chatux001Header(t, chatux001Fixture("en-US", 0, 3, 0), handlers{}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); strings.Contains(got, "open-todo") {
		t.Fatalf("a channel with no open tasks shows the to-do list: %s", got)
	}
	// An open to-do tray keeps its button, so it has somewhere to be pressed.
	header, _ = chatux001Header(t, chatux001Fixture("en-US", 0, 3, 0), handlers{local: localUI{tray: "todo"}}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); !strings.Contains(got, "open-todo") {
		t.Fatalf("the open to-do tray lost its button: %s", got)
	}

	// Pins add Pinned between Search and Members and state their number.
	header, _ = chatux001Header(t, chatux001Fixture("en-US", 0, 0, 3), handlers{}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); got != "chat-search-open,header-pinned,header-members,details" {
		t.Fatalf("header with pins = %s", got)
	}
	pinned := chatux001Button(t, header, "header-pinned")
	if got := chatPolishAttr(pinned, "aria-label"); got != "Pinned messages (3)" || chatPolishAttr(pinned, "title") != got {
		t.Fatalf("pinned name %q, tooltip %q", got, chatPolishAttr(pinned, "title"))
	}
	members := chatux001Button(t, header, "header-members")
	if got := chatPolishAttr(members, "aria-label"); got != "Members (18)" {
		t.Fatalf("members name %q", got)
	}
	if got := chatPolishAttr(chatux001Button(t, header, "chat-search-open"), "aria-label"); got != "Search Chat" {
		t.Fatalf("search name %q", got)
	}

	// The name opens the details, and under it sits the purpose, or the type and
	// the member count while none is set.
	m := chatux001Fixture("en-US", 0, 0, 0)
	header, _ = chatux001Header(t, m, handlers{}, 1440, "light")
	name := chatux001Button(t, header, "header-details")
	if chatbug030Text(name) != "general" || name.Parent.Data != "h1" {
		t.Fatalf("the name is %q inside %s", chatbug030Text(name), name.Parent.Data)
	}
	topic := chatPolishNodesIn(header, func(n *xhtml.Node) bool { return n.Data == "p" && chatPolishHasClass(n, "conversation-topic") })
	if len(topic) != 1 || !strings.Contains(chatbug030Text(topic[0]), "Public") || !strings.Contains(chatbug030Text(topic[0]), "18 members") {
		t.Fatalf("no purpose set, the line is %q", chatbug030Text(topic[0]))
	}
	m.ChannelTeam.Purpose = "Hiring plans and interview scheduling"
	header, _ = chatux001Header(t, m, handlers{}, 1440, "light")
	topic = chatPolishNodesIn(header, func(n *xhtml.Node) bool { return n.Data == "p" && chatPolishHasClass(n, "conversation-topic") })
	if len(topic) != 1 || chatbug030Text(topic[0]) != "Hiring plans and interview scheduling" {
		t.Fatalf("with a purpose, the line is %q", chatbug030Text(topic[0]))
	}
	m.ChannelTeam.Purpose = ""
	m.Conversations[0].Topic = "Company news"
	header, _ = chatux001Header(t, m, handlers{}, 1440, "light")
	if topic = chatPolishNodesIn(header, func(n *xhtml.Node) bool { return n.Data == "p" && chatPolishHasClass(n, "conversation-topic") }); chatbug030Text(topic[0]) != "Company news" {
		t.Fatalf("the conversation's own topic is not the fallback purpose: %q", chatbug030Text(topic[0]))
	}

	// A one-to-one message has no member list to open.
	dm := chat4Fixture("en-US", "sent", true)
	header, _ = chatux001Header(t, dm, handlers{}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); strings.Contains(got, "header-members") || strings.Contains(got, "open-todo") {
		t.Fatalf("a direct message header offers %s", got)
	}
}

func TestTodo_CHATUX_001_Actions(t *testing.T) {
	var calls []string
	m := chatux001Fixture("en-US", 1, 0, 1)
	m.Callbacks.ToggleDetails = func(open bool) { calls = append(calls, "details:"+strconv.FormatBool(open)) }
	m.Callbacks.CloseThread = func() { calls = append(calls, "close-thread") }
	m.Callbacks.ClosePerson = func() { calls = append(calls, "close-person") }
	m.Callbacks.OpenChannelTodo = func() { calls = append(calls, "todo") }
	m.Callbacks.OpenChannelPoll = func(id string) { calls = append(calls, "poll:"+id) }

	for _, action := range []string{"header-details", "header-pinned", "header-members"} {
		calls = nil
		if !chatux001Click(m, action) {
			t.Fatalf("%s was not handled", action)
		}
		if chatux001Join(calls) != "details:true" {
			t.Fatalf("%s calls %v", action, calls)
		}
	}
	// A thread or a person open over the details is closed first.
	calls = nil
	over := m
	over.ShowThread, over.ShowPerson = true, true
	chatux001Click(over, "header-pinned")
	if chatux001Join(calls) != "close-thread,close-person,details:true" {
		t.Fatalf("opening over a thread: %v", calls)
	}
	// Other actions are not this file's.
	if chatux001Click(m, "details") || chatux001Click(m, "open-poll") || chatux001Click(m, "") {
		t.Fatal("an unrelated action was taken")
	}
	// Without the callback the click is swallowed, not an error.
	bare := m
	bare.Callbacks.ToggleDetails = nil
	if !chatux001Click(bare, "header-members") {
		t.Fatal("a click with no details callback fell through")
	}

	// The two actions the composer's add menu calls are as they were.
	calls = nil
	m.actWith("open-poll", "general", "")
	m.actWith("open-todo", "", "")
	if chatux001Join(calls) != "poll:general,todo" {
		t.Fatalf("poll and to-do actions: %v", calls)
	}
}

func TestTodo_CHATUX_001_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatux001Fixture(locale, 2, 0, 2)
		header, _ := chatux001Header(t, m, handlers{}, 390, "light")
		names := map[string]string{}
		for _, b := range chatPolishNodesIn(header, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAncestor(n, "conversation-actions") }) {
			action := chatPolishAttr(b, "data-action")
			label := chatPolishAttr(b, "aria-label")
			if label == "" || strings.HasPrefix(label, "chat.") {
				t.Fatalf("%s: %s has no accessible name (%q)", locale, action, label)
			}
			if chatPolishAttr(b, "title") == "" {
				t.Fatalf("%s: %s has no tooltip", locale, action)
			}
			if chatPolishAttr(b, "type") != "button" {
				t.Fatalf("%s: %s is not type=button", locale, action)
			}
			names[action] = label
			// The number is spoken once, in the name; the visible digits are hidden from readers.
			for _, count := range chatPolishNodesIn(b, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatux001-count") }) {
				if chatPolishAttr(count, "aria-hidden") != "true" {
					t.Fatalf("%s: the count in %s is read twice", locale, action)
				}
			}
		}
		for _, action := range []string{"chat-search-open", "header-pinned", "header-members", "details", "open-todo"} {
			if names[action] == "" {
				t.Fatalf("%s: no %s button (%v)", locale, action, names)
			}
		}
		seen := map[string]string{}
		for action, label := range names {
			if other, dup := seen[label]; dup {
				t.Fatalf("%s: %s and %s are both named %q", locale, action, other, label)
			}
			seen[label] = action
		}
		if got := chatPolishAttr(chatux001Button(t, header, "chat-search-open"), "aria-keyshortcuts"); got != "Control+K" {
			t.Fatalf("%s: the search button does not announce its key: %q", locale, got)
		}
		if locale != "en-US" && names["header-members"] == chatux001Textf(chatux001Fixture("en-US", 0, 0, 0), "chat.ux001.members_count", map[string]string{"n": "18"}) {
			t.Fatalf("%s: members button is not translated", locale)
		}
		// The name is a real button: it can be reached and used from the keyboard.
		name := chatux001Button(t, header, "header-details")
		if name.Parent.Data != "h1" || chatPolishAttr(name, "type") != "button" {
			t.Fatalf("%s: the name is not a button inside the heading", locale)
		}
	}
}

// chatux001HiddenBelow reads the container width under which the stylesheet hides
// a header button class, or 0 when it never does.
func chatux001HiddenBelow(t *testing.T, class string) int {
	t.Helper()
	m := regexp.MustCompile(`@container chatmain \(max-width:(\d+)px\)\{[^@]*?` + regexp.QuoteMeta(class) + `[^{}]*\{display:none\}`).FindStringSubmatch(ChatUX001Styles)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func TestTodo_CHATUX_001_Browser(t *testing.T) {
	// The width the conversation column gets: the viewport less the 288 px
	// conversation list at 1440 and 800, the whole viewport at 390, where the list
	// is a drawer.
	columns := map[int]int{1440: 1440 - 288, 800: 800 - 288, 390: 390}
	pinnedBelow := chatux001HiddenBelow(t, ".chatux001-pinned")
	membersBelow := chatux001HiddenBelow(t, ".chatux001-members")
	if pinnedBelow == 0 || pinnedBelow != membersBelow {
		t.Fatalf("pinned and members must leave the header together at one width: %d, %d", pinnedBelow, membersBelow)
	}
	if strings.Contains(ChatUX001Styles, ".chatux001-search{display:none") || strings.Contains(ChatUX001Styles, "[data-action=\"details\"]{display:none") {
		t.Fatal("search or details can be hidden")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, width := range []int{1440, 800, 390} {
			for _, tasks := range []int{0, 2} {
				for _, pins := range []int{0, 2} {
					for _, theme := range []string{"light", "dark"} {
						m := chatux001Fixture(locale, tasks, 1, pins)
						header, markup := chatux001Header(t, m, handlers{}, width, theme)
						actions := chatux001Actions(t, header)
						has := func(action string) bool { return strings.Contains(","+chatux001Join(actions)+",", ","+action+",") }
						// Present in the tree at every width: Search, Details, the name, the way back.
						for _, action := range []string{"chat-search-open", "details", "header-details", "open-rail"} {
							if len(chatPolishNodesIn(header, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "data-action") == action })) != 1 {
								t.Fatalf("%s/%d: %s missing (%v)", locale, width, action, actions)
							}
						}
						if has("open-todo") != (tasks > 0) || has("header-pinned") != (pins > 0) {
							t.Fatalf("%s/%d tasks=%d pins=%d: header is %v", locale, width, tasks, pins, actions)
						}
						if strings.Contains(markup, "channel-poll-trigger") {
							t.Fatalf("%s/%d: the poll button is in the header", locale, width)
						}
						// What the stylesheet leaves on screen at this width.
						pinnedShown := columns[width] > pinnedBelow
						if width == 390 && pinnedShown {
							t.Fatalf("pinned and members still drawn at 390 px")
						}
						if width != 390 && !pinnedShown {
							t.Fatalf("pinned and members hidden at %d px", width)
						}
					}
				}
			}
		}
	}
	// The header's own rules are in the served stylesheet and sit after the older header rules.
	if !strings.Contains(Stylesheet, ChatUX001Styles) || strings.LastIndex(Stylesheet, ".chatux001-pinned") < strings.LastIndex(Stylesheet, ".channel-todo-trigger-label,.channel-poll-trigger-label{display:none}") {
		t.Fatal("the header rules are not served after the older rules")
	}
	if strings.Contains(ChatUX001Styles, "style=") || strings.Contains(ChatUX001Styles, "#") && regexp.MustCompile(`#[0-9a-fA-F]{3,6}\b`).MatchString(ChatUX001Styles) {
		t.Fatal("the header rules use a literal colour or an inline style")
	}
}
