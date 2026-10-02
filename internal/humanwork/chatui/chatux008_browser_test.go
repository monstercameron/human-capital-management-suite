package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatux008Page renders the whole page with the product's own catalog and
// returns the open thread pane of #random.
func chatux008Page(t *testing.T, locale string, followed bool) (pane string, ctx productui.LocaleContext) {
	t.Helper()
	ctx = productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "random", Name: "random", Kind: chatui.PublicChannel, Joined: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "jake", CurrentTenantID: "t",
		ShowThread: true, ThreadParentID: "root", ThreadFollowed: followed,
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room},
		Messages:      []chatui.Message{{ID: "root", AuthorID: "walt", Author: "Walt Brennan", Body: "Who has the Q3 numbers?", Replies: 2}},
		ThreadMessages: []chatui.Message{
			{ID: "r1", AuthorID: "jake", Author: "Jake Sullivan", Body: "I do"},
			{ID: "r2", AuthorID: "walt", Author: "Walt Brennan", Body: "Thanks"},
		},
		Callbacks: chatui.Callbacks{SetThreadFollow: func(bool) {}, CloseThread: func() {}, ReplyInThread: func(string, string) {}},
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(page, `chat-side thread-pane"`)
	if start < 0 {
		t.Fatalf("%s: no thread pane", locale)
	}
	start = strings.LastIndex(page[:start], "<aside")
	end := strings.Index(page[start:], "</aside>")
	if end < 0 {
		t.Fatalf("%s: unterminated thread pane", locale)
	}
	return html.UnescapeString(page[start : start+end]), ctx
}

func TestTodo_CHATUX_008_Browser(t *testing.T) {
	words := map[string]struct{ title, bell, reply, off, on string }{
		"en-US": {"Thread", "Notify me about replies", "Reply in thread", "Get a notification for each new reply in this thread", "Following: you get a notification for each reply. Select to stop."},
		"de-DE": {"Thread", "Über Antworten benachrichtigen", "Im Thread antworten", "Bei jeder neuen Antwort in diesem Thread benachrichtigt werden", "Folge ich: Sie werden über jede Antwort benachrichtigt. Zum Beenden auswählen."},
		"ar":    {"السلسلة", "أبلغني بالردود", "الرد في السلسلة", "احصل على إشعار بكل رد جديد في هذه السلسلة", "تتابع: ستصلك إشعارات بكل رد. اختر لإيقافها."},
	}
	keyLeak := regexp.MustCompile(`\bchat\.[a-z0-9_]+\.[a-z0-9_.]+`)
	sheet := chatui.ScopedStylesheet()
	for locale, w := range words {
		for _, followed := range []bool{false, true} {
			pane, ctx := chatux008Page(t, locale, followed)
			if strings.Contains(pane, "⟦") || keyLeak.MatchString(pane) || strings.Contains(pane, ` style="`) {
				t.Errorf("%s: a copy key, marker or style attribute reaches the pane: %s", locale, keyLeak.FindString(pane))
			}
			tip := map[bool]string{false: w.off, true: w.on}[followed]
			for _, marker := range []string{"<h2>" + w.title + "</h2>", `class="thread-channel-link"`, `aria-label="` + w.bell + `"`, `aria-pressed="` + map[bool]string{true: "true", false: "false"}[followed] + `"`, `title="` + tip + `"`,
				`class="thread-root"`, `class="thread-count"`, `class="thread-replies"`, `placeholder="` + w.reply + `"`} {
				if !strings.Contains(pane, marker) {
					t.Errorf("%s/followed=%v: the pane misses %s", locale, followed, marker)
				}
			}
			// The header order is the title, the channel link under it, then the two buttons.
			heading := pane[strings.Index(pane, `class="side-heading thread-heading"`):]
			last := -1
			for _, marker := range []string{"<h2>", "thread-channel-link", "thread-notify", "thread-back"} {
				at := strings.Index(heading, marker)
				if at < 0 || at < last {
					t.Fatalf("%s: the header has %q missing or out of order", locale, marker)
				}
				last = at
			}
			if strings.Contains(pane, ">"+ctx.Text(chatui.KeyFollow)+"<") || strings.Contains(pane, ">"+ctx.Text(chatui.KeyFollowing)+"<") || strings.Contains(pane, `type="checkbox"`) {
				t.Errorf("%s: a bare Follow button or a checkbox without a service behind it is in the pane", locale)
			}
		}
	}
	// The rules that hold at every width: the header grows to two lines, the
	// channel link truncates instead of wrapping out of a 390 px pane, the bell
	// keeps a touch-sized target, and the divider is drawn from both sides.
	for _, width := range []int{1440, 800, 390} {
		for _, rule := range []string{".thread-heading{height:auto;min-height:56px", ".thread-channel-link{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap", ".thread-count::before{content:\"\"", ".thread-notify[aria-pressed=\"true\"] .chat-icon{fill:currentColor}"} {
			if !strings.Contains(sheet, rule) {
				t.Errorf("%d: the stylesheet has no %s", width, rule)
			}
		}
		if !regexp.MustCompile(`\.icon-button\{[^}]*width:34px;height:34px`).MatchString(sheet) {
			t.Errorf("%d: the bell's button has no 34 px target", width)
		}
	}
	if fixed := regexp.MustCompile(`(?:^|[;{\s])(?:min-|max-)?(?:width|inline-size):\s*\d+px`).FindString(chatui.ChatUX008Styles); fixed != "" {
		t.Errorf("the thread styles fix a width in pixels: %q", fixed)
	}
}
