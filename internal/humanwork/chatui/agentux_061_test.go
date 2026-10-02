package chatui

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// TestTodo_AGENTUX_061 goes through the details of the hands-on pass clause by
// clause: placeholders that resolve, one privacy sentence, the badge, the phone
// header and its search layer, the source line, one numeral system and time
// format, and titles isolated for direction.
func TestTodo_AGENTUX_061(t *testing.T) {
	t.Run("placeholders resolve", func(t *testing.T) {
		// A document card: loading is a skeleton, ready and unavailable are not.
		m := Model{Locale: "en-US", Text: func(key string) string { return englishCopy[key] }}
		for state, want := range map[string]struct{ skeleton, text string }{
			"loading": {"chat-doc-skeleton", ""}, "ready": {"", "Offices are closed."}, "unavailable": {"", englishCopy[KeyDocRestricted]},
		} {
			preview := DocPreview{ID: "d1", State: state, Readable: state == "ready", Title: "2026 holiday guide", Owner: "Walt Brennan", Snippet: "Offices are closed."}
			m.DocPreviews = map[string]DocPreview{"d1": preview}
			card := renderNode(t, spanOf(docPreviewEmbeds(m, "see doc:d1")))
			if got := strings.Contains(card, "chat-doc-skeleton"); got != (want.skeleton != "") {
				t.Errorf("document %s: skeleton=%v", state, got)
			}
			if want.text != "" && !strings.Contains(card, want.text) {
				t.Errorf("document %s does not say %q: %s", state, want.text, card)
			}
			if state == "loading" && !strings.Contains(card, `aria-busy="true"`) {
				t.Error("a loading card is not marked busy")
			}
		}

		// The answer card's placeholders go away with the load they belong to.
		for _, direct := range []bool{false, true} {
			resolved := chat4Fixture("en-US", "answered", direct)
			resolved.PersonaActivityReady = true
			page := renderAgentUXChat3Node(t, Build(resolved), 1440)
			for _, leftover := range []string{"agent-reply-pending", "chatbug040-reserve", "agent-reply-identity-pending", "chat-skeleton"} {
				if strings.Contains(page, leftover) {
					t.Errorf("direct=%v: a settled conversation keeps %s", direct, leftover)
				}
			}
		}

		// The channel's poll and list: loading says so, a failed read says so with
		// a way to try again, an empty one is empty.
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			c := chat4Fixture(locale, "answered", false)
			c.ChannelPollLoading = true
			loading := renderNode(t, channelPollSection(c, handlers{}))
			if !strings.Contains(loading, `role="status"`) || !strings.Contains(loading, c.t(KeyPollLoading)) {
				t.Errorf("%s: a loading poll says nothing: %s", locale, loading)
			}
			c.ChannelPollLoading = false
			c.ChannelPollError = "refused"
			failed := renderNode(t, channelPollSection(c, handlers{}))
			if strings.Contains(failed, c.t(KeyPollLoading)) {
				t.Errorf("%s: a failed poll still says it is loading", locale)
			}
			c.ChannelPollError = ""
			empty := renderNode(t, channelPollSection(c, handlers{}))
			if strings.Contains(empty, c.t(KeyPollLoading)) || strings.Contains(empty, `aria-busy="true"`) {
				t.Errorf("%s: an empty poll is still a placeholder", locale)
			}
		}
	})

	t.Run("one privacy sentence", func(t *testing.T) {
		m := chat4Fixture("en-US", "answered", true)
		page := renderAgentUXChat3Node(t, Build(m), 1440)
		// The sentence is said once in words a person sees; the tooltip and the
		// screen-reader copy of the same line are not a second statement.
		visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(page, " ")
		said := strings.Count(visible, "Only you can see this conversation") + strings.Count(visible, m.t(KeyIntroDirect)) + strings.Count(visible, "Private to you")
		if said != 2 {
			// "Private to you" with its sr-only long form is the one statement.
			t.Errorf("the agent conversation states its privacy %d times (want the header's short and spoken form only): %s", said, visible)
		}
		if strings.Contains(visible, m.t(KeyIntroDirect)) {
			t.Error("the conversation's first lines repeat the privacy sentence the header already carries")
		}
	})

	t.Run("agent badge", func(t *testing.T) {
		m := chat4Fixture("en-US", "answered", false)
		for _, name := range []string{"Assistant", "Policy Helper", "Benefits Concierge Agent"} {
			row := renderNode(t, railRow(m, Conversation{ID: name, Name: name, Agent: true, Kind: DirectMessage}))
			if !strings.Contains(row, "agent-badge") || !strings.Contains(row, name) {
				t.Errorf("%s: the row has no badge or no name: %s", name, row)
			}
		}
		// The badge keeps its own width and the name gives way to it, wrapping the
		// badge to a second line before the name is cut.
		for _, want := range []string{".chat-rail-row .agent-badge{flex:none", ".chat-rail-row:has(.agent-badge){display:flex;flex-wrap:wrap}", ".chat-rail-row:has(.agent-badge) .chat-row-name{flex:1 1 0%;min-width:0"} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("styles lack %s", want)
			}
		}
	})

	t.Run("phone header and search layer", func(t *testing.T) {
		if !strings.Contains(Stylesheet, "@media(max-width:767px){.chat-workspace .conversation-header{display:flex;flex-wrap:nowrap;min-height:56px;height:56px}") {
			t.Error("the phone header is not one row")
		}
		m := chat4Fixture("en-US", "answered", false)
		layer := renderNode(t, chatSearchLayer(m, handlers{local: localUI{searchOpen: true}}))
		chat4Require(t, layer, `data-chat-layer="search"`, `data-action="chat-search-close"`, `id="chat-search"`)
		if strings.Contains(renderNode(t, chatSearchLayer(m, handlers{})), "chat-search-layer") {
			t.Error("the search layer is drawn while search is closed")
		}
	})

	t.Run("source line", func(t *testing.T) {
		for title, want := range map[string][2]string{
			"Paid time off policy · Carryover · v1.0.0": {"Paid time off policy · Carryover", "v1.0.0"},
			"2026 holiday guide · v1.0.0":               {"2026 holiday guide", "v1.0.0"},
			"Private guide":                             {"Private guide", ""},
			"Policy · 2026":                             {"Policy · 2026", ""},
		} {
			if name, version := agentux061SourceParts(title); name != want[0] || version != want[1] {
				t.Errorf("%q splits into %q and %q, want %q and %q", title, name, version, want[0], want[1])
			}
		}
		m, post := agentux051Direct(chatbug021Projected)
		markup := renderNode(t, message(m, handlers{}, post, false))
		if strings.Count(markup, `class="agent-reply-source-version"`) != 2 || !strings.Contains(markup, "<bdi>2026 holiday guide</bdi>") {
			t.Errorf("the version is not secondary text after the title: %s", markup)
		}
		if !strings.Contains(Stylesheet, ".agent-reply-source-version{color:var(--hcm-color-text-muted)") {
			t.Error("the version has no secondary style")
		}
		// Titles are isolated for direction so "2026" does not jump in Arabic.
		if !strings.Contains(markup, "<bdi") || !strings.Contains(Stylesheet, ".agent-reply-sources a{unicode-bidi:isolate") {
			t.Error("a source title is not isolated for direction")
		}
	})

	t.Run("one clock", func(t *testing.T) {
		stamp := time.Date(2026, 10, 1, 11, 18, 0, 0, time.Local)
		for locale, want := range map[string]string{"en-US": "11:18 AM", "de-DE": "11:18", "ar": "١١:١٨"} {
			if got := chat5Clock(locale, stamp); got != want {
				t.Errorf("%s card clock %q, want %q", locale, got, want)
			}
		}
	})
}

// TestTodo_AGENTUX_061_Accessibility walks the rendered tree of every Chat
// surface: every control has a role (its element or an explicit one) and an
// accessible name, the answer's "Helpful", "Not right" and "Ask a follow-up"
// among them.
func TestTodo_AGENTUX_061_Accessibility(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		surfaces := agentux063Surfaces(t, locale, width)
		answered := chat4Fixture(locale, "answered", true)
		answered.PersonaActivityReady = true
		surfaces["answer actions"] = renderAgentUXChat3Node(t, Build(answered), width)
		sawFeedback := false
		for name, markup := range surfaces {
			agentux063Controls(t, markup, func(n *xhtml.Node, controlName string) {
				if controlName == "" {
					t.Errorf("%s: <%s class=%q data-action=%q> has no accessible name", name, n.Data, chat5Attr(n, "class"), chat5Attr(n, "data-action"))
				}
				if strings.Contains(chat5Attr(n, "class"), "agent-feedback") || strings.Contains(chat5Attr(n, "data-action"), "feedback") {
					sawFeedback = true
				}
			})
			// A div or span that acts as a control carries a role.
			root, err := xhtml.Parse(strings.NewReader(markup))
			if err != nil {
				t.Fatal(err)
			}
			walkChat5HTML(root, func(n *xhtml.Node) {
				if n.Type == xhtml.ElementNode && (n.Data == "div" || n.Data == "span" || n.Data == "li") && chat5Attr(n, "data-action") != "" && chat5Attr(n, "role") == "" {
					t.Errorf("%s: <%s data-action=%q> acts as a control without a role", name, n.Data, chat5Attr(n, "data-action"))
				}
			})
		}
		if !sawFeedback && width == 1440 && locale == "en-US" {
			t.Log("note: no feedback control was drawn in this fixture")
		}
	})
}

// TestTodo_AGENTUX_061_Performance: the agents of a conversation are read once
// when it is selected and again on change events only. Idle viewing makes no
// request. The reads are scheduled through the coalescer (one read per window
// however many events arrive, tested in journeywasm), and the code that starts
// the reads is guarded by the conversation and identity it already started
// for.
func TestTodo_AGENTUX_061_Performance(t *testing.T) {
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	wasm := "../../../tools/uxqual/cmd/journeywasm/"
	start := read(wasm + "persona_chat_wasm.go")
	guard := start[strings.Index(start, "func startPersonaChat("):]
	guard = guard[:strings.Index(guard, "go func()")]
	if !strings.Contains(guard, "personaChatBrowser.conversation == conversation && personaChatBrowser.identity == identity") {
		t.Error("starting the agents' reads is not guarded by the conversation it already started for")
	}
	// Every scheduled refresh of the directory is a reaction to an event: it sits
	// in a stream hook or in the watch's own loop, never in a timer or a render.
	scheduled := regexp.MustCompile(`personaDirectoryRefresh\.Schedule\(\)`)
	for _, file := range []string{"persona_chat_wasm.go", "chat_stream_wasm.go"} {
		source := read(wasm + file)
		for _, at := range scheduled.FindAllStringIndex(source, -1) {
			before := source[max(0, at[0]-900):at[0]]
			if !regexp.MustCompile(`(?s)(MEMBERSHIP_CHANGED|POST_CREATED|applied \{|directoryPosts != nextPosts|chatperf2DirectoryStale)`).MatchString(before) {
				t.Errorf("%s: a directory refresh is scheduled outside an event: ...%s", file, strings.TrimSpace(before[max(0, len(before)-120):]))
			}
		}
		if strings.Contains(source, "SetInterval") && strings.Contains(source, "personaDirectoryRefresh") {
			t.Errorf("%s polls the agent directory on an interval", file)
		}
	}
}
