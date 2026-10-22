package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatux048Markup(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_CHATUX_048: the administrator rows are the preferences rows (label,
// help under it, a switch or a segmented control, the saved tick at the end),
// Remove is the danger style and never the filled one, the popover stays above
// the Archived band, and a removed message says what its reader may know.
func TestTodo_CHATUX_048(t *testing.T) {
	admin := chattoneRewordMember
	admin.CanAdmin = true
	admin.ChoiceAllowed = false
	admin.Workspace = ChattoneRewordSetting{Mode: "offered", MembersMayViewOriginal: true}
	admin.Channel = ChattoneRewordSetting{Mode: "on", MembersMayViewOriginal: false}

	// Each setting is a row of the same component as the rows above it, with its
	// question written out and no bare Yes or No.
	got := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin})
	if n := strings.Count(got, "chat-prefs-head chattone-reword-head"); n != 4 {
		t.Errorf("want four rows built on the preferences row, got %d: %s", n, got)
	}
	for _, want := range []string{"Reword heated messages", "Members may read messages as written", "Offered to writers", "Whole workspace", "This channel only"} {
		if strings.Count(got, want) < 1 {
			t.Errorf("administrator rows lack %q", want)
		}
	}
	if strings.Count(got, "Reword heated messages") != 2 || strings.Count(got, "Members may read messages as written") != 2 {
		t.Errorf("each scope asks each question once: %s", got)
	}
	for _, bare := range []string{">Yes<", ">No<"} {
		if strings.Contains(got, bare) {
			t.Errorf("a bare %s answers a question the row does not ask", bare)
		}
	}
	// The workspace allows reading as written (switch on); the channel does not.
	switches := regexp.MustCompile(`<button[^>]*role="switch"[^>]*>`).FindAllString(got, -1)
	if len(switches) != 2 || !strings.Contains(switches[0], `aria-checked="true"`) || !strings.Contains(switches[1], `aria-checked="false"`) {
		t.Errorf("the switches do not show the two settings: %v", switches)
	}
	for _, s := range switches {
		if !strings.Contains(s, "aria-labelledby=") || !strings.Contains(s, "aria-describedby=") {
			t.Errorf("a switch is not named by its row: %s", s)
		}
	}
	// One line of help sits under each label, before the control that needs more room.
	if !regexp.MustCompile(`(?s)chattone-reword-workspace-mode-label.*?chattone-reword-help.*?chatsave-seg`).MatchString(got) {
		t.Errorf("the mode row is not label, help, segmented control: %s", got)
	}
	if strings.Contains(got, "button secondary chattone-reword-choice") {
		t.Errorf("the choices are still page buttons")
	}

	// The tick is beside the row that was saved, and only there.
	saved := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin, Status: "saved:workspace-view"})
	if strings.Count(saved, "chatux031-tick") != 1 {
		t.Errorf("want one tick, got %d", strings.Count(saved, "chatux031-tick"))
	}
	row := saved[strings.Index(saved, `data-reword-row="workspace-view"`):]
	if i := strings.Index(row, "chatux031-tick"); i < 0 || i > strings.Index(row, `data-reword-row="channel-mode"`) {
		t.Errorf("the tick is not in the workspace view row")
	}
	if strings.Contains(chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin, Status: "saved"}), "chatux031-tick") {
		t.Errorf("a save that names no row shows a tick")
	}

	// de-DE and ar read the same rows in their own words.
	for locale, want := range map[string]string{"de-DE": "Aufgeheizte Nachrichten umformulieren", "ar": "إعادة صياغة الرسائل المتوترة"} {
		page := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: locale, View: admin})
		if !strings.Contains(page, want) || strings.Contains(page, "Reword heated messages") || strings.Contains(page, "⟦") {
			t.Errorf("%s: %s", locale, page)
		}
	}

	// Moderation: Remove is the danger style, never filled; Dismiss the plain secondary.
	mod := ModerationPageModel{Locale: "en-US", State: StateReady}
	report := chat.ModerationItem{ID: "report:1", Kind: "report", State: "OPEN", ConversationID: "c", PostID: "p", CanRemove: true}
	actions := chatux048Markup(t, moderationItemActions(mod, report))
	remove := regexp.MustCompile(`<button[^>]*>Remove</button>`).FindString(actions)
	dismiss := regexp.MustCompile(`<button[^>]*>Dismiss</button>`).FindString(actions)
	if !strings.Contains(remove, "danger") || strings.Contains(remove, "primary") || !strings.Contains(remove, "secondary") {
		t.Errorf("Remove is not the unfilled danger button: %q", remove)
	}
	if dismiss == "" || strings.Contains(dismiss, "danger") || strings.Contains(dismiss, "primary") || !strings.Contains(dismiss, "button secondary") {
		t.Errorf("Dismiss is not the plain secondary button: %q", dismiss)
	}
	if !strings.Contains(ChatUX048Styles, ".chatmod005-action.danger{") || !strings.Contains(ChatUX048Styles, "var(--hcm-color-danger)") {
		t.Errorf("the danger style is not drawn from the danger token")
	}

	// The panel opening downward ends above the Archived band and scrolls inside.
	down := chatLayerPlacement{top: 126, maxHeight: 762}
	if p := chatux048AboveFooter(down, 868); p.maxHeight != 868-126-8 {
		t.Errorf("max height %v does not end above the footer", p.maxHeight)
	}
	if p := chatux048AboveFooter(down, 0); p != down {
		t.Errorf("a missing footer changed the placement")
	}
	if up := (chatLayerPlacement{up: true, maxHeight: 300}); chatux048AboveFooter(up, 100) != up {
		t.Errorf("an upward panel was clamped")
	}
	if p := chatux048AboveFooter(chatLayerPlacement{top: 126, maxHeight: 100}, 868); p.maxHeight != 100 {
		t.Errorf("a panel already short was made taller: %v", p.maxHeight)
	}

	// The styles use tokens, logical insets, and no literal duration or shadow.
	for _, bad := range []string{"box-shadow", "transition", "animation", "margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right", "rgb(", "#"} {
		if strings.Contains(ChatUX048Styles, bad) {
			t.Errorf("ChatUX048Styles contains %q", bad)
		}
	}
	if !strings.Contains(Stylesheet, ChatUX048Styles) {
		t.Errorf("the page's stylesheet does not carry ChatUX048Styles")
	}
}

func TestTodo_CHATUX_048_Removals(t *testing.T) {
	at := time.Date(2026, 10, 1, 15, 4, 0, 0, time.UTC)
	items := []chat.ModerationItem{
		{Kind: "removal", PostID: "a", RemovedBy: "walt", At: at.Add(-time.Hour), Reason: "spam"},
		{Kind: "removal", PostID: "a", RemovedBy: "olive", At: at, Reason: "harassment"},
		{Kind: "removal", PostID: "b", RemovedBy: "walt", At: at, RestoredBy: "olive", RestoredAt: at.Add(time.Minute)},
		{Kind: "report", PostID: "c", At: at},
		{Kind: "removal", PostID: "d", Decision: "remove", DecidedBy: "walt", At: at, Reason: "spam"},
	}
	got := ModerationRemovalsFromItems(items)
	if len(got) != 2 || got["a"].By != "olive" || got["a"].Reason != "harassment" || got["d"].By != "walt" {
		t.Errorf("removals: %+v", got)
	}

	// What each reader may know of one removal.
	msg := Message{ID: "a", AuthorID: "jake"}
	people := []Member{{ID: "walt", Name: "Walt Brennan"}, {ID: "jake", Name: "Jake Sullivan"}}
	record := map[string]ModerationRemoval{"a": {By: "walt", At: at, Reason: "harassment"}}
	reviewer := Model{Locale: "en-US", SelectedID: "general", CurrentUser: "olive", Members: people,
		Moderation: ModerationState{Ready: true, Reviewable: map[string]bool{"general": true}, Removals: record}}
	title, why := chatux048RemovedLines(reviewer, msg, true)
	if !strings.HasPrefix(title, "Removed by Walt Brennan · ") || why != "Reason: Harassment or bullying" {
		t.Errorf("a reviewer reads who, when and why: %q / %q", title, why)
	}

	// The same record in the hands of someone who may not review it says nothing more.
	member := reviewer
	member.CurrentUser = "mia"
	member.Moderation = ModerationState{Ready: true, Removals: record}
	if title, why = chatux048RemovedLines(member, msg, true); title != "Removed by an administrator" || why != "" {
		t.Errorf("a member reads what only reviewers may: %q / %q", title, why)
	}

	// The author reads when and why from their own notice, never who.
	author := member
	author.CurrentUser = "jake"
	author.Moderation = ModerationState{Ready: true, Removals: record, Notices: map[string]ModerationOwnNotice{"a": {PostID: "a", Reason: "harassment", At: at}}}
	title, why = chatux048RemovedLines(author, msg, true)
	if !strings.HasPrefix(title, "Removed by an administrator · ") || strings.Contains(title, "Walt") || why != "Reason: Harassment or bullying" {
		t.Errorf("the author reads when and why: %q / %q", title, why)
	}

	// A notice without a time still reads, and a message its author deleted says so.
	author.Moderation.Notices = map[string]ModerationOwnNotice{"a": {PostID: "a", Reason: "spam"}}
	if title, _ = chatux048RemovedLines(author, msg, true); title != "Removed by an administrator" {
		t.Errorf("no time known: %q", title)
	}
	if title, why = chatux048RemovedLines(author, msg, false); title != "Deleted by the author" || why != "" {
		t.Errorf("author-deleted: %q / %q", title, why)
	}

	// The row is the message grid with its notice in the text column.
	markup := chatux048Markup(t, chatux048Tombstone(reviewer, msg, true))
	if !strings.Contains(markup, `class="message-row chatremove-tombstone chatux048-removed"`) || !strings.Contains(markup, "chatux048-removed-body") ||
		!strings.Contains(markup, "button secondary small") || !strings.Contains(markup, "action=restore") {
		t.Errorf("tombstone markup: %s", markup)
	}
}
