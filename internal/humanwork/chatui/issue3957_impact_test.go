package chatui

import (
	"strings"
	"testing"
	"time"
)

const issue3957Proposal = "Please move the onboarding announcement to 2026-12-01 so it lands after the holiday."

func issue3957Model() (Model, *[]string) {
	calls := &[]string{}
	m := chatux021Model()
	m.Messages = []Message{{ID: "root", Author: "Loretta Haynes", AuthorID: "loretta", Body: issue3957Proposal, Sequence: 1}}
	m.ThreadParentID = "root"
	m.ThreadMessages = []Message{{ID: "reply", Author: "Walt Brennan", AuthorID: "walt", Body: "Which date works?", Sequence: 2}}
	m.SearchDirectory = []SearchPerson{{ID: "loretta", Name: "Loretta Haynes"}}
	m.Impact = ImpactState{
		Enabled: true,
		Open:    func(id, date string) { *calls = append(*calls, "open:"+id+":"+date) },
		Close:   func() { *calls = append(*calls, "close") },
		Run:     func(id, date, pop string) { *calls = append(*calls, "run:"+id+":"+date+":"+pop) },
		Dismiss: func(id string) { *calls = append(*calls, "dismiss:"+id) },
		Restore: func(id string) { *calls = append(*calls, "restore:"+id) },
		Draft: func(id, date, pop string, deliver func(string)) {
			*calls = append(*calls, "draft:"+id+":"+date+":"+pop)
		},
	}
	return m, calls
}

func issue3957Items() []LinkedWorkItem {
	return []LinkedWorkItem{
		{Kind: "task", ID: "t-same", Title: "Send welcome kits", State: "open", OwnerID: "loretta", DueDate: "2026-12-01", ProjectID: "p1", TaskID: "t-same", Detail: "ACTIVE"},
		{Kind: "task", ID: "t-near", Title: "Book the hall", State: "blocked", OwnerID: "walt", DueDate: "2026-12-03", ProjectID: "p1", TaskID: "t-near"},
		{Kind: "task", ID: "t-far", Title: "Annual review", State: "open", DueDate: "2027-02-01", ProjectID: "p1", TaskID: "t-far"},
		{Kind: "task", ID: "t-nodate", Title: "Draft FAQ", State: "open", ProjectID: "p1", TaskID: "t-nodate"},
		{Kind: "task", ID: "t-done", Title: "Old kit run", State: "done", DueDate: "2026-12-01", ProjectID: "p1", TaskID: "t-done"},
		{Kind: "intent", ID: "i1", Title: "Promotion", State: "awaiting_approval"},
	}
}

func TestIssue3957_AssessSeparatesDateOverlapAndStatesWhatWasLeftOut(t *testing.T) {
	r := ImpactAssess(issue3957Items(), []string{"tasks"}, time.Time{}, "2026-12-01")
	if len(r.Findings) != 2 || r.Findings[0].Item.ID != "t-same" || r.Findings[0].Days != 0 || r.Findings[1].Item.ID != "t-near" || r.Findings[1].Days != 2 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	if r.Compared != 3 || r.NoDate != 1 || r.Finished != 1 || r.Actions != 1 || len(r.Incomplete) != 1 {
		t.Errorf("counts = %+v", r)
	}
	if got := ImpactAssess(issue3957Items(), nil, time.Time{}, "not a date"); len(got.Findings) != 0 || got.Compared != 0 {
		t.Errorf("a bad date compared work: %+v", got)
	}
	before := ImpactAssess([]LinkedWorkItem{{Kind: "task", ID: "x", State: "open", DueDate: "2026-11-28T09:00:00Z"}}, nil, time.Time{}, "2026-12-01")
	if len(before.Findings) != 1 || before.Findings[0].Days != -3 {
		t.Errorf("a due date with a time of day: %+v", before.Findings)
	}
}

func TestIssue3957_DateInMessageIsOnlyASuggestion(t *testing.T) {
	if got := ImpactDateIn(issue3957Proposal); got != "2026-12-01" {
		t.Errorf("date = %q", got)
	}
	if ImpactDateIn("move it to 2026-13-45 maybe") != "" || ImpactDateIn("no date here") != "" {
		t.Error("an impossible or missing date was suggested")
	}
}

func TestIssue3957_MenuIsOfferedInTheThreadPaneOnly(t *testing.T) {
	m, _ := issue3957Model()
	m.MenuID = "thread:root"
	got := renderNode(t, spanOf(chatux022MenuItems(m, m.Messages[0])))
	if !strings.Contains(got, `data-action="impact-open" data-id="root"`) || !strings.Contains(got, "Check downstream impact") {
		t.Fatalf("thread menu misses the row: %s", got)
	}
	m.MenuID = "message:root"
	if strings.Contains(renderNode(t, spanOf(chatux022MenuItems(m, m.Messages[0]))), "impact-open") {
		t.Error("the row is offered outside the thread pane, where there is no reply box to draft into")
	}
	m.MenuID = "thread:root"
	for name, mutate := range map[string]func(*Model){
		"disabled":   func(m *Model) { m.Impact.Enabled = false },
		"signed out": func(m *Model) { m.CurrentUser = "" },
		"no thread":  func(m *Model) { m.ThreadParentID = "" },
		"other company": func(m *Model) {
			for i := range m.Conversations {
				m.Conversations[i].HostTenantID = "other-tenant"
			}
		},
	} {
		c, _ := issue3957Model()
		mutate(&c)
		if len(impactMenuItem(c, c.Messages[0])) != 0 {
			t.Errorf("%s: the row is offered", name)
		}
	}
	empty, _ := issue3957Model()
	if len(impactMenuItem(empty, Message{ID: "x", Body: "  "})) != 0 {
		t.Error("an empty message is offered the check")
	}
}

func issue3957Ready(m Model) Model {
	m.Impact.PostID = "root"
	m.Impact.Date = "2026-12-01"
	m.Impact.Stage = "ready"
	m.Impact.Result = ImpactAssess(issue3957Items(), nil, time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC), "2026-12-01")
	return m
}

func TestIssue3957_ResultNamesSourcesOwnersAndWhatIsNotChecked(t *testing.T) {
	m, _ := issue3957Model()
	m = issue3957Ready(m)
	got := renderNodes3737(t, impactBlock(m, m.Messages[0]))
	for _, want := range []string{
		"Only you see this check", "does not look at calendars", issue3957Proposal,
		"2 open task(s) linked to this conversation are due on or within 3 days of 2026-12-01",
		"Send welcome kits", "Book the hall", "Owner: Loretta Haynes", "Owner: You", "Due on the proposed date", "Due 2 day(s) after the proposed date",
		"Explicit link: a task made from a message in this conversation",
		"3 open task(s) with a due date", "1 open task(s) have no due date", "1 finished or closed", "1 proposed action(s)",
		"Calendars: not connected here", "Relationships inferred from wording: not available", "Other conversations and projects",
		`data-action="impact-dismiss" data-id="t-same"`, `data-action="impact-draft" data-id="root"`, `role="status"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("result misses %q: %s", want, got)
		}
	}
	for _, gone := range []string{"Annual review", "Old kit run", "Draft FAQ", "Promotion"} {
		if strings.Contains(got, gone) {
			t.Errorf("a task that is not a finding is listed: %s", gone)
		}
	}
	if strings.Contains(got, "no conflicts") || strings.Contains(got, "all clear") {
		t.Error("the result implies the whole organization is clear")
	}
	if strings.Contains(renderNodes3737(t, impactBlock(m, m.ThreadMessages[0])), "impact-check") {
		t.Error("the check shows under a different message")
	}
}

func TestIssue3957_NoFindingsIsScopedToTheSourcesChecked(t *testing.T) {
	m, _ := issue3957Model()
	m = issue3957Ready(m)
	m.Impact.Result = ImpactAssess(nil, []string{"tasks"}, time.Time{}, "2026-12-01")
	got := renderNodes3737(t, impactBlock(m, m.Messages[0]))
	if !strings.Contains(got, "in the sources checked. This does not mean nothing else is affected") {
		t.Errorf("an empty result is not scoped: %s", got)
	}
	if !strings.Contains(got, "could not be read right now (tasks)") {
		t.Errorf("an unreadable source is not said: %s", got)
	}
	if !strings.Contains(got, `data-action="impact-draft"`) || !strings.Contains(got, "disabled") {
		t.Errorf("a draft is offered with nothing to ask: %s", got)
	}
}

func TestIssue3957_DismissedFindingsLeaveTheDraftAndCanBeRestored(t *testing.T) {
	m, _ := issue3957Model()
	m = issue3957Ready(m)
	m.Impact.Dismissed = map[string]bool{"t-same": true}
	got := renderNodes3737(t, impactBlock(m, m.Messages[0]))
	if !strings.Contains(got, `data-action="impact-restore" data-id="t-same"`) || !strings.Contains(got, "Dismissed") {
		t.Errorf("a dismissed finding cannot be restored: %s", got)
	}
	if ImpactKept(m.Impact.Result, m.Impact.Dismissed) != 1 {
		t.Error("kept count ignores dismissals")
	}
	m.Impact.Dismissed["t-near"] = true
	if ImpactKept(m.Impact.Result, m.Impact.Dismissed) != 0 || ImpactDraft("en-US", "2026-12-01", "", 0) != "" {
		t.Error("a draft is made when every finding is dismissed")
	}
}

// The draft is the one thing that can reach other people, so it carries the
// date and the person's own words only: no title, owner, count or status.
func TestIssue3957_DraftNamesNoWorkFromAnySource_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		plain := ImpactDraft(locale, "2026-12-01", "", 2)
		withGroup := ImpactDraft(locale, "2026-12-01", "  new   hires\nin Berlin ", 2)
		if !strings.Contains(plain, "2026-12-01") || !strings.Contains(withGroup, "new hires in Berlin") || strings.Contains(plain, "{") || strings.Contains(withGroup, "{") {
			t.Errorf("%s: draft = %q / %q", locale, plain, withGroup)
		}
		for _, secret := range []string{"Send welcome kits", "Book the hall", "Loretta", "Walt", "blocked", "t-same"} {
			if strings.Contains(plain, secret) || strings.Contains(withGroup, secret) {
				t.Errorf("%s: the draft names %q", locale, secret)
			}
		}
	}
	long := ImpactDraft("en-US", "2026-12-01", strings.Repeat("x", 500), 1)
	if strings.Count(long, "x") != impactPopulationMax {
		t.Errorf("the group text is not bounded: %d", strings.Count(long, "x"))
	}
	if ImpactDraft("en-US", "someday", "", 3) != "" {
		t.Error("a draft was made without a valid date")
	}
}

func TestIssue3957_ActionsRunTheCallbacksAndAskBeforeSendingNothing(t *testing.T) {
	m, calls := issue3957Model()
	opener := m
	opener.impactAction("impact-open", "root")
	m.Impact.PostID = "root"
	m.Impact.Date, m.Impact.Population = "2026-12-01", "hires"
	for _, step := range [][2]string{{"impact-run", "root"}, {"impact-dismiss", "t-same"}, {"impact-restore", "t-same"}, {"impact-draft", "root"}, {"impact-close", ""}} {
		m.impactAction(step[0], step[1])
	}
	want := "open:root:2026-12-01|run:root:2026-12-01:hires|dismiss:t-same|restore:t-same|draft:root:2026-12-01:hires|close"
	if got := strings.Join(*calls, "|"); got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	*calls = nil
	m.impactAction("impact-run", "reply")
	m.impactAction("impact-open", "")
	if len(*calls) != 0 {
		t.Errorf("a control ran for another message: %v", *calls)
	}
	m.impactAction("impact-open", "root")
	if strings.Join(*calls, "|") != "close" {
		t.Errorf("open on the open check = %v", *calls)
	}
}

func TestIssue3957_DraftIsDeliveredToTheReplyBoxNotSent(t *testing.T) {
	m, _ := issue3957Model()
	var filed []string
	sent := 0
	m.Callbacks.ThreadDraftChanged = func(parent, value string) { filed = append(filed, parent+"="+value) }
	m.Callbacks.SendMessageWithReferences = func(string, string, []ChatReference) { sent++ }
	m.ThreadDrafts = map[string]string{"root": "Thanks all."}
	impactDeliver(m)("Before we move this")
	impactDeliver(m)("   ")
	if len(filed) != 1 || filed[0] != "root=Thanks all.\n\nBefore we move this" {
		t.Errorf("filed = %q (existing words must stay)", filed)
	}
	if sent != 0 {
		t.Error("the draft was sent")
	}
}

func TestIssue3957_StatesAreSaidInWordsAndLive(t *testing.T) {
	m, _ := issue3957Model()
	m.Impact.PostID = "root"
	for stage, want := range map[string]string{"loading": "Checking the work linked", "error": "could not be read right now, so nothing is known"} {
		m.Impact.Stage = stage
		got := renderNodes3737(t, impactBlock(m, m.Messages[0]))
		if !strings.Contains(got, want) || !strings.Contains(got, `aria-live="polite"`) {
			t.Errorf("%s: %s", stage, got)
		}
	}
	m.Impact.Stage, m.Impact.Error = "", "invalid_date"
	if got := renderNodes3737(t, impactBlock(m, m.Messages[0])); !strings.Contains(got, "Choose a valid date") {
		t.Errorf("invalid date: %s", got)
	}
	m.Impact.Error, m.Impact.Notice = "", "draft_ready"
	if got := renderNodes3737(t, impactBlock(m, m.Messages[0])); !strings.Contains(got, "A draft is in the reply box") {
		t.Errorf("notice: %s", got)
	}
}

func TestIssue3957_CopyInEveryLocaleAndRTL(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for key := range issue3957Copy["en-US"] {
			if strings.TrimSpace(issue3957Copy[locale][key]) == "" {
				t.Errorf("%s misses %s", locale, key)
			}
		}
	}
	m, _ := issue3957Model()
	m = issue3957Ready(m)
	m.Locale = "ar"
	got := renderNodes3737(t, impactBlock(m, m.Messages[0]))
	if !strings.Contains(got, "فحص الأثر") || strings.Contains(got, "Check downstream") {
		t.Errorf("arabic check is not translated: %s", got)
	}
	if strings.Contains(Issue3957Styles, "margin-left") || strings.Contains(Issue3957Styles, "padding-right") || strings.Contains(Issue3957Styles, "border-left") {
		t.Error("physical properties break RTL")
	}
	if !strings.Contains(Issue3957Styles, "min-block-size:44px") {
		t.Error("no 44px phone targets")
	}
}

// Wired end to end: the thread pane renders the row, the dispatcher runs the
// controls, and the root and a reply both carry the open check.
func TestIssue3957_WiredThroughThreadPaneDispatcherAndRows_Integration(t *testing.T) {
	m, calls := issue3957Model()
	m.actWith("impact-open", "root", "")
	m.Impact.PostID, m.Impact.Date = "root", "2026-12-01"
	m.actWith("impact-run", "root", "")
	m.actWith("impact-dismiss", "t-same", "")
	if got, want := strings.Join(*calls, "|"), "open:root:2026-12-01|run:root:2026-12-01:|dismiss:t-same"; got != want {
		t.Errorf("dispatcher calls = %s, want %s", got, want)
	}
	pane := renderNode(t, threadPane(m, handlers{}))
	if !strings.Contains(pane, `id="impact-root"`) || !strings.Contains(pane, "Only you see this check") {
		t.Errorf("the thread root does not carry the check: %s", pane)
	}
	m.Impact.PostID = "reply"
	if pane = renderNode(t, threadPane(m, handlers{})); !strings.Contains(pane, `id="impact-reply"`) || strings.Contains(pane, `id="impact-root"`) {
		t.Errorf("the check is not under the chosen reply: %s", pane)
	}
	m.Impact.PostID = ""
	if strings.Contains(renderNode(t, threadPane(m, handlers{})), "impact-check") {
		t.Error("the check shows before it is opened")
	}
}
