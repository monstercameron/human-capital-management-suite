package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3630_ChatFollowsTheActiveRouteLocale: a Chat model still carrying the
// locale it was read under (German) must not paint German controls inside a
// shell the route has resolved to Arabic, or the reverse. The active route's
// locale wins for copy, document direction and accessible names, and a model
// that already names the same locale is left alone.
func TestIssue3630_ChatFollowsTheActiveRouteLocale(t *testing.T) {
	room := chatui.Conversation{ID: "qa", Name: "qa-audit", Kind: chatui.PrivateChannel, Joined: true, MemberCount: 1}
	stale := func(locale string) chatui.Model {
		return chatui.Model{State: chatui.StateReady, Locale: locale, Direction: "ltr", SelectedID: "qa", CurrentUser: "u", CurrentTenantID: "t", Conversations: []chatui.Conversation{room}}
	}
	labels := func(markup string) map[string]bool {
		out := map[string]bool{}
		for _, m := range regexp.MustCompile(`aria-label="([^"]*)"`).FindAllStringSubmatch(markup, -1) {
			out[m[1]] = true
		}
		return out
	}
	for _, c := range []struct{ route, previous string }{{"ar", "de-DE"}, {"de-DE", "ar"}, {"en-US", "de-DE"}, {"ar", "en-US"}} {
		view := View{Locale: ResolveProductLocale(c.route)}
		got := renderUXBlindK(t, BuildChatPage(view, stale(c.previous)))
		fresh := renderUXBlindK(t, BuildChatPage(view, stale(c.route)))
		workspace := regexp.MustCompile(`<[^>]*chat-workspace[^>]*>`).FindString(got)
		if !strings.Contains(workspace, `dir="`+string(view.Locale.Direction)+`"`) || !strings.Contains(workspace, `lang="`+view.Locale.Resolved+`"`) {
			t.Errorf("%s over a %s model: workspace %q does not carry the route's direction and language", c.route, c.previous, workspace)
		}
		gotLabels, freshLabels := labels(got), labels(fresh)
		for name := range gotLabels {
			if !freshLabels[name] {
				t.Errorf("%s over a %s model: accessible name %q is not in the route locale", c.route, c.previous, name)
			}
		}
		for name := range freshLabels {
			if !gotLabels[name] {
				t.Errorf("%s over a %s model: accessible name %q from the route locale is missing", c.route, c.previous, name)
			}
		}
	}
}
