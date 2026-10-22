package chatui

import (
	"fmt"
	stdhtml "html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

func chatgateSegmentFixture() GateView {
	both := chatgate.Visibility{Administrators: true, Members: true, Consumers: []string{chatgate.ConsumerSegments}}
	d := chatgate.Definition{Version: chatgate.Version{Major: 1}, Mode: "automatic", Purpose: "Join the payroll discussion", Fields: []chatgate.Field{
		{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Which office?", Purpose: "Place you", DataClass: "INTERNAL", Required: true, RetentionDays: 30, Options: []string{"Denver", "Austin"}, Visibility: both},
		{ID: "note", Kind: "short_text", KindVersion: "1.0.0", Label: "A note", Purpose: "Help", DataClass: "INTERNAL", RetentionDays: 30, Visibility: chatgate.Visibility{Administrators: true}},
	}}
	return GateView{Locale: "en-US", Conversation: "room", Administrator: true, Gate: chatgate.Gate{Versions: []chatgate.Definition{d}, Current: "1.0.0", State: "active"}, ConsumerNames: map[string]string{chatgate.ConsumerSegments: "Role labels and segments"},
		Segments: &chatgate.SegmentOverview{
			Rules:      chatgate.SegmentRuleSet{Version: 2, Reason: "Added Austin", Rules: []chatgate.SegmentRule{{ID: "rule-1", Field: "office", Value: "Denver", Label: "Denver crew", Segment: "Denver office"}}},
			History:    []chatgate.SegmentRuleSet{{Version: 1, Reason: "First rules"}},
			Labels:     []chatgate.SegmentSummary{{Name: "Denver crew", People: 12}},
			Segments:   []chatgate.SegmentSummary{{Name: "Denver office", People: 12}},
			Addressing: true,
		}}
}

func gateNodes(t *testing.T, markup string, match func(*xhtml.Node) bool) []*xhtml.Node {
	t.Helper()
	return chatPolishNodes(t, stdhtml.UnescapeString(markup), match)
}

// TestTodo_CHATGATE_009_Browser: the administrator's rule editor offers only
// questions that can feed a label, shows every rule with room to add, says plainly
// that labels give no permission, counts people and never names them, keeps every
// earlier version one press away; a member sees their own labels and why.
func TestTodo_CHATGATE_009_Browser(t *testing.T) {
	for _, locale := range chatgateLocales {
		v := chatgateSegmentFixture()
		v.Locale = locale
		markup := gateMarkup(t, v)
		text := stdhtml.UnescapeString(markup)
		for _, key := range []string{"segTitle", "segIntro", "segHint", "segReason", "segSave", "segCounts", "segUndo"} {
			if want := GateText(locale, key); !strings.Contains(text, want) || want == "" {
				t.Errorf("%s: the editor misses %q (%s)", locale, key, want)
			}
		}
		if want := fmt.Sprintf(GateText(locale, "segPeople"), 12); !strings.Contains(text, "Denver office · "+want) || !strings.Contains(text, "Denver crew · "+want) {
			t.Errorf("%s: the counts are not drawn: %s", locale, want)
		}
		if want := fmt.Sprintf(GateText(locale, "segVersion"), 1, "First rules"); !strings.Contains(text, want) {
			t.Errorf("%s: the history misses %q", locale, want)
		}
		// One select for each rule and two blank rows; only the question that
		// members see and that names the consumer is offered.
		rows := gateNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "fieldset" && chatPolishAttr(n, "data-gate-segment-rule") != ""
		})
		if len(rows) != 1+GateSegmentRows {
			t.Fatalf("%s: %d rule rows", locale, len(rows))
		}
		options := gateNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "option" && n.Parent != nil && chatPolishAttr(n.Parent, "id") == "gate-seg-0-field"
		})
		if len(options) != 2 { // "Choose" and the one question that qualifies
			t.Fatalf("%s: the first rule offers %d choices, want the prompt and one question", locale, len(options))
		}
		for _, id := range []string{"gate-seg-0-field", "gate-seg-0-value", "gate-seg-0-label", "gate-seg-0-segment", "gate-seg-2-field", "gate-seg-reason"} {
			if found := gateNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == id }); len(found) != 1 {
				t.Errorf("%s: %q appears %d times", locale, id, len(found))
			}
		}
		// Every control that does something is a labelled button with its action.
		for action, id := range map[string]string{"segments-save": "", "segments-revert": "1", "levels-save": ""} {
			found := gateNodes(t, markup, func(n *xhtml.Node) bool {
				return n.Data == "button" && chatPolishAttr(n, "data-gate-action") == action && chatPolishAttr(n, "data-id") == id && chatPolishAttr(n, "type") == "button"
			})
			if len(found) != 1 {
				t.Errorf("%s: %d buttons for %s", locale, len(found), action)
			}
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Error("ar: not right to left")
		}
	}
	// No question can be used: the editor says what to change and offers nothing.
	v := chatgateSegmentFixture()
	v.Gate.Versions[0].Fields[0].Visibility.Consumers = nil
	if text := stdhtml.UnescapeString(gateMarkup(t, v)); !strings.Contains(text, GateText("en-US", "segNoField")) || strings.Contains(text, "gate-seg-0-field") {
		t.Fatalf("a gate with no usable question: %s", text)
	}
	// The consumer is listed by its name beside a question.
	if text := stdhtml.UnescapeString(gateMarkup(t, chatgateSegmentFixture())); !strings.Contains(text, "Role labels and segments") {
		t.Fatal("the consumer's name is not drawn")
	}
	// A member: their own labels and the reason, and what a label is.
	for _, locale := range chatgateLocales {
		m := chatgateSegmentFixture()
		m.Locale, m.Administrator, m.Segments = locale, false, nil
		m.Submission = &chatgate.Submission{ID: "s", Status: "admitted", Version: "1.0.0"}
		m.MySegments = []chatgate.Placement{
			{Kind: chatgate.PlacementLabel, Name: "Denver crew", RuleID: "rule-1", Field: "office", FieldLabel: "Which office?", Value: "Denver"},
			{Kind: chatgate.PlacementSegment, Name: "Night shift", RuleID: "agent", Source: "agent:welcomer"}}
		text := stdhtml.UnescapeString(gateMarkup(t, m))
		for _, want := range []string{GateText(locale, "mySegs"), "Denver crew · " + fmt.Sprintf(GateText(locale, "mySegsWhy"), "Denver", "Which office?"), "Night shift · " + GateText(locale, "mySegsAgent"), GateText(locale, "mySegsNote")} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: a member's page misses %q", locale, want)
			}
		}
		// Nothing of the rules or of anybody else.
		if strings.Contains(text, GateText(locale, "segCounts")) || strings.Contains(text, "gate-seg-0-field") {
			t.Errorf("%s: a member sees the editor", locale)
		}
		none := m
		none.MySegments = nil
		if !strings.Contains(stdhtml.UnescapeString(gateMarkup(t, none)), GateText(locale, "mySegsNone")) {
			t.Errorf("%s: an admitted member with no label is not told so", locale)
		}
		// A person who is not in yet sees none of it.
		out := m
		out.Submission, out.MySegments = nil, nil
		if strings.Contains(gateMarkup(t, out), `data-gate-my-segments`) {
			t.Errorf("%s: an applicant sees a labels section", locale)
		}
	}
}

// TestTodo_CHATGATE_009_Reads: a read by the segments consumer or by an agent is
// named by what it is in the person's list of who has read their answers.
func TestTodo_CHATGATE_009_Reads(t *testing.T) {
	for _, locale := range chatgateLocales {
		v := chatgateSegmentFixture()
		v.Locale, v.Administrator, v.Segments = locale, false, nil
		v.Submission = &chatgate.Submission{ID: "s", Status: "admitted", Version: "1.0.0"}
		v.Reads = []chatgate.ReadAudit{
			{Person: "me", Consumer: chatgate.ConsumerSegments, Purpose: "Role labels and segments", At: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
			{Person: "me", Reader: "welcomer", Consumer: "agent:welcomer", Purpose: "Agent welcome", At: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}}
		text := stdhtml.UnescapeString(gateMarkup(t, v))
		for _, want := range []string{GateText(locale, "readSegments") + " · Role labels and segments · 2026-10-01", GateText(locale, "readAgent") + " · Agent welcome · 2026-10-02"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the list of readers misses %q", locale, want)
			}
		}
	}
}

func chatgatePollModel(locale string) Model {
	return Model{State: StateReady, Locale: locale, Direction: direction(locale), CurrentUser: "walt", CurrentTenantID: "t", Chatcmd002: Chatcmd002Callbacks{Mutate: func(string, uint64, chat.Chatcmd002Mutation) {}}}
}

func chatgatePollNodes(m Model, view chat.Chatcmd002View) ui.Node {
	return html.Div(html.Props{}, chatcmd002Poll(m, "post1", view, false, false)...)
}

func chatgatePollView() chat.Chatcmd002View {
	return chat.Chatcmd002View{
		Card:           chat.Chatcmd002Card{Kind: "poll", Title: "Night shift swap?", Audience: []string{"Night shift"}, Poll: &chat.Chatcmd002Poll{Anonymous: true, Results: "always", AddOptions: "author", Options: []chat.ChannelPollOption{{ID: "a", Text: "Yes", Count: 9}, {ID: "b", Text: "No", Count: 3}}}},
		ResultsVisible: true,
		BySegment: &chatgate.Breakdown{K: 5, Total: 12, Withheld: 1, Segments: []chatgate.SegmentResult{
			{Segment: "Night shift", Voters: 12, Options: map[string]int{"a": 9, "b": 3}}}},
	}
}

// TestTodo_CHATGATE_010_Browser: a poll addressed to a segment says who it is
// for; a reader outside it can read it and cannot vote, and is told why; the
// breakdown names each reported group with its voters, and says how many groups
// were left out and why, never which.
func TestTodo_CHATGATE_010_Browser(t *testing.T) {
	for _, locale := range chatgateLocales {
		m := chatgatePollModel(locale)
		view := chatgatePollView()
		text := stdhtml.UnescapeString(chatremoveMarkup(t, chatgatePollNodes(m, view)))
		for _, want := range []string{fmt.Sprintf(GateText(locale, "pollFor"), "Night shift"), GateText(locale, "pollByGroup"), fmt.Sprintf(GateText(locale, "pollGroup"), "Night shift", 12), fmt.Sprintf(GateText(locale, "pollWithheld"), 1, 5)} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the poll misses %q: %s", locale, want, text)
			}
		}
		if strings.Contains(text, GateText(locale, "pollOutside")[:12]) && !view.NotInAudience {
			t.Errorf("%s: a reader in the audience is told they cannot vote", locale)
		}
		// A reader outside the segment: the explanation, and every choice is off.
		view.NotInAudience = true
		outside := stdhtml.UnescapeString(chatremoveMarkup(t, chatgatePollNodes(m, view)))
		if !strings.Contains(outside, fmt.Sprintf(GateText(locale, "pollOutside"), "Night shift")) {
			t.Errorf("%s: a reader outside the segment is not told why", locale)
		}
		for _, n := range chatPolishNodes(t, outside, func(n *xhtml.Node) bool {
			return (n.Data == "input" || n.Data == "button") && (chatPolishAttr(n, "data-action") == "chatcmd002-vote" || chatPolishAttr(n, "data-chatcmd002-choice") != "")
		}) {
			if chatPolishAttr(n, "disabled") == "" && !hasBoolAttr(n, "disabled") {
				t.Errorf("%s: a choice is enabled for a reader outside the segment", locale)
			}
		}
		// No group had enough voters: the poll says when results will appear.
		empty := chatgatePollView()
		empty.BySegment = &chatgate.Breakdown{K: 5, Total: 3}
		if !strings.Contains(stdhtml.UnescapeString(chatremoveMarkup(t, chatgatePollNodes(m, empty))), fmt.Sprintf(GateText(locale, "pollNoGroups"), 5)) {
			t.Errorf("%s: an empty breakdown is not explained", locale)
		}
		// A poll for everyone draws none of it.
		plain := chatgatePollView()
		plain.Card.Audience, plain.BySegment = nil, nil
		if got := stdhtml.UnescapeString(chatremoveMarkup(t, chatgatePollNodes(m, plain))); strings.Contains(got, GateText(locale, "pollByGroup")) || strings.Contains(got, "chatgate-poll") {
			t.Errorf("%s: a poll for everyone shows group results", locale)
		}
	}
}

func hasBoolAttr(n *xhtml.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// TestTodo_CHATGATE_011_Browser: the levels are two plain choices, off until an
// administrator turns them on, with what each does and, for restricted
// visibility, that administrators see how many messages exist and not what they
// say; the count is drawn only when there are some.
func TestTodo_CHATGATE_011_Browser(t *testing.T) {
	for _, locale := range chatgateLocales {
		v := chatgateSegmentFixture()
		v.Locale = locale
		v.Segments.Addressing, v.Segments.Restricted = false, false
		markup := gateMarkup(t, v)
		text := stdhtml.UnescapeString(markup)
		for _, key := range []string{"segLevels", "segAddress", "segRestrict", "segRestrictN", "segLevelsSave"} {
			if !strings.Contains(text, GateText(locale, key)) {
				t.Errorf("%s: the levels miss %q", locale, key)
			}
		}
		boxes := gateNodes(t, markup, func(n *xhtml.Node) bool {
			id := chatPolishAttr(n, "id")
			return n.Data == "input" && (id == "gate-levels-address" || id == "gate-levels-restrict")
		})
		if len(boxes) != 2 {
			t.Fatalf("%s: %d level checkboxes", locale, len(boxes))
		}
		for _, b := range boxes {
			if hasBoolAttr(b, "checked") {
				t.Errorf("%s: a level is on by default", locale)
			}
		}
		if strings.Contains(text, fmt.Sprintf(GateText(locale, "segRestricted"), 0)) || strings.Contains(markup, "gate-restricted-count") {
			t.Errorf("%s: a count of none is drawn", locale)
		}
		v.Segments.Addressing, v.Segments.Restricted, v.RestrictedMessages = true, true, 3
		on := gateMarkup(t, v)
		for _, b := range gateNodes(t, on, func(n *xhtml.Node) bool {
			id := chatPolishAttr(n, "id")
			return n.Data == "input" && (id == "gate-levels-address" || id == "gate-levels-restrict")
		}) {
			if !hasBoolAttr(b, "checked") {
				t.Errorf("%s: a level that is on is drawn off", locale)
			}
		}
		if !strings.Contains(stdhtml.UnescapeString(on), fmt.Sprintf(GateText(locale, "segRestricted"), 3)) {
			t.Errorf("%s: the count of restricted messages is not drawn", locale)
		}
	}
}

// TestTodo_CHATGATE_012_Browser: the member's list of who has read their answers
// names an agent that did, and an administrator reads how the gate is running.
func TestTodo_CHATGATE_012_Browser(t *testing.T) {
	for _, locale := range chatgateLocales {
		v := chatgateSegmentFixture()
		v.Locale = locale
		v.Metrics = &chatgate.GateMetrics{Started: 10, Submitted: 8, Admitted: 5, Declined: 1, Waiting: 2, MedianDecision: 90 * time.Minute, OldestWaiting: 3 * 24 * time.Hour, DropOff: []chatgate.QuestionDropOff{{Field: "office", Answered: 8, Dropped: 2}}}
		v.Ops = &chatgate.Ops{ReviewAfter: 24 * time.Hour, EscalateAfter: 72 * time.Hour, Existing: chatgate.ExistingMembers{Mode: chatgate.ExistingAsk, AnswerBy: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}}
		v.Unanswered = []string{"p1", "p2"}
		v.Names = map[string]string{"p1": "Ana Lopez", "p2": "Ben Okoye"}
		markup := gateMarkup(t, v)
		text := stdhtml.UnescapeString(markup)
		for _, want := range []string{GateText(locale, "metTitle"), fmt.Sprintf(GateText(locale, "metStarted"), 10), fmt.Sprintf(GateText(locale, "metWaiting"), 2), fmt.Sprintf(GateText(locale, "metMedian"), fmt.Sprintf(GateText(locale, "hours"), 1)), fmt.Sprintf(GateText(locale, "metOldest"), fmt.Sprintf(GateText(locale, "days"), 3)), fmt.Sprintf(GateText(locale, "metDrop"), "Which office?", 2), GateText(locale, "opsTitle"), GateText(locale, "exStays"), fmt.Sprintf(GateText(locale, "exUnanswered"), 2), "Ana Lopez", "Ben Okoye", GateText(locale, "exRemove")} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the operating block misses %q", locale, want)
			}
		}
		for _, want := range []string{GateText(locale, "ovTitle"), GateText(locale, "ovHint"), GateText(locale, "ovPerson"), GateText(locale, "ovReason")} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the override block misses %q", locale, want)
			}
		}
		if found := gateNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-gate-action") == "override" && chatPolishAttr(n, "type") == "button"
		}); len(found) != 1 {
			t.Errorf("%s: %d override buttons", locale, len(found))
		}
		for _, id := range []string{"gate-override-person", "gate-override-reason", "gate-ops-review", "gate-ops-escalate", "gate-existing-mode", "gate-existing-by"} {
			if len(gateNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == id })) != 1 {
				t.Errorf("%s: %s is not drawn once", locale, id)
			}
		}
		// Nobody is offered for removal under "only new members".
		v.Ops.Existing.Mode = chatgate.ExistingNewOnly
		if strings.Contains(gateMarkup(t, v), "gate-unanswered") {
			t.Errorf("%s: a removal is offered when current members are not asked", locale)
		}
	}
	// A name for the agent's read, in each language, beside the member's answers.
	for _, locale := range chatgateLocales {
		if GateText(locale, "readAgent") == "" || GateText(locale, "readAgent") == "readAgent" {
			t.Errorf("%s: the agent has no name in the list of readers", locale)
		}
	}
}
