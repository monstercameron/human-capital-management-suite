package chatui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// chatux030Model is chatux021Model with enough people that the member list is
// longer than its window.
func chatux030Model(people int) Model {
	m := chatux021Model()
	m.Members = nil
	for i := 0; i < people; i++ {
		m.Members = append(m.Members, Member{ID: fmt.Sprintf("p%d", i), HomeTenantID: "t", Name: fmt.Sprintf("Person %02d", i)})
	}
	m.Conversations[0].MemberCount = people
	return m
}

// TestTodo_CHATUX_030: Conversation details names its things in plain words
// and puts the reader's own settings first. The page of a workspace
// administrator in English: the joining row is not called "Gate", the sample
// request is a quiet button under "For developers", Notifications for me and
// the joining row come before the member list, the list shows eight people and
// a button for the rest, and the three sentences that were circular, wordy or
// unclear are written once and plainly in all three languages.
func TestTodo_CHATUX_030(t *testing.T) {
	page := chatux021Details(t, chatux030Model(12), handlers{})
	if !strings.Contains(page, ">Joining questions<") || regexp.MustCompile(`>Gate<`).MatchString(page) {
		t.Errorf("the joining row is not named in plain words: %s", regexp.MustCompile(`<a[^>]*data-gate-open[^>]*>[^<]*`).FindString(page))
	}
	if !strings.Contains(page, `data-gate-open="general"`) {
		t.Error("the joining row no longer carries data-gate-open, so the client cannot open it")
	}
	// The sample request: secondary, and under its own disclosure.
	dev := strings.Index(page, ">For developers<")
	button := regexp.MustCompile(`<button[^>]*data-action="copy-conversation-api-curl"[^>]*>`).FindString(page)
	if dev < 0 || button == "" || !strings.Contains(button, `class="button secondary small"`) || strings.Index(page, button) < dev {
		t.Errorf("the sample request is not a secondary button under For developers: %q", button)
	}
	// Order: the reader's own settings above the member list.
	notify, joining, members := strings.Index(page, `details-notify`), strings.Index(page, ">Joining questions<"), strings.Index(page, `id="chat-details-members"`)
	if !(notify >= 0 && notify < joining && joining < members) {
		t.Errorf("order is notifications %d, joining %d, members %d", notify, joining, members)
	}
	// Eight people and Show all 12; open, every person and Show fewer.
	section := page[members:]
	section = section[:strings.Index(section, "</section>")]
	if got := strings.Count(section, `class="member-row"`); got != chatux030MembersVisible || !strings.Contains(section, ">Show all 12<") {
		t.Errorf("collapsed list shows %d people, want %d with Show all 12", got, chatux030MembersVisible)
	}
	open := chatux021Details(t, chatux030Model(12), handlers{local: localUI{detailGroups: map[string]bool{chatux030MembersKey: true}}})
	if got := strings.Count(open[strings.Index(open, `id="chat-details-members"`):], `class="member-row"`); got < 12 || !strings.Contains(open, ">Show fewer<") {
		t.Errorf("open list shows %d people, want all 12 with Show fewer", got)
	}
	if few := chatux021Details(t, chatux030Model(5), handlers{}); strings.Contains(few, "details-members-more") {
		t.Error("a short list offers Show all")
	}
	// A filtered list is never cut.
	filtered := chatux030Model(12)
	filtered.MemberQuery = "Person"
	if page := chatux021Details(t, filtered, handlers{}); strings.Contains(page, "details-members-more") {
		t.Error("a filtered list is cut")
	}
	// A member (not an administrator) reads "My answers".
	member := chatux030Model(3)
	member.IsTenantAdmin, member.CurrentUser = false, "p1"
	member.Conversations[0].OwnerID = "p0"
	if page := chatux021Details(t, member, handlers{}); strings.Contains(page, ">Joining questions<") {
		t.Error("a member is shown the administrator's joining row")
	}
}

// TestTodo_CHATUX_030_Browser: the copy of the three rewritten lines, in
// English, German and Arabic, and the styles that make the row and the compact
// title look as written.
func TestTodo_CHATUX_030_Browser(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Owner:", "de-DE": "Verantwortlich:", "ar": "المسؤول:"} {
		if got := chatux017Text(locale, "looked_after"); got != want {
			t.Errorf("%s: the owner line starts %q, want %q", locale, got, want)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		intro := modadminText(m, "intro")
		if strings.Contains(intro, "also apply") || strings.Contains(intro, "auch in Direktnachrichten") || strings.Contains(intro, "تسري أيضاً") {
			t.Errorf("%s: the filter help is still circular: %s", locale, intro)
		}
		reads := chat5Text(m, "chat.agents.reads_channel")
		if strings.Contains(reads, "placed") || strings.Contains(reads, "الموضوعة") || reads == "" {
			t.Errorf("%s: the agent read caption is still wordy: %q", locale, reads)
		}
		for _, key := range []string{chatux030KeyGateAdmin, chatux030KeyShowAll, chatux030KeyShowFewer, chatux030KeyDevelopers} {
			if laneText(m, chatux030Copy, key) == "" || strings.HasPrefix(laneText(m, chatux030Copy, key), "⟦") {
				t.Errorf("%s: no text for %s", locale, key)
			}
		}
	}
	if got := modadminText(Model{Locale: "en-US"}, "intro"); !strings.Contains(got, "Direct messages are checked only by workspace-wide filters.") {
		t.Errorf("the English filter help reads %q", got)
	}
	for _, rule := range []string{
		".details-summary{display:grid;grid-template-columns:auto minmax(0,1fr)",
		"min-block-size:48px",
		".chatstate-badge>span[aria-hidden=true]{display:inline-block;inline-size:8px;block-size:8px;border-radius:50%",
		".pinned-preview{white-space:normal;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2",
		".details-purpose-row{align-items:center;min-block-size:36px;padding:0 8px;border:1px solid",
	} {
		if !strings.Contains(ChatUX027Styles, rule) {
			t.Errorf("the panel styles lack %s", rule)
		}
	}
}
