package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatmodPage renders the whole Chat page with the product's own catalog, the
// one that answers a missing key with a marker, for one viewer of #general.
func chatmodPage(t *testing.T, locale, viewer string, state chatui.ModerationState) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "owner", MemberCount: 4, Joined: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: viewer, CurrentTenantID: "t",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room},
		Members:       []chatui.Member{{ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}, {ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}},
		Moderation:    state,
		Messages: []chatui.Message{
			{ID: "m1", AuthorID: "jake", Author: "Jake Sullivan", Body: "an ordinary message", Revision: 1},
			{ID: "m2", AuthorID: "jake", Author: "Jake Sullivan", Body: chat.RemovedByAdministrator, Replies: 2, Revision: 3, Attachments: []chatui.Attachment{{Name: "secret-attachment.pdf"}}, Chips: []chatui.ReactionChip{{Emoji: "SECRETEMOJI"}}, Reactions: 4},
			{ID: "m3", AuthorID: "walt", Author: "Walt Brennan", Body: chat.RemovedByAdministrator, Revision: 2},
		},
		Callbacks: chatui.Callbacks{OpenThread: func(string) {}, ToggleDetails: func(bool) {}},
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(page)
}

// TestTodo_CHATMOD_004_Browser_RealCatalog renders what a reader sees where an
// administrator removed a message, in the three languages the product ships,
// with the product's catalog: the words, the thread kept, nothing of the text,
// the reactions or the files, and no copy key printed.
func TestTodo_CHATMOD_004_Browser_RealCatalog(t *testing.T) {
	removed := map[string]string{"en-US": "Removed by an administrator", "de-DE": "Von einem Administrator entfernt", "ar": "أزالها مسؤول"}
	reason := map[string]string{"en-US": "Reason: Harassment or bullying", "de-DE": "Grund: Belästigung oder Mobbing", "ar": "السبب: مضايقة أو تنمر"}
	appeal := map[string]string{"en-US": "Ask for a review", "de-DE": "Überprüfung anfordern", "ar": "طلب مراجعة"}
	restore := map[string]string{"en-US": "Restore", "de-DE": "Wiederherstellen", "ar": "استعادة"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		// A reader (not the author, not a moderator): the words, the thread, nothing else.
		reader := chatmodPage(t, locale, "mia", chatui.ModerationState{})
		if strings.Contains(reader, "⟦") {
			t.Errorf("%s: a copy key is printed: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(reader))
		}
		if strings.Count(reader, removed[locale]) != 2 {
			t.Errorf("%s: want the removal shown twice (two messages), got %d", locale, strings.Count(reader, removed[locale]))
		}
		for _, leaked := range []string{"secret-attachment", "SECRETEMOJI"} {
			if strings.Contains(reader, leaked) {
				t.Errorf("%s: a removed message still shows %q", locale, leaked)
			}
		}
		if !strings.Contains(reader, `data-action="thread" data-id="m2"`) && !regexp.MustCompile(`data-action="thread"[^>]*data-id="m2"|data-id="m2"[^>]*data-action="thread"`).MatchString(reader) {
			t.Errorf("%s: the thread under a removed message is not kept", locale)
		}
		if strings.Contains(reader, appeal[locale]) || strings.Contains(reader, restore[locale]) || strings.Contains(reader, "data-chatmod005") || strings.Contains(reader, `id="chatmod005-sidebar"`) {
			t.Errorf("%s: a reader is offered a moderator's or an author's control", locale)
		}

		// The author of m3: told privately why, and how to ask for a review.
		own := chatmodPage(t, locale, "walt", chatui.ModerationState{Ready: true, NoticeCount: 1, Notices: map[string]chatui.ModerationOwnNotice{"m3": {ConversationID: "general", PostID: "m3", Reason: "harassment", HostTenantID: "t", CanAppeal: true}}})
		if strings.Contains(own, "⟦") {
			t.Errorf("%s: the author's view prints a copy key", locale)
		}
		if !strings.Contains(own, reason[locale]) || !strings.Contains(own, appeal[locale]) || !strings.Contains(own, `data-chatremove="appeal"`) || !strings.Contains(own, `data-post-id="m3"`) {
			t.Errorf("%s: the author is not told the reason and how to ask for a review: %s", locale, regexp.MustCompile(`(?s)<div class="message-row chatremove-tombstone"[^>]*data-message-id="m3".*?</div>`).FindString(own))
		}
		// An author who has no notice (it is another person's message) sees no reason.
		if strings.Count(own, reason[locale]) != 1 {
			t.Errorf("%s: the reason appears %d times", locale, strings.Count(own, reason[locale]))
		}
		// The sidebar entry shows the notice count to a person who has notices.
		if !strings.Contains(own, `id="chatmod005-sidebar"`) || !strings.Contains(own, `data-moderation-count="true"`) {
			t.Errorf("%s: the sidebar does not show the notices entry", locale)
		}

		// A moderator: the entry with the count of open items, Restore on a removed message.
		mod := chatmodPage(t, locale, "olive", chatui.ModerationState{Ready: true, Moderator: true, Open: 3, Reviewable: map[string]bool{"general": true}, Removable: map[string]bool{"general": true}})
		if strings.Contains(mod, "⟦") {
			t.Errorf("%s: the moderator's view prints a copy key", locale)
		}
		side := regexp.MustCompile(`(?s)<div class="chatmod005-sidebar".*?</div>`).FindString(mod)
		if side == "" || !strings.Contains(side, `data-chatremove-open="/api/chat/moderation/page?locale=`+locale+`"`) || !regexp.MustCompile(`data-moderation-count="true"[^>]*>[^<]*[3٣]|>[^<]*[3٣][^<]*</span>`).MatchString(side) {
			t.Errorf("%s: the sidebar's moderation entry: %q", locale, side)
		}
		if strings.Count(mod, "action=restore") != 2 {
			t.Errorf("%s: want Restore on both removed messages, got %d", locale, strings.Count(mod, "action=restore"))
		}
	}
}

// chatmodAt is a time every fixture of the Moderation page shares.
var chatmodAt = time.Date(2026, 10, 1, 15, 4, 0, 0, time.UTC)

func chatmodQueue() []chat.ModerationItem {
	before := []chat.Post{{ID: "p0", AuthorID: "mia-internal", Body: "the line before", CreatedAt: chatmodAt.Add(-time.Minute)}}
	return []chat.ModerationItem{
		{ID: "report:r1", Kind: "report", State: "OPEN", ConversationID: "general", ConversationName: "general", PostID: "p1", AuthorID: "jake-internal", ReporterID: "mia-internal", At: chatmodAt, Reason: "harassment — this was aimed at me", Message: chat.Post{ID: "p1", Body: "the reported words", CreatedAt: chatmodAt.Add(-2 * time.Minute)}, Context: append(append([]chat.Post{}, before...), chat.Post{ID: "p1", AuthorID: "jake-internal", Body: "the reported words"}), CanRemove: true},
		{ID: "filter:7", Kind: "filter", State: "OPEN", ConversationID: "general", ConversationName: "general", PostID: "p2", AuthorID: "jake-internal", At: chatmodAt.Add(time.Minute), Rule: "Project Falcon 1.2.0", Reason: "Project Falcon", Message: chat.Post{ID: "p2", Body: "flagged words", CreatedAt: chatmodAt}, CanRemove: true},
		{ID: "report:appeal:p3:2", Kind: "appeal", State: "OPEN", ConversationID: "general", ConversationName: "general", PostID: "p3", AuthorID: "jake-internal", ReporterID: "jake-internal", At: chatmodAt.Add(2 * time.Minute), Reason: "appeal", Message: chat.Post{ID: "p3", Body: "the removed words", Deleted: true, CreatedAt: chatmodAt}, CanRestore: true},
	}
}

func chatmodResolved() []chat.ModerationItem {
	return []chat.ModerationItem{
		{ID: "report:r0", Kind: "report", State: "CLOSED", ConversationID: "general", ConversationName: "general", PostID: "p5", AuthorID: "jake-internal", ReporterID: "mia-internal", At: chatmodAt, Reason: "spam", Message: chat.Post{ID: "p5", Body: "the old words", CreatedAt: chatmodAt}, Decision: "dismiss", DecidedBy: "walt-internal", DecidedAt: chatmodAt.Add(time.Hour), DecisionReason: "no_action"},
		{ID: "removal:p6", Kind: "removal", State: "CLOSED", ConversationID: "general", ConversationName: "general", PostID: "p6", AuthorID: "jake-internal", At: chatmodAt, Reason: "spam", Message: chat.Post{ID: "p6", Body: "the removed words", Deleted: true, CreatedAt: chatmodAt}, DecidedBy: "walt-internal", Decision: "remove", DecidedAt: chatmodAt, CanRestore: true},
	}
}

// TestTodo_CHATMOD_005_Browser_RealCatalog renders the Moderation page and its
// dialogs in the three languages: empty, with one item of each kind, resolved,
// and for a person who only has notices. Every word comes from the feature's own
// table: no marker, no placeholder, no internal identifier; the page is a page
// with a heading row and the close control, not a sheet over the header.
func TestTodo_CHATMOD_005_Browser_RealCatalog(t *testing.T) {
	names := map[string]string{"jake-internal": "Jake Sullivan", "mia-internal": "Mia Member", "walt-internal": "Walt Brennan"}
	render := func(m chatui.ModerationPageModel) string {
		t.Helper()
		m.Names, m.TimeZone = names, time.UTC
		out, err := ui.RenderToString(chatui.ModerationPage(m))
		if err != nil {
			t.Fatal(err)
		}
		return html.UnescapeString(out)
	}
	clean := func(locale, what, page string) {
		t.Helper()
		visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(page, " ")
		for _, bad := range []string{"⟦", "{n}", "{name}", "{when}", "{reason}", "-internal"} {
			if strings.Contains(visible, bad) {
				t.Errorf("%s %s: the page shows %q", locale, what, bad)
			}
		}
		for _, bad := range []string{"<style", " style="} {
			if strings.Contains(page, bad) {
				t.Errorf("%s %s: the page carries %q", locale, what, bad)
			}
		}
		closeLabel := chatui.ModerationText(locale, "close_moderation")
		if !strings.Contains(page, `aria-label="`+closeLabel+`"`) || !strings.Contains(page, `data-chatremove-close="true"`) || !strings.Contains(page, `class="side-heading chat-panel-head chatmod005-heading"`) || !strings.Contains(page, `<h2 id="chatremove-title"`) {
			t.Errorf("%s %s: no heading row with the close control %q", locale, what, closeLabel)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		text := func(key string) string { return chatui.ModerationText(locale, key) }
		// Empty: one line and a muted second sentence, no count, no search.
		empty := render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady})
		clean(locale, "empty", empty)
		if !strings.Contains(empty, text("nothing")) || !strings.Contains(empty, text("nothing_hint")) || strings.Contains(empty, `data-chatremove="filter"`) {
			t.Errorf("%s: the empty queue: %s", locale, empty)
		}
		if strings.Count(empty, text("nothing")) != 1 {
			t.Errorf("%s: the empty state says it twice", locale)
		}
		if !strings.Contains(empty, text("tab_open")) || !strings.Contains(empty, text("tab_resolved")) || !strings.Contains(empty, `class="chatsave-seg chatmod005-tabs"`) {
			t.Errorf("%s: the tabs: %s", locale, empty)
		}

		// One of each kind, open.
		open := render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, Items: chatmodQueue(), OpenCount: 3})
		clean(locale, "open", open)
		for _, want := range []string{
			text("tab_open") + `</span><span class="chatsave-seg-count">` + map[bool]string{true: "٣", false: "3"}[locale == "ar"] + `</span>`, text("kind_report"), text("filter"), text("appeal_item"), text("rule") + ": Project Falcon 1.2.0",
			strings.ReplaceAll(strings.ReplaceAll(text("reported_line"), "{name}", "Mia Member"), "{when}", ""),
			strings.ReplaceAll(strings.ReplaceAll(text("appeal_line"), "{name}", "Jake Sullivan"), "{when}", ""),
			"Jake Sullivan", "the reported words", "flagged words", "the removed words", "#general", text("show_context"), text("removed_badge"),
			text("remove_short"), text("dismiss"), text("message_author"), text("restore"), `data-chatremove="filter"`, text("search_placeholder"),
		} {
			if !strings.Contains(open, want) {
				t.Errorf("%s: the open queue misses %q", locale, want)
			}
		}
		// The message is the conversation's own rendering, not another.
		if strings.Count(open, `class="message`) < 3 || !strings.Contains(open, `message-author`) || !strings.Contains(open, `class="message-body"`) {
			t.Errorf("%s: the message is not drawn as the conversation draws it", locale)
		}
		// Primary action first: Remove leads a report and a flagged message, Restore an appeal.
		articles := strings.Split(open, `<article class="chatmod005-item"`)[1:]
		if len(articles) != 3 {
			t.Fatalf("%s: %d items", locale, len(articles))
		}
		first := func(article string) string {
			row := regexp.MustCompile(`(?s)<div class="chatmod005-actions".*?</div>`).FindString(article)
			m := regexp.MustCompile(`class="chatmod005-action(?: primary)?"[^>]*>([^<]*)<`).FindStringSubmatch(row)
			if m == nil {
				return ""
			}
			return m[1]
		}
		if first(articles[0]) != text("remove_short") || first(articles[1]) != text("remove_short") || first(articles[2]) != text("restore") {
			t.Errorf("%s: the primary action of each kind: %q %q %q", locale, first(articles[0]), first(articles[1]), first(articles[2]))
		}
		for _, article := range articles {
			if strings.Count(article, `<div class="chatmod005-actions"`) != 1 || strings.Contains(article, `<select`) || strings.Contains(article, `<textarea`) {
				t.Errorf("%s: an item is not one row of buttons: %s", locale, article)
			}
		}
		// Dismiss and Restore happen at once; Remove and Message the author ask in a dialog.
		if !strings.Contains(open, `data-chatremove-act="dismiss"`) || !strings.Contains(open, `data-chatremove-act="restore"`) || !strings.Contains(open, `action=remove`) || !strings.Contains(open, `action=message`) || !strings.Contains(open, `case=filter%3A7`) {
			t.Errorf("%s: the buttons are not wired", locale)
		}

		// Resolved: how each item was closed, and Restore on a removal.
		resolved := render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, Tab: "resolved", Items: chatmodResolved(), OpenCount: 2})
		clean(locale, "resolved", resolved)
		for _, want := range []string{strings.ReplaceAll(strings.ReplaceAll(text("decided_dismiss"), "{name}", "Walt Brennan"), "{when}", ""), strings.ReplaceAll(strings.ReplaceAll(text("removal_line"), "{name}", "Walt Brennan"), "{when}", ""), text("restore")} {
			if !strings.Contains(resolved, want) {
				t.Errorf("%s: the resolved list misses %q", locale, want)
			}
		}
		if strings.Contains(resolved, `data-chatremove-act="dismiss"`) || strings.Contains(resolved, text("message_author")) {
			t.Errorf("%s: a resolved item offers a decision again", locale)
		}
		if !regexp.MustCompile(`aria-selected="true"[^>]*><span>` + regexp.QuoteMeta(text("tab_resolved"))).MatchString(resolved) {
			t.Errorf("%s: Resolved is not the selected tab: %s", locale, regexp.MustCompile(`(?s)<div class="chatsave-seg chatmod005-tabs".*?</div>`).FindString(resolved))
		}
		emptyResolved := render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, Tab: "resolved"})
		if !strings.Contains(emptyResolved, text("nothing_resolved")) || !strings.Contains(emptyResolved, text("nothing_resolved_hint")) {
			t.Errorf("%s: the empty resolved list", locale)
		}

		// A person with no moderation permission: notices, no queue, no tabs.
		notices := []chat.ModerationNotice{{ID: "remove:p9:1", ConversationID: "general", PostID: "p9", Reason: "spam", Outcome: "remove", CanAppeal: true, At: chatmodAt}, {ID: "outcome:report:r0", ConversationID: "general", PostID: "p8", Reason: "no_action", Outcome: "dismiss", At: chatmodAt}, {ID: "message:filter:1", ConversationID: "general", PostID: "p7", Reason: "Please keep client names out.", Outcome: "message_author", At: chatmodAt}}
		plain := render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, NoQueue: true, Notices: notices})
		clean(locale, "notices", plain)
		for _, want := range []string{text("notices_title"), text("notice"), text("from_moderator"), text("outcome"), text("appeal"), `data-chatremove="appeal"`, text("no_action")} {
			if !strings.Contains(plain, want) {
				t.Errorf("%s: the notices page misses %q", locale, want)
			}
		}
		if strings.Contains(plain, `chatmod005-tabs`) || strings.Contains(plain, `data-chatremove="filter"`) || strings.Contains(plain, `chatmod005-actions`) {
			t.Errorf("%s: a person with no permission sees queue controls", locale)
		}
		if !strings.Contains(render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, NoQueue: true}), text("notices_empty")) {
			t.Errorf("%s: the empty notices page says nothing", locale)
		}
		for _, state := range []chatui.LoadState{chatui.StateLoading, chatui.StateError} {
			key := "loading"
			if state == chatui.StateError {
				key = "error"
			}
			if page := render(chatui.ModerationPageModel{Locale: locale, State: state}); !strings.Contains(page, text(key)) || strings.Contains(page, "⟦") {
				t.Errorf("%s: the %s page: %s", locale, key, page)
			}
		}

		// The dialogs: removal names the author and shows the message; the report
		// is private; restore explains itself; a queue item's dialogs decide that
		// item; Message the author needs words.
		target := chatui.ModerationTargetView{AuthorID: "jake-internal", HomeTenantID: "t", AuthorName: "Jake Sullivan", Body: "the reported words"}
		for _, tc := range []struct {
			name   string
			model  chatui.ModerationDialogModel
			want   []string
			absent []string
			wire   []string
		}{
			{"remove", chatui.ModerationDialogModel{Action: "remove"}, []string{"remove_title", "remove_help", "why_removed", "note_for_author", "several", "several_help", "cancel"}, nil, []string{`data-chatremove="quick"`}},
			{"report", chatui.ModerationDialogModel{Action: "report", Report: true}, []string{"report", "report_private", "why_reported", "note", "submit"}, []string{"several"}, []string{`data-chatremove="report"`}},
			{"restore", chatui.ModerationDialogModel{Action: "restore"}, []string{"restore_title", "restore_help", "restore", "note_for_author"}, []string{"several", "why_removed"}, []string{`data-removal-action="restore"`}},
			{"queue remove", chatui.ModerationDialogModel{Action: "remove", CaseID: "report:r1"}, []string{"remove_title", "why_removed", "note_for_author", "remove"}, []string{"several"}, []string{`data-chatremove="resolve"`, `data-case-id="report:r1"`, `name="action"`, `value="remove"`}},
			{"queue message", chatui.ModerationDialogModel{Action: "message", CaseID: "filter:7"}, []string{"message_title", "message_help", "message_label", "send_message"}, []string{"several", "why_removed"}, []string{`data-chatremove="resolve"`, `value="message_author"`, `required`}},
		} {
			tc.model.Model = chatui.Model{Locale: locale}
			tc.model.Selection = chat.RemovalSelection{ConversationID: "general", PostIDs: []string{"p1"}}
			tc.model.Target = target
			dialog, err := ui.RenderToString(chatui.ModerationDialog(tc.model))
			if err != nil {
				t.Fatal(err)
			}
			dialog = html.UnescapeString(dialog)
			if strings.Contains(dialog, "⟦") || strings.Contains(dialog, "{name}") || strings.Contains(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(dialog, " "), "-internal") {
				t.Errorf("%s %s: the dialog prints a marker, a placeholder or an identifier", locale, tc.name)
			}
			for _, key := range tc.want {
				want := strings.ReplaceAll(chatui.ModerationText(locale, key), "{name}", "Jake Sullivan")
				if want == "" || !strings.Contains(dialog, want) {
					t.Errorf("%s %s: the dialog does not show %q (%q)", locale, tc.name, key, want)
				}
			}
			for _, key := range tc.absent {
				if want := chatui.ModerationText(locale, key); want != "" && strings.Contains(dialog, want) {
					t.Errorf("%s %s: the dialog shows %q", locale, tc.name, key)
				}
			}
			for _, want := range tc.wire {
				if !strings.Contains(dialog, want) {
					t.Errorf("%s %s: the dialog misses %q", locale, tc.name, want)
				}
			}
			if tc.name != "restore" && (!strings.Contains(dialog, "Jake Sullivan") || !strings.Contains(dialog, "the reported words")) {
				t.Errorf("%s %s: the dialog does not name the author and show the message", locale, tc.name)
			}
			wantRadios := 4
			switch tc.name {
			case "restore", "queue message":
				wantRadios = 0
			case "remove":
				wantRadios = 8
			}
			if got := strings.Count(dialog, `type="radio"`); got != wantRadios {
				t.Errorf("%s %s: %d reasons, want %d", locale, tc.name, got, wantRadios)
			}
		}

		// The sidebar entry: no number at all while nothing is open.
		quiet := chatmodPage(t, locale, "olive", chatui.ModerationState{Ready: true, Moderator: true, Removable: map[string]bool{"general": true}})
		if side := regexp.MustCompile(`(?s)<div class="chatmod005-sidebar".*?</div>`).FindString(quiet); !strings.Contains(side, `hidden`) || strings.Contains(side, "⟦") {
			t.Errorf("%s: the sidebar shows a count with nothing open: %q", locale, side)
		}
	}
}
