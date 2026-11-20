package chatui

import (
	"strings"
	"testing"
)

// TestIssue3928_RecentPopoverOffersClear: the recent-searches popover under the
// search box carries a labelled Clear control and says where the history lives,
// in every shipped language.
func TestIssue3928_RecentPopoverOffersClear(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatux002Model(locale)
		m.SearchRecent = []string{"payroll close", "deadline"}
		m.Callbacks.SearchClearRecent = func() {}
		markup := renderNode(t, rail(m, handlers{}))
		at := strings.Index(markup, `class="rail-search-recent"`)
		if at < 0 {
			t.Fatalf("%s: no popover: %s", locale, markup)
		}
		list := markup[at:]
		if end := strings.Index(list, "</div>"); end > 0 {
			list = list[:end]
		}
		label := issue3928Text(locale, "clear")
		if label == "" || !strings.Contains(list, `data-action="search-clear-recent"`) || !strings.Contains(list, label) {
			t.Fatalf("%s: no Clear control in the popover: %s", locale, list)
		}
		if !strings.Contains(list, `type="button"`) || strings.Contains(list, "disabled") {
			t.Fatalf("%s: Clear is not an enabled button: %s", locale, list)
		}
		if scope := issue3928Text(locale, "scope"); scope == "" || !strings.Contains(list, scope) {
			t.Fatalf("%s: the popover does not say whose history this is: %s", locale, list)
		}
		for _, key := range []string{"clear", "scope", "cleared", "failed"} {
			if issue3928Text(locale, key) == "" || (locale != "en-US" && issue3928Text(locale, key) == issue3928Text("en-US", key)) {
				t.Fatalf("%s: %s is not translated", locale, key)
			}
		}
	}
}

// TestIssue3928_ClearDisabledWithoutCallback: without a client to delete the
// stored history the control is disabled, never a no-op.
func TestIssue3928_ClearDisabledWithoutCallback(t *testing.T) {
	m := chatux002Model("en-US")
	m.SearchRecent = []string{"deadline"}
	markup := renderNode(t, chatsearchRecentList(m))
	if !strings.Contains(markup, "search-clear-recent") || !strings.Contains(markup, "disabled") {
		t.Fatalf("Clear should be disabled without a callback: %s", markup)
	}
}

// TestIssue3928_ClearOutcomeIsAnnounced: success is announced once the list is
// gone; failure is shown inside the popover and the list stays, so the history
// is never claimed removed when it was not.
func TestIssue3928_ClearOutcomeIsAnnounced(t *testing.T) {
	m := chatux002Model("en-US")
	m.Callbacks.SearchClearRecent = func() {}
	m.SearchRecentStatus = "cleared"
	got := renderNode(t, chatsearchRecentList(m))
	if !strings.Contains(got, `role="status"`) || !strings.Contains(got, issue3928Text("en-US", "cleared")) || strings.Contains(got, "rail-search-recent-item") {
		t.Fatalf("success is not announced: %s", got)
	}
	m.SearchRecent = []string{"deadline"}
	m.SearchRecentStatus = "failed"
	got = renderNode(t, chatsearchRecentList(m))
	if !strings.Contains(got, `role="alert"`) || !strings.Contains(got, issue3928Text("en-US", "failed")) || !strings.Contains(got, "deadline") {
		t.Fatalf("failure is not shown beside the kept list: %s", got)
	}
	m.SearchRecent = nil
	m.SearchRecentStatus = "failed"
	if got := renderNode(t, chatsearchRecentList(m)); strings.Contains(got, issue3928Text("en-US", "cleared")) {
		t.Fatalf("a stale failure claims success: %s", got)
	}
}

// TestIssue3928_ClearActionDispatches: pressing Clear reaches the client.
func TestIssue3928_ClearActionDispatches(t *testing.T) {
	called := 0
	m := chatux002Model("en-US")
	m.Callbacks.SearchClearRecent = func() { called++ }
	if !issue3928DispatchClear(m.Callbacks) || called != 1 {
		t.Fatalf("Clear did not reach the client (called %d)", called)
	}
	if issue3928DispatchClear(Callbacks{}) {
		t.Fatal("Clear dispatched without a callback")
	}
	// The rendered control's action name reaches the callback through act.
	m.act("search-clear-recent", "")
	if called != 2 {
		t.Fatalf("the search-clear-recent action did not reach the client (called %d)", called)
	}
}
