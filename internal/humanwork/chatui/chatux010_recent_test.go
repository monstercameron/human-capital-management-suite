package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// TestTodo_CHATBUG_077_AgentResult: a result an agent wrote shows the agent's
// own icon, drawn by the function the conversation's messages use, not the
// initials of its name; a person's result keeps theirs. A mention in a result
// is the ordinary small chip, not a bordered control.
func TestTodo_CHATBUG_077_AgentResult(t *testing.T) {
	m := chatbug077Model()
	people := chatsearchPeopleOf(m)
	at := time.Date(2026, time.October, 1, 11, 18, 0, 0, time.UTC)
	agent := chatsearch.Row{Kind: chatsearch.Message, ID: "a", AuthorID: "policy-helper", Text: "Employees may carry over 40 hours.", At: at, Target: chatsearch.Target{ConversationID: "policy", MessageID: "a", Sequence: 4}}
	got := renderNode(t, chatsearchResult(m, people, agent, "carry over", false))
	if strings.Contains(got, ">PH<") || !strings.Contains(got, "agent-dm-avatar") || !strings.Contains(got, "<svg") {
		t.Fatalf("an agent's result wears initials: %s", got)
	}
	// An agent answer is an agent's result wherever it sits.
	answer := chatsearch.Row{Kind: chatsearch.AgentAnswer, ID: "b", AuthorID: "policy-helper", Text: "Carry over 40 hours.", At: at, Private: true, Target: chatsearch.Target{ConversationID: "general", MessageID: "b", Sequence: 5}}
	if got := renderNode(t, chatsearchResult(m, people, answer, "carry", false)); !strings.Contains(got, "agent-dm-avatar") {
		t.Fatalf("an agent answer wears initials: %s", got)
	}
	// A roster entry marked as an agent is enough, even outside its own direct message.
	m.Members = append(m.Members, Member{ID: "scribe", HomeTenantID: "t", Name: "Scribe", Agent: true})
	byRoster := chatsearch.Row{Kind: chatsearch.Message, ID: "c", AuthorID: "scribe", Text: "Summary posted.", At: at, Target: chatsearch.Target{ConversationID: "general", MessageID: "c", Sequence: 6}}
	if got := renderNode(t, chatsearchResult(m, chatsearchPeopleOf(m), byRoster, "summary", false)); !strings.Contains(got, "agent-dm-avatar") {
		t.Fatalf("a roster agent wears initials: %s", got)
	}
	person := chatsearch.Row{Kind: chatsearch.Message, ID: "d", AuthorID: "loretta", Text: "Ask HR what will carry over.", At: at, Target: chatsearch.Target{ConversationID: "loretta", MessageID: "d", Sequence: 9}}
	if got := renderNode(t, chatsearchResult(m, people, person, "carry over", false)); !strings.Contains(got, ">LH<") || strings.Contains(got, "agent-dm-avatar") {
		t.Fatalf("a person's result lost its initials: %s", got)
	}

	// The generic control rule of the results page no longer reaches a mention chip.
	if strings.Contains(ChatSearchStyles, ".chatsearch-view :is(input,select,button){") || !strings.Contains(ChatSearchStyles, ".chatsearch-view :is(input,select,button):not(.mention-chip){") {
		t.Fatal("the results page still gives a mention chip a control's border and height")
	}
}

// TestTodo_CHATBUG_077_RecentList: recent searches are a small list under the
// search box, drawn while the box is empty, shown by the style while it holds
// the cursor, and a press fills the box and searches.
func TestTodo_CHATBUG_077_RecentList(t *testing.T) {
	m := chatux002Model("en-US")
	m.SearchRecent = []string{"carry over", "payroll close", "  ", "*", "a", "b", "c", "d"}
	markup := renderNode(t, rail(m, handlers{}))
	at := strings.Index(markup, `class="rail-search-recent"`)
	if at < 0 {
		t.Fatalf("no recent searches under the box: %s", markup)
	}
	list := markup[at:]
	if end := strings.Index(list, "</div>"); end > 0 {
		list = list[:end]
	}
	if strings.Count(list, `data-action="search-recent"`) != chatsearchRecentMax || !strings.Contains(list, `data-id="carry over"`) || !strings.Contains(list, "Recent searches") {
		t.Fatalf("the list: %s", list)
	}
	if strings.Contains(list, `data-id="*"`) || strings.Contains(list, `data-id="  "`) {
		t.Fatalf("a blank or wildcard search is offered: %s", list)
	}
	// It sits inside the box's own container, so :focus-within shows it.
	if box := strings.Index(markup, `class="rail-search"`); box < 0 || box > at {
		t.Fatal("the list is not inside the search box's container")
	}
	for _, rule := range []string{".rail-search:focus-within .rail-search-recent{display:block}", ".rail-search-recent{display:none"} {
		if !strings.Contains(chatux010RecentStyles, rule) {
			t.Errorf("the style lacks %q", rule)
		}
	}
	// Not while words are in the box, and not when there is nothing to offer.
	typed := m
	typed.Search = "carry"
	if strings.Contains(renderNode(t, rail(typed, handlers{})), "rail-search-recent") {
		t.Error("the list is drawn while the box holds words")
	}
	none := m
	none.SearchRecent = nil
	if strings.Contains(renderNode(t, rail(none, handlers{})), "rail-search-recent") {
		t.Error("an empty list is drawn")
	}
	// A press searches for it.
	var searched string
	m.Callbacks.Search = func(q string) { searched = q }
	m.act("search-recent", "payroll close")
	if searched != "payroll close" {
		t.Fatalf("pressing a recent search asked for %q", searched)
	}
}

// TestTodo_CHATBUG_051_SavedMenuPlacement: the Saved panel's Remind me menu is
// placed by the same rule as every other menu: against its opener, on the side
// with room, inside its bounds.
func TestTodo_CHATBUG_051_SavedMenuPlacement(t *testing.T) {
	bounds := LayerRect{Left: 0, Top: 100, Right: 360, Bottom: 700}
	// A bell near the top: the menu opens under it, its right edge on the bell's.
	bell := LayerRect{Left: 300, Top: 150, Right: 330, Bottom: 180}
	got := PlaceChatLayer(bell, bounds, 224, 200, false, false)
	if got.Top != bell.Bottom+4 || got.Left+got.Width != bell.Right || got.Height != 200 {
		t.Fatalf("under the bell: %+v", got)
	}
	// A bell at the foot of the panel: there is no room below, so it opens above.
	low := LayerRect{Left: 300, Top: 650, Right: 330, Bottom: 680}
	got = PlaceChatLayer(low, bounds, 224, 200, false, false)
	if got.Top+got.Height > low.Top || got.Top < bounds.Top {
		t.Fatalf("above the bell: %+v", got)
	}
	// A bell at the far edge: the menu stays inside the bounds.
	edge := LayerRect{Left: 20, Top: 150, Right: 50, Bottom: 180}
	got = PlaceChatLayer(edge, bounds, 224, 200, false, false)
	if got.Left < bounds.Left || got.Left+got.Width > bounds.Right {
		t.Fatalf("outside the panel: %+v", got)
	}
	// Never taller than the room: it scrolls inside itself.
	short := LayerRect{Left: 0, Top: 0, Right: 360, Bottom: 120}
	if got = PlaceChatLayer(bell, short, 224, 400, false, false); got.Height > short.Bottom-short.Top-16 {
		t.Fatalf("taller than the bounds: %+v", got)
	}
	// Right to left opens from the bell's left edge.
	if got = PlaceChatLayer(LayerRect{Left: 100, Top: 150, Right: 130, Bottom: 180}, bounds, 224, 200, false, true); got.Left != 100 {
		t.Fatalf("right to left: %+v", got)
	}
}

// TestTodo_CHATUX_018_RuleLine: a blocked draft's warning line names the rule
// that refused it, after the sentence, in each language, and says nothing extra
// when the server named none.
func TestTodo_CHATUX_018_RuleLine(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Rule: Profanity", "de-DE": "Regel: Profanity", "ar": "القاعدة: ⁨Profanity⁩"} {
		m := Model{Locale: locale, SelectedID: "room", AuthorBlocked: map[string]AuthorBlocked{
			ModAuthorKeyComposer("room"): {Surface: ModAuthorSurfaceMessage, Words: []string{"damn"}, Text: "you damn fool", Rule: "Profanity", Stamp: 1},
		}}
		got := renderNode(t, modAuthorLine(m, localUI{}, ModAuthorKeyComposer("room"), "chatmod002-blocked"))
		if !strings.Contains(got, want) || !strings.Contains(got, `class="chatmod002-rule"`) || strings.Index(got, "chatmod002-rule") < strings.Index(got, "damn") {
			t.Errorf("%s: the warning line does not name the rule: %s", locale, got)
		}
	}
	m := Model{Locale: "en-US", AuthorBlocked: map[string]AuthorBlocked{ModAuthorKeyComposer("room"): {Surface: ModAuthorSurfaceMessage, Words: []string{"damn"}, Text: "you damn fool", Stamp: 1}}}
	if got := renderNode(t, modAuthorLine(m, localUI{}, ModAuthorKeyComposer("room"), "x")); strings.Contains(got, "chatmod002-rule") || strings.Contains(got, "Rule:") {
		t.Errorf("a rule is named though the server named none: %s", got)
	}
}
