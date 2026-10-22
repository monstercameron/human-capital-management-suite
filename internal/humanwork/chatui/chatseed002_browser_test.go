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
)

// chatseedItem is one queue item as the demo seed (cmd/migrate/chat_seed_moderation.go)
// leaves it: who raised it, who wrote the message, which conversation, why, and for
// a resolved one how it was closed.
func chatseedItems(at time.Time) (open, resolved []chat.ModerationItem) {
	message := func(id, body string, deleted bool) chat.Post {
		return chat.Post{ID: id, Body: body, Deleted: deleted, CreatedAt: at.Add(-3 * time.Hour)}
	}
	in := func(id, name string) chat.ModerationItem {
		return chat.ModerationItem{ConversationID: "room-" + name, ConversationName: name, PostID: id}
	}
	item := func(base chat.ModerationItem, fill func(*chat.ModerationItem)) chat.ModerationItem {
		fill(&base)
		return base
	}
	open = []chat.ModerationItem{
		item(in("p1", "people-ops"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.ReporterID, i.At = "report:r1", "report", "OPEN", "ir-014-jordan-hale", "ir-022-sam-ortiz", at
			i.Reason = "harassment — This was aimed at one colleague in front of the whole channel, and it is not the first time."
			i.Message, i.CanRemove = message("p1", "Honestly, if you had read the policy document before posting you would not need to ask this every week.", false), true
		}),
		item(in("p2", "general"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.ReporterID, i.At = "report:s1", "report", "OPEN", "ir-031-casey-nguyen", "ir-040-lee-park", at.Add(time.Minute)
			i.Reason, i.Message, i.CanRemove = "spam", message("p2", "FINAL NOTICE: you have been selected for a free gift card.", false), true
		}),
		item(in("p2", "general"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.ReporterID, i.At = "report:s2", "report", "OPEN", "ir-031-casey-nguyen", "ir-043-ana-reyes", at.Add(2*time.Minute)
			i.Reason, i.Message, i.CanRemove = "spam — Looks like a scam link, please take it down.", message("p2", "FINAL NOTICE: you have been selected for a free gift card.", false), true
		}),
		item(in("p3", "engineering"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.At = "filter:11", "filter", "OPEN", "ir-018-dana-brooks", at.Add(3*time.Minute)
			i.Rule, i.Reason = "profanity 1.1.0", "profanity"
			i.Message, i.CanRemove = message("p3", "The damn staging deploy failed again overnight.", false), true
		}),
		item(in("p4", "leadership-private"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.ReporterID, i.At = "report:r5", "report", "OPEN", "ir-009-kim-adler", "ir-012-ira-shah", at.Add(4*time.Minute)
			i.Reason = "sensitive_information — Individual pay figures should not be shared outside HR."
			i.Message, i.CanRemove = message("p4", "The draft bonus figures for each team are in the shared planning sheet.", false), true
		}),
	}
	resolved = []chat.ModerationItem{
		item(in("p5", "people-ops"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.ReporterID, i.At = "report:r6", "report", "CLOSED", "ir-014-jordan-hale", "ir-022-sam-ortiz", at
			i.Reason, i.Message = "harassment — It felt like a dig at the team that wrote it.", message("p5", "The new onboarding checklist is a mess.", false)
			i.Decision, i.DecidedBy, i.DecidedAt, i.DecisionReason = "dismiss", "ir-001-walt-brennan", at.Add(20*time.Hour), "Blunt feedback about a document, not directed at a person. No action needed."
		}),
		item(in("p6", "general"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.At = "removal:p6", "removal", "CLOSED", "ir-027-mo-diaz", at.Add(-time.Hour)
			i.Reason = "sensitive_information — A personal phone number was shared in a public channel."
			i.Message = message("p6", "If the benefits portal keeps timing out, call the carrier's rep directly.", true)
			i.Decision, i.DecidedBy, i.DecidedAt, i.CanRestore = "remove", "ir-001-walt-brennan", at.Add(-time.Hour), true
		}),
		item(in("p7", "engineering"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.ReporterID, i.At = "report:appeal:p7:2", "appeal", "CLOSED", "ir-035-eli-frost", "ir-035-eli-frost", at.Add(-2*time.Hour)
			i.Reason, i.Message = "appeal", message("p7", "That vendor demo wasted a whole afternoon.", false)
			i.Decision, i.DecidedBy, i.DecidedAt, i.DecisionReason = "restore", "ir-001-walt-brennan", at.Add(-90*time.Minute), "On review this was frustration about the schedule and was not aimed at a person. Restored."
		}),
		item(in("p7", "engineering"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.At = "removal:p7", "removal", "CLOSED", "ir-035-eli-frost", at.Add(-3*time.Hour)
			i.Reason, i.Message = "policy_violation — Reported as hostile toward a colleague.", message("p7", "That vendor demo wasted a whole afternoon.", false)
			i.Decision, i.DecidedBy, i.DecidedAt = "restore", "ir-001-walt-brennan", at.Add(-90*time.Minute)
		}),
		item(in("p8", "engineering"), func(i *chat.ModerationItem) {
			i.ID, i.Kind, i.State, i.AuthorID, i.At = "filter:7", "filter", "CLOSED", "ir-044-pat-cole", at.Add(-4*time.Hour)
			i.Rule, i.Reason, i.Message = "profanity 1.1.0", "profanity", message("p8", "This crap build cache is finally fixed.", false)
			i.Decision, i.DecidedBy, i.DecidedAt, i.DecisionReason = "dismiss", "ir-001-walt-brennan", at.Add(-time.Hour), "A casual remark that was not aimed at anyone. Released."
		}),
	}
	return open, resolved
}

// TestTodo_CHATSEED_002_Browser renders the Moderation page with what the demo
// seed leaves in it, in the three languages the product ships: each kind of open
// item with the reporter, author, conversation and rule or reason, each kind of
// resolved item with how it was closed, and the notice a workspace filter sends.
// No marker, placeholder or internal identifier may show.
func TestTodo_CHATSEED_002_Browser(t *testing.T) {
	at := time.Date(2026, 10, 1, 16, 5, 0, 0, time.UTC)
	names := map[string]string{
		"ir-014-jordan-hale": "Jordan Hale", "ir-022-sam-ortiz": "Sam Ortiz", "ir-031-casey-nguyen": "Casey Nguyen", "ir-040-lee-park": "Lee Park",
		"ir-043-ana-reyes": "Ana Reyes", "ir-018-dana-brooks": "Dana Brooks", "ir-009-kim-adler": "Kim Adler", "ir-012-ira-shah": "Ira Shah",
		"ir-001-walt-brennan": "Walt Brennan", "ir-027-mo-diaz": "Mo Diaz", "ir-035-eli-frost": "Eli Frost", "ir-044-pat-cole": "Pat Cole",
	}
	open, resolved := chatseedItems(at)
	render := func(m chatui.ModerationPageModel) string {
		t.Helper()
		m.Names, m.TimeZone = names, time.UTC
		out, err := ui.RenderToString(chatui.ModerationPage(m))
		if err != nil {
			t.Fatal(err)
		}
		return html.UnescapeString(out)
	}
	digits := func(locale, n string) string {
		if locale != "ar" {
			return n
		}
		return strings.NewReplacer("0", "٠", "1", "١", "2", "٢", "3", "٣", "4", "٤", "5", "٥", "6", "٦", "7", "٧", "8", "٨", "9", "٩").Replace(n)
	}
	articles := func(page string) []string {
		return strings.Split(page, `<article class="chatmod005-item"`)[1:]
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		text := func(key string) string { return chatui.ModerationText(locale, key) }
		fill := func(key, name string) string {
			return strings.ReplaceAll(strings.ReplaceAll(text(key), "{name}", name), "{when}", "")
		}
		clean := func(what, page string) {
			t.Helper()
			visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(page, " ")
			for _, bad := range []string{"⟦", "{n}", "{name}", "{when}", "{reason}", "ir-0", "report:", "filter:", "removal:"} {
				if strings.Contains(visible, bad) {
					t.Errorf("%s %s: the page shows %q", locale, what, bad)
				}
			}
		}

		// Open: five items, one harassment report with its note, two spam reports of
		// one message by two people, a message held by a word list, a private channel.
		page := render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, Items: open, OpenCount: 5, ResolvedCount: 5, ResolvedKnown: true,
			Notices: []chat.ModerationNotice{{ID: "filter:seed-client-codenames:abc:1", ConversationID: "room-engineering", Reason: "Client code names", Outcome: chat.ModerationOutcomeFilterNotify, At: at}}})
		clean("open", page)
		if got := len(articles(page)); got != 5 {
			t.Fatalf("%s: %d open items, want 5", locale, got)
		}
		for _, want := range []string{
			text("tab_open") + `</span><span class="chatsave-seg-count">` + digits(locale, "5") + `</span>`,
			text("tab_resolved") + `</span><span class="chatsave-seg-count">` + digits(locale, "5") + `</span>`,
			text("kind_report"), text("filter"), text("rule") + ": profanity 1.1.0",
			text("reason") + ": " + text("harassment") + " — This was aimed at one colleague in front of the whole channel, and it is not the first time.",
			text("reason") + ": " + text("spam") + " — Looks like a scam link, please take it down.",
			text("reason") + ": " + text("sensitive_information") + " — Individual pay figures should not be shared outside HR.",
			fill("reported_line", "Sam Ortiz"), fill("reported_line", "Lee Park"), fill("reported_line", "Ana Reyes"), fill("reported_line", "Ira Shah"), strings.ReplaceAll(text("flag_line"), "{when}", ""),
			"Jordan Hale", "Casey Nguyen", "Dana Brooks", "Kim Adler",
			"#people-ops", "#general", "#engineering", "#leadership-private",
			text("remove_short"), text("dismiss"), text("message_author"),
			text("filter_notice"), strings.ReplaceAll(text("filter_notice_rule"), "{name}", "Client code names"),
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: the open queue misses %q", locale, want)
			}
		}
		// The two spam reports show the one message twice, each naming its reporter.
		if strings.Count(page, "FINAL NOTICE") != 2 {
			t.Errorf("%s: the spam reports show the message %d times", locale, strings.Count(page, "FINAL NOTICE"))
		}
		for _, a := range articles(page) {
			if strings.Count(a, `<div class="chatmod005-actions"`) != 1 {
				t.Errorf("%s: an open item is not one row of buttons: %s", locale, a)
			}
		}

		// Resolved: how each was closed, by whom, with the reason given.
		done := render(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, Tab: "resolved", Items: resolved, OpenCount: 5, ResolvedCount: 5, ResolvedKnown: true})
		clean("resolved", done)
		if got := len(articles(done)); got != 5 {
			t.Fatalf("%s: %d resolved items, want 5", locale, got)
		}
		for _, want := range []string{
			fill("decided_dismiss", "Walt Brennan"), " — Blunt feedback about a document, not directed at a person. No action needed.",
			" — A casual remark that was not aimed at anyone. Released.",
			fill("removal_line", "Walt Brennan"), text("removed_badge"),
			text("reason") + ": " + text("sensitive_information") + " — A personal phone number was shared in a public channel.",
			fill("decided_restore", "Walt Brennan"), " — On review this was frustration about the schedule and was not aimed at a person. Restored.",
			fill("appeal_line", "Eli Frost"), text("appeal_item"),
			text("rule") + ": profanity 1.1.0",
			"Mo Diaz", "Eli Frost", "Pat Cole",
		} {
			if !strings.Contains(done, want) {
				t.Errorf("%s: the resolved list misses %q", locale, want)
			}
		}
		// Only the removed message can be brought back; nothing decided is decided again.
		if strings.Count(done, `data-chatremove-act="restore"`) != 1 || strings.Contains(done, `data-chatremove-act="dismiss"`) || strings.Contains(done, text("message_author")) {
			t.Errorf("%s: the resolved list offers the wrong buttons: restore x%d", locale, strings.Count(done, `data-chatremove-act="restore"`))
		}
		if strings.Count(done, `chatmod005-removed">`+text("removed_badge")+`<`) != 1 {
			t.Errorf("%s: the removed badge shows %d times, want once", locale, strings.Count(done, `chatmod005-removed">`+text("removed_badge")+`<`))
		}
	}
}
