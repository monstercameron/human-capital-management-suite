package productclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// #3771: on Chat the navigation opens as its rail unless the person chose
// otherwise. The address wins, then the choice stored for Chat, and the
// workspace-wide stored collapse wins over a stored Chat expand. No other page
// reads the Chat choice.
func TestChatDesign_3771_ChatNavigationChoiceIsRememberedAndWins(t *testing.T) {
	chatExpanded := map[string]*journeyv1.TablePreferences{productui.ChatNavigationTable: {Filters: map[string]string{productui.ChatNavigationFilter: "expanded"}}}
	chatCollapsed := map[string]*journeyv1.TablePreferences{productui.ChatNavigationTable: {Filters: map[string]string{productui.ChatNavigationFilter: "collapsed"}}}
	for _, tc := range []struct {
		name, path, query string
		user              *journeyv1.UserPreferences
		collapsed, expand bool
		rail              bool
	}{
		{"chat, nothing stored: the rail", "/workspace/app/chat", "", &journeyv1.UserPreferences{}, false, false, true},
		{"chat, expanded stored for chat", "/workspace/app/chat", "", &journeyv1.UserPreferences{Tables: chatExpanded}, false, true, false},
		{"chat, collapsed stored for chat", "/workspace/app/chat", "", &journeyv1.UserPreferences{Tables: chatCollapsed}, false, false, true},
		{"chat, workspace collapse beats a stored chat expand", "/workspace/app/chat", "", &journeyv1.UserPreferences{NavCollapsed: true, Tables: chatExpanded}, true, false, true},
		{"chat, the address expands", "/workspace/app/chat", "nav=expanded", &journeyv1.UserPreferences{NavCollapsed: true, Tables: chatCollapsed}, false, true, false},
		{"chat, the address collapses", "/workspace/app/chat", "nav=collapsed", &journeyv1.UserPreferences{Tables: chatExpanded}, true, false, true},
		{"home ignores the chat choice", "/workspace/app/home", "", &journeyv1.UserPreferences{Tables: chatExpanded}, false, false, false},
		{"home keeps the workspace collapse", "/workspace/app/home", "", &journeyv1.UserPreferences{NavCollapsed: true}, true, false, true},
		{"agents is not a rail-by-default page", "/workspace/app/chat/agents", "", &journeyv1.UserPreferences{}, false, false, false},
	} {
		state, err := ParseState(tc.path, tc.query)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		view := productui.NewView(state.Page, "tenant", "viewer", "")
		applyPreferences(&view, &state, &journeyv1.GetProductPreferencesResponse{User: tc.user})
		if state.Request.NavCollapsed != tc.collapsed || state.Request.NavExpanded != tc.expand {
			t.Errorf("%s: collapsed=%v expanded=%v, want %v %v", tc.name, state.Request.NavCollapsed, state.Request.NavExpanded, tc.collapsed, tc.expand)
		}
		rail := state.Request.NavCollapsed || (productui.ChatRailDefault(state.Page) && !state.Request.NavExpanded)
		if rail != tc.rail {
			t.Errorf("%s: the navigation is a rail = %v, want %v", tc.name, rail, tc.rail)
		}
	}
}
