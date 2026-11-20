package chatui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func issue3990Model(locale string) (Model, *[]string) {
	calls := &[]string{}
	room := Conversation{ID: "benefits", Name: "benefits", Topic: "Open enrollment and perks", Kind: PublicChannel, Joined: true}
	m := Model{State: StateReady, Locale: locale, CurrentUser: "alice", CurrentTenantID: "tenant", SelectedID: room.ID,
		Conversations: []Conversation{room, {ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
		Callbacks: Callbacks{
			SendMessage:               func(id, body string) { *calls = append(*calls, "send:"+id+":"+body) },
			SendMessageWithReferences: func(id, body string, _ []ChatReference) { *calls = append(*calls, "send:"+id+":"+body) },
			DraftChanged:              func(id, body string) { *calls = append(*calls, "draft:"+id+":"+body) },
		}}
	return m, calls
}

func issue3990Ready(kind string) EnrollmentGuideStatus {
	return EnrollmentGuideStatus{
		Kind: kind, Eligibility: "eligible", Window: "open",
		WindowStart: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), TimeZone: "UTC",
		PolicyName: "Health plan eligibility", PolicyVersion: "2026.2", Source: "Benefits service", Revision: "rev-1",
		ReadAt: time.Now(), EnrollHref: "/workspace/app/benefits/enroll",
		Checklist: []EnrollmentGuideStep{{ID: "cert", Title: "Marriage certificate", Evidence: "A copy of the certificate", Source: "Policy 2026.2 section 4"}},
	}
}

func issue3990Store() localStore { return localStore{box: &localUI{}} }

// opened walks to the read state a person reaches by pressing Open, choosing a
// kind and pressing Check.
func issue3990Walk(t *testing.T, m Model, local localStore, kind string, status EnrollmentGuideStatus, err error) {
	t.Helper()
	var finish func(EnrollmentGuideStatus, error)
	m.Callbacks.ReadEnrollmentGuide = func(k string, done func(EnrollmentGuideStatus, error)) {
		if k != kind {
			t.Fatalf("asked for %q, want %q", k, kind)
		}
		finish = done
	}
	issue3990Apply(m, local, issue3990OpenAction, "")
	issue3990Apply(m, local, issue3990KindAction, kind)
	issue3990Apply(m, local, issue3990ReadAction, "")
	if finish == nil {
		t.Fatal("the read was never asked")
	}
	finish(status, err)
}

func TestIssue3990_OfferedFromBenefitsDiscussionOnly(t *testing.T) {
	m, _ := issue3990Model("en-US")
	got := renderNode(t, issue3990Panel(m, localUI{}))
	if !strings.Contains(got, "Help me understand my enrollment options") || !strings.Contains(got, `aria-expanded="false"`) {
		t.Fatalf("offer missing: %s", got)
	}
	other := m
	other.SelectedID = "general"
	if issue3990Panel(other, localUI{}) != nil {
		t.Fatal("offered in an unrelated channel")
	}
	dm := m
	dm.Conversations = []Conversation{{ID: "dm", Name: "Policy Helper", Kind: DirectMessage, Agent: true, Joined: true}}
	dm.SelectedID = "dm"
	if issue3990Panel(dm, localUI{}) != nil {
		t.Fatal("offered in an agent conversation with nothing about benefits")
	}
	dm.Draft = "Can I add my dependent after the qualifying event?"
	if issue3990Panel(dm, localUI{}) == nil {
		t.Fatal("not offered in a private agent conversation asking about a dependent")
	}
	away := m
	away.Conversations = []Conversation{{ID: "benefits", Name: "benefits", Kind: PublicChannel, Joined: true, HostTenantID: "other"}}
	if issue3990Panel(away, localUI{}) != nil {
		t.Fatal("offered for a conversation hosted by another tenant")
	}
	unjoined := m
	unjoined.Conversations = []Conversation{{ID: "benefits", Name: "benefits", Kind: PublicChannel}}
	if issue3990Panel(unjoined, localUI{}) != nil {
		t.Fatal("offered in a channel the person has not joined")
	}
}

// Before anything is read the panel says who sees it, what sources are used and
// what is not copied, and offers the choice of kind of question.
func TestIssue3990_ScopeIsShownBeforeAnythingIsRead(t *testing.T) {
	m, _ := issue3990Model("en-US")
	got := renderNode(t, issue3990Panel(m, localUI{enroll: issue3990UI{Open: true, Room: "benefits"}}))
	for _, want := range []string{"Only you can see this", "published policy", "not copied", "New-hire enrollment", "Life event", "I am not sure", `type="radio"`, "Nothing is elected, uploaded or sent", "not your deadline"} {
		if !strings.Contains(got, want) {
			t.Errorf("scope lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Your status") || strings.Contains(got, "textarea") {
		t.Errorf("a result or a free-text field before any read: %s", got)
	}
}

func TestIssue3990_ReadNeedsAKindFirst(t *testing.T) {
	m, _ := issue3990Model("en-US")
	asked := 0
	m.Callbacks.ReadEnrollmentGuide = func(string, func(EnrollmentGuideStatus, error)) { asked++ }
	local := issue3990Store()
	issue3990Apply(m, local, issue3990OpenAction, "")
	issue3990Apply(m, local, issue3990ReadAction, "")
	if asked != 0 || local.get().enroll.Phase != "" || !local.get().enroll.NeedKind {
		t.Fatalf("read without a kind: asked=%d state=%#v", asked, local.get().enroll)
	}
	got := renderNode(t, issue3990Panel(m, local.get()))
	if !strings.Contains(got, "Choose which kind of question") {
		t.Fatalf("no prompt to choose: %s", got)
	}
}

// A published, worker-specific status is shown with its policy version, source,
// freshness and time zone, and nothing is posted, elected or sent.
func TestIssue3990_ReadyStatusShowsSourceVersionFreshnessAndSendsNothing(t *testing.T) {
	m, calls := issue3990Model("en-US")
	local := issue3990Store()
	issue3990Walk(t, m, local, EnrollmentGuideLifeEvent, issue3990Ready(EnrollmentGuideLifeEvent), nil)
	got := renderNode(t, issue3990Panel(m, local.get()))
	for _, want := range []string{"Your status", "Eligible", "Open", "UTC", "Health plan eligibility", "2026.2", "Benefits service", "Read at", "Marriage certificate", "A copy of the certificate", "Policy 2026.2 section 4", `href="/workspace/app/benefits/enroll"`, `href="/workspace/app/help/hr-service-request"`, "ticking a step only marks it here"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Errorf("status lacks %q: %s", want, got)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("guidance sent or drafted something: %v", *calls)
	}
}

// A chat claim is never turned into an individual verdict: with the kind not
// chosen, with both windows possible, with no event date, or with no zone or
// policy version, no window or deadline is shown and support is offered.
func TestIssue3990_NoVerdictOrDeadlineWithoutAuthoritativeInputs(t *testing.T) {
	cases := map[string]struct {
		kind   string
		change func(*EnrollmentGuideStatus)
		want   string
	}{
		"not sure":   {EnrollmentGuideNotSure, func(s *EnrollmentGuideStatus) {}, "more than one"},
		"both":       {EnrollmentGuideNewHire, func(s *EnrollmentGuideStatus) { s.MoreThanOneWindow = true }, "more than one"},
		"event date": {EnrollmentGuideLifeEvent, func(s *EnrollmentGuideStatus) { s.NeedsEventDate = true }, "event date"},
		"no zone":    {EnrollmentGuideNewHire, func(s *EnrollmentGuideStatus) { s.TimeZone = "" }, "time zone"},
		"no policy":  {EnrollmentGuideNewHire, func(s *EnrollmentGuideStatus) { s.PolicyVersion = "" }, "policy version"},
	}
	for name, c := range cases {
		m, _ := issue3990Model("en-US")
		local := issue3990Store()
		status := issue3990Ready(c.kind)
		c.change(&status)
		issue3990Walk(t, m, local, c.kind, status, nil)
		got := renderNode(t, issue3990Panel(m, local.get()))
		if strings.Contains(got, "March") || strings.Contains(got, "Mar 1") || strings.Contains(got, "3/1") || strings.Contains(got, "2026-03") {
			t.Errorf("%s: a window date was shown: %s", name, got)
		}
		if !strings.Contains(strings.ToLower(got), c.want) || !strings.Contains(got, `href="/workspace/app/help/hr-service-request"`) {
			t.Errorf("%s: no honest explanation (%q) or no support route: %s", name, c.want, got)
		}
	}
}

func TestIssue3990_UnavailableAndFailedAreHonestAndKeepTheNote(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	// No benefits service reachable at all.
	issue3990Apply(m, local, issue3990OpenAction, "")
	issue3990Apply(m, local, issue3990KindAction, EnrollmentGuideLifeEvent)
	issue3990Apply(m, local, issue3990ReadAction, "")
	if local.get().enroll.Phase != issue3990Unavailable {
		t.Fatalf("phase = %q, want unavailable", local.get().enroll.Phase)
	}
	issue3990Apply(m, local, issue3990NoteAction, "My spouse joined on 4 May.")
	got := renderNode(t, issue3990Panel(m, local.get()))
	for _, want := range []string{"could not be checked", "no window, deadline or eligibility", `href="/workspace/app/help/hr-service-request"`, "My spouse joined on 4 May.", "<textarea"} {
		if !strings.Contains(got, want) {
			t.Errorf("unavailable lacks %q: %s", want, got)
		}
	}
	// The person reaches support and comes back: the note is still there.
	issue3990Apply(m, local, issue3990CloseAction, "")
	issue3990Apply(m, local, issue3990OpenAction, "")
	if local.get().enroll.Note != "My spouse joined on 4 May." {
		t.Fatalf("note lost: %q", local.get().enroll.Note)
	}
	// A failed read says so and offers retry.
	failing, _ := issue3990Model("en-US")
	l2 := issue3990Store()
	issue3990Walk(t, failing, l2, EnrollmentGuideNewHire, EnrollmentGuideStatus{}, errors.New("boom"))
	out := renderNode(t, issue3990Panel(failing, l2.get()))
	if !strings.Contains(out, "Try again") || strings.Contains(out, "boom") || !strings.Contains(out, "not an answer about your eligibility") {
		t.Fatalf("failed state: %s", out)
	}
	// The service saying it has nothing is the unavailable state, not a verdict.
	l3 := issue3990Store()
	issue3990Walk(t, failing, l3, EnrollmentGuideNewHire, EnrollmentGuideStatus{Kind: EnrollmentGuideNewHire, Unavailable: true}, nil)
	if l3.get().enroll.Phase != issue3990Unavailable {
		t.Fatalf("service-unavailable phase = %q", l3.get().enroll.Phase)
	}
}

// A read answering another kind of question than the one asked is not shown.
func TestIssue3990_AnswerForAnotherKindIsNotShown(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	issue3990Walk(t, m, local, EnrollmentGuideLifeEvent, issue3990Ready(EnrollmentGuideNewHire), nil)
	if local.get().enroll.Phase == issue3990PhaseReady {
		t.Fatalf("a new-hire answer was shown for a life-event question")
	}
}

// A second read that finds a different plan or window revision says so.
func TestIssue3990_ChangedRevisionIsExplained(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	issue3990Walk(t, m, local, EnrollmentGuideLifeEvent, issue3990Ready(EnrollmentGuideLifeEvent), nil)
	if local.get().enroll.Changed {
		t.Fatal("first read reported as changed")
	}
	next := issue3990Ready(EnrollmentGuideLifeEvent)
	next.Revision, next.Window = "rev-2", "closed"
	var finish func(EnrollmentGuideStatus, error)
	m.Callbacks.ReadEnrollmentGuide = func(_ string, done func(EnrollmentGuideStatus, error)) { finish = done }
	issue3990Apply(m, local, issue3990ReadAction, "")
	finish(next, nil)
	if !local.get().enroll.Changed {
		t.Fatal("changed revision not noticed")
	}
	if got := renderNode(t, issue3990Panel(m, local.get())); !strings.Contains(got, "changed while this guide was open") {
		t.Fatalf("no change notice: %s", got)
	}
}

// Closing and opening again keeps the status but says it must be checked again.
func TestIssue3990_ReopenedStatusIsMarkedForRecheck(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	issue3990Walk(t, m, local, EnrollmentGuideNewHire, issue3990Ready(EnrollmentGuideNewHire), nil)
	issue3990Apply(m, local, issue3990CloseAction, "")
	issue3990Apply(m, local, issue3990OpenAction, "")
	if got := renderNode(t, issue3990Panel(m, local.get())); !strings.Contains(got, "Check again before you act") {
		t.Fatalf("no recheck prompt: %s", got)
	}
}

// Ticking a step is private state only; the support question starts from the
// source the person read and can be edited.
func TestIssue3990_ChecklistAndSupportQuestionStayPrivate(t *testing.T) {
	m, calls := issue3990Model("en-US")
	local := issue3990Store()
	issue3990Walk(t, m, local, EnrollmentGuideLifeEvent, issue3990Ready(EnrollmentGuideLifeEvent), nil)
	issue3990Apply(m, local, issue3990StepAction, "cert")
	if !local.get().enroll.Done["cert"] {
		t.Fatal("step not ticked")
	}
	issue3990Apply(m, local, issue3990StepAction, "cert")
	if local.get().enroll.Done["cert"] {
		t.Fatal("step not unticked")
	}
	issue3990Apply(m, local, issue3990SourceAction, "")
	note := local.get().enroll.Note
	if !strings.Contains(note, "Health plan eligibility") || !strings.Contains(note, "2026.2") || strings.Contains(strings.ToLower(note), "spouse") {
		t.Fatalf("source-linked question = %q", note)
	}
	if len(*calls) != 0 {
		t.Fatalf("something was sent or drafted: %v", *calls)
	}
}

func TestIssue3990_StaleCallbackAfterCloseIsIgnored(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	var finish func(EnrollmentGuideStatus, error)
	m.Callbacks.ReadEnrollmentGuide = func(_ string, done func(EnrollmentGuideStatus, error)) { finish = done }
	issue3990Apply(m, local, issue3990OpenAction, "")
	issue3990Apply(m, local, issue3990KindAction, EnrollmentGuideNewHire)
	issue3990Apply(m, local, issue3990ReadAction, "")
	issue3990Apply(m, local, issue3990CloseAction, "")
	finish(issue3990Ready(EnrollmentGuideNewHire), nil)
	if local.get().enroll.Phase == issue3990PhaseReady || local.get().enroll.Status.PolicyVersion != "" {
		t.Fatalf("an answer arrived into a closed guide: %#v", local.get().enroll)
	}
}

func TestIssue3990_UnsafeLinksAreNotDrawn(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	status := issue3990Ready(EnrollmentGuideNewHire)
	status.EnrollHref, status.SupportHref = "https://evil.example/x", "//evil.example"
	issue3990Walk(t, m, local, EnrollmentGuideNewHire, status, nil)
	got := renderNode(t, issue3990Panel(m, local.get()))
	if strings.Contains(got, "evil.example") || !strings.Contains(got, `href="/workspace/app/help/hr-service-request"`) {
		t.Fatalf("unsafe link or lost support route: %s", got)
	}
}

func TestIssue3990_CopyInEveryLanguageAndRTL(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m, _ := issue3990Model(locale)
		local := issue3990Store()
		offer := renderNode(t, issue3990Panel(m, localUI{}))
		issue3990Walk(t, m, local, EnrollmentGuideLifeEvent, issue3990Ready(EnrollmentGuideLifeEvent), nil)
		scope := renderNode(t, issue3990Panel(m, localUI{enroll: issue3990UI{Open: true, Room: "benefits"}}))
		ready := renderNode(t, issue3990Panel(m, local.get()))
		for _, markup := range []string{offer, scope, ready} {
			for _, english := range []string{"Help me understand", "Only you can see this", "Your status", "Check my status", "issue3990"} {
				if strings.Contains(markup, english) {
					t.Errorf("%s: English or raw key %q: %s", locale, english, markup)
				}
			}
		}
	}
	m, _ := issue3990Model("ar")
	got := renderNode(t, issue3990Panel(m, localUI{enroll: issue3990UI{Open: true, Room: "benefits"}}))
	if !strings.Contains(got, `dir="auto"`) {
		t.Errorf("text does not follow its own direction: %s", got)
	}
	if !strings.Contains(chatIssue3990Styles, "padding-inline") && !strings.Contains(chatIssue3990Styles, "inline") {
		t.Error("styles are not logical-property based for RTL")
	}
	if !strings.Contains(chatIssue3990Styles, "min-height:44px") {
		t.Error("no 44px touch target on phones")
	}
}

// The negative cases above only prove something if a complete status does show
// its dates, so this pins the positive control.
func TestIssue3990_CompleteStatusShowsItsWindowDates(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	issue3990Walk(t, m, local, EnrollmentGuideNewHire, issue3990Ready(EnrollmentGuideNewHire), nil)
	got := renderNode(t, issue3990Panel(m, local.get()))
	if !strings.Contains(got, "Mar") || !strings.Contains(got, "(UTC)") {
		t.Fatalf("complete status shows no window dates: %s", got)
	}
}

// After a read the person can go back and ask about another kind of question;
// what was read for the first kind is not carried over, the note is kept, and
// the read that was in flight is ignored.
func TestIssue3990_ChangingTheKindStartsOverAndKeepsTheNote(t *testing.T) {
	m, _ := issue3990Model("en-US")
	local := issue3990Store()
	issue3990Walk(t, m, local, EnrollmentGuideNotSure, issue3990Ready(EnrollmentGuideNotSure), nil)
	issue3990Apply(m, local, issue3990NoteAction, "my note")
	if got := renderNode(t, issue3990Panel(m, local.get())); !strings.Contains(got, "Change the kind of question") {
		t.Fatalf("no way to change the kind after a read: %s", got)
	}
	issue3990Apply(m, local, issue3990ChangeAction, "")
	st := local.get().enroll
	if st.Phase != "" || st.Status.PolicyVersion != "" || st.Note != "my note" {
		t.Fatalf("change did not start over cleanly: %#v", st)
	}
	got := renderNode(t, issue3990Panel(m, local.get()))
	if !strings.Contains(got, `type="radio"`) || strings.Contains(got, "Your status") {
		t.Fatalf("kind choice not shown again: %s", got)
	}
	issue3990Apply(m, local, issue3990KindAction, EnrollmentGuideLifeEvent)
	if local.get().enroll.Kind != EnrollmentGuideLifeEvent {
		t.Fatal("new kind not chosen")
	}
}

func TestIssue3990_LoadingCanBeClosedAndGermanOfferIsRecognised(t *testing.T) {
	m, _ := issue3990Model("en-US")
	m.Callbacks.ReadEnrollmentGuide = func(string, func(EnrollmentGuideStatus, error)) {}
	local := issue3990Store()
	issue3990Apply(m, local, issue3990OpenAction, "")
	issue3990Apply(m, local, issue3990KindAction, EnrollmentGuideNewHire)
	issue3990Apply(m, local, issue3990ReadAction, "")
	if got := renderNode(t, issue3990Panel(m, local.get())); !strings.Contains(got, "Checking your status") || !strings.Contains(got, issue3990CloseAction) {
		t.Fatalf("a pending read cannot be closed: %s", got)
	}
	de, _ := issue3990Model("de-DE")
	de.Conversations = []Conversation{{ID: "benefits", Name: "sozialleistungen", Kind: PublicChannel, Joined: true}}
	if issue3990Panel(de, localUI{}) == nil {
		t.Fatal("German benefits channel not offered the guide")
	}
}
