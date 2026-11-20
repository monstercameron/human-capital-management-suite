package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3791Model(locale string) Model {
	m := Model{Locale: locale, SelectedID: "general", CurrentUser: "walt",
		Conversations:   []Conversation{{ID: "general", Name: "general", Kind: "PUBLIC_CHANNEL"}},
		SearchDirectory: []SearchPerson{{ID: "dana", Name: "Dana Reyes"}}}
	m.LinkedWork = LinkedWorkState{Enabled: true, State: "ready", AsOf: time.Date(2026, 11, 19, 9, 30, 0, 0, time.Local), Items: []LinkedWorkItem{
		{Kind: "intent", ID: "i-1", Title: "Promotion request", State: "awaiting_approval", Source: "approvals", Href: "/workspace/app/journeys?journey=i-1", ObservedAt: time.Date(2026, 11, 19, 9, 30, 0, 0, time.Local)},
		{Kind: "task", ID: "t-1", Title: "Swap laptop", State: "open", Source: "tasks", Detail: "To do", OwnerID: "dana", DueDate: "2026-12-04", ProjectID: "ops", TaskID: "t-1", ObservedAt: time.Date(2026, 11, 19, 9, 30, 0, 0, time.Local)},
		{Kind: "intent", ID: "i-2", Title: "Transfer request", State: "done", Source: "approvals", Href: "/workspace/app/journeys?journey=i-2"},
	}}
	return m
}

func issue3791Render(t *testing.T, m Model) string {
	t.Helper()
	node := linkedWorkDetailsSection(m)
	if node == nil {
		return ""
	}
	return renderNode(t, node)
}

// The details panel lists what the conversation spawned, each with a plain
// state, where the state came from, when it was read and a link to open it.
func TestIssue3791_DetailsListsWorkWithStateSourceAndLinks(t *testing.T) {
	html := issue3791Render(t, issue3791Model("en-US"))
	for _, want := range []string{
		"Work from this conversation", "Promotion request", "Awaiting approval", "Swap laptop", "Open", "Done",
		"From approvals", "From tasks", "Owner: Dana Reyes", "Due: 2026-12-04", "To do",
		`href="/workspace/app/journeys?journey=i-1"`, `href="/workspace/app/project?project=ops&amp;task=t-1"`,
		`data-linked-work-refresh`, `aria-live="polite"`, "9:30",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel lacks %q: %s", want, html)
		}
	}
	if strings.Contains(html, "dana</") || strings.Contains(html, ">dana<") {
		t.Errorf("an internal id is shown as a name: %s", html)
	}
	// What needs a person comes in the order the server gave it.
	if strings.Index(html, "Promotion request") > strings.Index(html, "Transfer request") {
		t.Errorf("order changed: %s", html)
	}
}

func TestIssue3791_DetailsHidesTheSectionWhereWorkIsNotOffered(t *testing.T) {
	cases := map[string]func(*Model){
		"not offered": func(m *Model) { m.LinkedWork.Enabled = false },
		"no room":     func(m *Model) { m.SelectedID = "" },
		"agent chat": func(m *Model) {
			m.Conversations[0].Agent = true
		},
		"hosted by another company": func(m *Model) {
			m.CurrentTenantID = "tenant-a"
			m.Conversations[0].HostTenantID = "tenant-b"
		},
	}
	for name, mutate := range cases {
		m := issue3791Model("en-US")
		mutate(&m)
		if got := issue3791Render(t, m); got != "" {
			t.Errorf("%s: section shown: %s", name, got)
		}
	}
}

// A list that could not be fully read says so: it is never an empty state, a
// calm "nothing yet", or Done.
func TestIssue3791_DetailsNeverPassesOffAnIncompleteReadAsEmptyOrDone(t *testing.T) {
	m := issue3791Model("en-US")
	m.LinkedWork.Items = nil
	m.LinkedWork.Incomplete = []string{"approvals"}
	html := issue3791Render(t, m)
	if !strings.Contains(html, "could not be checked") || strings.Contains(html, "Nothing has been proposed") {
		t.Errorf("incomplete empty read: %s", html)
	}
	m.LinkedWork.Incomplete = []string{"approvals", "tasks"}
	m.LinkedWork.Items = []LinkedWorkItem{{Kind: "task", ID: "t-1", Title: "Swap laptop", State: "open", Source: "tasks", ProjectID: "ops", TaskID: "t-1"}}
	html = issue3791Render(t, m)
	if !strings.Contains(html, "Swap laptop") || !strings.Contains(html, "may be missing") {
		t.Errorf("partial list not flagged: %s", html)
	}
	// A read that failed outright is an error with a retry, not a list.
	m = issue3791Model("en-US")
	m.LinkedWork = LinkedWorkState{Enabled: true, State: "error", Error: "unavailable"}
	html = issue3791Render(t, m)
	if !strings.Contains(html, `role="alert"`) || !strings.Contains(html, "Try again") || strings.Contains(html, "Nothing has been proposed") {
		t.Errorf("error state: %s", html)
	}
}

func TestIssue3791_DetailsEmptyAndLoadingStates(t *testing.T) {
	m := issue3791Model("en-US")
	m.LinkedWork.Items = nil
	if html := issue3791Render(t, m); !strings.Contains(html, "Nothing has been proposed or created from this conversation yet.") {
		t.Errorf("empty: %s", html)
	}
	m.LinkedWork = LinkedWorkState{Enabled: true, State: "loading"}
	html := issue3791Render(t, m)
	if !strings.Contains(html, "Loading work from this conversation") || !strings.Contains(html, `aria-busy="true"`) {
		t.Errorf("loading: %s", html)
	}
}

// Only same-origin workspace paths become links, so a bad href from any source
// is not a way to send a person elsewhere.
func TestIssue3791_DetailsLinksOnlyToTheWorkspace_Security(t *testing.T) {
	m := issue3791Model("en-US")
	m.LinkedWork.Items = []LinkedWorkItem{
		{Kind: "intent", ID: "x", Title: "Odd", State: "proposed", Source: "approvals", Href: "javascript:alert(1)"},
		{Kind: "intent", ID: "y", Title: "Other site", State: "proposed", Source: "approvals", Href: "https://evil.example/x"},
		{Kind: "intent", ID: "z", Title: "Protocol relative", State: "proposed", Source: "approvals", Href: "//evil.example/x"},
	}
	html := issue3791Render(t, m)
	if strings.Contains(html, "javascript:") || strings.Contains(html, "evil.example") || strings.Contains(html, "<a ") {
		t.Errorf("unsafe link rendered: %s", html)
	}
	if !strings.Contains(html, "Odd") {
		t.Errorf("the item itself should still be listed: %s", html)
	}
}

func TestIssue3791_CopyCoversEveryLanguage(t *testing.T) {
	for key, english := range linkedWorkCopy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			got := linkedWorkCopy[locale][key]
			if strings.TrimSpace(got) == "" {
				t.Errorf("%s has no %s copy", key, locale)
			}
			if got == english && locale == "ar" {
				t.Errorf("%s is not translated to ar", key)
			}
		}
	}
	for locale := range linkedWorkCopy {
		if len(linkedWorkCopy[locale]) != len(linkedWorkCopy["en-US"]) {
			t.Errorf("%s has %d entries, en-US has %d", locale, len(linkedWorkCopy[locale]), len(linkedWorkCopy["en-US"]))
		}
	}
	// Every state the server can send has its own word in each language.
	for _, state := range []string{"proposed", "awaiting_approval", "approved", "scheduled", "executing", "executed", "done", "failed", "closed", "blocked", "open", "unknown"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if LinkedWorkStateText(locale, state) == "" || LinkedWorkStateText(locale, state) == state {
				t.Errorf("state %s has no %s word", state, locale)
			}
		}
	}
	if LinkedWorkStateText("en-US", "something-new") != LinkedWorkStateText("en-US", "unknown") {
		t.Error("a state this page does not know must read as unknown, not as itself")
	}
}

func TestIssue3791_DetailsReadInGermanAndArabic(t *testing.T) {
	de := issue3791Render(t, issue3791Model("de-DE"))
	for _, want := range []string{"Arbeit aus dieser Unterhaltung", "Wartet auf Genehmigung", "Aus Genehmigungen", "Aus Aufgaben"} {
		if !strings.Contains(de, want) {
			t.Errorf("de lacks %q: %s", want, de)
		}
	}
	ar := issue3791Render(t, issue3791Model("ar"))
	if !strings.Contains(ar, LinkedWorkText("ar", lwTitle)) || !strings.Contains(ar, LinkedWorkStateText("ar", "awaiting_approval")) {
		t.Errorf("ar: %s", ar)
	}
	if !strings.Contains(ar, `dir="auto"`) {
		t.Errorf("titles must set their own direction: %s", ar)
	}
}

func TestIssue3791_StylesKeepTargetsAndRTL(t *testing.T) {
	for _, want := range []string{"min-block-size:44px", ".linked-work-chip", "overflow-wrap:anywhere"} {
		if !strings.Contains(ChatLinkedWorkStyles, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right"} {
		if strings.Contains(ChatLinkedWorkStyles, physical) {
			t.Errorf("styles use %s, which breaks right to left", physical)
		}
	}
}
