package chatui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatux027Page is the page of a channel manager with Manage channel in the
// product's own catalog; unavailable makes the channel status fail to load.
func chatux027Page(t *testing.T, locale string, unavailable bool) string {
	t.Helper()
	return lane3Page(t, locale, func(m *chatui.Model) {
		m.ShowDetails, m.IsTenantAdmin = true, true
		m.ChatFeatures = &chatui.ChatFeatures{Filters: true, Translation: true}
		m.ChannelTeam = chatui.ChannelTeamWidget{Revision: 1, Members: []chatui.ChannelTeamMember{{HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}}
		m.ChannelProject = chatui.ChannelProjectWidget{Revision: 1}
		m.Callbacks.SetChannelTeamPurpose = func(string) {}
		m.Callbacks.CopyConversationAPICurl = func(string) {}
		view := chatui.ChannelStatusView{
			Status:      chat.ChannelStatus{TenantID: "t", ConversationID: "general", Status: chatpolicy.StatusOpen, Revision: 1},
			Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
		}
		view.Unavailable = unavailable
		m.ChannelStatuses = map[string]chatui.ChannelStatusView{"general": view}
		m.ChangeChannelStatus = func(chat.ChangeChannelStatusRequest) {}
	})
}

// manageBlock is the markup of the Manage channel block of a page.
func chatux027Block(t *testing.T, page string) string {
	t.Helper()
	at := strings.Index(page, `id="chat-details-group-manage"`)
	if at < 0 {
		t.Fatalf("the page has no Manage channel block")
	}
	end := strings.Index(page[at:], "</aside>")
	if end < 0 {
		end = len(page) - at
	}
	return page[at : at+end]
}

// TestTodo_CHATUX_027_Browser: Manage channel as the product serves it in
// English, German and Arabic: every section row is the same button (one class
// set, a state and the block it controls, a chevron), nothing is open before it
// is pressed, the groups are labelled in one style, and a section that failed
// shows its error in full inside itself, open, with Try again, as the only open one.
func TestTodo_CHATUX_027_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		page := chatux027Page(t, locale, false)
		lane3NoLeaks(t, locale+" manage channel", page)
		block := chatux027Block(t, page)
		rows := regexp.MustCompile(`<button[^>]*class="manage-sec-row"[^>]*>`).FindAllString(block, -1)
		if len(rows) < 3 {
			t.Fatalf("%s: %d section rows: %s", locale, len(rows), block)
		}
		if every := strings.Count(block, `manage-sec-row`); every != len(rows)+strings.Count(block, `<div class="manage-sec-row"`) {
			t.Errorf("%s: a row has another class set (%d rows, %d mentions)", locale, len(rows), every)
		}
		for _, row := range rows {
			for _, want := range []string{`aria-expanded="false"`, `aria-controls="chat-manage-`, `type="button"`, `data-action="manage-section"`} {
				if !strings.Contains(row, want) {
					t.Errorf("%s: a row misses %s: %s", locale, want, row)
				}
			}
		}
		if strings.Contains(block, `aria-expanded="true"`) {
			t.Errorf("%s: a row is open before anything was pressed", locale)
		}
		// Every block of a row, help sentences included, is hidden until its row is pressed.
		for _, body := range regexp.MustCompile(`<div aria-labelledby="chat-manage-[^"]*" class="manage-sec-body"[^>]*>`).FindAllString(block, -1) {
			if !strings.Contains(body, " hidden") {
				t.Errorf("%s: a block is shown while its row is closed: %s", locale, body)
			}
		}
		if got := strings.Count(block, "manage-sec-chevron"); got != len(rows) {
			t.Errorf("%s: %d chevrons for %d rows", locale, got, len(rows))
		}
		if strings.Count(block, `<h4 class="manage-caption">`) < 2 {
			t.Errorf("%s: the groups are not labelled in one style", locale)
		}
		for _, old := range []string{"chat-disclosure-button", "manage-row-summary", "channel-widget-summary"} {
			if strings.Contains(block, old) {
				t.Errorf("%s: Manage channel still draws the old row %q", locale, old)
			}
		}

		failed := chatux027Block(t, chatux027Page(t, locale, true))
		if got := strings.Count(failed, `aria-expanded="true"`); got != 1 {
			t.Fatalf("%s: %d rows open with a failed section, want 1", locale, got)
		}
		start := strings.Index(failed, `data-manage-section="status"`)
		end := start + strings.Index(failed[start+10:], `data-manage-section=`)
		if start < 0 || end < start {
			t.Fatalf("%s: no status section in %s", locale, failed)
		}
		start = strings.LastIndex(failed[:start], "<div")
		own := failed[start:end]
		retry := productui.ResolveProductLocale(locale).Text(chatui.KeyRetry)
		if !strings.Contains(own, `aria-expanded="true"`) || !strings.Contains(own, `role="alert"`) || !strings.Contains(own, ">"+retry+"<") || !strings.Contains(own, `data-failed="true"`) {
			t.Errorf("%s: the error is not inside its own open section with %q: %s", locale, retry, own)
		}
		if strings.Contains(failed[:start], `role="alert"`) || strings.Contains(failed[end:], `role="alert"`) {
			t.Errorf("%s: the error is printed under another section", locale)
		}
	}
}

// TestTodo_CHATUX_027_Integration: no server behaviour changes in this todo, so
// this is a served-markup test: the page the product serves carries markup whose
// every class is styled by the stylesheet it serves with it, the panel scrolls
// as one column, and the rows are mirrored by logical properties in Arabic.
func TestTodo_CHATUX_027_Integration(t *testing.T) {
	sheet := chatui.ScopedStylesheet()
	for _, locale := range []string{"en-US", "ar"} {
		block := chatux027Block(t, chatux027Page(t, locale, true))
		classes := map[string]bool{}
		for _, attr := range regexp.MustCompile(`class="([^"]*manage[^"]*)"`).FindAllStringSubmatch(block, -1) {
			for _, class := range strings.Fields(attr[1]) {
				if strings.HasPrefix(class, "manage-sec") || class == "manage-caption" {
					classes[class] = true
				}
			}
		}
		for _, want := range []string{"manage-sec", "manage-sec-row", "manage-sec-label", "manage-sec-value", "manage-sec-chevron", "manage-sec-body", "manage-caption"} {
			if !classes[want] {
				t.Errorf("%s: the served markup has no %s", locale, want)
			}
		}
		for class := range classes {
			if !strings.Contains(sheet, "."+class) {
				t.Errorf("%s: the served stylesheet does not style .%s", locale, class)
			}
		}
	}
	for _, rule := range []string{".manage-sec-row{display:flex", "min-block-size:36px", "border-inline-start:1px solid", "margin-inline-start:4px", "button.manage-sec-row:hover{background:var(--soft)}"} {
		if !strings.Contains(sheet, rule) {
			t.Errorf("the served stylesheet lacks %s", rule)
		}
	}
	// One scroll area: no section rule makes its own.
	for _, own := range regexp.MustCompile(`\.manage-sec[^{]*\{[^}]*\}`).FindAllString(sheet, -1) {
		if strings.Contains(own, "overflow:auto") || strings.Contains(own, "overflow:scroll") || strings.Contains(own, "max-block-size") {
			t.Errorf("a section rule scrolls on its own: %s", own)
		}
	}
}
