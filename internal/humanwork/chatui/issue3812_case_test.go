package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3812Model(locale, user string) Model {
	m := chatux021Model()
	m.Locale = locale
	m.CurrentUser = user
	m.Conversations = []Conversation{{ID: "thread", Name: "Case thread", Kind: PrivateChannel, OwnerID: "hr", MemberCount: 3, Joined: true}}
	m.SelectedID = "thread"
	m.Members = []Member{
		{ID: "hr", HomeTenantID: "t", Name: "Hana Roth"},
		{ID: "mgr", HomeTenantID: "t", Name: "Max Manager"},
		{ID: "bot", HomeTenantID: "t", Name: "Policy Helper", Agent: true},
		{ID: "guest", HomeTenantID: "other", Name: "Guest Visitor"},
	}
	m.IsTenantAdmin = false
	return m
}

func issue3812Bound(m Model, restricted, manage bool) Model {
	m.Case = CaseState{ConversationID: "thread", Loaded: true, Bound: true, Restricted: restricted, CaseRef: "ER-2041", OwnerID: "hr", OwnerIsMe: m.CurrentUser == "hr", CanManage: manage}
	return m
}

func issue3812Message() Message {
	return Message{ID: "post", AuthorID: "mgr", Author: "Max Manager", Body: "Please keep this between us.", SentAt: time.Date(2026, 11, 19, 9, 41, 0, 0, time.UTC)}
}

func issue3812Menu(t *testing.T, m Model) string {
	t.Helper()
	var out strings.Builder
	for _, n := range chatux022MenuItems(m, issue3812Message()) {
		out.WriteString(renderNode(t, n))
	}
	return out.String()
}

func TestIssue3812_HeaderSaysCaseBoundRestricted(t *testing.T) {
	m := issue3812Bound(issue3812Model("en-US", "mgr"), true, false)
	_, markup := chatux001Header(t, m, handlers{}, 1440, "light")
	for _, want := range []string{"Case-bound, restricted", `data-case-badge="true"`, "cannot be forwarded, linked or saved outside it"} {
		if !strings.Contains(markup, want) {
			t.Errorf("header lacks %q", want)
		}
	}
	// Bound but open: the badge says so and does not claim a restriction.
	open := issue3812Bound(issue3812Model("en-US", "mgr"), false, false)
	_, markup = chatux001Header(t, open, handlers{}, 1440, "light")
	if !strings.Contains(markup, "Case-bound") || strings.Contains(markup, "Case-bound, restricted") || strings.Contains(markup, "cannot be forwarded") {
		t.Errorf("an unrestricted case must not read as restricted")
	}
	// The badge names no case and no owner.
	if strings.Contains(markup, "ER-2041") {
		t.Errorf("the header names the case reference")
	}
}

func TestIssue3812_HeaderShowsNothingWithoutAnAnswerForThisConversation(t *testing.T) {
	for name, mutate := range map[string]func(*Model){
		"not loaded":         func(m *Model) { m.Case.Loaded = false },
		"another room":       func(m *Model) { m.Case.ConversationID = "elsewhere" },
		"unbound":            func(m *Model) { m.Case.Bound = false },
		"nothing was asked":  func(m *Model) { m.Case = CaseState{} },
		"stale for the room": func(m *Model) { m.SelectedID = "other-thread" },
	} {
		m := issue3812Bound(issue3812Model("en-US", "mgr"), true, false)
		mutate(&m)
		if got := issue3812Badge(m); got != nil {
			t.Errorf("%s: badge shown: %s", name, renderNode(t, got))
		}
	}
}

func TestIssue3812_RestrictedConversationOffersNoWayOut(t *testing.T) {
	plain := issue3812Model("en-US", "mgr")
	menu := issue3812Menu(t, plain)
	for _, want := range []string{`data-action="copy-link"`, `data-action="open-share"`, `data-saved-action="save"`} {
		if !strings.Contains(menu, want) {
			t.Fatalf("an unbound conversation lost %s: the test would prove nothing", want)
		}
	}
	restricted := issue3812Menu(t, issue3812Bound(plain, true, false))
	for _, gone := range []string{`data-action="copy-link"`, `data-action="open-share"`, `data-saved-action="save"`} {
		if strings.Contains(restricted, gone) {
			t.Errorf("a restricted case conversation still offers %s", gone)
		}
	}
	if bar := renderNode(t, chatsaveAction(issue3812Bound(plain, true, false), issue3812Message(), false)); strings.Contains(bar, "saved-action") {
		t.Errorf("the hover bar still offers Save: %s", bar)
	}
	// A bound conversation that is not restricted keeps its menu.
	open := issue3812Menu(t, issue3812Bound(plain, false, false))
	for _, want := range []string{`data-action="copy-link"`, `data-action="open-share"`, `data-saved-action="save"`} {
		if !strings.Contains(open, want) {
			t.Errorf("an unrestricted case lost %s", want)
		}
	}
}

func TestIssue3812_DetailsExplainTheRulesToEveryParticipant(t *testing.T) {
	m := issue3812Bound(issue3812Model("en-US", "mgr"), true, false)
	got := renderNode(t, issue3812Section(m))
	for _, want := range []string{"Bound to case ER-2041", "Case owner: Hana Roth", "Only the case owner adds or removes people", "cannot be forwarded, linked, saved or turned into tasks", "photograph the screen"} {
		if !strings.Contains(got, want) {
			t.Errorf("section lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "<form") || strings.Contains(got, "case-unbind") {
		t.Errorf("a participant who is not an administrator sees the binding form: %s", got)
	}
	if mine := renderNode(t, issue3812Section(issue3812Bound(issue3812Model("en-US", "hr"), true, false))); !strings.Contains(mine, "You are the case owner") {
		t.Errorf("the owner is not told so: %s", mine)
	}
}

func TestIssue3812_AdministratorSeesTheFormAndOwnersAreSensible(t *testing.T) {
	m := issue3812Model("en-US", "hr")
	m.Case = CaseState{ConversationID: "thread", Loaded: true, CanManage: true}
	got := renderNode(t, issue3812Section(m))
	for _, want := range []string{`data-case-form="thread"`, `name="case_ref"`, `maxLength="120"`, `for="chatcase-ref"`, `name="owner"`, `Hana Roth`, `Max Manager`, `name="restricted"`, "Bind to case"} {
		if !strings.Contains(got, want) {
			t.Errorf("form lacks %q: %s", want, got)
		}
	}
	// An agent, and a person of another company, cannot own a case.
	for _, gone := range []string{"Policy Helper", "Guest Visitor", "case-unbind"} {
		if strings.Contains(got, gone) {
			t.Errorf("form offers %q", gone)
		}
	}
	if !strings.Contains(got, `checked`) {
		t.Errorf("a new binding must start restricted: %s", got)
	}
	bound := renderNode(t, issue3812Section(issue3812Bound(m, true, true)))
	for _, want := range []string{"Change the case binding", "Save changes", `data-action="case-unbind"`, "Lift the case binding", `value="ER-2041"`} {
		if !strings.Contains(bound, want) {
			t.Errorf("bound form lacks %q: %s", want, bound)
		}
	}
	// Nothing to show an unbound conversation to someone who cannot bind it.
	if got := issue3812Section(func() Model { x := m; x.Case.CanManage = false; return x }()); got != nil {
		t.Errorf("section shown without a binding or the right to make one")
	}
}

func TestIssue3812_BusyAndNoticeStates(t *testing.T) {
	m := issue3812Bound(issue3812Model("en-US", "hr"), true, true)
	m.Case.Busy = true
	busy := renderNode(t, issue3812Section(m))
	if !strings.Contains(busy, "Saving...") || !strings.Contains(busy, `aria-busy="true"`) || strings.Count(busy, "disabled") < 3 {
		t.Errorf("a change in flight can be sent again: %s", busy)
	}
	for notice, want := range map[string]string{"failed": "could not be saved", "denied": "Only a workspace administrator", "invalid": "Enter a case reference", "done": "is saved", "lifted": "is lifted"} {
		m.Case.Busy, m.Case.Notice = false, notice
		got := renderNode(t, issue3812Section(m))
		role := `role="alert"`
		if notice == "done" || notice == "lifted" {
			role = `role="status"`
		}
		if !strings.Contains(got, want) || !strings.Contains(got, role) {
			t.Errorf("%s: %s", notice, got)
		}
	}
}

func TestIssue3812_UnbindClickReachesTheClient(t *testing.T) {
	var lifted int
	m := issue3812Bound(issue3812Model("en-US", "hr"), true, true)
	m.Case.Unbind = func() { lifted++ }
	m.caseUnbind()
	m.Case.Busy = true
	m.caseUnbind()
	if lifted != 1 {
		t.Fatalf("unbind ran %d times; want once, and not while a change is in flight", lifted)
	}
}

func TestIssue3812_CopyIsInEveryLanguage(t *testing.T) {
	english := issue3812Copy["en-US"]
	if len(english) < 30 {
		t.Fatalf("only %d english strings", len(english))
	}
	for _, locale := range []string{"de-DE", "ar"} {
		table := issue3812Copy[locale]
		if len(table) != len(english) {
			t.Errorf("%s has %d strings, want %d", locale, len(table), len(english))
		}
		for key, en := range english {
			got := table[key]
			if strings.TrimSpace(got) == "" || got == en {
				t.Errorf("%s: %s is missing or untranslated", locale, key)
			}
			for _, placeholder := range []string{"{case}", "{name}"} {
				if strings.Contains(en, placeholder) != strings.Contains(got, placeholder) {
					t.Errorf("%s: %s changes the placeholder %s", locale, key, placeholder)
				}
			}
		}
	}
	for reason, key := range map[string]string{"chat.case_membership": keyIssue3812RefusedMember, "chat.case_forward": keyIssue3812RefusedForward, "chat.case_share": keyIssue3812RefusedShare, "chat.case_save": keyIssue3812RefusedSave} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if got := Issue3812RefusalNotice(locale, reason); got == "" || got != Issue3812Text(locale, key) {
				t.Errorf("%s %s: %q", locale, reason, got)
			}
		}
	}
	if Issue3812RefusalNotice("en-US", "chat.permission_denied") != "" {
		t.Error("an ordinary denial reads as a case refusal")
	}
}

func TestIssue3812_ArabicAndGermanRender(t *testing.T) {
	ar := renderNode(t, issue3812Section(issue3812Bound(issue3812Model("ar", "mgr"), true, false)))
	if !strings.Contains(ar, "مرتبطة بالقضية ER-2041") || !strings.Contains(ar, `dir="auto"`) {
		t.Errorf("arabic section: %s", ar)
	}
	de := issue3812Badge(issue3812Bound(issue3812Model("de-DE", "mgr"), true, false))
	if got := renderNode(t, de); !strings.Contains(got, "Fallgebunden, eingeschränkt") {
		t.Errorf("german badge: %s", got)
	}
}

func TestIssue3812_StylesHoldOnAPhoneAndInRightToLeft(t *testing.T) {
	for _, want := range []string{"min-block-size:44px", "overflow-wrap:anywhere", "prefers-reduced-motion", "padding:12px 16px"} {
		if !strings.Contains(Issue3812Styles, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right"} {
		if strings.Contains(Issue3812Styles, physical) {
			t.Errorf("styles use %q, which does not mirror", physical)
		}
	}
	if !strings.Contains(ChatLane3Styles, Issue3812Styles) {
		t.Error("the case styles are not part of the page stylesheet")
	}
}

func TestIssue3812_LiftStaysAvailableWhenNobodyCanBeChosenAsOwner(t *testing.T) {
	m := issue3812Model("en-US", "hr")
	m.Members = []Member{{ID: "bot", HomeTenantID: "t", Name: "Policy Helper", Agent: true}}
	got := renderNode(t, issue3812Section(issue3812Bound(m, true, true)))
	if !strings.Contains(got, `data-action="case-unbind"`) {
		t.Fatalf("a bound conversation with no eligible owner cannot be lifted: %s", got)
	}
}
