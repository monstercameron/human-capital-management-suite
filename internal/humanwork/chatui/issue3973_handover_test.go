package chatui

import (
	"strings"
	"testing"
)

const issue3973Origin = "https://hcm.example"

func issue3973Link(project, task string) string {
	return issue3973Origin + "/workspace/app/project?project=" + project + "&task=" + task
}

func issue3973Model() Model {
	m := Model{
		Locale: "en-US", SelectedID: "ops", CurrentUser: "me", CurrentTenantID: "t1", EmbedOrigin: issue3973Origin,
		Conversations: []Conversation{{ID: "ops", Name: "ops", Kind: PublicChannel}},
		Messages: []Message{
			{ID: "p1", Body: "Please look at " + issue3973Link("proj-1", "task-1")},
			{ID: "p2", Body: "and " + issue3973Link("proj-1", "task-1") + " again, plus " + issue3973Link("proj-2", "task-9") + " and the board " + issue3973Origin + "/workspace/app/project?project=proj-3"},
		},
		Members: []Member{{ID: "me", Name: "Me"}, {ID: "kim", Name: "Kim"}, {ID: "bot", Name: "Helper", Agent: true}, {ID: "ext", Name: "Guest", HomeTenantID: "other"}},
		ProjectTaskPreviews: map[string]ProjectTaskPreview{
			ProjectTaskPreviewKey("proj-1", "task-1"): {ProjectID: "proj-1", TaskID: "task-1", Title: "Ship the release", Key: "T-1", Readable: true, State: "ready", Status: "Doing", Assignee: "Ana", Due: "2026-12-01"},
			ProjectTaskPreviewKey("proj-2", "task-9"): {ProjectID: "proj-2", TaskID: "task-9", State: "restricted"},
		},
		Callbacks: Callbacks{Handover: func(HandoverCommand) {}},
	}
	return m
}

func issue3973Fresh(title, status, owner, due string) ProjectTaskPreview {
	return ProjectTaskPreview{ProjectID: "proj-1", TaskID: "task-1", Title: title, Key: "T-1", Readable: true, State: "ready", Status: status, Assignee: owner, Due: due}
}

func TestIssue3973_CandidatesAreDistinctTasksOnly(t *testing.T) {
	got := HandoverCandidates(issue3973Model())
	if len(got) != 2 {
		t.Fatalf("want the two distinct tasks (not the repeat, not the board), got %#v", got)
	}
	if got[0].Key != "proj-1|task-1" || !got[0].Readable || got[0].Title != "Ship the release" {
		t.Fatalf("first candidate = %#v", got[0])
	}
	if got[1].Readable || got[1].Title != "" {
		t.Fatalf("a task the person cannot open must carry no title: %#v", got[1])
	}
}

func TestIssue3973_RecipientsAreColleaguesOfTheOrganisation(t *testing.T) {
	got := HandoverRecipients(issue3973Model())
	if len(got) != 1 || got[0].ID != "kim" {
		t.Fatalf("recipients = %#v, want only Kim (not me, an agent or another organisation)", got)
	}
}

func TestIssue3973_BriefSeparatesFactsFromSuggestionsAndNeverReassigns(t *testing.T) {
	res := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, RecipientName: "Kim", From: "2026-12-01", To: "2026-12-15", Items: []HandoverItem{
		{ProjectID: "proj-1", TaskID: "task-1", Fresh: issue3973Fresh("Ship the release", "Doing", "Ana", "2026-12-05"), Recipient: "yes"},
	}})
	for _, want := range []string{"Work handover", "For: Kim", "Handover window:", "Facts from Projects:", "Status: Doing", "Owner: Ana", "Due:", "Link: " + issue3973Link("proj-1", "task-1"), "Suggestions (check and edit):", "Next step: not set", "Open questions: none known", "Nothing has been reassigned."} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("brief lacks %q:\n%s", want, res.Text)
		}
	}
	if strings.Index(res.Text, "Facts from Projects:") > strings.Index(res.Text, "Suggestions (check and edit):") {
		t.Fatalf("suggestions must follow the facts:\n%s", res.Text)
	}
	lower := strings.ToLower(res.Text)
	for _, banned := range []string{"vacation", "leave", "sick", "health", "absence", "away"} {
		if strings.Contains(lower, banned) {
			t.Fatalf("the brief states or implies a reason for the absence (%q):\n%s", banned, res.Text)
		}
	}
	if len(res.Rows) != 1 || res.Rows[0].Outcome != HandoverIncluded {
		t.Fatalf("rows = %#v", res.Rows)
	}
}

func TestIssue3973_MissingInformationIsLabelledNotInvented(t *testing.T) {
	res := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, Items: []HandoverItem{
		{ProjectID: "proj-1", TaskID: "task-1", Fresh: issue3973Fresh("Ship it", "", "", ""), Recipient: "yes"},
	}})
	for _, want := range []string{"Status: not set", "Owner: not set", "Due: not set", "Handover window: not set"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("brief lacks %q:\n%s", want, res.Text)
		}
	}
}

func TestIssue3973_StaleCardIsMarkedChangedFromFreshRead(t *testing.T) {
	shown := issue3973Fresh("Ship the release", "Doing", "Ana", "2026-12-01")
	fresh := issue3973Fresh("Ship the release", "Blocked", "Ben", "2026-12-09")
	res := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, Items: []HandoverItem{{ProjectID: "proj-1", TaskID: "task-1", Shown: shown, Fresh: fresh, Recipient: "yes"}}})
	if !strings.Contains(res.Text, "Status: Blocked") || strings.Contains(res.Text, "Status: Doing") {
		t.Fatalf("the brief must carry the fresh status, not the card's:\n%s", res.Text)
	}
	for _, want := range []string{"the status was “Doing”", "the owner was “Ana”", "the due date was"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("brief lacks the change note %q:\n%s", want, res.Text)
		}
	}
	if !res.Rows[0].Changed {
		t.Fatalf("row not marked changed: %#v", res.Rows[0])
	}
	same := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, Items: []HandoverItem{{ProjectID: "proj-1", TaskID: "task-1", Shown: shown, Fresh: shown, Recipient: "yes"}}})
	if same.Rows[0].Changed || strings.Contains(same.Text, "Changed since") {
		t.Fatalf("an unchanged task is marked changed:\n%s", same.Text)
	}
}

func TestIssue3973_TaskTheColleagueCannotOpenIsLeftOutWithoutContent_Security(t *testing.T) {
	secret := issue3973Fresh("Layoff plan for Q3", "Doing", "Ana", "2026-12-05")
	for _, access := range []string{"no", "unknown"} {
		res := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, RecipientName: "Kim", Items: []HandoverItem{
			{ProjectID: "proj-1", TaskID: "task-1", Fresh: secret, Recipient: access},
		}})
		for _, leak := range []string{"Layoff", "Ana", "proj-1", "task-1", "T-1", "Doing"} {
			if strings.Contains(res.Text, leak) {
				t.Fatalf("recipient %s: the brief leaks %q:\n%s", access, leak, res.Text)
			}
		}
		if !strings.Contains(res.Text, "Not included (1)") || !strings.Contains(res.Text, "Task 1:") {
			t.Fatalf("recipient %s: the left-out task is not listed:\n%s", access, res.Text)
		}
		if res.Rows[0].Outcome != HandoverWithheld {
			t.Fatalf("recipient %s: row = %#v", access, res.Rows[0])
		}
	}
	no := BuildHandover(HandoverInput{Locale: "en-US", Items: []HandoverItem{{ProjectID: "proj-1", TaskID: "task-1", Fresh: secret, Recipient: "no"}}})
	if !strings.Contains(no.Text, "ask the project owner for access") {
		t.Fatalf("no approved way to ask for access is named:\n%s", no.Text)
	}
}

func TestIssue3973_TaskTheAuthorCannotReadNowIsUnavailable(t *testing.T) {
	for state, want := range map[string]string{"restricted": "no longer open", "missing": "no longer exists", "archived": "archived", "failed": "Try again"} {
		res := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, Items: []HandoverItem{
			{ProjectID: "proj-1", TaskID: "task-1", Fresh: ProjectTaskPreview{State: state, Title: "Leaky title", Readable: state == "restricted"}, Recipient: "yes"},
		}})
		if res.Rows[0].Outcome != HandoverUnavailable || res.Rows[0].Title != "" {
			t.Fatalf("%s: row = %#v", state, res.Rows[0])
		}
		if strings.Contains(res.Text, "Leaky") || !strings.Contains(res.Text, want) {
			t.Fatalf("%s: brief = %q, want %q and no title", state, res.Text, want)
		}
	}
}

func TestIssue3973_UntrustedTextCannotMentionOrBreakLines(t *testing.T) {
	res := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, RecipientName: "Kim\nSuggestions: @everyone", Items: []HandoverItem{
		{ProjectID: "proj-1", TaskID: "task-1", Fresh: issue3973Fresh("Fix\n@channel now", "Doing", "Ana", ""), Recipient: "yes"},
	}})
	if strings.Contains(res.Text, "@channel") || strings.Contains(res.Text, "@everyone") {
		t.Fatalf("a task title or name can notify people:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "Kim\n") {
		t.Fatalf("a name broke out of its line:\n%s", res.Text)
	}
}

func TestIssue3973_BriefIsLocalisedInGermanAndArabic(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"de-DE", "Arbeitsübergabe"}, {"ar", "تسليم العمل"}} {
		res := BuildHandover(HandoverInput{Locale: tc.locale, Origin: issue3973Origin, Items: []HandoverItem{
			{ProjectID: "proj-1", TaskID: "task-1", Fresh: issue3973Fresh("Ship it", "Doing", "Ana", "2026-12-05"), Recipient: "yes"},
			{ProjectID: "proj-2", TaskID: "task-2", Fresh: issue3973Fresh("Other", "", "", ""), Recipient: "no"},
		}})
		if !strings.Contains(res.Text, tc.want) || strings.Contains(res.Text, "{") || strings.Contains(res.Text, "Facts from Projects") {
			t.Fatalf("%s brief not localised:\n%s", tc.locale, res.Text)
		}
	}
	for key := range issue3973Copy {
		for i, text := range issue3973Copy[key] {
			if strings.TrimSpace(text) == "" {
				t.Fatalf("copy %q has no text for language %d", key, i)
			}
		}
	}
}

func TestIssue3973_WindowMustNotRunBackwards(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		ok       bool
	}{{"", "", true}, {"2026-12-01", "", true}, {"", "2026-12-01", true}, {"2026-12-01", "2026-12-01", true}, {"2026-12-02", "2026-12-01", false}, {"nope", "", false}} {
		if got := HandoverWindowValid(tc.from, tc.to); got != tc.ok {
			t.Errorf("window %q..%q valid = %v, want %v", tc.from, tc.to, got, tc.ok)
		}
	}
}

func TestIssue3973_DraftKeepsWhatWasAlreadyTyped(t *testing.T) {
	if got := HandoverDraftText("", "brief\n"); got != "brief" {
		t.Fatalf("got %q", got)
	}
	if got := HandoverDraftText("hello ", "brief"); got != "hello\n\nbrief" {
		t.Fatalf("typed text was lost or glued: %q", got)
	}
}

func TestIssue3973_HeaderButtonOnlyWhereTaskCardsAre(t *testing.T) {
	m := issue3973Model()
	markup := renderNode(t, issue3973Button(m))
	for _, want := range []string{`data-action="handover-toggle"`, `aria-label="Prepare a work handover"`, `aria-expanded="false"`, `aria-controls="chat-handover-panel"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("button lacks %q:\n%s", want, markup)
		}
	}
	m.Messages = []Message{{ID: "p1", Body: "no cards here"}}
	if issue3973Button(m) != nil {
		t.Fatal("a conversation without task cards offers a handover")
	}
	m.Callbacks.Handover = nil
	m.Messages = issue3973Model().Messages
	if got := renderNode(t, issue3973Button(m)); !strings.Contains(got, "disabled") {
		t.Fatalf("button live without a client:\n%s", got)
	}
}

func TestIssue3973_PanelAbsentUntilOpenedAndScopedToItsConversation(t *testing.T) {
	m := issue3973Model()
	if issue3973Panel(m) != nil {
		t.Fatal("panel drawn before anyone asked")
	}
	m.Handover = HandoverView{ConversationID: "other", Open: true, Stage: "pick"}
	if issue3973Panel(m) != nil {
		t.Fatal("another conversation's handover leaked into this one")
	}
}

func TestIssue3973_PickStageIsKeyboardLabelledAndHidesClosedTasks(t *testing.T) {
	m := issue3973Model()
	m.Handover = HandoverView{ConversationID: "ops", Open: true, Stage: "pick", Selected: map[string]bool{"proj-1|task-1": true}, Recipient: "kim", From: "2026-12-01"}
	markup := renderNode(t, issue3973Panel(m))
	for _, want := range []string{`id="chat-handover-panel"`, `role="region"`, `aria-labelledby="chat-handover-heading"`, `type="checkbox"`, `data-action="handover-task"`, `data-id="proj-1|task-1"`, `checked`, "T-1 Ship the release", "A task you cannot open", `for="chat-handover-recipient"`, `value="kim"`, `for="chat-handover-from"`, `for="chat-handover-to"`, `data-chat-value="2026-12-01"`, `data-action="handover-prepare"`, "Prepare handover", `role="status"`, "private draft"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("pick stage lacks %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Guest") || strings.Contains(markup, "Helper") || strings.Contains(markup, ">Me<") {
		t.Fatalf("recipient list offers someone it should not:\n%s", markup)
	}
	if !strings.Contains(markup, "disabled") {
		t.Fatalf("the task the person cannot open is tickable:\n%s", markup)
	}
}

func TestIssue3973_NoticesAreAnnouncedAsAlerts(t *testing.T) {
	m := issue3973Model()
	for status, want := range map[string]string{"none": "Tick at least one task", "recipient": "Choose who to hand over to", "window": "end day is before"} {
		m.Handover = HandoverView{ConversationID: "ops", Open: true, Stage: "pick", Status: status}
		markup := renderNode(t, issue3973Panel(m))
		if !strings.Contains(markup, want) || !strings.Contains(markup, `role="alert"`) {
			t.Fatalf("%s: notice %q not announced:\n%s", status, want, markup)
		}
	}
	m.Handover = HandoverView{ConversationID: "ops", Open: true, Stage: "preparing"}
	markup := renderNode(t, issue3973Panel(m))
	if !strings.Contains(markup, "Reading the latest from Projects") || !strings.Contains(markup, `aria-busy="true"`) {
		t.Fatalf("loading not announced:\n%s", markup)
	}
	m.Handover = HandoverView{ConversationID: "ops", Open: true, Stage: "error"}
	markup = renderNode(t, issue3973Panel(m))
	if !strings.Contains(markup, "Try again") || !strings.Contains(markup, `data-action="handover-prepare"`) {
		t.Fatalf("a failed read offers no retry:\n%s", markup)
	}
}

func TestIssue3973_DraftStageShowsEditableBriefAndWhatWasLeftOut(t *testing.T) {
	m := issue3973Model()
	res := BuildHandover(HandoverInput{Locale: "en-US", Origin: issue3973Origin, RecipientName: "Kim", Items: []HandoverItem{
		{ProjectID: "proj-1", TaskID: "task-1", Fresh: issue3973Fresh("Ship the release", "Doing", "Ana", ""), Recipient: "yes"},
		{ProjectID: "proj-2", TaskID: "task-9", Fresh: ProjectTaskPreview{ProjectID: "proj-2", TaskID: "task-9", Title: "Secret merger", Readable: true, State: "ready"}, Recipient: "no"},
	}})
	m.Handover = HandoverView{ConversationID: "ops", Open: true, Stage: "draft", Text: res.Text, Rows: res.Rows, RecipientName: "Kim"}
	markup := renderNode(t, issue3973Panel(m))
	for _, want := range []string{`<textarea`, `id="chat-handover-text"`, `for="chat-handover-text"`, "Handover (you can edit it)", `data-action="handover-draft"`, "Put in message box", `data-action="handover-back"`, "Not included", "Secret merger", "cannot open it", "does not reassign anything", "Everyone in this conversation will see it"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("draft stage lacks %q:\n%s", want, markup)
		}
	}
	// The author sees the withheld title in the list, but the editable brief must not carry it.
	if strings.Contains(res.Text, "Secret merger") {
		t.Fatalf("withheld title is in the brief:\n%s", res.Text)
	}
}

func TestIssue3973_PanelHoldsInRtlAndGerman(t *testing.T) {
	m := issue3973Model()
	m.Locale = "ar"
	m.Handover = HandoverView{ConversationID: "ops", Open: true, Stage: "pick"}
	if markup := renderNode(t, issue3973Panel(m)); !strings.Contains(markup, `dir="rtl"`) || !strings.Contains(markup, "تسليم العمل") {
		t.Fatalf("arabic panel not rtl/localised:\n%s", markup)
	}
	m.Locale = "de-DE"
	if markup := renderNode(t, issue3973Panel(m)); !strings.Contains(markup, "Übergabe erstellen") {
		t.Fatalf("german panel not localised:\n%s", markup)
	}
	if !strings.Contains(issue3973Styles, "@media(max-width:760px)") || !strings.Contains(issue3973Styles, "min-height:44px") {
		t.Fatal("no phone layout with 44px targets")
	}
}

func TestIssue3973_KeySplitRejectsForgedIds(t *testing.T) {
	if p, k, ok := HandoverSplitKey("proj-1|task-1"); !ok || p != "proj-1" || k != "task-1" {
		t.Fatalf("good key rejected: %q %q %v", p, k, ok)
	}
	for _, bad := range []string{"", "proj-1", "proj-1|", "|task", "a|b|c", "pr oj|t"} {
		if _, _, ok := HandoverSplitKey(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}
