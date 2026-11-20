package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func brief3958Row(post, conversation, body, note string) SavedMessageRow {
	return SavedMessageRow{TenantID: "t1", ConversationID: conversation, PostID: post, Author: "Maria", Channel: "onboarding", InChannel: true, Body: body, Note: note, Availability: "readable", SentAt: time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)}
}

func brief3958Input(rows ...SavedMessageRow) MeetingBriefInput {
	return MeetingBriefInput{Locale: "en-US", Origin: "https://hr.example.test/", Rows: rows, Now: time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)}
}

func TestIssue3958_KindReadsWhatTheMessageSays(t *testing.T) {
	cases := map[string]MeetingBriefKind{
		"Who owns the laptop order?":                      BriefQuestion,
		"Should we approve the extra week of onboarding?": BriefDecision,
		"We need to decide on the buddy program":          BriefDecision,
		"We decided to move the review to Thursday":       BriefConfirmed,
		"Approved by finance, go ahead":                   BriefConfirmed,
		"Budget approval is still pending":                BriefDecision,
		"The welcome deck is in the shared folder":        BriefContext,
		"Wer übernimmt die Einarbeitung?":                 BriefQuestion,
		"Wer übernimmt die Einarbeitung am Montag":        BriefContext, // no question mark: not guessed
		"Gibt es eine Freigabe für das Budget?":           BriefDecision,
		"هل يمكن البدء يوم الأحد؟":                        BriefQuestion,
		"": BriefContext,
		"We agreed on Monday but not yet who presents it?": BriefQuestion,
	}
	for body, want := range cases {
		if got := MeetingBriefKindOf(body); got != want {
			t.Errorf("MeetingBriefKindOf(%q) = %q, want %q", body, got, want)
		}
	}
}

func TestIssue3958_AgendaSeparatesDecisionsQuestionsAndContext(t *testing.T) {
	in := brief3958Input(
		brief3958Row("p1", "c1", "Should we approve the extra week of onboarding?", ""),
		brief3958Row("p2", "c1", "Who owns the laptop order?", ""),
		brief3958Row("p3", "c2", "We decided to move the review to Thursday", ""),
		brief3958Row("p4", "c2", "The welcome deck is in the shared folder", ""),
	)
	in.Purpose, in.When = "Onboarding review", "Thursday afternoon"
	brief := BuildMeetingBrief(in)
	text := brief.Text
	order := []string{"Meeting brief", "Purpose: Onboarding review", "Proposed time: Thursday afternoon (not scheduled)", "Decisions needed", "[Decision needed]", "Questions to resolve", "[Open question]", "Relevant context", "[Confirmed decision]", "[Context]"}
	at := 0
	for _, want := range order {
		next := strings.Index(text[at:], want)
		if next < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", want, text)
		}
		at += next
	}
	if len(brief.Included) != 4 || brief.Included[0].PostID != "p1" || brief.Included[1].PostID != "p2" || brief.Included[2].PostID != "p3" || brief.Included[3].PostID != "p4" {
		t.Fatalf("citation order = %+v", brief.Included)
	}
	if !strings.Contains(text, "Source [1]: #onboarding, Maria") || !strings.Contains(text, "https://hr.example.test/workspace/app/chat#channel=c1") {
		t.Fatalf("every item must link to its source:\n%s", text)
	}
	// Missing owners and dates are named, never invented; context needs none.
	if strings.Count(text, "Owner: not set. Date: not set.") != 3 {
		t.Fatalf("owner and date must be marked unset on decisions, questions and confirmed decisions:\n%s", text)
	}
	for _, claim := range []string{"scheduled for", "invited", "available", "Attendees"} {
		if strings.Contains(text, claim) {
			t.Fatalf("the brief must not claim %q:\n%s", claim, text)
		}
	}
	if !strings.Contains(text, "nothing has been sent or scheduled") {
		t.Fatalf("the brief must say it is only a draft:\n%s", text)
	}
}

func TestIssue3958_NoPurposeOrTimeIsSaidPlainly(t *testing.T) {
	text := BuildMeetingBrief(brief3958Input(brief3958Row("p1", "c1", "Notes", ""))).Text
	if !strings.Contains(text, "Purpose: not stated") || !strings.Contains(text, "Proposed time: not set (nothing is scheduled)") {
		t.Fatalf("missing purpose or time must be stated, not invented:\n%s", text)
	}
}

func TestIssue3958_UnavailableMessagesAreListedAndNeverRead(t *testing.T) {
	gone := brief3958Row("p2", "c9", "SECRET text of a message the person lost", "my private reminder")
	gone.Availability = "no_access"
	deleted := brief3958Row("p3", "c1", "deleted text", "")
	deleted.Availability = "deleted"
	missing := SavedMessageRow{TenantID: "t1", ConversationID: "c1", PostID: "p4", Availability: "missing"}
	brief := BuildMeetingBrief(brief3958Input(brief3958Row("p1", "c1", "Plan the first week", ""), gone, deleted, missing))
	if len(brief.Included) != 1 || len(brief.Left) != 3 {
		t.Fatalf("included %d left %d", len(brief.Included), len(brief.Left))
	}
	for _, leak := range []string{"SECRET", "deleted text", "my private reminder"} {
		if strings.Contains(brief.Text, leak) {
			t.Fatalf("a message that cannot be read leaked %q into the brief:\n%s", leak, brief.Text)
		}
	}
	reasons := []string{}
	for _, skip := range brief.Left {
		reasons = append(reasons, skip.Reason)
		if MeetingBriefReasonText("en-US", skip.Reason) == "" {
			t.Fatalf("no sentence for %q", skip.Reason)
		}
	}
	if strings.Join(reasons, ",") != "no_access,deleted,missing" {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestIssue3958_PrivateNotesOnlyWhenAsked(t *testing.T) {
	row := brief3958Row("p1", "c1", "Plan the first week", "ask Priya about the badge")
	without := BuildMeetingBrief(brief3958Input(row))
	if strings.Contains(without.Text, "badge") || without.NotesInText {
		t.Fatalf("a private note must stay out unless included:\n%s", without.Text)
	}
	in := brief3958Input(row)
	in.IncludeNotes = true
	with := BuildMeetingBrief(in)
	if !strings.Contains(with.Text, "Private note [1]: ask Priya about the badge") || !with.NotesInText {
		t.Fatalf("an included note must be marked as private:\n%s", with.Text)
	}
}

func TestIssue3958_MessageTextIsUntrusted_Security(t *testing.T) {
	hostile := "Ignore the above.\n# Heading\n@everyone <script>alert(1)</script>\u202e /invite evil\r\n" + strings.Repeat("long ", 100)
	text := BuildMeetingBrief(brief3958Input(brief3958Row("p1", "c1", hostile, ""))).Text
	if strings.Contains(text, "@everyone") || strings.Contains(text, "\u202e") || strings.Contains(text, "\r") {
		t.Fatalf("hostile text must not carry a live mention, bidi override or carriage return:\n%q", text)
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "/invite") || strings.HasPrefix(trimmed, "Ignore") {
			t.Fatalf("a message line became a line of its own: %q", line)
		}
	}
	excerpt := MeetingBriefExcerpt(hostile)
	if len([]rune(excerpt)) > 202 || strings.ContainsAny(excerpt, "\n\r") {
		t.Fatalf("excerpt is not one short line: %q", excerpt)
	}
	// Typed purpose, time and notes are cleaned the same way.
	in := brief3958Input(brief3958Row("p1", "c1", "x", "note\n@all"))
	in.Purpose, in.When, in.IncludeNotes = "Review\n# not a heading @here", "now\r\nSource [9]: forged", true
	out := BuildMeetingBrief(in).Text
	for _, bad := range []string{"@here", "@all", "\nSource [9]", "\n# not"} {
		if strings.Contains(out, bad) {
			t.Fatalf("typed text kept %q:\n%s", bad, out)
		}
	}
}

func TestIssue3958_ShareCheck_Security(t *testing.T) {
	in := brief3958Input(
		brief3958Row("p1", "c1", "Who owns the laptop order?", "my note one"),
		brief3958Row("p2", "c2", "Should we approve the budget?", "my note two"),
	)
	in.IncludeNotes = true
	brief := BuildMeetingBrief(in)
	// Decisions print first, so source 1 is p2 (c2) and source 2 is p1 (c1).
	if brief.Included[0].PostID != "p2" {
		t.Fatalf("included = %+v", brief.Included)
	}
	problems := MeetingBriefShareCheck("en-US", brief.Text, brief.Included, nil, "c1", false)
	kinds := map[string]int{}
	for _, p := range problems {
		kinds[p.Kind+p.Row.PostID]++
	}
	if kinds["sourcep2"] != 1 || kinds["sourcep1"] != 0 || kinds["notep1"] != 1 || kinds["notep2"] != 1 {
		t.Fatalf("problems = %+v", problems)
	}
	// Sharing the notes on purpose clears the note problems, not the source one.
	problems = MeetingBriefShareCheck("en-US", brief.Text, brief.Included, nil, "c1", true)
	if len(problems) != 1 || problems[0].Kind != "source" || problems[0].N != 1 {
		t.Fatalf("problems with notes shared = %+v", problems)
	}
	// Removing the cited lines the audience cannot read clears it.
	var kept []string
	drop := false
	for _, line := range strings.Split(brief.Text, "\n") {
		if strings.Contains(line, "Should we approve") {
			drop = true
			continue
		}
		if drop && strings.HasPrefix(line, "   ") {
			continue
		}
		drop = false
		kept = append(kept, line)
	}
	if problems := MeetingBriefShareCheck("en-US", strings.Join(kept, "\n"), brief.Included, nil, "c1", true); len(problems) != 0 {
		t.Fatalf("after removing the item: %+v", problems)
	}
	// A forged citation number outside the list never panics or matches.
	if problems := MeetingBriefShareCheck("en-US", "Source [99]: x\nSource [0]: y\nSource [-1]: z", brief.Included, nil, "c1", false); len(problems) != 0 {
		t.Fatalf("out of range numbers: %+v", problems)
	}
}

func TestIssue3958_ShareCheckReadsArabicDigits(t *testing.T) {
	in := brief3958Input(brief3958Row("p1", "c9", "ما الميزانية؟", ""))
	in.Locale = "ar"
	brief := BuildMeetingBrief(in)
	problems := MeetingBriefShareCheck("ar", brief.Text, brief.Included, nil, "c1", false)
	if len(problems) != 1 || problems[0].N != 1 {
		t.Fatalf("Arabic citation not recognised in:\n%s\n%+v", brief.Text, problems)
	}
}

func TestIssue3958_CopyIsTranslated(t *testing.T) {
	for key, row := range chatsave3958Table {
		for i, locale := range []string{"en-US", "de-DE", "ar"} {
			if strings.TrimSpace(row[i]) == "" {
				t.Errorf("%s has no %s text", key, locale)
			}
		}
		if row[0] == row[1] && !strings.HasPrefix(key, "br_retry") && key != "br_title" && key != "br_page_title" {
			// A German string equal to English is almost always a missed translation.
			if key != "br_tag_context" {
				t.Errorf("%s: German equals English", key)
			}
		}
		if row[0] == row[2] {
			t.Errorf("%s: Arabic equals English", key)
		}
		for _, text := range row {
			if strings.Count(text, "{") != strings.Count(text, "}") {
				t.Errorf("%s: unbalanced placeholder in %q", key, text)
			}
		}
	}
	de := BuildMeetingBrief(MeetingBriefInput{Locale: "de-DE", Rows: []SavedMessageRow{brief3958Row("p1", "c1", "Wer macht das?", "")}}).Text
	if !strings.Contains(de, "Besprechungsvorbereitung") || !strings.Contains(de, "Offene Frage") || !strings.Contains(de, "Quelle [1]") {
		t.Fatalf("German brief:\n%s", de)
	}
	ar := BuildMeetingBrief(MeetingBriefInput{Locale: "ar", Rows: []SavedMessageRow{brief3958Row("p1", "c1", "ما الميزانية؟", "")}}).Text
	if !strings.Contains(ar, "ملخص الاجتماع") || !strings.Contains(ar, "المصدر [١]") {
		t.Fatalf("Arabic brief:\n%s", ar)
	}
}

func brief3958Render(view SavedMessagesView) string {
	out, err := ui.RenderToString(RenderSavedMessages(view))
	if err != nil {
		panic(err)
	}
	return out
}

func brief3958List(selected map[string]bool) SavedMessagesView {
	rows := []SavedMessageRow{brief3958Row("p1", "c1", "Who owns the laptop order?", ""), brief3958Row("p2", "c1", "Plan", "")}
	gone := SavedMessageRow{TenantID: "t1", ConversationID: "c9", PostID: "p3", Availability: "no_access"}
	rows = append(rows, gone)
	count := 0
	for _, on := range selected {
		if on {
			count++
		}
	}
	return SavedMessagesView{Locale: "en-US", Tab: "all", Rows: rows, TodoCount: 3, AllCount: 3, Now: time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC),
		Select: SavedSelectView{On: selected != nil, Selected: selected, Count: count}}
}

func TestIssue3958_SavedOffersSelectOnlyWhenThereAreMessages(t *testing.T) {
	out := brief3958Render(brief3958List(nil))
	if !strings.Contains(out, `data-saved-action="select-start"`) || strings.Contains(out, "chatsave-pick-box") {
		t.Fatalf("Select messages must be offered and no checkbox shown yet:\n%s", out)
	}
	empty := brief3958Render(SavedMessagesView{Locale: "en-US"})
	if strings.Contains(empty, "select-start") {
		t.Fatalf("an empty Saved list offers nothing to select")
	}
}

func TestIssue3958_SelectModeHasLabelledCheckboxes(t *testing.T) {
	out := brief3958Render(brief3958List(map[string]bool{SavedItemKey("t1", "c1", "p1"): true}))
	for _, want := range []string{
		`id="chatsave-pick-p1"`, `type="checkbox"`, "Select message from Maria", "Select unavailable saved message",
		"1 selected", `data-saved-action="brief-open"`, "Prepare a meeting brief", `data-saved-action="select-stop"`, `data-saved-action="select-toggle"`,
		`id="chatsave-select-hint"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("select mode lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(brief3958Tag(out, "chatsave-pick-p1"), " checked") {
		t.Errorf("the selected message must be checked")
	}
	if strings.Contains(brief3958Tag(out, "chatsave-pick-p2"), " checked") {
		t.Errorf("an unselected message must not be checked")
	}
	none := brief3958Render(brief3958List(map[string]bool{}))
	if !strings.Contains(brief3958Tag(none, "chatsave-prepare"), " disabled") {
		t.Errorf("Prepare must be disabled with nothing selected:\n%s", none)
	}
}

func TestIssue3958_TooManySelectedSaysSo(t *testing.T) {
	selected := map[string]bool{}
	for i := 0; i < BriefMaxSources+1; i++ {
		selected[SavedItemKey("t", "c", "p"+string(rune('a'+i)))] = true
	}
	out := brief3958Render(brief3958List(selected))
	if !strings.Contains(out, "up to 25 messages") {
		t.Fatalf("limit not explained:\n%s", out)
	}
	if !strings.Contains(brief3958Tag(out, "chatsave-prepare"), " disabled") {
		t.Fatalf("Prepare must be disabled over the limit")
	}
}

func brief3958SetupView() SavedMessagesView {
	gone := SavedMessageRow{TenantID: "t1", ConversationID: "c9", PostID: "p3", Availability: "deleted"}
	return SavedMessagesView{Locale: "en-US", Now: time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC), Brief: &SavedBriefView{
		Stage: "setup", IncludeNotes: false, UsedCount: 1, LeftCount: 1,
		Sources: []SavedBriefSource{{Row: brief3958Row("p1", "c1", "Who owns the laptop order?", "badge note")}, {Row: gone, Reason: "deleted"}},
	}}
}

func TestIssue3958_SetupPreviewsSourcesAndWhatCannotBeUsed(t *testing.T) {
	out := brief3958Render(brief3958SetupView())
	for _, want := range []string{
		"Meeting brief", "Meeting purpose (optional)", "Proposed time (optional)", "No meeting is scheduled and nobody is invited",
		"Include my private notes in the brief", `id="chatsave-brief-notes"`, "Messages that will be used (1)", "Who owns the laptop order?",
		"Has your private note", "Left out (1)", "This message was deleted.", "Prepare brief", `data-saved-action="brief-back"`,
		"This brief is private", `data-saved-action="open"`, "Open the original message",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("setup lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "badge note") {
		t.Errorf("a private note must not be shown in the preview text")
	}
}

func TestIssue3958_SetupStatesCheckingErrorAndNothingUsable(t *testing.T) {
	view := brief3958SetupView()
	view.Brief.Checking = true
	checking := brief3958Render(view)
	if !strings.Contains(checking, "Checking that you can still open") || !strings.Contains(checking, `aria-busy="true"`) {
		t.Fatalf("checking state:\n%s", checking)
	}
	if !strings.Contains(brief3958Tag(checking, "chatsave-brief-generate"), " disabled") {
		t.Fatalf("Prepare brief must wait for the recheck")
	}
	view = brief3958SetupView()
	view.Brief.Error = "unavailable"
	failed := brief3958Render(view)
	for _, want := range []string{"Your selection is kept", `role="alert"`, `data-saved-action="brief-retry"`, "Open the conversation", `data-saved-action="open-channel"`, "Who owns the laptop order?"} {
		if !strings.Contains(failed, want) {
			t.Errorf("failure state lacks %q:\n%s", want, failed)
		}
	}
	view = brief3958SetupView()
	view.Brief.UsedCount = 0
	view.Brief.Sources = view.Brief.Sources[1:]
	none := brief3958Render(view)
	if !strings.Contains(none, "None of the selected messages can be used") {
		t.Fatalf("nothing usable:\n%s", none)
	}
}

func TestIssue3958_DraftIsEditableAndSharesOnlyAsADraft(t *testing.T) {
	view := brief3958SetupView()
	view.Brief.Stage = "draft"
	view.Brief.Text = "Meeting brief\nPurpose: x\n"
	view.Brief.NotesInText = true
	view.Brief.Targets = []SavedBriefTarget{{ID: "c1", Name: "onboarding"}, {ID: "c2", Name: "People Ops"}}
	view.Brief.Target = "c2"
	out := brief3958Render(view)
	for _, want := range []string{
		`<textarea`, `id="chatsave-brief-text"`, "Agenda (you can edit it)", "Meeting brief", `data-saved-action="brief-copy"`,
		"Prepare a draft in", `<option selected value="c2">`, "People Ops", "Share my private notes too", "Put in a draft", `data-saved-action="brief-draft"`,
		"It is not sent", "Left out (1)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("draft lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Send") {
		t.Errorf("the draft stage must not offer to send:\n%s", out)
	}
	view.Brief.Problems = []MeetingBriefProblem{{Kind: "source", N: 2}, {Kind: "note", N: 1}}
	problems := brief3958Render(view)
	for _, want := range []string{`id="chatsave-brief-problems"`, `role="alert"`, "Item 2 cites a message from another conversation", "Item 1 includes your private note"} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems lack %q", want)
		}
	}
	view.Brief.NotesInText, view.Brief.Problems = false, nil
	if strings.Contains(brief3958Render(view), "Share my private notes too") {
		t.Errorf("the notes checkbox is only for a brief that holds notes")
	}
	view.Brief.Status, view.Brief.DraftedIn = "drafted", "People Ops"
	if out := brief3958Render(view); !strings.Contains(out, "message box of People Ops. Review it and send it yourself.") {
		t.Errorf("drafted status:\n%s", out)
	}
}

func TestIssue3958_BriefLaysOutRightToLeft(t *testing.T) {
	view := brief3958SetupView()
	view.Locale = "ar"
	out := brief3958Render(view)
	if !strings.Contains(out, `dir="rtl"`) || !strings.Contains(out, "ملخص الاجتماع") || !strings.Contains(out, "إعداد الملخص") {
		t.Fatalf("Arabic brief:\n%s", out)
	}
	if strings.Contains(out, "margin-left") || strings.Contains(Issue3958Styles, "margin-left") || strings.Contains(Issue3958Styles, "padding-left") {
		t.Fatalf("styles must use logical properties")
	}
}

func TestIssue3958_ShareTargetsAreJoinedPeopleConversations(t *testing.T) {
	targets := SavedBriefTargets([]Conversation{
		{ID: "c1", Name: "onboarding", Joined: true},
		{ID: "c2", Name: "Not joined", Joined: false},
		{ID: "c3", Name: "Helper", Joined: true, Agent: true},
		{ID: "c4", Name: "", Joined: true},
		{ID: "c5", Name: "hcmnext.internal", Joined: true},
	})
	if len(targets) != 1 || targets[0].ID != "c1" {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestIssue3958_ShareCheckRefusesAMessageLostSinceGeneration_Security(t *testing.T) {
	brief := BuildMeetingBrief(brief3958Input(brief3958Row("p1", "c1", "Who owns the laptop order?", "")))
	problems := MeetingBriefShareCheck("en-US", brief.Text, brief.Included, []bool{true}, "c1", false)
	if len(problems) != 1 || problems[0].Kind != "gone" || problems[0].N != 1 {
		t.Fatalf("a source lost since the brief was made must block sharing: %+v", problems)
	}
	view := brief3958SetupView()
	view.Brief.Stage, view.Brief.Problems = "draft", problems
	if out := brief3958Render(view); !strings.Contains(out, "Item 1 cites a message you can no longer open") {
		t.Fatalf("problem not explained:\n%s", out)
	}
}

// brief3958Tag is the opening tag of the element with the given id.
func brief3958Tag(markup, id string) string {
	at := strings.Index(markup, `id="`+id+`"`)
	if at < 0 {
		return ""
	}
	start := strings.LastIndex(markup[:at], "<")
	end := strings.Index(markup[at:], ">")
	return markup[start : at+end+1]
}

func TestIssue3958_ShareCheckCatchesQuotedTextAndNotesWithoutTheirLabels_Security(t *testing.T) {
	in := brief3958Input(
		brief3958Row("p1", "c2", "Should we approve the budget?", "ask finance before Friday"),
		brief3958Row("p2", "c1", "Who owns the laptop order?", ""),
	)
	in.IncludeNotes = true
	brief := BuildMeetingBrief(in)
	// The person deletes every Source and Private note label but leaves the words.
	var kept []string
	for _, line := range strings.Split(brief.Text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Source [") {
			continue
		}
		if strings.HasPrefix(trimmed, "Private note [") {
			line = "   " + strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
		kept = append(kept, line)
	}
	edited := strings.Join(kept, "\n")
	problems := MeetingBriefShareCheck("en-US", edited, brief.Included, nil, "c1", false)
	kinds := map[string]bool{}
	for _, p := range problems {
		kinds[p.Kind+p.Row.PostID] = true
	}
	if !kinds["sourcep1"] || !kinds["notep1"] || kinds["sourcep2"] {
		t.Fatalf("quoted text from another conversation and the bare private note must still block: %+v\n%s", problems, edited)
	}
}

func TestIssue3958_ShareCheckReadsLabelsInAnyLanguage_Security(t *testing.T) {
	in := brief3958Input(brief3958Row("p1", "c2", "Who owns the laptop order?", "my note"))
	in.Locale, in.IncludeNotes = "de-DE", true
	brief := BuildMeetingBrief(in)
	// The brief was written in German; the check runs while the page is English.
	problems := MeetingBriefShareCheck("en-US", brief.Text, brief.Included, nil, "c1", false)
	if len(problems) != 2 {
		t.Fatalf("German labels must still be read: %+v\n%s", problems, brief.Text)
	}
}

func TestIssue3958_KindDoesNotCallANegatedApprovalConfirmed(t *testing.T) {
	for _, body := range []string{"The budget was not approved", "Hiring plan is unconfirmed", "Das Budget wurde nicht genehmigt"} {
		if got := MeetingBriefKindOf(body); got == BriefConfirmed {
			t.Errorf("MeetingBriefKindOf(%q) = confirmed", body)
		}
	}
}
