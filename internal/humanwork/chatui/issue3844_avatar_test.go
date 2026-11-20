package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// issue3844Specificity is the (ids, classes, elements) weight of one selector
// inside the chat scope, where ".chat-workspace" stands for the scope root
// (a pseudo-class, so it weighs one class) and a bare rule weighs what it
// says (the implicit scope selector is zero).
func issue3844Specificity(selector string) int {
	selector = strings.TrimSpace(selector)
	weight := 0
	weight += strings.Count(selector, "#") * 10000
	attrs := regexp.MustCompile(`\[[^\]]*\]`).FindAllString(selector, -1)
	weight += len(attrs) * 100
	stripped := regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(selector, "")
	stripped = regexp.MustCompile(`:(where|is|not|has)\(`).ReplaceAllString(stripped, "(")
	weight += len(regexp.MustCompile(`\.[A-Za-z_-][\w-]*`).FindAllString(stripped, -1)) * 100
	weight += len(regexp.MustCompile(`::?[A-Za-z-]+`).FindAllString(stripped, -1)) * 100
	return weight
}

// issue3844Rules lists the style rules of the sheet that set the property, as
// (selector, order) pairs, so that the winner of a tie by order is known.
type issue3844Rule struct {
	selector string
	order    int
}

func issue3844RulesSetting(sheet, property string) []issue3844Rule {
	var out []issue3844Rule
	rule := regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	for index, match := range rule.FindAllStringSubmatch(sheet, -1) {
		if !regexp.MustCompile(`(^|;)\s*` + regexp.QuoteMeta(property) + `\s*:`).MatchString(match[2]) {
			continue
		}
		for _, selector := range strings.Split(match[1], ",") {
			out = append(out, issue3844Rule{selector: strings.TrimSpace(selector), order: index})
		}
	}
	return out
}

// TestIssue3844_PersonAvatarShapeBeatsEveryAvatarSizeRule: the round person
// shape has to win over every rule that gives an avatar of one size or place a
// square corner, so a person looks the same in a message, a search result, a
// direct-message row, a saved message and a thread.
func TestIssue3844_PersonAvatarShapeBeatsEveryAvatarSizeRule(t *testing.T) {
	sheet := Stylesheet
	hue := issue3844RulesSetting(sheet, "border-radius")
	var round issue3844Rule
	for _, r := range hue {
		if r.selector == `.chat-workspace [data-avatar-hue]` {
			round = r
		}
	}
	if round.selector == "" {
		t.Fatal("the person shape rule is missing from the stylesheet")
	}
	roundWeight := issue3844Specificity(round.selector)
	for _, r := range hue {
		if r == round || !strings.Contains(r.selector, "avatar") || strings.Contains(r.selector, "data-avatar-hue") {
			continue
		}
		// Agent avatars, the large profile portrait and decorative skeletons
		// are not person tiles in the person slot.
		if strings.Contains(r.selector, "agent") || strings.Contains(r.selector, "large-avatar") || strings.Contains(r.selector, "header-avatar") || strings.Contains(r.selector, "loading-avatar") || strings.Contains(r.selector, "online") || strings.Contains(r.selector, "person-avatar-button") || strings.Contains(r.selector, "photo") {
			continue
		}
		if r.selector == ".person-chip .avatar.tiny" { // a round corner too (50%)
			continue
		}
		weight := issue3844Specificity(r.selector)
		if weight > roundWeight || (weight == roundWeight && r.order > round.order) {
			t.Errorf("%q gives an avatar its own corner and beats the person shape", r.selector)
		}
	}
}

// TestIssue3844_SearchResultAndDirectRowUsePersonColour renders the same person
// as a search result and as a direct-message row and holds both to the
// person's own palette slot, the one a message by them is drawn with.
func TestIssue3844_SearchResultAndDirectRowUsePersonColour(t *testing.T) {
	const person = "usr-ben-whitaker"
	hueAttr := `data-avatar-hue="` + strconv.Itoa(AvatarHue(person)) + `"`
	at := time.Date(2026, time.October, 1, 22, 10, 0, 0, time.UTC)
	m := Model{State: StateReady, Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t", SelectedID: "design",
		Conversations: []Conversation{{ID: "design", Name: "design", Kind: PublicChannel, Joined: true}, {ID: "dm", Name: "Ben Whitaker", Kind: DirectMessage, Joined: true}},
		PeerIDs:       map[string]string{"dm": person},
		Members:       []Member{{ID: person, HomeTenantID: "t", Name: "Ben Whitaker"}},
		Messages:      []Message{{ID: "post", AuthorID: person, Author: "Ben Whitaker", Body: "x"}},
		SearchPeople:  []SearchPerson{{ID: person, Name: "Ben Whitaker"}},
		SearchMessages: []SearchMessage{{ConversationID: "design", ConversationName: "design",
			Message: Message{ID: "post", AuthorID: person, Author: "Ben Whitaker", Body: "open enrollment"}}}}

	message := renderNode(t, chatux047MessageAvatar(m, m.Messages[0], "avatar"))
	if !strings.Contains(message, hueAttr) {
		t.Fatalf("the message avatar lacks %s: %s", hueAttr, message)
	}

	view := ChatSearchView{Query: "open enrollment", Context: &m, Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Rows: []chatsearch.Row{
		{Kind: chatsearch.Message, ID: "post", AuthorID: person, At: at, Text: "open enrollment steps", Target: chatsearch.Target{ConversationID: "design", MessageID: "post", Sequence: 3}}}}}}}
	for name, markup := range map[string]string{
		"search result":        renderNode(t, RenderChatSearch("en-US", view)),
		"search quick results": renderNode(t, searchResultsPanel(m)),
		"direct-message row":   renderNode(t, railRow(m, m.Conversations[1])),
		"direct-message glyph": renderNode(t, kindGlyph(m, m.Conversations[1], "Direct message")),
		"conversation heading": renderNode(t, conversationAvatar(m, m.Conversations[1])),
	} {
		if !strings.Contains(markup, hueAttr) {
			t.Errorf("%s does not draw the person in their own colour (%s): %s", name, hueAttr, markup)
		}
	}
}
