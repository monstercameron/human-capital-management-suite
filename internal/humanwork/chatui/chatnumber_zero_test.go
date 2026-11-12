package chatui

import (
	"regexp"
	"strconv"
	"testing"
)

// The browser's Number formatter (chatNumberFormatter) answers "" for zero, the
// tray's no-value signal. A visible count that must read zero goes through nz,
// which writes the localized zero when Number answers "". Each test below uses
// the client's own formatter and asserts the zero is written, not blank.

func chatZeroModel(locale string) Model {
	return Model{State: StateReady, Locale: locale, SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t", IsTenantAdmin: true,
		Number: func(n int) string {
			if n == 0 {
				return ""
			}
			return chatNumeral(locale, strconv.Itoa(n))
		},
		Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel}}}
}

// TestChatCountZero_ComposerSegmentPeople: a restricted segment with no people
// offers "only 0 people" in the audience select.
func TestChatCountZero_ComposerSegmentPeople(t *testing.T) {
	for _, locale := range chatmodLocales {
		m := chatZeroModel(locale)
		m.GateAudience = map[string]GateAudienceOptions{"room": {Segments: []GateAudienceChoice{{Name: "ops", People: 0}}, Restricted: true}}
		zero := chatNumeral(locale, "0")
		want := laneTextf(m, chatgate014ComposerCopy, chatgate014KeyOnlyMany, map[string]string{"segment": "ops", "n": zero})
		markup := renderNode(t, chatgate014Composer(m, false))
		option := regexp.MustCompile(`<option[^>]*>\s*` + regexp.QuoteMeta(want) + `\s*</option>`)
		if !option.MatchString(markup) {
			t.Errorf("%s: the restricted option does not read %q: %s", locale, want, markup)
		}
	}
}

// TestChatCountZero_MessageAudiencePeople: a restricted message delivered to
// nobody tags itself "only 0 people".
func TestChatCountZero_MessageAudiencePeople(t *testing.T) {
	for _, locale := range chatmodLocales {
		m := chatZeroModel(locale)
		msg := Message{Audience: &MessageAudience{Segments: []string{"ops"}, Restricted: true, People: 0}}
		want := laneTextf(m, chatgate014ComposerCopy, chatgate014KeyTagMany, map[string]string{"segments": chatgate014Join(m, []string{"ops"}), "n": chatNumeral(locale, "0")})
		markup := renderNode(t, chatgate014Tag(m, msg))
		tag := regexp.MustCompile(`<span[^>]*>\s*` + regexp.QuoteMeta(want) + `\s*</span>`)
		if !tag.MatchString(markup) {
			t.Errorf("%s: the audience tag does not read %q: %s", locale, want, markup)
		}
	}
}

// TestChatCountZero_SidebarSectionCount: a sidebar section with no conversations
// shows its count as zero, not as a blank badge.
func TestChatCountZero_SidebarSectionCount(t *testing.T) {
	for _, locale := range chatmodLocales {
		m := chatZeroModel(locale)
		markup := ""
		for _, node := range chatside001Header(m, SidebarSection{ID: "direct", Name: "Direct messages"}, "Direct messages", nil) {
			markup += renderNode(t, node)
		}
		count := regexp.MustCompile(`class="section-count"[^>]*>\s*` + regexp.QuoteMeta(chatNumeral(locale, "0")) + `\s*</span>`)
		if !count.MatchString(markup) {
			t.Errorf("%s: the empty section does not show a zero count: %s", locale, markup)
		}
	}
}

// TestChatCountZero_ProjectBoardProgress: a board with nothing done, and one
// with everything done, write their zero counts in the progress line.
func TestChatCountZero_ProjectBoardProgress(t *testing.T) {
	cases := []struct {
		name        string
		done, total int
		wantDone    int
		wantOpen    int
	}{
		{name: "nothing done", done: 0, total: 2, wantDone: 0, wantOpen: 2},
		{name: "everything done", done: 2, total: 2, wantDone: 2, wantOpen: 0},
	}
	for _, locale := range chatmodLocales {
		m := chatZeroModel(locale)
		for _, c := range cases {
			preview := ProjectTaskPreview{ProjectID: "p", TaskID: "t", Done: c.done, Total: c.total}
			want := m.tf(KeyProjectBoardProgress, map[string]string{
				"done":  chatNumeral(locale, strconv.Itoa(c.wantDone)),
				"total": chatNumeral(locale, strconv.Itoa(c.total)),
				"open":  chatNumeral(locale, strconv.Itoa(c.wantOpen)),
			})
			markup := renderNode(t, projectBoardSummary(m, preview))
			counts := regexp.MustCompile(`class="chat-project-embed-counts"[^>]*>([^<]*)</span>`).FindStringSubmatch(markup)
			if counts == nil || counts[1] != want {
				t.Errorf("%s, %s: the progress line is %v, want %q: %s", locale, c.name, counts, want, markup)
			}
		}
	}
}

// TestChatCountZero_SearchGroupHeading: a result group still loading more, with
// nothing loaded yet, writes its zero count.
func TestChatCountZero_SearchGroupHeading(t *testing.T) {
	for _, locale := range chatmodLocales {
		m := chatZeroModel(locale)
		markup := renderNode(t, searchGroupHeading(m, KeySearchChannels, 0))
		count := regexp.MustCompile(`class="search-group-count"[^>]*>\s*` + regexp.QuoteMeta(chatNumeral(locale, "0")) + `\s*</span>`)
		if !count.MatchString(markup) {
			t.Errorf("%s: the empty result group does not show a zero count: %s", locale, markup)
		}
	}
}
