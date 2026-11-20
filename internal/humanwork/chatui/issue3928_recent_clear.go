package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ISSUE-3928: the recent-searches popover under the search box can clear the
// stored history, says whose history it is, and reports whether the deletion
// worked. The history lives on the person's account on the server, so Clear
// deletes it there; the list is emptied only after the server confirms.

// issue3928Text is the popover's copy in en-US, de-DE and ar.
func issue3928Text(locale, key string) string {
	copy := map[string][3]string{
		"clear":   {"Clear recent searches", "Letzte Suchen löschen", "مسح عمليات البحث الأخيرة"},
		"scope":   {"Saved to your account, so it follows you to other devices.", "In Ihrem Konto gespeichert und auf anderen Geräten sichtbar.", "محفوظة في حسابك وتظهر على أجهزتك الأخرى."},
		"cleared": {"Recent searches cleared.", "Letzte Suchen gelöscht.", "تم مسح عمليات البحث الأخيرة."},
		"failed":  {"Could not clear recent searches. They are still saved. Try again.", "Letzte Suchen konnten nicht gelöscht werden. Sie sind weiterhin gespeichert. Bitte erneut versuchen.", "تعذّر مسح عمليات البحث الأخيرة. ما زالت محفوظة. حاول مرة أخرى."},
	}[key]
	idx := 0
	switch {
	case strings.HasPrefix(locale, "de"):
		idx = 1
	case strings.HasPrefix(locale, "ar"):
		idx = 2
	}
	return chatbug039Text(key, copy[idx], copy[0])
}

// issue3928DispatchClear sends the Clear press to the client; false when the
// client offers no way to delete the history.
func issue3928DispatchClear(cb Callbacks) bool {
	if cb.SearchClearRecent == nil {
		return false
	}
	cb.SearchClearRecent()
	return true
}

// issue3928Footer is the popover's last rows: the failure note, the scope line
// and the Clear control.
func issue3928Footer(m Model) []ui.Node {
	nodes := []ui.Node{}
	if m.SearchRecentStatus == "failed" {
		nodes = append(nodes, html.Span(html.Props{Class: "rail-search-recent-note rail-search-recent-error", Role: "alert", Text: issue3928Text(m.Locale, "failed")}))
	}
	nodes = append(nodes,
		html.Span(html.Props{Class: "rail-search-recent-note", Text: issue3928Text(m.Locale, "scope")}),
		html.Button(html.Props{Class: "rail-search-recent-clear", Type: "button", Disabled: m.Callbacks.SearchClearRecent == nil,
			Data: map[string]string{"action": "search-clear-recent"}, Text: issue3928Text(m.Locale, "clear")}))
	return nodes
}

// issue3928ClearedNotice announces a finished clear after the list is gone.
func issue3928ClearedNotice(m Model) ui.Node {
	if m.SearchRecentStatus != "cleared" || len(m.SearchRecent) != 0 {
		return nil
	}
	return html.Span(html.Props{Class: "sr-only", Role: "status", Text: issue3928Text(m.Locale, "cleared")})
}

const issue3928Styles = `.chat-workspace .rail-search-recent-note{display:block;padding:4px 8px;color:var(--muted);font-size:.75rem;overflow-wrap:anywhere}` +
	`.chat-workspace .rail-search-recent-error{color:var(--danger,var(--ink))}` +
	`.chat-workspace .rail-search-recent-clear{display:block;inline-size:100%;min-block-size:32px;margin:0;padding:4px 8px;border:0;border-radius:var(--hcm-radius-xs);background:transparent;color:var(--ink);font:inherit;font-size:.875rem;text-align:start;cursor:pointer;text-decoration:underline}` +
	`.chat-workspace .rail-search-recent-clear:hover,.chat-workspace .rail-search-recent-clear:focus-visible{background:var(--soft)}` +
	`.chat-workspace .rail-search-recent-clear:disabled{opacity:.5;cursor:not-allowed}` +
	`@media (pointer:coarse){.chat-workspace .rail-search-recent-clear{min-block-size:44px}}`
