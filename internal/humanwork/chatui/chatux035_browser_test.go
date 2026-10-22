package chatui_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_035_Browser reads an agent's English answer in an Arabic page
// and in the two left-to-right ones: the member count takes the plural form of
// its number in each language, every number Chat prints is in the numerals of the
// locale while version strings and citation marks stay Latin inside their own
// isolate, an English block in an Arabic page is isolated for direction, and the
// Archived footer is built the way the section headers are.
func TestTodo_CHATUX_035_Browser(t *testing.T) {
	counts := map[string][]string{
		"en-US": {"1 member", "2 members", "8 members", "18 members", "100 members"},
		"de-DE": {"1 Mitglied", "2 Mitglieder", "8 Mitglieder", "18 Mitglieder", "100 Mitglieder"},
		"ar":    {"عضو واحد", "عضوان", "٨ أعضاء", "١٨ عضوًا", "١٠٠ عضو"},
	}
	times := map[string]string{"en-US": "11:18 AM", "de-DE": "11:18", "ar": "١١:١٨"}
	body := "Carry over up to 40 hours.\n\n- first point\n- second point\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](/workspace/app/docs?document=doc-1#carryover) [1]"
	for _, locale := range w7Locales {
		// Counts follow the plural rules of the language.
		for i, n := range []int{1, 2, 8, 18, 100} {
			page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
				m.Conversations = []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true, MemberCount: n}}
			}))
			topic := w7One(t, locale+" header count", page, func(x *xhtml.Node) bool { return w7Class(x, "topic-count") })
			if got := w7Text(topic); !strings.HasSuffix(got, counts[locale][i]) {
				t.Errorf("%s: %d members read %q, want the form %q", locale, n, got, counts[locale][i])
			}
		}

		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.Conversations = chatbug090Rooms()
			m.Conversations[1].Unread = 12
			m.ChatFeatures = &chatui.ChatFeatures{Status: true}
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{
				"old-ideas": {Status: chat.ChannelStatus{ConversationID: "old-ideas", Name: "old-ideas", Status: chatpolicy.StatusArchived}},
			}
			m.Messages = []chatui.Message{{ID: "m1", AuthorID: "policy-helper", Author: "Policy Helper", Body: body, Revision: 1, SentAt: time.Date(2026, 10, 1, 11, 18, 0, 0, time.Local), Sequence: 1}}
		}))

		// One numeral system per locale for every number printed.
		if got := w7Text(w7One(t, locale+" message time", page, func(x *xhtml.Node) bool { return w7Class(x, "message-time") })); got != times[locale] {
			t.Errorf("%s: the message time is %q, want %q", locale, got, times[locale])
		}
		channels := w7One(t, locale+" Channels section", page, func(x *xhtml.Node) bool { return w7Attr(x, "data-section-id") == "channels" })
		count := w7One(t, locale+" section count", channels, func(x *xhtml.Node) bool { return w7Class(x, "section-count") })
		if got, want := w7Text(count), s31Digits(locale, strconv.Itoa(8)); got != want {
			t.Errorf("%s: the Channels count is %q, want %q", locale, got, want)
		}
		badge := w7One(t, locale+" unread badge", page, func(x *xhtml.Node) bool { return w7Class(x, "chat-badge") && !w7Class(x, "mention") })
		if got, want := w7Text(badge), s31Digits(locale, "12"); got != want {
			t.Errorf("%s: the unread badge is %q, want %q", locale, got, want)
		}

		// The answer's blocks: an Arabic page isolates its paragraphs for direction
		// (dir=auto) so English words are ordered by their own language; the
		// left-to-right pages add nothing.
		message := w7One(t, locale+" message", page, func(x *xhtml.Node) bool { return x.Data == "article" && w7Class(x, "message") })
		bodyDiv := w7One(t, locale+" message body", message, func(x *xhtml.Node) bool { return w7Class(x, "message-body") })
		paragraphs := w7All(bodyDiv, func(x *xhtml.Node) bool { return x.Data == "p" })
		if len(paragraphs) != 2 {
			t.Fatalf("%s: %d paragraphs in the answer, want 2", locale, len(paragraphs))
		}
		for _, p := range paragraphs {
			want := ""
			if locale == "ar" {
				want = "auto"
			}
			if got := w7Attr(p, "dir"); got != want {
				t.Errorf("%s: a paragraph carries dir=%q, want %q", locale, got, want)
			}
		}
		if got := w7Attr(bodyDiv, "dir"); got != "auto" {
			t.Errorf("%s: the message body carries dir=%q, want auto", locale, got)
		}
		// The version string and the citation mark are identifiers: they stay Latin,
		// and in an Arabic page they sit inside their own isolate.
		sources := w7All(bodyDiv, func(x *xhtml.Node) bool { return x.Data == "a" })
		if len(sources) != 1 || !strings.Contains(w7Text(sources[0]), "v1.0.0") {
			t.Fatalf("%s: the source link does not keep its Latin version string", locale)
		}
		line := sources[0]
		for line != nil && line.Data != "li" {
			line = line.Parent
		}
		if line == nil || !strings.Contains(w7Text(line), "[1]") {
			t.Errorf("%s: the citation mark [1] is not Latin", locale)
		} else if locale == "ar" && len(w7All(line, func(x *xhtml.Node) bool { return x.Data == "bdi" })) == 0 {
			t.Errorf("%s: the source line is not isolated for direction", locale)
		}

		// Archived is the same section component as Channels and Direct messages.
		archived := w7One(t, locale+" Archived section", page, func(x *xhtml.Node) bool { return w7Attr(x, "data-section-id") == "archived" })
		toggle := w7One(t, locale+" Archived toggle", archived, func(x *xhtml.Node) bool {
			return x.Data == "button" && w7Class(x, "section-title") && w7Attr(x, "data-action") == "toggle-archived"
		})
		title := w7One(t, locale+" Channels header", channels, func(x *xhtml.Node) bool { return x.Data == "button" && w7Class(x, "section-title") })
		for name, button := range map[string]*xhtml.Node{"Archived section": toggle, "Channels section": title} {
			chevrons := w7All(button, func(x *xhtml.Node) bool { return w7Class(x, "icon-chevron-down") || w7Class(x, "icon-chevron-right") })
			if w7Attr(button, "aria-expanded") == "" || len(chevrons) != 1 {
				t.Errorf("%s: the %s is not an expandable button with one chevron", locale, name)
			}
		}
	}
	sheet := strings.ReplaceAll(chatui.ScopedStylesheet(), ":scope", ".chat-workspace")
	for _, want := range []string{
		`.section-title{flex:1;display:flex;align-items:center;gap:4px;min-width:0`,
		`.chat-workspace[dir="rtl"] .message-body,.chat-workspace[dir="rtl"] .agent-reply-answer{direction:rtl;text-align:start}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the page's stylesheet lacks %q", want)
		}
	}
}
