package chatui_test

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatuxCatalogRail renders the whole page with the product's own catalog, the
// one that answers an unknown key with a marker, and returns the conversation
// list's navigation element as text a reader would see (tags kept).
func chatuxCatalogRail(t *testing.T, locale string, quiet bool) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true, Unread: 4}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "alice",
		Text:           func(key string) string { return ctx.Text(key) },
		SavedOpenCount: 2,
		Preferences:    chatui.Preferences{QuietHours: quiet, QuietTimezone: "UTC", QuietStartMinute: 22 * 60, QuietEndMinute: 7 * 60},
		Conversations: []chatui.Conversation{room,
			{ID: "read", Name: "read-room", Kind: chatui.PublicChannel, Joined: true},
			{ID: "unread", Name: "unread-room", Kind: chatui.PublicChannel, Joined: true, Unread: 3},
			{ID: "mention", Name: "mention-room", Kind: chatui.PublicChannel, Joined: true, Unread: 5, Mentions: 2}},
		Callbacks: chatui.Callbacks{SavePreferences: func(chatui.Preferences) {}, OpenBrowse: func() {}, OpenCreate: func() {}, CreateSection: func(string) {}, SelectConversation: func(string) {}, ToggleSection: func(string) {}}}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(page, "<nav")
	end := strings.Index(page, "</nav>")
	if start < 0 || end < start {
		t.Fatalf("%s: no conversation list in the page", locale)
	}
	return html.UnescapeString(page[start:end])
}

var chatuxMarker = regexp.MustCompile(`⟦[^⟧]*⟧|chat\.ux0\d\d\.[a-z_]+`)

func TestTodo_CHATUX_002_Browser(t *testing.T) {
	// What each language shows in the list's headings, the preferences panel and
	// the Channels menu, taken from the product's own catalog: never a copy key or
	// a marker, and the words this todo adds are in the language.
	want := map[string][]string{
		"en-US": {"Chat preferences", "Add or find channels", "Create a channel", "Browse channels", "New section", "Quiet hours", "Reading languages", "Change", "Jump to unread"},
		"de-DE": {"Chat-Einstellungen", "Kanäle hinzufügen oder finden", "Kanal erstellen", "Zu Ungelesenem springen"},
		"ar":    {"تفضيلات الدردشة", "إضافة قنوات أو العثور عليها", "إنشاء قناة", "الانتقال إلى غير المقروء"},
	}
	for locale, words := range want {
		for _, width := range []int{1440, 800, 390} {
			t.Run(fmt.Sprintf("%s/%d", locale, width), func(t *testing.T) {
				rail := chatuxCatalogRail(t, locale, true)
				if found := chatuxMarker.FindString(rail); found != "" {
					t.Fatalf("the list prints a copy key: %s", found)
				}
				for _, word := range words {
					if !strings.Contains(rail, word) {
						t.Errorf("the list lacks %q", word)
					}
				}
				for _, gone := range []string{`class="rail-footer"`, `class="rail-link"`, `rail-add`} {
					if strings.Contains(rail, gone) {
						t.Errorf("the foot of the list still has %s", gone)
					}
				}
				// Everything the three old rows held is in a panel that is closed until asked.
				if strings.Count(rail, `data-chat-layer="chat-prefs"`) != 1 || strings.Count(rail, `data-chat-layer="channels-menu"`) != 1 || strings.Count(rail, "rail-quiet-moon") != 1 {
					t.Errorf("want one preferences panel, one channels menu and one moon")
				}
				// Not a word of the old fixed rows is outside a panel: the markup after the
				// list's scroll area is only the pane handle.
				tail := rail[strings.LastIndex(rail, `class="rail-scroll"`):]
				tail = tail[strings.Index(tail, "chatux007-jump-down"):]
				for _, row := range []string{"Quiet hours", "Reading languages", "New section", "Browse channels"} {
					if strings.Contains(tail[strings.Index(tail, "</div></div>"):], row) {
						t.Errorf("%q is drawn under the list", row)
					}
				}
			})
		}
	}
	// The styles that place and draw the new controls are in the page's stylesheet,
	// with the touch size for phones.
	for _, rule := range []string{".chatux002-gear[aria-expanded=true]", ".chat-prefs-head", ".chatux002-item-note", "@media(pointer:coarse){.chatux002-gear"} {
		if !strings.Contains(chatui.Stylesheet, rule) {
			t.Errorf("the stylesheet lacks %q", rule)
		}
	}
	if rtl := chatuxCatalogRail(t, "ar", false); strings.Contains(rtl, "rail-quiet-moon") {
		t.Error("a moon is drawn while quiet hours are off")
	}
}

func TestTodo_CHATUX_007_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, width := range []int{1440, 800, 390} {
			t.Run(fmt.Sprintf("%s/%d", locale, width), func(t *testing.T) {
				rail := chatuxCatalogRail(t, locale, false)
				if found := chatuxMarker.FindString(rail); found != "" {
					t.Fatalf("the list prints a copy key: %s", found)
				}
				// Saved: plain muted text with its count; no badge anywhere on that row.
				saved := rail[strings.Index(rail, `id="chatsave-sidebar"`):]
				saved = saved[:strings.Index(saved, "</button>")]
				if !strings.Contains(saved, `class="chat-count chatsave-count"`) || !strings.Contains(saved, ">2<") || strings.Contains(saved, "chat-badge") {
					t.Errorf("the Saved count is not plain text: %s", saved)
				}
				// Unread is bold and counted; a mention is counted as a mention; the read
				// room has neither; the selected room is the one marked selected.
				for id, wants := range map[string][]string{
					"read":    {`class="chat-row"`},
					"unread":  {`class="chat-row unread"`, `class="chat-badge"`, ">3<"},
					"mention": {`class="chat-row unread"`, `class="chat-badge mention"`, ">2<"},
					"general": {`class="chat-row selected unread"`},
				} {
					at := strings.Index(rail, `data-id="`+id+`"`)
					row := rail[strings.LastIndex(rail[:at], "<button"):]
					row = row[:strings.Index(row, "</button>")]
					for _, want := range wants {
						if !strings.Contains(row, want) {
							t.Errorf("row %s lacks %q: %s", id, want, row)
						}
					}
				}
				if strings.Count(rail, "chat-row selected") != 1 {
					t.Errorf("%d selected rows", strings.Count(rail, "chat-row selected"))
				}
				// Jump to unread: both controls are in the list, closed, named in the language.
				if strings.Count(rail, `data-action="jump-unread"`) != 2 {
					t.Errorf("want the up and the down control")
				}
			})
		}
	}
	for _, rule := range []string{".chatsave-count{", ".chat-rail-row .chat-badge.mention{background:none;color:var(--accent)", ".chatux007-jump-slot{position:sticky"} {
		if !strings.Contains(chatui.Stylesheet, rule) {
			t.Errorf("the stylesheet lacks %q", rule)
		}
	}
}
