package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatbug082Page renders the whole Chat page, with the product's own catalog,
// for an archived channel that holds a purpose, a to-do list and a poll. The
// model goes through WithReadOnlyChannelWidgets, as the client's does.
func chatbug082Page(t *testing.T, locale string, mayRestore bool, change func(*chatui.Model)) (string, productui.LocaleContext) {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	view := chatui.ChannelStatusView{Status: chat.ChannelStatus{ConversationID: "room", Name: "scratch", Status: chatpolicy.StatusArchived, Reason: "No longer needed"}}
	if mayRestore {
		view.Transitions = []chat.StatusTransition{{Status: chatpolicy.StatusOpen}}
	}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t",
		Text:           func(key string) string { return ctx.Text(key) },
		Conversations:  []chatui.Conversation{{ID: "room", Name: "scratch", Kind: chatui.PublicChannel, Joined: true, MemberCount: 2}},
		Messages:       []chatui.Message{{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", SentAt: at, Body: "Last message before the archive", Revision: 1}},
		ChatFeatures:   &chatui.ChatFeatures{Status: true},
		ChannelTeam:    chatui.ChannelTeamWidget{Revision: 3, Purpose: "Scratch space for QA", CanPin: true},
		ChannelProject: chatui.ChannelProjectWidget{Revision: 2, CanPin: true},
		ChannelPoll:    chatui.ChannelPoll{Revision: 4, Question: "Lunch spot?", Options: []chatui.ChannelPollOption{{ID: "a", Text: "Tacos"}, {ID: "b", Text: "Pho"}}},
		ChannelTodo: chatui.ChannelTodoList{Revision: 3, Items: []chatui.ChannelTodoItem{
			{ID: "open", Text: "Order pizza", CanToggle: true, CompletionMode: "EVERYONE"},
			{ID: "done", Text: "Book the room", Completed: true, CanToggle: true, CompletionMode: "EVERYONE"},
		}},
		ChannelStatuses:     map[string]chatui.ChannelStatusView{"room": view},
		ChangeChannelStatus: func(chat.ChangeChannelStatusRequest) {},
		ShowDetails:         true,
	}
	m.Callbacks.SendMessage = func(string, string) {}
	m.Callbacks.ToggleDetails = func(bool) {}
	m.Callbacks.AddChannelTodo = func(string, string) {}
	m.Callbacks.SetChannelTodoCompleted = func(string, bool) {}
	m.Callbacks.DeleteChannelTodo = func(string) {}
	m.Callbacks.VoteChannelPoll = func(string) {}
	m.Callbacks.SetChannelTeamPurpose = func(string) {}
	m.Callbacks.RetryChannelWidgets = func() {}
	if change != nil {
		change(&m)
	}
	rendered, err := ui.RenderToString(chatui.Build(chatui.WithReadOnlyChannelWidgets(m)))
	if err != nil {
		t.Fatal(err)
	}
	page := html.UnescapeString(rendered)
	if strings.Contains(page, "⟦") {
		t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
	}
	return page, ctx
}

// TestTodo_CHATBUG_082_Browser draws an archived channel as a person opens it,
// in the three languages: the purpose, the to-do list and the poll are there
// and read-only, nothing says the widgets could not be loaded, and the notice
// in place of the composer offers Restore to a person who may restore and only
// says who can to a person who may not.
func TestTodo_CHATBUG_082_Browser(t *testing.T) {
	for locale, want := range map[string]struct{ notice, restore string }{
		"en-US": {"This channel is archived. You can read and search; a workspace administrator can restore it.", "Restore"},
		"de-DE": {"Dieser Kanal ist archiviert. Sie können lesen und suchen; die Arbeitsbereichsadministration kann ihn wiederherstellen.", "Wiederherstellen"},
		"ar":    {"هذه القناة مؤرشفة. يمكنك القراءة والبحث؛ يمكن لمسؤول مساحة العمل استعادتها.", "استعادة"},
	} {
		page, ctx := chatbug082Page(t, locale, true, nil)

		// The chips under the header summarise the list and the poll.
		for _, chip := range []string{`data-action="tray-todo"`, `data-action="tray-poll"`} {
			if !strings.Contains(page, chip) {
				t.Errorf("%s: the archived channel lost its %s chip", locale, chip)
			}
		}
		chips := regexp.MustCompile(`<button[^>]*class="tray-chip[^"]*"[^>]*>.*?</button>`).FindAllString(page, -1)
		if len(chips) != 2 || !strings.Contains(chips[0], `data-action="tray-todo"`) || !strings.Contains(chips[1], "Lunch spot?") {
			t.Errorf("%s: the chips do not summarise the list and the poll: %v", locale, chips)
		}
		// The purpose is read from the channel, not "Not set".
		if !strings.Contains(page, "Scratch space for QA") {
			t.Errorf("%s: the archived channel lost its purpose", locale)
		}
		// Read-only: the page offers no way to add to the channel (no composer,
		// so no Add menu with its poll and to-do items), and the purpose in
		// details cannot be edited. What the opened cards hold, each control
		// off, is TestTodo_CHATBUG_082.
		for _, adds := range []string{`data-action="composer-add"`, `data-action="purpose-edit"`} {
			tags := regexp.MustCompile(`<button[^>]*`+regexp.QuoteMeta(adds)+`[^>]*>`).FindAllString(page, -1)
			for _, tag := range tags {
				if !strings.Contains(tag, " disabled") {
					t.Errorf("%s: an archived channel offers %s: %s", locale, adds, tag)
				}
			}
		}
		// Nothing failed, and nothing says it did.
		for _, failure := range []string{"Could not save or load channel widgets", ctx.Text(chatui.KeyWidgetError), `data-action="widget-retry"`} {
			if failure != "" && strings.Contains(page, failure) {
				t.Errorf("%s: an archived channel shows a widget failure (%q)", locale, failure)
			}
		}
		// The notice stands where the composer was, with Restore.
		if strings.Contains(page, `id="chat-composer"`) {
			t.Errorf("%s: an archived channel still offers the composer", locale)
		}
		bar := regexp.MustCompile(`<div[^>]*class="chatstate-notice-bar"[^>]*>.*?</div>`).FindString(page)
		if !strings.Contains(bar, want.notice) || !strings.Contains(bar, `role="status"`) {
			t.Errorf("%s: the archive notice does not read %q: %s", locale, want.notice, bar)
		}
		button := regexp.MustCompile(`<button[^>]*data-action="channel-restore"[^>]*>([^<]*)</button>`).FindStringSubmatch(bar)
		if button == nil || button[1] != want.restore || !strings.Contains(button[0], `data-id="room"`) || strings.Contains(button[0], " disabled") {
			t.Errorf("%s: the notice has no %q button for a person who may restore: %s", locale, want.restore, bar)
		}

		// A person who may not restore reads the same sentence and is offered nothing.
		plain, _ := chatbug082Page(t, locale, false, nil)
		if !strings.Contains(plain, want.notice) || strings.Contains(plain, `data-action="channel-restore"`) || strings.Contains(plain, "chatstate-notice-bar") {
			t.Errorf("%s: a person who may not restore is offered Restore, or lost the notice", locale)
		}
		// So does a person on a page that has no way to send the change.
		unsendable, _ := chatbug082Page(t, locale, true, func(m *chatui.Model) { m.ChangeChannelStatus = nil })
		if strings.Contains(unsendable, `data-action="channel-restore"`) {
			t.Errorf("%s: Restore is offered with no way to send it", locale)
		}

		// A read that really fails is said once: in Conversation details while
		// that is open, above the messages while it is closed. Never in both.
		inDetails, _ := chatbug082Page(t, locale, true, func(m *chatui.Model) { m.ChannelWidgetsError = "load" })
		said := regexp.MustCompile(`<p[^>]*role="alert"[^>]*>([^<]+)</p>`).FindStringSubmatch(inDetails)
		if said == nil {
			t.Fatalf("%s: a failed widget read is not said in Conversation details", locale)
		}
		for name, change := range map[string]func(*chatui.Model){
			"details open":   func(m *chatui.Model) { m.ChannelWidgetsError = "load" },
			"details closed": func(m *chatui.Model) { m.ChannelWidgetsError, m.ShowDetails = "load", false },
		} {
			failed, _ := chatbug082Page(t, locale, true, change)
			if got := strings.Count(failed, `data-action="widget-retry"`); got != 1 {
				t.Errorf("%s, %s: a failed widget read offers Try again %d times, want once", locale, name, got)
			}
			if got := strings.Count(failed, ">"+said[1]+"<"); got != 1 {
				t.Errorf("%s, %s: %q stands on the page %d times, want once", locale, name, said[1], got)
			}
		}
		if locale == "ar" && !strings.Contains(page, `dir="rtl"`) {
			t.Error("ar: the page is not right-to-left")
		}
	}
}
